package util

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRotatingFileWriter_Creation(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	rfw, err := NewRotatingFileWriter(logDir)
	if err != nil {
		t.Fatalf("NewRotatingFileWriter failed: %v", err)
	}
	defer func() { _ = rfw.Close() }()

	if rfw.logDir != logDir {
		t.Errorf("logDir mismatch: got %s, want %s", rfw.logDir, logDir)
	}

	expectedDate := time.Now().Format("20060102")
	if rfw.lastDate != expectedDate {
		t.Errorf("lastDate mismatch: got %s, want %s", rfw.lastDate, expectedDate)
	}
}

func TestRotatingFileWriter_Write(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	rfw, err := NewRotatingFileWriter(logDir)
	if err != nil {
		t.Fatalf("NewRotatingFileWriter failed: %v", err)
	}
	defer func() { _ = rfw.Close() }()

	testData := []byte("test log message\n")
	n, err := rfw.Write(testData)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if n != len(testData) {
		t.Errorf("Write returned %d bytes, want %d", n, len(testData))
	}

	// Verify the log file was created and contains the data
	logFileName := "keyop." + time.Now().Format("20060102") + ".log"
	logFilePath := filepath.Join(logDir, logFileName)
	content, err := os.ReadFile(logFilePath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if string(content) != string(testData) {
		t.Errorf("Log file content mismatch: got %q, want %q", string(content), string(testData))
	}
}

func TestRotatingFileWriter_Close(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	rfw, err := NewRotatingFileWriter(logDir)
	if err != nil {
		t.Fatalf("NewRotatingFileWriter failed: %v", err)
	}

	if err := rfw.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestRotatingFileWriter_MultipleWrites(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	rfw, err := NewRotatingFileWriter(logDir)
	if err != nil {
		t.Fatalf("NewRotatingFileWriter failed: %v", err)
	}
	defer func() { _ = rfw.Close() }()

	messages := []string{
		"first message\n",
		"second message\n",
		"third message\n",
	}

	for _, msg := range messages {
		_, err := rfw.Write([]byte(msg))
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}

	logFileName := "keyop." + time.Now().Format("20060102") + ".log"
	logFilePath := filepath.Join(logDir, logFileName)
	content, err := os.ReadFile(logFilePath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	expected := "first message\nsecond message\nthird message\n"
	if string(content) != expected {
		t.Errorf("Log file content mismatch: got %q, want %q", string(content), expected)
	}
}
