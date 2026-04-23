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
type ChannelInfo struct {
	Name        string
	Remote      string // optional: channel name to use on the remote server; defaults to Name
	Description string
	MaxAge      time.Duration
}

// AsType returns the error as a specific type, or false if it is not that type.
func AsType[T any](err any) (T, bool) {
	val, ok := err.(T)
	return val, ok
}
