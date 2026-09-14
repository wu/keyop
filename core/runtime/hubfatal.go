package runtime

import (
	"fmt"
	"sync"

	"github.com/wu/keyop/core"
)

// hubFatalShutdown stops keyop when the messenger's connection to a hub fails
// with a non-retryable error after startup — the hub rejected this instance, or
// a certificate failed verification. It logs the failure, cancels the root
// context so every service shuts down, and keeps the error so the run command
// can return it and the process exits non-zero.
//
// A hub that is merely unreachable never reaches it: the messenger keeps
// reconnecting in the background.
type hubFatalShutdown struct {
	logger core.Logger
	cancel func()

	once  sync.Once
	mu    sync.Mutex
	fatal error
}

func newHubFatalShutdown(logger core.Logger, cancel func()) *hubFatalShutdown {
	return &hubFatalShutdown{logger: logger, cancel: cancel}
}

// handle is the messenger's HubFatalHandler. Only the first failure is kept;
// with several hubs configured, later ones arrive during shutdown.
func (h *hubFatalShutdown) handle(hubAddr string, err error) {
	h.once.Do(func() {
		h.mu.Lock()
		h.fatal = fmt.Errorf("messenger: connection to hub %q failed permanently: %w", hubAddr, err)
		h.mu.Unlock()
		h.logger.Error("messenger: hub connection failed permanently, shutting down", "hub", hubAddr, "error", err)
		h.cancel()
	})
}

// err returns the recorded failure, or nil if none occurred.
func (h *hubFatalShutdown) err() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.fatal
}
