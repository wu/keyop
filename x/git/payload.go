// Package git registers a typed payload for content-change events so consumers
// (like the git service) can decode them from Message.Data using DataType.
package git

import (
	"fmt"
	"keyop/core"
)

// ContentChangeEvent represents a note/content change event payload. Services
// emitting content changes should set Message.DataType to "notes.content_change.v1"
// and supply Data matching this structure (old/new/name/updated_at).
type ContentChangeEvent struct {
	Name      string `json:"name"`
	Old       string `json:"old,omitempty"`
	New       string `json:"new,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// PayloadType returns the payload type name for ContentChangeEvent.
func (c ContentChangeEvent) PayloadType() string { return "notes.content_change.v1" }

func init() {
	if err := core.RegisterPayload("notes.content_change.v1", func() any { return &ContentChangeEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			fmt.Printf("git: failed to register notes.content_change.v1 payload: %v\n", err)
		}
	}
}
