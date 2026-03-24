//go:build !linux

package switchgpio

import (
	"fmt"
	"keyop/core"
)

// Service is a stub for non-Linux platforms where RPIO is unavailable.
type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig
}

// NewService returns a stub service on non-Linux platforms.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{Deps: deps, Cfg: cfg}
}

// ValidateConfig always returns an error on non-Linux platforms.
func (svc *Service) ValidateConfig() []error {
	return []error{fmt.Errorf("switch: RPIO is only supported on Linux")}
}

// Initialize returns an error on non-Linux platforms.
func (svc *Service) Initialize() error {
	return fmt.Errorf("switch: RPIO is only supported on Linux")
}

// Check is a no-op stub.
func (svc *Service) Check() error {
	return nil
}
