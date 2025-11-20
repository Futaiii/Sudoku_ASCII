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
	// 1. Sudoku Layer (With recording enabled)
	sConn := sudoku.NewConn(rawConn, table, cfg.PaddingMin, cfg.PaddingMax, true)

	// 2. Crypto Layer
	cConn, err := crypto.NewAEADConn(sConn, cfg.Key, cfg.AEAD)
	if err != nil {
		rawConn.Close()
		return
	}

	// 3. Handshake
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
	handler.HandleSocks5(cConn)
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
