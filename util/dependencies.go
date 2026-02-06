package util

import (
	"context"
	"keyop/core"
	"log/slog"
	"os"

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

	deps.SetStateStore(core.NewFileStateStore("data", deps.MustGetOsProvider()))

	deps.SetMessenger(core.NewMessenger(logger, deps.MustGetOsProvider()))

	return deps
}
