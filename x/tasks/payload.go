package tasks

import "keyop/core"

// Compile-time assertion that Service implements PayloadProvider.
var _ core.PayloadProvider = (*Service)(nil)

// TaskCreateEvent is published when a new task should be created from an external source (e.g. reminders).
type TaskCreateEvent struct {
	Title            string `json:"title"`
	Note             string `json:"note,omitempty"`
	DueAt            string `json:"due_at,omitempty"` // RFC3339 timestamp
	HasScheduledTime bool   `json:"has_scheduled_time,omitempty"`
	Tags             string `json:"tags,omitempty"`
	UserID           int64  `json:"user_id,omitempty"`
	Source           string `json:"source,omitempty"`      // originating service, e.g. "reminders"
	ExternalID       string `json:"external_id,omitempty"` // ID in the source system
	Color            string `json:"color,omitempty"`
	Importance       int    `json:"importance,omitempty"`
}

// PayloadType satisfies core.TypedPayload.
func (e TaskCreateEvent) PayloadType() string { return "service.tasks.create.v1" }

// Name satisfies core.PayloadProvider.
func (svc *Service) Name() string { return "tasks" }

// RegisterPayloads registers the typed task payloads with the core registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	types := map[string]func() any{
		"service.tasks.create.v1": func() any { return &TaskCreateEvent{} },
	}
	for name, factory := range types {
		if err := reg.Register(name, factory); err != nil {
			if !core.IsDuplicatePayloadRegistration(err) {
				return err
			}
		}
	}
	return nil
}
