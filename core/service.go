// Package core implements the core service for keyop and provides ValidateConfig, Initialize and Check hooks.
package core

import (
	"time"
)

// Service defines the lifecycle methods a service must implement.
// Check performs a health check; ValidateConfig returns configuration issues; Initialize sets up resources.
type Service interface {
	Check() error
	ValidateConfig() []error
	Initialize() error
}

// StateStoreApi persists and retrieves arbitrary state by key.
type StateStoreApi interface {
	Save(key string, value interface{}) error
	Load(key string, value interface{}) error
	Delete(key string) error
}

// ServiceConfig holds configuration for a service, including channels and arbitrary config.
type ServiceConfig struct {
	Name   string
	Freq   time.Duration
	Type   string
	Pubs   map[string]ChannelInfo
	Subs   map[string]ChannelInfo
	Config map[string]interface{}
}

// ChannelInfo describes a channel's metadata used by services.
//
// The delivery-tuning fields (MaxAge, MaxRetries, RetryBackoffBase,
// RetryBackoffMax) apply to subscriptions: when a service subscribes to a
// channel configured in its Subs, the runtime translates them into
// keyop-messenger SubscribeOptions via SubscribeOptions and applies them
// automatically. They are ignored for Pubs.
type ChannelInfo struct {
	Name        string
	Remote      string // optional: channel name to use on the remote server; defaults to Name
	Description string
	// MaxAge enables startup freshness filtering (km.WithMaxAge): on first start
	// the subscriber skips buffered messages older than this. 0 disables.
	MaxAge time.Duration
	// MaxRetries overrides the messenger's subscribers.max_retries for this
	// subscription (km.WithMaxRetries). nil leaves the instance default in place.
	MaxRetries *int
	// RetryBackoffBase and RetryBackoffMax override the retry backoff schedule for
	// this subscription (km.WithRetryBackoff): the first retry waits Base, doubling
	// each attempt, capped at Max. Both 0 leaves the instance defaults in place.
	RetryBackoffBase time.Duration
	RetryBackoffMax  time.Duration
}

// AsType returns the error as a specific type, or false if it is not that type.
func AsType[T any](err any) (T, bool) {
	val, ok := err.(T)
	return val, ok
}
