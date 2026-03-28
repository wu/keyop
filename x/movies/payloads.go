package movies

import (
	"fmt"
	"keyop/core"
	"time"
)

// MovieWatchedEvent is published when a movie has been watched past the
// configured threshold. The movies service listens for this event on the
// configured 'movie' channel and updates last_played accordingly.
type MovieWatchedEvent struct {
	Title     string    `json:"title"`
	WatchedAt time.Time `json:"watchedAt"`
}

// PayloadType returns the canonical type identifier for MovieWatchedEvent.
func (e MovieWatchedEvent) PayloadType() string { return "movie.watched.v1" }

// Name returns the service name for the PayloadProvider interface.
func (svc *Service) Name() string { return svc.Cfg.Name }

// RegisterPayloads registers the movies payload types with the provided registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	if err := reg.Register("movie.watched.v1", func() any { return &MovieWatchedEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("movies: failed to register movie.watched.v1: %w", err)
		}
	}
	return nil
}
