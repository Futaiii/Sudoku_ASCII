// internal/app/client.go
package app

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Futaiii/Sudoku_ASCII/internal/config"
	"github.com/Futaiii/Sudoku_ASCII/internal/protocol"
	mieruAdapter "github.com/Futaiii/Sudoku_ASCII/pkg/adapter/mieru"
	"github.com/Futaiii/Sudoku_ASCII/pkg/crypto"
	"github.com/Futaiii/Sudoku_ASCII/pkg/geodata"
	"github.com/Futaiii/Sudoku_ASCII/pkg/obfs/sudoku"
)

// PeekConn 允许查看第一个字节不消耗它
type PeekConn struct {
	net.Conn
	peeked []byte
}

func (c *PeekConn) Read(p []byte) (n int, err error) {
	if len(c.peeked) > 0 {
		n = copy(p, c.peeked)
		c.peeked = c.peeked[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

func RunClient(cfg *config.Config, table *sudoku.Table) {
	if cfg.MieruConfigPath != "" {
		go func() {
			if _, err := mieruAdapter.StartMieruClient(cfg.MieruConfigPath); err != nil {
				log.Printf("[Mieru] Failed to start client: %v", err)
			}
		}()
	}

	var geoMgr *geodata.Manager
	if cfg.ProxyMode == "pac" {
		geoMgr = geodata.GetInstance(cfg.RuleURLs)
	}

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.LocalPort))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Client (Mixed) on :%d -> %s | Mode: %s | Rules: %d",
		cfg.LocalPort, cfg.ServerAddress, cfg.ProxyMode, len(cfg.RuleURLs))

	for {
		c, err := l.Accept()
		if err != nil {
			continue
		}
		go handleMixedConn(c, cfg, table, geoMgr)
	}
}

func handleMixedConn(c net.Conn, cfg *config.Config, table *sudoku.Table, geoMgr *geodata.Manager) {
	// peek第一个字节以确定协议
	buf := make([]byte, 1)
	if _, err := io.ReadFull(c, buf); err != nil {
		c.Close()
		return
	}

	// 把读取的字节放回去
	pConn := &PeekConn{Conn: c, peeked: buf}

	if buf[0] == 0x05 {
		// SOCKS5
		handleClientSocks5(pConn, cfg, table, geoMgr)
	} else {
		// 假设是 HTTP/HTTPS
		handleHTTP(pConn, cfg, table, geoMgr)
	}
}

// ==== SOCKS5 Handler ====

func handleClientSocks5(conn net.Conn, cfg *config.Config, table *sudoku.Table, geoMgr *geodata.Manager) {
	defer conn.Close()

	// 1. SOCKS5 握手
	buf := make([]byte, 262)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	nMethods := int(buf[1])
	if _, err := io.ReadFull(conn, buf[:nMethods]); err != nil {
		return
	}
	conn.Write([]byte{0x05, 0x00})

	// 2. 读取请求
	header := make([]byte, 3)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}

	// CMD: header[1] (0x01 Connect)
	if header[1] != 0x01 {
		// 不支持 Bind 或 UDP Associate
		return
	}

	destAddrStr, _, destIP, err := protocol.ReadAddress(conn)
	if err != nil {
		return
	}

	// 3. 路由与连接
	targetConn, success := dialTarget(destAddrStr, destIP, cfg, table, geoMgr)
	if !success {
		// SOCKS5 Error
		conn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	// SOCKS5 Success
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// 4. 转发
	startPipe(conn, targetConn)
}

// ==== HTTP Handler ====

func handleHTTP(conn net.Conn, cfg *config.Config, table *sudoku.Table, geoMgr *geodata.Manager) {
	defer conn.Close()

	req, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return
	}

	host := req.Host
	// 如果不带端口，默认补全
	if !strings.Contains(host, ":") {
		if req.Method == http.MethodConnect {
			host += ":443"
		} else {
			host += ":80"
		}
	}

	// 解析 IP (为了路由决策)
	hostName, _, _ := net.SplitHostPort(host)
	destIP := net.ParseIP(hostName)

	// 路由决策与连接
	targetConn, success := dialTarget(host, destIP, cfg, table, geoMgr)
	if !success {
		conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}

	if req.Method == http.MethodConnect {
		// HTTPS Tunnel: 建立连接后回复 200 OK，然后纯透传
		conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		startPipe(conn, targetConn)
	} else {
		req.RequestURI = ""
		// 如果是绝对路径转换为相对路径
		if req.URL.Scheme != "" {
			req.URL.Scheme = ""
			req.URL.Host = ""
		}

		if err := req.Write(targetConn); err != nil {
			targetConn.Close()
			return
		}
		startPipe(conn, targetConn)
	}
}

