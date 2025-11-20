package app

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/Futaiii/Sudoku_ASCII/internal/config"
	"github.com/Futaiii/Sudoku_ASCII/pkg/crypto"
	"github.com/Futaiii/Sudoku_ASCII/pkg/obfs/sudoku"
)

func RunClient(cfg *config.Config, table *sudoku.Table) {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.LocalPort))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Client on :%d -> %s", cfg.LocalPort, cfg.ServerAddress)

	for {
		c, err := l.Accept()
		if err != nil {
			continue
		}
		go func(local net.Conn) {
			defer local.Close()
			remoteRaw, err := net.Dial("tcp", cfg.ServerAddress)
			if err != nil {
				return
			}
			defer remoteRaw.Close()

			sConn := sudoku.NewConn(remoteRaw, table, cfg.PaddingMin, cfg.PaddingMax, false)
			cConn, err := crypto.NewAEADConn(sConn, cfg.Key, cfg.AEAD)
			if err != nil {
				return
			}

			handshake := make([]byte, 16)
			binary.BigEndian.PutUint64(handshake[:8], uint64(time.Now().Unix()))
			rand.Read(handshake[8:])
			if _, err := cConn.Write(handshake); err != nil {
				return
			}

			go io.Copy(cConn, local)
			io.Copy(local, cConn)
		}(c)
	}
}
