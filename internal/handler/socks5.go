package handler

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

func HandleSocks5(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 262)

	// 协商
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

	// 请求
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		return
	}
	if buf[1] != 0x01 { // CONNECT only
		return
	}

	var destAddr string
	switch buf[3] {
	case 0x01: // IPv4
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return
		}
		portBuf := make([]byte, 2)
		if _, err := io.ReadFull(conn, portBuf); err != nil {
			return
		}
		destAddr = fmt.Sprintf("%s:%d", net.IP(buf[:4]).String(), binary.BigEndian.Uint16(portBuf))
	case 0x03: // Domain
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return
		}
		dLen := int(buf[0])
		domainBuf := make([]byte, dLen)
		if _, err := io.ReadFull(conn, domainBuf); err != nil {
			return
		}
		portBuf := make([]byte, 2)
		if _, err := io.ReadFull(conn, portBuf); err != nil {
			return
		}
		destAddr = fmt.Sprintf("%s:%d", string(domainBuf), binary.BigEndian.Uint16(portBuf))
	default:
		return
	}

	log.Printf("[SOCKS5] Connecting to %s", destAddr)
	target, err := net.DialTimeout("tcp", destAddr, 10*time.Second)
	if err != nil {
		log.Printf("[SOCKS5] Connect failed: %v", err)
		return
	}
	defer target.Close()

	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// Bridge
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { io.Copy(target, conn); target.Close(); wg.Done() }()
	go func() { io.Copy(conn, target); conn.Close(); wg.Done() }()
	wg.Wait()
}
