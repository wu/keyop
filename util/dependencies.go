package util

import (
	"context"
	"keyop/core"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/MatusOllah/slogcolor"
)

func InitializeDependencies(console bool) core.Dependencies {

	// Set timezone to Pacific
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		// Fallback to UTC if Pacific timezone cannot be loaded
		location = time.UTC
	}
	time.Local = location

	deps := core.Dependencies{}

	deps.SetOsProvider(core.OsProvider{})

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
	if console {
		logger = slog.New(slogcolor.NewHandler(os.Stdout, slogOptions))
	} else {
		logDir := filepath.Join(home, ".keyop", "logs")
		if err := os.MkdirAll(logDir, 0755); err != nil {
			// Fallback to stderr if we can't create the log directory
			logger = slog.New(slogcolor.NewHandler(os.Stderr, slogOptions))
			logger.Error("Failed to create log directory", "path", logDir, "error", err)
		} else {
			logFileName := "keyop." + time.Now().Format("20060102") + ".log"
			logFilePath := filepath.Join(logDir, logFileName)
			f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				// Fallback to stderr if we can't open the log file
				logger = slog.New(slogcolor.NewHandler(os.Stderr, slogOptions))
				logger.Error("Failed to open log file", "path", logFilePath, "error", err)
			} else {
				logger = slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{
					Level: slogOptions.Level,
				}))
			}
		}
	}
	deps.SetLogger(logger)

	if homeErr != nil {
		logger.Warn("Failed to get user home directory, using current directory as fallback", "error", homeErr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	dataDir := filepath.Join(home, ".keyop", "data")
	deps.SetStateStore(core.NewFileStateStore(dataDir, deps.MustGetOsProvider()))

	deps.SetMessenger(core.NewMessenger(logger, deps.MustGetOsProvider()))

	return deps
}
