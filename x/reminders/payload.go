package reminders

import "keyop/core"

// Compile-time interface assertion.
var _ core.PayloadProvider = (*Service)(nil)

// ReminderCreatedEvent is published when a new reminder is detected.
type ReminderCreatedEvent struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Note   string `json:"note,omitempty"`
	DueRaw string `json:"due_raw,omitempty"`
	Inbox  string `json:"inbox,omitempty"`
	UserID int64  `json:"user_id,omitempty"`
	Task   *Task  `json:"task,omitempty"`
}

// PayloadType satisfies core.TypedPayload.
func (e ReminderCreatedEvent) PayloadType() string { return "service.reminders.created.v1" }

// ReminderRemovedEvent is published when a previously seen reminder disappears.
type ReminderRemovedEvent struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// PayloadType satisfies core.TypedPayload.
func (e ReminderRemovedEvent) PayloadType() string { return "service.reminders.removed.v1" }

// Name satisfies core.PayloadProvider.
func (svc *Service) Name() string { return "reminders" }

// RegisterPayloads registers the typed reminder payloads with the core registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	types := map[string]func() any{
		"service.reminders.created.v1": func() any { return &ReminderCreatedEvent{} },
		"service.reminders.removed.v1": func() any { return &ReminderRemovedEvent{} },
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
