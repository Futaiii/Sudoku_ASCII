// internal/app/server.go
package app

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/Futaiii/Sudoku_ASCII/internal/config"
	"github.com/Futaiii/Sudoku_ASCII/internal/handler"
	"github.com/Futaiii/Sudoku_ASCII/internal/protocol"
	"github.com/Futaiii/Sudoku_ASCII/pkg/crypto"
	"github.com/Futaiii/Sudoku_ASCII/pkg/obfs/sudoku"
	"github.com/Futaiii/Sudoku_ASCII/pkg/transport"
)

const HandshakeTimeout = 5 * time.Second

func RunServer(cfg *config.Config, table *sudoku.Table) {
	// Use transport abstraction
	l, err := transport.Listen(cfg.Transport, fmt.Sprintf(":%d", cfg.LocalPort))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Server on :%d (%s) (Fallback: %s)", cfg.LocalPort, cfg.Transport, cfg.FallbackAddr)

	for {
		c, err := l.Accept()
		if err != nil {
			continue
		}
		go handleServerConn(c, cfg, table)
	}
}

func handleServerConn(rawConn net.Conn, cfg *config.Config, table *sudoku.Table) {
	// 1. Sudoku 层 (开启记录以支持回落)
	sConn := sudoku.NewConn(rawConn, table, cfg.PaddingMin, cfg.PaddingMax, true)

	// 2. 加密层
	cConn, err := crypto.NewAEADConn(sConn, cfg.Key, cfg.AEAD)
	if err != nil {
		rawConn.Close()
		return
	}
	defer cConn.Close()

	// 3. 验证握手
	handshakeBuf := make([]byte, 16)
	rawConn.SetReadDeadline(time.Now().Add(HandshakeTimeout))
	_, err = io.ReadFull(cConn, handshakeBuf)
	rawConn.SetReadDeadline(time.Time{})

	if err != nil {
		log.Printf("[Security] Handshake fail: %v", err)
		handler.HandleSuspicious(sConn, rawConn, cfg)
		return
	}

	ts := int64(binary.BigEndian.Uint64(handshakeBuf[:8]))
	if abs(time.Now().Unix()-ts) > 60 {
		log.Printf("[Security] Time skew/Replay")
		handler.HandleSuspicious(sConn, rawConn, cfg)
		return
	}

	sConn.StopRecording()

	// 4. 读取目标地址 (由 Client 端在握手后立即发送)
	netType, destAddrStr, _, err := protocol.ReadHeader(cConn)
	if err != nil {
		log.Printf("[Server] Failed to read header: %v", err)
		return
	}

	log.Printf("[Server] Connecting to %s", destAddrStr)

	if netType == protocol.NetTypeUDP {
		handleUDPForward(cConn, destAddrStr)
	} else {
		handleTCPForward(cConn, destAddrStr)
	}
}

func handleTCPForward(conn net.Conn, destAddr string) {
	target, err := net.DialTimeout("tcp", destAddr, 10*time.Second)
	if err != nil {
		log.Printf("[Server] Connect failed: %v", err)
		return
	}
	defer target.Close()

	go func() {
		buf := make([]byte, 32*1024)
		io.CopyBuffer(target, conn, buf)
		target.Close()
	}()

	buf2 := make([]byte, 32*1024)
	io.CopyBuffer(conn, target, buf2)
}

func handleUDPForward(tunnelConn net.Conn, destAddr string) {
	// Dial UDP to target
	udpConn, err := net.DialTimeout("udp", destAddr, 10*time.Second)
	if err != nil {
		log.Printf("[UDP] Dial failed: %v", err)
		return
	}
	defer udpConn.Close()

	// 1. Tunnel -> Target
	go func() {
		buf := make([]byte, 65535)
		io.CopyBuffer(udpConn, tunnelConn, buf)
	}()

	// 2. Target -> Tunnel
	// UDP is connectionless, but net.Dial("udp") binds it to the remote addr,
	// so Read() only returns packets from that remote addr.
	buf2 := make([]byte, 65535)
	io.CopyBuffer(tunnelConn, udpConn, buf2)
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
