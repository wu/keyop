// Package core implements the core service for keyop and provides ValidateConfig, Initialize and Check hooks.
package core

// RegisterPayloadTypesProvider defines the interface for external plugins
// that need to register their own payload types with the new messenger.
type RegisterPayloadTypesProvider interface {
	RegisterPayloadTypes(newMsgr interface {
		RegisterPayloadType(typeStr string, prototype any) error
	}, logger Logger) error
}
