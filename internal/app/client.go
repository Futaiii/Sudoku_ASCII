package app

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/Futaiii/Sudoku_ASCII/internal/config"
	"github.com/Futaiii/Sudoku_ASCII/internal/protocol"
	"github.com/Futaiii/Sudoku_ASCII/pkg/crypto"
	"github.com/Futaiii/Sudoku_ASCII/pkg/geoip"
	"github.com/Futaiii/Sudoku_ASCII/pkg/obfs/sudoku"
)

func RunClient(cfg *config.Config, table *sudoku.Table) {
	// 初始化 GeoIP 管理器
	var geoMgr *geoip.Manager
	if cfg.ProxyMode == "pac" {
		geoMgr = geoip.GetInstance(cfg.GeoIPURL)
	}

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.LocalPort))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Client (SOCKS5) on :%d -> Server: %s | Mode: %s", cfg.LocalPort, cfg.ServerAddress, cfg.ProxyMode)

	for {
		c, err := l.Accept()
		if err != nil {
			continue
		}
		go handleClientSocks5(c, cfg, table, geoMgr)
	}
}

func handleClientSocks5(conn net.Conn, cfg *config.Config, table *sudoku.Table, geoMgr *geoip.Manager) {
	defer conn.Close()

	// 1. SOCKS5 握手 (无认证)
	buf := make([]byte, 262)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	if buf[0] != 0x05 {
		return
	}
	nMethods := int(buf[1])
	if _, err := io.ReadFull(conn, buf[:nMethods]); err != nil {
		return
	}
	conn.Write([]byte{0x05, 0x00})

	// 2. 读取请求详情 (CMD, DST.ADDR, DST.PORT)
	// 注意：我们手动读取头部，但使用 protocol.ReadAddress 读取地址部分
	header := make([]byte, 3) // VER, CMD, RSV
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}

	if header[1] != 0x01 { // 仅支持 CONNECT
		log.Printf("[SOCKS5] Unsupported CMD: %d", header[1])
		return
	}

	// 读取目标地址
	destAddrStr, _, destIP, err := protocol.ReadAddress(conn)
	if err != nil {
		log.Printf("[SOCKS5] Failed to read addr: %v", err)
		return
	}

	// 3. 路由决策 (PAC)
	shouldProxy := true

	if cfg.ProxyMode == "global" {
		shouldProxy = true
	} else if cfg.ProxyMode == "direct" {
		shouldProxy = false
	} else if cfg.ProxyMode == "pac" {
		// GeoIP 匹配逻辑
		checkIP := destIP

		// 如果是域名，需要解析 IP 来判断 (PAC 标准行为)
		if checkIP == nil {
			host, _, _ := net.SplitHostPort(destAddrStr)
			// 使用本地 DNS 解析，设置超时
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
			if err == nil && len(ips) > 0 {
				checkIP = ips[0]
			}
		}

		if checkIP != nil {
			// 如果在 CN 列表内 -> 直连 (Don't Proxy)
			if geoMgr.Contains(checkIP) {
				shouldProxy = false
				log.Printf("[PAC] %s (%s) -> DIRECT (CN Rule)", destAddrStr, checkIP)
			} else {
				shouldProxy = true
				log.Printf("[PAC] %s (%s) -> PROXY", destAddrStr, checkIP)
			}
		} else {
			// 解析失败，默认走代理以防万一
			shouldProxy = true
			log.Printf("[PAC] %s -> PROXY (Resolution Failed)", destAddrStr)
		}
	}

	// 4. 建立连接
	var targetConn net.Conn

	if shouldProxy {
		// ==== 走代理通道 ====
		// 连接 Sudoku Server
		rawRemote, err := net.DialTimeout("tcp", cfg.ServerAddress, 5*time.Second)
		if err != nil {
			log.Printf("[Proxy] Dial Server Failed: %v", err)
			// SOCKS5 Error Reply
			conn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
			return
		}

		// 包装连接
		sConn := sudoku.NewConn(rawRemote, table, cfg.PaddingMin, cfg.PaddingMax, false)
		cConn, err := crypto.NewAEADConn(sConn, cfg.Key, cfg.AEAD)
		if err != nil {
			rawRemote.Close()
			return
		}

		// 4.1 发送 Sudoku 握手
		handshake := make([]byte, 16)
		binary.BigEndian.PutUint64(handshake[:8], uint64(time.Now().Unix()))
		rand.Read(handshake[8:])
		if _, err := cConn.Write(handshake); err != nil {
			cConn.Close()
			return
		}

		// 4.2 发送目标地址给服务端
		if err := protocol.WriteAddress(cConn, destAddrStr); err != nil {
			cConn.Close()
			return
		}

		targetConn = cConn
	} else {
		// ==== 直连 ====
		dConn, err := net.DialTimeout("tcp", destAddrStr, 5*time.Second)
		if err != nil {
			conn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
			return
		}
		targetConn = dConn
	}

	// 5. 发送 SOCKS5 成功响应给本地客户端
	// 无论直连还是代理，此时连接已建立
	// 响应 0x00 (Succeeded), BIND.ADDR 0.0.0.0:0
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// 6. 双向转发
	// 使用 io.CopyBuffer 减少内存分配
	go func() {
		buf := make([]byte, 32*1024)
		io.CopyBuffer(targetConn, conn, buf)
		targetConn.Close()
		conn.Close()
	}()

	buf2 := make([]byte, 32*1024)
	io.CopyBuffer(conn, targetConn, buf2)
	conn.Close()
	targetConn.Close()
}
