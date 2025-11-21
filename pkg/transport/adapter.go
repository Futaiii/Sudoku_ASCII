// pkg/transport/adapter.go
package transport

import (
	"crypto/sha256"
	"net"

	"github.com/xtaci/kcp-go/v5"
	"golang.org/x/crypto/pbkdf2"
)

// Listen 统一监听接口
func Listen(network, addr string) (net.Listener, error) {
	if network == "kcp" {
		// KCP 不需要像 TCP 那样复杂的握手
		// 这里为了兼容性保持无底层加密，由上层 crypto 包处理
		l, err := kcp.ListenWithOptions(addr, nil, 10, 3)
		if err != nil {
			return nil, err
		}
		// KCP 性能调优: Fast mode
		l.SetReadBuffer(4 * 1024 * 1024)
		l.SetWriteBuffer(4 * 1024 * 1024)
		l.SetDSCP(46)
		return l, nil
	}
	return net.Listen("tcp", addr)
}

// Dial 统一连接接口
func Dial(network, addr string) (net.Conn, error) {
	if network == "kcp" {
		// KCP 客户端连接
		kcpConn, err := kcp.DialWithOptions(addr, nil, 10, 3)
		if err != nil {
			return nil, err
		}
		// KCP 客户端性能调优
		// NoDelay: 1, Interval: 10, Resend: 2, NC: 1
		kcpConn.SetNoDelay(1, 10, 2, 1)
		kcpConn.SetWindowSize(128, 128)
		kcpConn.SetACKNoDelay(true)
		kcpConn.SetReadBuffer(4 * 1024 * 1024)
		kcpConn.SetWriteBuffer(4 * 1024 * 1024)
		return kcpConn, nil
	}
	return net.Dial("tcp", addr)
}

// DeriveKey 用于生成 KCP block 密钥（如果未来需要底层加密）
func DeriveKey(password string, salt []byte) []byte {
	return pbkdf2.Key([]byte(password), salt, 4096, 32, sha256.New)
}