// ==== Common Logic ====

func dialTarget(destAddrStr string, destIP net.IP, cfg *config.Config, table *sudoku.Table, geoMgr *geodata.Manager) (net.Conn, bool) {
	shouldProxy := true

	if cfg.ProxyMode == "global" {
		shouldProxy = true
	} else if cfg.ProxyMode == "direct" {
		shouldProxy = false
	} else if cfg.ProxyMode == "pac" {
		// 1. 检查域名或已知 IP 是否在 CN 列表
		if geoMgr.IsCN(destAddrStr, destIP) {
			shouldProxy = false
			log.Printf("[PAC] %s -> DIRECT (Rule Match)", destAddrStr)
		} else {
			// 2. 如果没有匹配且 destIP 未知 (是域名)，尝试解析 IP 再检查
			if destIP == nil {
				host, _, _ := net.SplitHostPort(destAddrStr)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
				cancel()

				if err == nil && len(ips) > 0 {
					if geoMgr.IsCN(destAddrStr, ips[0]) {
						shouldProxy = false
						log.Printf("[PAC] %s (%s) -> DIRECT (IP Rule Match)", destAddrStr, ips[0])
					} else {
						log.Printf("[PAC] %s (%s) -> PROXY", destAddrStr, ips[0])
					}
				} else {
					// 解析失败或无 IP，默认代理
					log.Printf("[PAC] %s -> PROXY (Default)", destAddrStr)
				}
			} else {
				log.Printf("[PAC] %s -> PROXY", destAddrStr)
			}
		}
	}

	if shouldProxy {
		// 1. 建立 TCP 连接
		rawRemote, err := net.DialTimeout("tcp", cfg.ServerAddress, 5*time.Second)
		if err != nil {
			log.Printf("[Proxy] Dial Server Failed: %v", err)
			return nil, false
		}

		// 2. 决定 Sudoku 方向
		// 默认是 Duplex
		dir := sudoku.DirDuplex

		// 如果上行是 sudoku，下行不是 sudoku (例如 mieru)，则 Sudoku 只负责写 (Upstream)
		if cfg.UpstreamProto == "sudoku" && cfg.DownstreamProto != "sudoku" {
			dir = sudoku.DirWriteOnly
		} else if cfg.UpstreamProto != "sudoku" && cfg.DownstreamProto == "sudoku" {
			// 这种情况比较少见，下行混淆上行不混淆
			dir = sudoku.DirReadOnly
		}

		// 3. 包装 Sudoku 层
		sConn := sudoku.NewConn(rawRemote, table, cfg.PaddingMin, cfg.PaddingMax, false, dir)

		// 4. 加密层 (AEAD)
		// 如果协议分离，暂时禁用 AEAD (设为 none)
		// 为了生产环境稳定，建议在分离模式下，config中AEAD 设为 "none" (完全依赖协议自身的安全性)

		effectiveAEAD := cfg.AEAD
		if dir != sudoku.DirDuplex {
			// 在混合协议模式下，为了避免加密层冲突，暂时强制 AEAD 失效（或者需要类似 Sudoku 的单向改造）
			effectiveAEAD = "none"
		}

		cConn, err := crypto.NewAEADConn(sConn, cfg.Key, effectiveAEAD)
		if err != nil {
			rawRemote.Close()
			return nil, false
		}

		// 5. 握手
		// 如果上行是 Sudoku，我们需要发送握手。
		if cfg.UpstreamProto == "sudoku" {
			handshake := make([]byte, 16)
			binary.BigEndian.PutUint64(handshake[:8], uint64(time.Now().Unix()))
			rand.Read(handshake[8:])
			if _, err := cConn.Write(handshake); err != nil {
				cConn.Close()
				return nil, false
			}

			if err := protocol.WriteAddress(cConn, destAddrStr); err != nil {
				cConn.Close()
				return nil, false
			}
		}

		// 返回连接。

		return cConn, true
	} else {
		// 直连逻辑保持不变
		dConn, err := net.DialTimeout("tcp", destAddrStr, 5*time.Second)
		if err != nil {
			log.Printf("[Direct] Dial Failed: %v", err)
			return nil, false
		}
		return dConn, true
	}
}

func startPipe(c1, c2 net.Conn) {
	go func() {
		io.Copy(c1, c2)
		c1.Close()
		c2.Close()
	}()
	io.Copy(c2, c1)
	c2.Close()
	c1.Close()
}
