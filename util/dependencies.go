package util

import (
	"context"
	"keyop/core"
	"log/slog"
	"os"
)

func InitializeDependencies() core.Dependencies {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	//logger := slog.New(slogcolor.NewHandler(os.Stderr, slogcolor.DefaultOptions))

	deps := core.Dependencies{}
	deps.SetOsProvider(core.OsProvider{})
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetMessenger(core.NewMessenger(logger, deps.MustGetOsProvider()))

	return deps
}
