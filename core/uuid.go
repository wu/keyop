package core

import (
	"github.com/google/uuid"
)

// NewUUID generates a new UUIDv7 and returns it as a string.
// UUIDv7 is a time-based UUID that is sortable and suitable for database keys.
func NewUUID() string {
	id, err := uuid.NewV7()
	if err != nil {
		// Fallback to random UUID if V7 generation fails
		id = uuid.New()
	}
	return id.String()
}
