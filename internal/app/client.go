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
	"github.com/Futaiii/Sudoku_ASCII/internal/hybrid"
	"github.com/Futaiii/Sudoku_ASCII/internal/protocol"
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
	mgr := hybrid.GetInstance(cfg)
	if err := mgr.StartMieruClient(); err != nil {
		log.Fatalf("Failed to start Mieru Client: %v", err)
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
		go handleMixedConn(c, cfg, table, geoMgr, mgr)
	}
}

func handleMixedConn(c net.Conn, cfg *config.Config, table *sudoku.Table, geoMgr *geodata.Manager, mgr *hybrid.Manager) {
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
		handleClientSocks5(pConn, cfg, table, geoMgr, mgr)
	} else {
		// 假设是 HTTP/HTTPS
		handleHTTP(pConn, cfg, table, geoMgr, mgr)
	}
}

// ==== SOCKS5 Handler ====

func handleClientSocks5(conn net.Conn, cfg *config.Config, table *sudoku.Table, geoMgr *geodata.Manager, mgr *hybrid.Manager) {
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
	targetConn, success := dialTarget(destAddrStr, destIP, cfg, table, geoMgr, mgr)
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

func handleHTTP(conn net.Conn, cfg *config.Config, table *sudoku.Table, geoMgr *geodata.Manager, mgr *hybrid.Manager) {
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
	targetConn, success := dialTarget(host, destIP, cfg, table, geoMgr, mgr)
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

func dialTarget(destAddrStr string, destIP net.IP, cfg *config.Config, table *sudoku.Table, geoMgr *geodata.Manager, mgr *hybrid.Manager) (net.Conn, bool) {
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
		// 1. Sudoku Dial (Uplink)
		rawRemote, err := net.DialTimeout("tcp", cfg.ServerAddress, 5*time.Second)
		if err != nil {
			log.Printf("[Proxy] Dial Server Failed: %v", err)
			return nil, false
		}

		sConn := sudoku.NewConn(rawRemote, table, cfg.PaddingMin, cfg.PaddingMax, false)
		cConn, err := crypto.NewAEADConn(sConn, cfg.Key, cfg.AEAD)
		if err != nil {
			rawRemote.Close()
			return nil, false
		}

		// 2. 握手逻辑
		handshake := make([]byte, 16+1+32) // Expand buffer for potential UUID
		binary.BigEndian.PutUint64(handshake[:8], uint64(time.Now().Unix()))
		rand.Read(handshake[8:16])

		var splitUUID string

		if cfg.EnableMieru {
			// 标记位：0x01 = Standard, 0x02 = Split Tunnel
			// 这里我们不仅需要发时间戳，还需要发 UUID
			// 为了兼容性，我们在标准握手后由协议层约定
			// 让我们稍微修改一下协议：在写完 Address 之前
		}

		if _, err := cConn.Write(handshake[:16]); err != nil {
			cConn.Close()
			return nil, false
		}

		// *** Split Mode Logic ***
		if cfg.EnableMieru {
			splitUUID = hybrid.GenerateUUID()
			// 发送 Split 标志 (0xFF) + UUID
			// 这是自定义扩展协议
			magic := []byte{0xFF}
			uuidBytes := []byte(splitUUID) // hex string usually 32 bytes
			lenByte := byte(len(uuidBytes))

			// 发送 [Magic][Len][UUID]
			cConn.Write(magic)
			cConn.Write([]byte{lenByte})
			cConn.Write(uuidBytes)

			// 3. 并行建立 Mieru Downlink
			// 注意：这里会阻塞等待，直到 Mieru 建立完成。
			// 实际生产中建议用 errgroup 并行，但这里为了逻辑清晰顺序写

			mConn, err := mgr.DialMieruForDownlink(splitUUID)
			if err != nil {
				log.Printf("[Split] Failed to dial Mieru: %v", err)
				cConn.Close()
				return nil, false
			}

			// 4. 组合连接
			// Sudoku (cConn) 用于写 (上行)
			// Mieru (mConn) 用于读 (下行)
			// 但注意：我们之前写入了 "BIND"，Mieru Conn 可能包含服务端的握手响应。
			// 服务端在配对成功后，应该直接开始转发数据。

			// 创建混合连接对象
			hybridConn := &hybrid.SplitConn{
				Conn:   cConn, // 基础接口用 Sudoku 的
				Writer: cConn,
				Reader: mConn,
				CloseFn: func() error {
					e1 := cConn.Close()
					e2 := mConn.Close()
					if e1 != nil {
						return e1
					}
					return e2
				},
			}

			// 5. 发送目标地址 (通过 Sudoku 上行发送)
			if err := protocol.WriteAddress(hybridConn, destAddrStr); err != nil {
				hybridConn.Close()
				return nil, false
			}

			return hybridConn, true

		} else {
			// 标准模式
			// 发送 0x00 表示非 Split 模式，或者如果服务端不强制检查，则不需要
			// 为了兼容旧版 Server，如果旧版 Server 读到 Address 的第一个字节不是 0xFF 而是 AddrType (1,3,4)，则正常
			// 0xFF 不是有效的 AddrType，所以是兼容的。
			if err := protocol.WriteAddress(cConn, destAddrStr); err != nil {
				cConn.Close()
				return nil, false
			}
			return cConn, true
		}
	} else {
		// 直连
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
