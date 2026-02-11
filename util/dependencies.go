package util

import (
	"context"
	"keyop/core"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/MatusOllah/slogcolor"
)

func InitializeDependencies() core.Dependencies {

	deps := core.Dependencies{}

	//logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slogOptions := slogcolor.DefaultOptions
	slogOptions.SrcFileMode = slogcolor.Nop

	if os.Getenv("KEYOP_LOG_DEBUG") == "" {
		slogOptions.Level = slog.LevelInfo
	} else {
		slogOptions.Level = slog.LevelDebug
	}

	logger := slog.New(slogcolor.NewHandler(os.Stderr, slogOptions))
	deps.SetLogger(logger)

	ctx, cancel := context.WithCancel(context.Background())
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	deps.SetOsProvider(core.OsProvider{})

	home, err := deps.MustGetOsProvider().UserHomeDir()
	if err != nil {
		logger.Error("Failed to get user home directory, using current directory as fallback", "error", err)
		home = "."
	}
	dataDir := filepath.Join(home, ".keyop", "data")
	deps.SetStateStore(core.NewFileStateStore(dataDir, deps.MustGetOsProvider()))

	deps.SetMessenger(core.NewMessenger(logger, deps.MustGetOsProvider()))

	return deps
}
