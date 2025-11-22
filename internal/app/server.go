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
	"github.com/Futaiii/Sudoku_ASCII/internal/hybrid"
	"github.com/Futaiii/Sudoku_ASCII/internal/protocol"
	"github.com/Futaiii/Sudoku_ASCII/pkg/crypto"
	"github.com/Futaiii/Sudoku_ASCII/pkg/obfs/sudoku"
)

const HandshakeTimeout = 5 * time.Second

func RunServer(cfg *config.Config, table *sudoku.Table) {
	mgr := hybrid.GetInstance(cfg)
	if err := mgr.StartMieruServer(); err != nil {
		log.Fatalf("Failed to start Mieru Server: %v", err)
	}

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
		go handleServerConn(c, cfg, table, mgr)
	}
}

func handleServerConn(rawConn net.Conn, cfg *config.Config, table *sudoku.Table, mgr *hybrid.Manager) {
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

	// *** Detect Split Tunneling ***
	// 预读一个字节查看是否是 Magic 0xFF
	magicBuf := make([]byte, 1)
	if _, err := io.ReadFull(cConn, magicBuf); err != nil {
		return
	}

	var downstreamConn net.Conn = cConn // 默认为全双工 Sudoku
	var upstreamConn net.Conn = cConn

	if magicBuf[0] == 0xFF && cfg.EnableMieru {
		// Split Mode!
		// 读取 UUID
		lenBuf := make([]byte, 1)
		io.ReadFull(cConn, lenBuf)
		uuidBuf := make([]byte, int(lenBuf[0]))
		io.ReadFull(cConn, uuidBuf)
		uuid := string(uuidBuf)

		log.Printf("[Server] Split request UUID: %s, waiting for Mieru...", uuid)

		// 等待 Mieru 连接
		mConn, err := mgr.RegisterSudokuConn(uuid)
		if err != nil {
			log.Printf("[Server] Pairing failed: %v", err)
			return
		}

		// 成功配对
		downstreamConn = mConn

		// 完整读取 "BIND" 这
		discardBuf := make([]byte, 4)
		if _, err := io.ReadFull(mConn, discardBuf); err != nil {
			log.Printf("[Server] Failed to read BIND magic from Mieru: %v", err)
			mConn.Close()
			return
		}

	} else {

		// 重新封装一下
		upstreamConn = &PreBufferedConn{Conn: cConn, buf: magicBuf}
	}

	// 4. 读取目标地址 (从上行连接读取)
	destAddrStr, _, _, err := protocol.ReadAddress(upstreamConn)
	if err != nil {
		log.Printf("[Server] Failed to read target address: %v", err)
		return
	}

	log.Printf("[Server] Connecting to %s (Split: %v)", destAddrStr, downstreamConn != cConn)

	target, err := net.DialTimeout("tcp", destAddrStr, 10*time.Second)
	if err != nil {
		return
	}
	defer target.Close()

	// 6. 转发数据
	// 上行: Client (Sudoku) -> Target
	go func() {
		buf := make([]byte, 32*1024)
		io.CopyBuffer(target, upstreamConn, buf)
		target.Close()
	}()

	// 下行: Target -> Client (Mieru if split, else Sudoku)
	buf2 := make([]byte, 32*1024)
	io.CopyBuffer(downstreamConn, target, buf2)

	// 清理
	if downstreamConn != cConn {
		downstreamConn.Close()
	}
}

// Helper for peeking
type PreBufferedConn struct {
	net.Conn
	buf []byte
}

func (p *PreBufferedConn) Read(b []byte) (int, error) {
	if len(p.buf) > 0 {
		n := copy(b, p.buf)
		p.buf = p.buf[n:]
		return n, nil
	}
	return p.Conn.Read(b)
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
