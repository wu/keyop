// Package core implements the core service for keyop and provides ValidateConfig, Initialize and Check hooks.
package core

// PayloadProvider defines the interface for external plugins that can register
// their own payload types at runtime.
type PayloadProvider interface {
	Name() string
	RegisterPayloads(reg PayloadRegistry) error
}
