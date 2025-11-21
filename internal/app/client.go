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
	"sync"
	"time"

	"github.com/Futaiii/Sudoku_ASCII/internal/config"
	"github.com/Futaiii/Sudoku_ASCII/internal/protocol"
	"github.com/Futaiii/Sudoku_ASCII/pkg/crypto"
	"github.com/Futaiii/Sudoku_ASCII/pkg/geoip"
	"github.com/Futaiii/Sudoku_ASCII/pkg/obfs/sudoku"
	"github.com/Futaiii/Sudoku_ASCII/pkg/transport"
)

// UDP Session Manager
type udpSession struct {
	conn       net.Conn
	lastActive time.Time
}

type clientHandler struct {
	cfg         *config.Config
	table       *sudoku.Table
	geoMgr      *geoip.Manager
	udpSessions sync.Map // map[string]*udpSession (SourceAddr -> TunnelConn)
}

func RunClient(cfg *config.Config, table *sudoku.Table) {
	var geoMgr *geoip.Manager
	if cfg.ProxyMode == "pac" {
		geoMgr = geoip.GetInstance(cfg.GeoIPURL)
	}

	h := &clientHandler{
		cfg:    cfg,
		table:  table,
		geoMgr: geoMgr,
	}

	// 启动 UDP 垃圾回收
	go h.cleanupUDPSessions()

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.LocalPort))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Client on :%d -> %s (%s) | Mode: %s | Obfs: %s",
		cfg.LocalPort, cfg.ServerAddress, cfg.Transport, cfg.ProxyMode, cfg.ASCII)

	for {
		c, err := l.Accept()
		if err != nil {
			continue
		}
		go h.handleSocks5(c)
	}
}

