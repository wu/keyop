package rss

import (
	"fmt"
	"time"

	"keyop/core"
)

// ArticleEvent represents a new article found in an RSS/Atom feed.
type ArticleEvent struct {
	GUID        string    `json:"guid"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Link        string    `json:"link"`
	Published   time.Time `json:"published"`
	FeedTitle   string    `json:"feedTitle"`
	FeedURL     string    `json:"feedURL"`
}

// PayloadType returns the canonical payload type for RSS article events.
func (a ArticleEvent) PayloadType() string { return "service.rss.article.v1" }

// Compile-time interface assertion.
var _ core.PayloadProvider = (*Service)(nil)

// Name returns the canonical service name.
func (svc *Service) Name() string { return "rss" }

// RegisterPayloads registers the RSS article payload type with the provided registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	if err := reg.Register("service.rss.article.v1", func() any { return &ArticleEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("rss: failed to register service.rss.article.v1: %w", err)
		}
	}
	return nil
}
