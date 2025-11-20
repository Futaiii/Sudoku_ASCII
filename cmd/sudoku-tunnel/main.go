package main

import (
	"log"

	"github.com/Futaiii/Sudoku_ASCII/internal/app"
	"github.com/Futaiii/Sudoku_ASCII/internal/config"
	"github.com/Futaiii/Sudoku_ASCII/pkg/obfs/sudoku"
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	// Initialize shared tables once
	table := sudoku.NewTable(cfg.Key)

	if cfg.Mode == "client" {
		app.RunClient(cfg, table)
	} else {
		app.RunServer(cfg, table)
	}
}
