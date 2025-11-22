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

	mieruAdapter "github.com/Futaiii/Sudoku_ASCII/pkg/adapter/mieru"
)

const HandshakeTimeout = 5 * time.Second

func RunServer(cfg *config.Config, table *sudoku.Table) {
	if cfg.MieruConfigPath != "" {
		go func() {
			if _, err := mieruAdapter.StartMieruServer(cfg.MieruConfigPath); err != nil {
				log.Printf("[Mieru] Failed to start server: %v", err)
			}
		}()
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
		go handleServerConn(c, cfg, table)
	}
}

func handleServerConn(rawConn net.Conn, cfg *config.Config, table *sudoku.Table) {
	// 1. 确定方向配置
	// 服务端的 Upstream 是指 "Client -> Server"，与 Client 端定义一致
	dir := sudoku.DirDuplex
	if cfg.UpstreamProto == "sudoku" && cfg.DownstreamProto != "sudoku" {
		// 服务端视角：读（来自客户端）是 Sudoku，写（发给客户端）是透传
		dir = sudoku.DirReadOnly
	} else if cfg.UpstreamProto != "sudoku" && cfg.DownstreamProto == "sudoku" {
		dir = sudoku.DirWriteOnly
	}

	// 2. Sudoku 层 (开启记录以支持回落)
	sConn := sudoku.NewConn(rawConn, table, cfg.PaddingMin, cfg.PaddingMax, true, dir)

	// 3. 加密层
	// 同 Client，如果是混合模式，暂时禁用 AEAD 以免冲突
	effectiveAEAD := cfg.AEAD
	if dir != sudoku.DirDuplex {
		effectiveAEAD = "none"
	}

	cConn, err := crypto.NewAEADConn(sConn, cfg.Key, effectiveAEAD)
	if err != nil {
		rawConn.Close()
		return
	}
	defer cConn.Close()

	// 4. 验证握手
	// 只有当 Upstream (Read side for server) 是 Sudoku 时才需要握手
	if cfg.UpstreamProto == "sudoku" {
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
	}

	// 5. 读取目标地址
	// 如果 Upstream 是 Sudoku，地址在流的前面
	var destAddrStr string
	if cfg.UpstreamProto == "sudoku" {
		var err error
		destAddrStr, _, _, err = protocol.ReadAddress(cConn)
		if err != nil {
			log.Printf("[Server] Failed to read target address: %v", err)
			return
		}
	} else {
		// 如果上行不是 Sudoku
		// 1. 这是一个纯 Mieru 端口（应该用 Mieru Server 接管）
		// 2. 或者协议设计为透明代理
		log.Printf("[Server] Error: Upstream must be Sudoku to parse destination in this mode")
		return
	}

	log.Printf("[Server] Connecting to %s (Downstream: %s)", destAddrStr, cfg.DownstreamProto)

	// 6. 连接目标
	target, err := net.DialTimeout("tcp", destAddrStr, 10*time.Second)
	if err != nil {
		log.Printf("[Server] Connect failed: %v", err)
		return
	}
	defer target.Close()

	// 7. 转发数据
	// 如果 DownstreamProto == "mieru"，这里实现为 "Separation Ready"：
	// Server 端解码 Sudoku 上行，然后将下行数据直接发回（不做 Sudoku 混淆）。

	go func() {
		buf := make([]byte, 32*1024)
		io.CopyBuffer(target, cConn, buf)
		target.Close()
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
