// internal/app/client.go
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
	var geoMgr *geoip.Manager
	if cfg.ProxyMode == "pac" {
		geoMgr = geoip.GetInstance(cfg.GeoIPURL)
	}

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.LocalPort))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Client on :%d -> %s | Mode: %s | Obfs: %s",
		cfg.LocalPort, cfg.ServerAddress, cfg.ProxyMode, cfg.ASCII)

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
	header := make([]byte, 3)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}
	if header[1] != 0x01 {
		return
	}

	destAddrStr, _, destIP, err := protocol.ReadAddress(conn)
	if err != nil {
		return
	}

	// 3. 路由决策
	shouldProxy := true

	if cfg.ProxyMode == "global" {
		shouldProxy = true
	} else if cfg.ProxyMode == "direct" {
		shouldProxy = false
	} else if cfg.ProxyMode == "pac" {
		checkIP := destIP

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
			if geoMgr.Contains(checkIP) {
				shouldProxy = false // CN IP 直连
				log.Printf("[PAC] %s (%s) -> DIRECT", destAddrStr, checkIP)
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
		// 连接 Sudoku Server
		rawRemote, err := net.DialTimeout("tcp", cfg.ServerAddress, 5*time.Second)
		if err != nil {
			log.Printf("[Proxy] Dial Server Failed: %v", err)
			// SOCKS5 Error Reply
			conn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
			return
		}

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
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// 6. 双向转发
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
