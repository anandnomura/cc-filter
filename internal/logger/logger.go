package logger

import (
	"log"
	"os"
	"path/filepath"
)

func Setup() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	
	logDir := filepath.Join(homeDir, ".cc-filter")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return
	}
	
	logFile := filepath.Join(logDir, "filter.log")
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	
	log.SetOutput(file)
	log.SetFlags(log.LstdFlags)
}
