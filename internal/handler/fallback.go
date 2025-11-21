// internal/handler/fallback.go
package handler

import (
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/Futaiii/Sudoku_ASCII/internal/config"
	"github.com/Futaiii/Sudoku_ASCII/pkg/obfs/sudoku"
)

func HandleSuspicious(sConn *sudoku.Conn, rawConn net.Conn, cfg *config.Config) {
	remoteAddr := rawConn.RemoteAddr().String()

	if cfg.SuspiciousAction == "silent" {
		log.Printf("[Silent] Suspicious %s. Tarpit.", remoteAddr)
		io.Copy(io.Discard, rawConn)
		time.Sleep(5 * time.Second)
		rawConn.Close()
		return
	}

	if cfg.FallbackAddr == "" {
		rawConn.Close()
		return
	}

	log.Printf("[Fallback] %s -> %s", remoteAddr, cfg.FallbackAddr)
	dst, err := net.DialTimeout("tcp", cfg.FallbackAddr, 3*time.Second)
	if err != nil {
		rawConn.Close()
		return
	}

	badData := sConn.GetBufferedAndRecorded()
	if len(badData) > 0 {
		if _, err := dst.Write(badData); err != nil {
			dst.Close()
			rawConn.Close()
			return
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer dst.Close()
		io.Copy(dst, rawConn)
	}()
	go func() {
		defer wg.Done()
		defer rawConn.Close()
		io.Copy(rawConn, dst)
	}()
	wg.Wait()
}
