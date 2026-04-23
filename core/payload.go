//nolint:revive
package core

import (
	"errors"

	"github.com/wu/keyop-messenger"
)

// ErrPayloadTypeAlreadyRegistered is returned when a payload type is already registered.
var ErrPayloadTypeAlreadyRegistered = errors.New("payload type already registered")

// IsDuplicatePayloadRegistration returns true if the error is due to a duplicate payload registration.
func IsDuplicatePayloadRegistration(err error) bool {
	if errors.Is(err, ErrPayloadTypeAlreadyRegistered) {
		return true
	}
	// Also check the ephemeral messenger's error type
	if errors.Is(err, messenger.ErrPayloadTypeAlreadyRegistered) {
		return true
	}
	return false
}
