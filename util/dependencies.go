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
	logger := slog.New(slogcolor.NewHandler(os.Stderr, slogcolor.DefaultOptions))
	deps.SetLogger(logger)

	ctx, cancel := context.WithCancel(context.Background())
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	deps.SetOsProvider(core.OsProvider{})

	deps.SetMessenger(core.NewMessenger(logger, deps.MustGetOsProvider()))

	return deps
}
