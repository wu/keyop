//nolint:revive
package util

import (
	"context"
	"fmt"
	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/adapter"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/MatusOllah/slogcolor"
)

// InitializeDependencies sets up core dependencies including logger, OS provider, and state/messenger.
func InitializeDependencies(console bool) core.Dependencies {

	// Set timezone to Pacific
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		// Fallback to UTC if Pacific timezone cannot be loaded
		location = time.UTC
	}
	time.Local = location

	// 1. Initial creation of core dependencies
	deps := core.Dependencies{}

	deps.SetOsProvider(adapter.OsProvider{})

	var homeErr error
	home, homeErr := deps.MustGetOsProvider().UserHomeDir()
	if homeErr != nil {
		home = "."
	}

	slogOptions := slogcolor.DefaultOptions
	slogOptions.SrcFileMode = slogcolor.Nop

	if os.Getenv("KEYOP_LOG_DEBUG") == "" {
		slogOptions.Level = slog.LevelInfo
	} else {
		slogOptions.Level = slog.LevelDebug
	}

	var logger *slog.Logger
	var logWriter *RotatingFileWriter
	if console {
		logger = slog.New(slogcolor.NewHandler(os.Stdout, slogOptions))
	} else {
		logDir := filepath.Join(home, ".keyop", "logs")
		rfw, err := NewRotatingFileWriter(logDir)
		if err != nil {
			// Fallback to stderr if we can't create rotating file writer
			logger = slog.New(slogcolor.NewHandler(os.Stderr, slogOptions))
			logger.Error("Failed to create rotating log file writer", "error", err)
		} else {
			logger = slog.New(slog.NewTextHandler(rfw, &slog.HandlerOptions{
				Level: slogOptions.Level,
			}))
			logWriter = rfw
		}
	}
	deps.SetLogger(logger)

	if homeErr != nil {
		logger.Warn("Failed to get user home directory, using current directory as fallback", "error", homeErr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	deps.SetContext(ctx)
	// Wrap cancel to close the log writer on shutdown.
	deps.SetCancel(func() {
		cancel()
		if logWriter != nil {
			if err := logWriter.Close(); err != nil {
				// The log file is closing, so write directly to stderr.
				fmt.Fprintf(os.Stderr, "ERROR: failed to close log file: %v\n", err)
			}
		}
	})

	// 2. Setup storage and messaging
	dataDir := filepath.Join(home, ".keyop", "data")
	deps.SetStateStore(adapter.NewFileStateStore(dataDir, deps.MustGetOsProvider()))

	return deps
}
