//nolint:revive
package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// EnvelopeVersion defines the version of the message envelope.
type EnvelopeVersion string

const (
	// EnvelopeV1 is the first version of the message envelope.
	EnvelopeV1 EnvelopeVersion = "v1"
)

// Envelope wraps a domain payload with transport-level metadata.
type Envelope struct {
	Version       EnvelopeVersion   `json:"v"`
	ID            string            `json:"id"`
	CorrelationID string            `json:"correlationId,omitempty"`
	Timestamp     time.Time         `json:"timestamp"`
	Source        string            `json:"source"`
	Topic         string            `json:"topic"`
	RetryCount    int               `json:"retryCount,omitempty"`
	Trace         []string          `json:"trace,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Payload       any               `json:"payload"`
}

// NewEnvelope creates a new v1 envelope with a unique ID and current timestamp.
func NewEnvelope(topic, source string, payload any) Envelope {
	return Envelope{
		Version:   EnvelopeV1,
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Topic:     topic,
		Source:    source,
		Payload:   payload,
	}
}

// ToMessage converts the envelope back to the legacy Message struct for backward compatibility.
// If the payload is already a Message, it is returned with metadata updated.
// Otherwise, it attempts to unmarshal the payload into a Message or populates common fields.
func (e Envelope) ToMessage() Message {
	var m Message

	// Try to extract legacy fields from payload if it's a map or Message
	if e.Payload != nil {
		switch p := e.Payload.(type) {
		case Message:
			m = p
		case *Message:
			if p != nil {
				m = *p
			}
		case map[string]any:
			// Manual mapping for common fields if payload is map[string]any (from JSON unmarshal)
			if val, ok := p["timestamp"].(string); ok {
				if t, err := time.Parse(time.RFC3339, val); err == nil {
					m.Timestamp = t
				}
			}
			if val, ok := p["uuid"].(string); ok {
				m.Uuid = val
			}
			if val, ok := p["correlation"].(string); ok {
				m.Correlation = val
			}
			if val, ok := p["hostname"].(string); ok {
				m.Hostname = val
			}
			if val, ok := p["channelName"].(string); ok {
				m.ChannelName = val
			}
			if val, ok := p["serviceType"].(string); ok {
				m.ServiceType = val
			}
			if val, ok := p["serviceName"].(string); ok {
				m.ServiceName = val
			}
			if val, ok := p["event"].(string); ok {
				m.Event = val
			}
			if val, ok := p["status"].(string); ok {
				m.Status = val
			}
			if val, ok := p["text"].(string); ok {
				m.Text = val
			}
			if val, ok := p["summary"].(string); ok {
				m.Summary = val
			}
			if val, ok := p["metric"].(float64); ok {
				m.Metric = val
			}
			if val, ok := p["metricName"].(string); ok {
				m.MetricName = val
			}
			if val, ok := p["state"].(string); ok {
				m.State = val
			}
			if val, ok := p["data"]; ok {
				m.Data = val
			}
			if val, ok := p["route"].([]any); ok {
				for _, r := range val {
					if rs, ok := r.(string); ok {
						m.Route = append(m.Route, rs)
					}
				}
			}
			if val, ok := p["log"].([]any); ok {
				for _, l := range val {
					if ls, ok := l.(string); ok {
						m.Log = append(m.Log, ls)
					}
				}
			}
		default:
			// If it's some other type, we might not be able to map it directly to Message fields,
			// but we can put it in Message.Data.
			m.Data = e.Payload
		}
	}

	// Override with envelope metadata if envelope fields are non-zero
	if e.ID != "" {
		m.Uuid = e.ID
	}
	if e.CorrelationID != "" {
		m.Correlation = e.CorrelationID
	}
	if !e.Timestamp.IsZero() {
		m.Timestamp = e.Timestamp
	}
	if e.Source != "" {
		m.Hostname = e.Source
	}
	if e.Topic != "" {
		m.ChannelName = e.Topic
	}
	if len(e.Trace) > 0 {
		m.Route = e.Trace
	}

	return m
}

// NewEnvelopeFromMessage wraps a legacy Message into a versioned Envelope.
func NewEnvelopeFromMessage(m Message) Envelope {
	return Envelope{
		Version:       EnvelopeV1,
		ID:            m.Uuid,
		CorrelationID: m.Correlation,
		Timestamp:     m.Timestamp,
		Source:        m.Hostname,
		Topic:         m.ChannelName,
		Trace:         m.Route,
		Payload:       m,
	}
}

// UnmarshalEnvelope parses JSON into an Envelope, handling the dynamic Payload.
func UnmarshalEnvelope(data []byte) (Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return e, err
	}
	return e, nil
}

// Typed Payload Support

// ErrPayloadTypeAlreadyRegistered is returned when a payload type is already registered.
var ErrPayloadTypeAlreadyRegistered = fmt.Errorf("payload type already registered")

// IsDuplicatePayloadRegistration returns true if the error is due to a duplicate payload registration.
func IsDuplicatePayloadRegistration(err error) bool {
	return errors.Is(err, ErrPayloadTypeAlreadyRegistered)
}

// PayloadFactory creates a new instance of a payload type.
type PayloadFactory func() any

// PayloadRegistry manages registration and decoding of typed payloads.
type PayloadRegistry interface {
	Register(typeName string, f PayloadFactory) error
	Decode(typeName string, payload any) (any, error)
	KnownTypes() []string
}

// defaultRegistry implements PayloadRegistry with thread safety.
type defaultRegistry struct {
	mu        sync.RWMutex
	factories map[string]PayloadFactory
	warned    map[string]bool
	logger    Logger
}

func newDefaultRegistry(logger Logger) *defaultRegistry {
	return &defaultRegistry{
		factories: make(map[string]PayloadFactory),
		warned:    make(map[string]bool),
		logger:    logger,
	}
}

func (r *defaultRegistry) Register(typeName string, f PayloadFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.factories[typeName]; ok {
		return fmt.Errorf("%w: %s", ErrPayloadTypeAlreadyRegistered, typeName)
	}
	r.factories[typeName] = f
	return nil
}

func (r *defaultRegistry) Decode(typeName string, payload any) (any, error) {
	if payload == nil {
		return nil, nil
	}

	r.mu.RLock()
	f, ok := r.factories[typeName]
	r.mu.RUnlock()

	if !ok {
		r.mu.Lock()
		if !r.warned[typeName] {
			if r.logger != nil {
				r.logger.Warn("Unknown payload type, falling back to raw payload", "type", typeName)
			}
			r.warned[typeName] = true
		}
		r.mu.Unlock()
		return payload, nil
	}

	// Re-marshal and unmarshal into the specific type
	bytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	typed := f()
	if err := json.Unmarshal(bytes, typed); err != nil {
		return nil, err
	}

	return typed, nil
}

func (r *defaultRegistry) KnownTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.factories))
	for t := range r.factories {
		types = append(types, t)
	}
	return types
}

// globalPayloadRegistry stores constructors for typed payloads.
var (
	globalPayloadRegistry   PayloadRegistry
	globalPayloadRegistryMu sync.RWMutex
)

// NewPayloadRegistry creates a new instance of the default payload registry.
func NewPayloadRegistry(logger Logger) PayloadRegistry {
	return newDefaultRegistry(logger)
}

func init() {
	reg := newDefaultRegistry(nil) // logger will be set later if needed, or we use a global one
	if err := reg.Register("core.device.status.v1", func() any { return &DeviceStatusEvent{} }); err != nil {
		panic(fmt.Sprintf("failed to register core.device.status.v1: %v", err))
	}
	if err := reg.Register("core.metric.v1", func() any { return &MetricEvent{} }); err != nil {
		panic(fmt.Sprintf("failed to register core.metric.v1: %v", err))
	}

	// Compatibility aliases
	if err := reg.Register("device.status", func() any { return &DeviceStatusEvent{} }); err != nil {
		panic(fmt.Sprintf("failed to register device.status: %v", err))
	}
	if err := reg.Register("metric", func() any { return &MetricEvent{} }); err != nil {
		panic(fmt.Sprintf("failed to register metric: %v", err))
	}

	globalPayloadRegistry = reg
}

// GetPayloadRegistry returns the global payload registry.
func GetPayloadRegistry() PayloadRegistry {
	globalPayloadRegistryMu.RLock()
	defer globalPayloadRegistryMu.RUnlock()
	return globalPayloadRegistry
}

// SetPayloadRegistry sets the global payload registry.
func SetPayloadRegistry(r PayloadRegistry) {
	globalPayloadRegistryMu.Lock()
	defer globalPayloadRegistryMu.Unlock()
	globalPayloadRegistry = r
}

// RegisterPayload registers a constructor for a specific payload type in the global registry.
func RegisterPayload(typeName string, constructor func() any) error {
	reg := GetPayloadRegistry()
	if reg != nil {
		return reg.Register(typeName, constructor)
	}
	return fmt.Errorf("global payload registry not initialized")
}

// UnmarshalPayload unmarshals the envelope's payload into a specific type if registered.
// The type is determined by the "payload-type" header in the envelope.
func (e Envelope) UnmarshalPayload() (any, error) {
	if e.Headers == nil {
		return e.Payload, nil
	}
	payloadType := e.Headers["payload-type"]
	reg := GetPayloadRegistry()
	if reg == nil {
		return e.Payload, nil
	}
	return reg.Decode(payloadType, e.Payload)
}

// TypedPayload is a marker interface for typed payloads.
type TypedPayload interface {
	PayloadType() string
}

// DeviceStatusEvent represents a common event for device status updates.
type DeviceStatusEvent struct {
	DeviceID string `json:"deviceId"`
	Status   string `json:"status"`
	Battery  int    `json:"battery,omitempty"`
}

func (d DeviceStatusEvent) PayloadType() string { return "core.device.status.v1" }

// MetricEvent represents a common event for metric data.
type MetricEvent struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit,omitempty"`
}

func (m MetricEvent) PayloadType() string { return "core.metric.v1" }
