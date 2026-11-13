package main

import (
	"log"
	"os"
	"path/filepath"

	"easy-command/sidecar/internal/bash"
	"easy-command/sidecar/internal/history"
	"easy-command/sidecar/internal/ipc"
	"easy-command/sidecar/internal/script"
	"easy-command/sidecar/internal/store"
)

const version = "0.1.0"

func main() {
	logger := log.New(os.Stderr, "easy-command-engine: ", log.LstdFlags)
	appData := os.Getenv("APPDATA")
	if !filepath.IsAbs(appData) {
		logger.Print("APPDATA must be an absolute path")
		os.Exit(1)
	}
	dataDir := filepath.Join(appData, "easy-command")
	db, err := store.Open(dataDir)
	if err != nil {
		logger.Print(err)
		os.Exit(1)
	}
	library, err := script.Open(db)
	if err != nil {
		logger.Print(err)
		os.Exit(1)
	}
	// Only the protocol encoder owns stdout.
	records := history.New(db, os.Stderr)
	if err := ipc.ServeLibrary(os.Stdin, os.Stdout, os.Stderr, version, dataDir, library, bash.New(db), records, db); err != nil {
		logger.Print(err)
		os.Exit(1)
	}
}
