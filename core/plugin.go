package core

// RuntimePlugin defines the interface for external plugins that can register
// their own payload types at runtime.
type RuntimePlugin interface {
	Name() string
	RegisterPayloads(reg PayloadRegistry) error
}
