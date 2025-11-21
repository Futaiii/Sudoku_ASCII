// internal/protocol/address.go
package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
)

// AddrType 定义
const (
	AddrTypeIPv4   = 0x01
	AddrTypeDomain = 0x03
	AddrTypeIPv6   = 0x04
)

// NetType 定义 (Tunnel 协议)
const (
	NetTypeTCP = 0x01
	NetTypeUDP = 0x02
)

// ReadHeader 读取协议头：[NetType] [Addr...]
func ReadHeader(r io.Reader) (byte, string, net.IP, error) {
	buf := make([]byte, 262)

	// 1. 读取 NetType (TCP/UDP)
	if _, err := io.ReadFull(r, buf[:1]); err != nil {
		return 0, "", nil, err
	}
	netType := buf[0]
	if netType != NetTypeTCP && netType != NetTypeUDP {
		return 0, "", nil, fmt.Errorf("unknown net type: %d", netType)
	}

	// 2. 读取地址类型
	if _, err := io.ReadFull(r, buf[:1]); err != nil {
		return 0, "", nil, err
	}
	addrType := buf[0]

	var host string
	var ip net.IP

	switch addrType {
	case AddrTypeIPv4:
		if _, err := io.ReadFull(r, buf[:4]); err != nil {
			return 0, "", nil, err
		}
		ip = net.IP(buf[:4])
		host = ip.String()
	case AddrTypeDomain:
		if _, err := io.ReadFull(r, buf[:1]); err != nil {
			return 0, "", nil, err
		}
		domainLen := int(buf[0])
		if _, err := io.ReadFull(r, buf[:domainLen]); err != nil {
			return 0, "", nil, err
		}
		host = string(buf[:domainLen])
	case AddrTypeIPv6:
		if _, err := io.ReadFull(r, buf[:16]); err != nil {
			return 0, "", nil, err
		}
		ip = net.IP(buf[:16])
		host = fmt.Sprintf("[%s]", ip.String())
	default:
		return 0, "", nil, fmt.Errorf("unknown address type: %d", addrType)
	}

	// 3. 读取端口
	if _, err := io.ReadFull(r, buf[:2]); err != nil {
		return 0, "", nil, err
	}
	port := binary.BigEndian.Uint16(buf[:2])

	return netType, fmt.Sprintf("%s:%d", host, port), ip, nil
}

// WriteHeader 写入协议头 [NetType] [Addr...]
func WriteHeader(w io.Writer, netType byte, rawAddr string) error {
	// 写入 NetType
	if _, err := w.Write([]byte{netType}); err != nil {
		return err
	}

	// 写入地址 (复用原有逻辑，但内联以减少 buffer 创建)
	host, portStr, err := net.SplitHostPort(rawAddr)
	if err != nil {
		return err
	}
	portInt, _ := net.LookupPort("tcp", portStr)
	ip := net.ParseIP(host)

	buf := make([]byte, 0, 300)

	if ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			buf = append(buf, AddrTypeIPv4)
			buf = append(buf, ip4...)
		} else {
			buf = append(buf, AddrTypeIPv6)
			buf = append(buf, ip...)
		}
	} else {
		buf = append(buf, AddrTypeDomain)
		if len(host) > 255 {
			return errors.New("domain too long")
		}
		buf = append(buf, byte(len(host)))
		buf = append(buf, []byte(host)...)
	}

	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(portInt))
	buf = append(buf, portBytes...)

	_, err = w.Write(buf)
	return err
}

// ReadAddress 仅用于 Client 解析 SOCKS5 请求，保留原逻辑
// 但需要注意：SOCKS5 UDP 请求中的地址在数据包里，不在握手阶段
func ReadAddress(r io.Reader) (string, byte, net.IP, error) {
	// 复用上面的 ReadHeader 逻辑，只是没有 NetType
	buf := make([]byte, 262)
	if _, err := io.ReadFull(r, buf[:1]); err != nil {
		return "", 0, nil, err
	}
	addrType := buf[0]
	var host string
	var ip net.IP

	switch addrType {
	case AddrTypeIPv4:
		if _, err := io.ReadFull(r, buf[:4]); err != nil {
			return "", 0, nil, err
		}
		ip = net.IP(buf[:4])
		host = ip.String()
	case AddrTypeDomain:
		if _, err := io.ReadFull(r, buf[:1]); err != nil {
			return "", 0, nil, err
		}
		domainLen := int(buf[0])
		if _, err := io.ReadFull(r, buf[:domainLen]); err != nil {
			return "", 0, nil, err
		}
		host = string(buf[:domainLen])
	case AddrTypeIPv6:
		if _, err := io.ReadFull(r, buf[:16]); err != nil {
			return "", 0, nil, err
		}
		ip = net.IP(buf[:16])
		host = fmt.Sprintf("[%s]", ip.String())
	default:
		return "", 0, nil, fmt.Errorf("unknown address type: %d", addrType)
	}

	if _, err := io.ReadFull(r, buf[:2]); err != nil {
		return "", 0, nil, err
	}
	port := binary.BigEndian.Uint16(buf[:2])
	return fmt.Sprintf("%s:%d", host, port), addrType, ip, nil
}
