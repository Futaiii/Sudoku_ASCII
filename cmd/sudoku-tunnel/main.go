// cmd/sudoku-tunnel/main.go
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/Futaiii/Sudoku_ASCII/internal/app"
	"github.com/Futaiii/Sudoku_ASCII/internal/config"
	"github.com/Futaiii/Sudoku_ASCII/pkg/obfs/sudoku"
)

var (
	configPath = flag.String("c", "config.json", "Path to configuration file")
	testConfig = flag.Bool("test", false, "Test configuration file and exit")
)

func main() {
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config from %s: %v", *configPath, err)
	}

	if *testConfig {
		fmt.Printf("Configuration %s is valid.\n", *configPath)
		fmt.Printf("Mode: %s\n", cfg.Mode)
		if cfg.Mode == "client" {
			fmt.Printf("Rules: %d URLs configured\n", len(cfg.RuleURLs))
		}
		os.Exit(0)
	}

	// Pass the ASCII mode preference
	table := sudoku.NewTable(cfg.Key, cfg.ASCII)

	if cfg.Mode == "client" {
		app.RunClient(cfg, table)
	} else {
		app.RunServer(cfg, table)
	}
}