func (h *clientHandler) handleSocks5(conn net.Conn) {
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
	if header[0] != 0x05 {
		return
	}

	cmd := header[1]
	// Handle UDP ASSOCIATE (CMD 3)
	if cmd == 0x03 {
		h.handleUDP(conn)
		return
	}

	// Handle CONNECT (CMD 1)
	if cmd != 0x01 {
		log.Printf("[SOCKS5] Unsupported CMD: %d", header[1])
		// Unsupported CMD
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	destAddrStr, _, destIP, err := protocol.ReadAddress(conn)
	if err != nil {
		return
	}

	// 3. 路由决策
	shouldProxy := h.evaluateRouting(destAddrStr, destIP)

	// 4. 建立连接
	var targetConn net.Conn
	if shouldProxy {
		// Use transport abstraction
		rawRemote, err := transport.Dial(h.cfg.Transport, h.cfg.ServerAddress)
		if err != nil {
			log.Printf("[Proxy] Dial Server Failed: %v", err)
			conn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
			return
		}

		sConn := sudoku.NewConn(rawRemote, h.table, h.cfg.PaddingMin, h.cfg.PaddingMax, false)
		cConn, err := crypto.NewAEADConn(sConn, h.cfg.Key, h.cfg.AEAD)
		if err != nil {
			rawRemote.Close()
			return
		}

		// 4.1 发送 Sudoku 握手
		if err := h.sendHandshake(cConn); err != nil {
			cConn.Close()
			return
		}

		// 4.2 Write Header (TCP)
		if err := protocol.WriteHeader(cConn, protocol.NetTypeTCP, destAddrStr); err != nil {
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

// handleUDP deals with SOCKS5 UDP ASSOCIATE
func (h *clientHandler) handleUDP(conn net.Conn) {
	// Consume the dummy address sent with UDP Associate
	_, _, _, err := protocol.ReadAddress(conn)
	if err != nil {
		return
	}

	// 1. Start a local UDP listener
	udpListener, err := net.ListenPacket("udp", ":0")
	if err != nil {
		log.Printf("[UDP] Failed to listen: %v", err)
		conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer udpListener.Close()

	udpAddr := udpListener.LocalAddr().(*net.UDPAddr)

	// 2. Respond to client with the IP/Port to send UDP packets to
	// We need to send the IP that the client can reach.
	// For simplicity, we use 0.0.0.0 (or the incoming connection's local addr)
	reply := make([]byte, 10)
	copy(reply, []byte{0x05, 0x00, 0x00, 0x01})
	copy(reply[4:], net.IPv4(0, 0, 0, 0).To4()) // Bind Addr
	binary.BigEndian.PutUint16(reply[8:], uint16(udpAddr.Port))
	conn.Write(reply)

	// Keep the TCP connection alive (SOCKS5 requirement)
	go func() {
		io.Copy(io.Discard, conn)
		udpListener.Close() // Close UDP if TCP control closes
	}()

	// 3. UDP Relay Loop
	buf := make([]byte, 65535)
	for {
		n, clientAddr, err := udpListener.ReadFrom(buf)
		if err != nil {
			break
		}

		// SOCKS5 UDP Header: RSV(2) FRAG(1) ATYP(1) DST.ADDR DST.PORT DATA
		if n < 3 {
			continue
		}
		// Frag is buf[2], we don't support fragmentation (0x00)
		if buf[2] != 0x00 {
			continue
		}

		// Extract Destination Address from the packet
		// protocol.ReadAddress expects a Reader, let's wrap the buffer slice
		// Skip first 3 bytes (RSV, RSV, FRAG)
		reader := &byteReader{data: buf[3:n]}
		destAddrStr, _, _, err := protocol.ReadAddress(reader)
		if err != nil {
			continue
		}
		headerLen := 3 + reader.offset // Total SOCKS header length
		payload := buf[headerLen:n]

		// 4. Send through Tunnel
		go h.forwardUDP(clientAddr, destAddrStr, payload, udpListener)
	}
}

// forwardUDP manages the tunnel connection for a specific UDP source
func (h *clientHandler) forwardUDP(clientAddr net.Addr, destAddr string, payload []byte, udpConn net.PacketConn) {
	key := clientAddr.String()

	var tunnel net.Conn
	val, ok := h.udpSessions.Load(key)

	if ok {
		sess := val.(*udpSession)
		tunnel = sess.conn
		sess.lastActive = time.Now()
	} else {
		// Create new tunnel
		rawRemote, err := transport.Dial(h.cfg.Transport, h.cfg.ServerAddress)
		if err != nil {
			return
		}
		sConn := sudoku.NewConn(rawRemote, h.table, h.cfg.PaddingMin, h.cfg.PaddingMax, false)
		cConn, err := crypto.NewAEADConn(sConn, h.cfg.Key, h.cfg.AEAD)
		if err != nil {
			rawRemote.Close()
			return
		}

		if err := h.sendHandshake(cConn); err != nil {
			cConn.Close()
			return
		}

		// Write Header (UDP)
		if err := protocol.WriteHeader(cConn, protocol.NetTypeUDP, destAddr); err != nil {
			cConn.Close()
			return
		}

		tunnel = cConn
		h.udpSessions.Store(key, &udpSession{conn: tunnel, lastActive: time.Now()})

		// Start reader for this tunnel (Server -> Client)
		go func() {
			defer func() {
				tunnel.Close()
				h.udpSessions.Delete(key)
			}()

			respBuf := make([]byte, 65535)
			for {
				tunnel.SetReadDeadline(time.Now().Add(60 * time.Second))
				n, err := tunnel.Read(respBuf)
				if err != nil {
					return
				}

				// Wrap in SOCKS5 UDP Header and send to Client
				// RSV(2) FRAG(1) -> 0,0,0
				// We need to reconstruct the header. The server sent raw data.
				// SOCKS5 expects the original destination address in the reply header.
				// For simplicity/performance, we can put 0.0.0.0 or the actual source.
				// Let's put 0.0.0.0:0 as many clients ignore this or just need valid format.

				// Construct packet: [0 0 0] [ATYP 1] [0 0 0 0] [0 0] [DATA]
				packet := make([]byte, 0, 10+n)
				packet = append(packet, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0)
				packet = append(packet, respBuf[:n]...)
				udpConn.WriteTo(packet, clientAddr)
			}
		}()
	}

	// Write payload to tunnel
	// Need to frame it?
	// The current protocol is Stream. For UDP over Stream, if we reuse the connection,
	// we assume the server just forwards everything.
	// Since we open a NEW tunnel per "Source Client", the stream is exclusive to that flow.
	// So we can just write raw bytes.
	tunnel.SetWriteDeadline(time.Now().Add(10 * time.Second))
	tunnel.Write(payload)
}

func (h *clientHandler) sendHandshake(conn net.Conn) error {
	handshake := make([]byte, 16)
	binary.BigEndian.PutUint64(handshake[:8], uint64(time.Now().Unix()))
	rand.Read(handshake[8:])
	_, err := conn.Write(handshake)
	return err
}

func (h *clientHandler) evaluateRouting(destAddrStr string, destIP net.IP) bool {
	if h.cfg.ProxyMode == "global" {
		return true
	} else if h.cfg.ProxyMode == "direct" {
		return false
	}

	checkIP := destIP
	if checkIP == nil {
		host, _, _ := net.SplitHostPort(destAddrStr)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
		if err == nil && len(ips) > 0 {
			checkIP = ips[0]
		}
	}

	if checkIP != nil && h.geoMgr.Contains(checkIP) {
		log.Printf("[PAC] %s (%s) -> DIRECT", destAddrStr, checkIP)
		return false // CN Direct
	}

	log.Printf("[PAC] %s (%s) -> PROXY", destAddrStr, checkIP)
	return true
}

func (h *clientHandler) cleanupUDPSessions() {
	for {
		time.Sleep(30 * time.Second)
		now := time.Now()
		h.udpSessions.Range(func(key, value interface{}) bool {
			sess := value.(*udpSession)
			if now.Sub(sess.lastActive) > 60*time.Second {
				sess.conn.Close()
				h.udpSessions.Delete(key)
			}
			return true
		})
	}
}

// byteReader helper for ReadAddress from slice
type byteReader struct {
	data   []byte
	offset int
}

func (r *byteReader) Read(p []byte) (n int, err error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}
