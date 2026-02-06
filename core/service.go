package core

import (
	"time"
)

type Service interface {
	Check() error
	ValidateConfig() []error
	Initialize() error
}

type StateStore interface {
	Save(key string, value interface{}) error
	Load(key string, value interface{}) error
}

type ServiceConfig struct {
	Name   string
	Freq   time.Duration
	Type   string
	Pubs   map[string]ChannelInfo
	Subs   map[string]ChannelInfo
	Config map[string]interface{}
}

type ChannelInfo struct {
	Name        string
	Description string
}
