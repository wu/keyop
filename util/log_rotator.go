package util

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RotatingFileWriter writes logs to a file and rotates daily at midnight.
type RotatingFileWriter struct {
	logDir   string
	prefix   string
	file     *os.File
	lastDate string
	mu       sync.Mutex
}

// NewRotatingFileWriter creates a new rotating file writer for the given log
// directory, writing to keyop.YYYYMMDD.log.
func NewRotatingFileWriter(logDir string) (*RotatingFileWriter, error) {
	return NewRotatingFileWriterWithPrefix(logDir, "keyop")
}

// NewRotatingFileWriterWithPrefix creates a rotating file writer that names its
// files prefix.YYYYMMDD.log, so a caller can keep a distinct stream of log
// lines in its own file within the shared log directory.
func NewRotatingFileWriterWithPrefix(logDir, prefix string) (*RotatingFileWriter, error) {
	if err := os.MkdirAll(logDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	rfw := &RotatingFileWriter{
		logDir:   logDir,
		prefix:   prefix,
		lastDate: time.Now().Format("20060102"),
	}

	if err := rfw.openFile(); err != nil {
		return nil, err
	}

	return rfw, nil
}

func (rfw *RotatingFileWriter) openFile() error {
	logFileName := rfw.prefix + "." + rfw.lastDate + ".log"
	logFilePath := filepath.Join(rfw.logDir, logFileName)
	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600) //nolint:gosec
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	rfw.file = f
	return nil
}

// Write writes to the log file, rotating if necessary.
func (rfw *RotatingFileWriter) Write(p []byte) (int, error) {
	rfw.mu.Lock()
	defer rfw.mu.Unlock()

	// Check if date has changed and rotate if needed
	currentDate := time.Now().Format("20060102")
	if currentDate != rfw.lastDate {
		if rfw.file != nil {
			_ = rfw.file.Close()
		}
		rfw.lastDate = currentDate
		if err := rfw.openFile(); err != nil {
			// Fall back to stderr on rotation failure
			fmt.Fprintf(os.Stderr, "ERROR: failed to rotate log file: %v\n", err)
			return len(p), nil
		}
	}

	return rfw.file.Write(p)
}

// Close closes the log file.
func (rfw *RotatingFileWriter) Close() error {
	rfw.mu.Lock()
	defer rfw.mu.Unlock()

	if rfw.file != nil {
		return rfw.file.Close()
	}
	return nil
}
