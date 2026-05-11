package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

func setupLogger(path string) {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		log.Printf("Warning: Cannot create log dir for %s: %v", path, err)
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Warning: Cannot open log file %s: %v", path, err)
		return
	}
	log.SetOutput(io.MultiWriter(os.Stdout, f))
}