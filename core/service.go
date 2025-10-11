package core

import (
	"time"
)

type Service interface {
	Check() error
}

type ServiceConfig struct {
	Name string
	Freq time.Duration
	Type string
	Pubs map[string]ChannelInfo
}

type ChannelInfo struct {
	Name        string
	Description string
}
