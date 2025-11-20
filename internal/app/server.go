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
)

const HandshakeTimeout = 5 * time.Second

func RunServer(cfg *config.Config, table *sudoku.Table) {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.LocalPort))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Server on :%d (Fallback: %s)", cfg.LocalPort, cfg.FallbackAddr)

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

	// 握手成功，停止记录
	sConn.StopRecording()

	// 4. 读取目标地址 (由 Client 端在握手后立即发送)
	destAddrStr, _, _, err := protocol.ReadAddress(cConn)
	if err != nil {
		log.Printf("[Server] Failed to read target address: %v", err)
		return
	}

	log.Printf("[Server] Connecting to %s", destAddrStr)

	// 5. 连接目标
	target, err := net.DialTimeout("tcp", destAddrStr, 10*time.Second)
	if err != nil {
		log.Printf("[Server] Connect failed: %v", err)
		return
	}
	defer target.Close()

	// 6. 转发数据
	go func() {
		buf := make([]byte, 32*1024)
		io.CopyBuffer(target, cConn, buf)
		target.Close() // 即使单向结束也关闭连接
	}()

	buf2 := make([]byte, 32*1024)
	io.CopyBuffer(cConn, target, buf2)
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
