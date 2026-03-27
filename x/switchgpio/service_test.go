//go:build !linux

package switchgpio

import (
	"fmt"
	"keyop/core"
	"keyop/core/testutil"
	"strings"
	"testing"
)

func newTestDeps() core.Dependencies {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	messenger := testutil.NewFakeMessenger()
	deps.SetMessenger(messenger)
	return deps
}

func newTestCfg(name string) core.ServiceConfig {
	return core.ServiceConfig{
		Name:   name,
		Type:   "switchgpio",
		Config: map[string]interface{}{},
	}
}

// TestService_Name verifies Name() returns the configured service name.
func TestService_Name(t *testing.T) {
	svc := &Service{Cfg: core.ServiceConfig{Name: "my-switch"}}
	if got := svc.Name(); got != "my-switch" {
		t.Errorf("expected %q, got %q", "my-switch", got)
	}
}

// TestService_ValidateConfig_Stub verifies the stub returns an error on non-Linux.
func TestService_ValidateConfig_Stub(t *testing.T) {
	svc := &Service{Deps: newTestDeps(), Cfg: newTestCfg("sw")}
	errs := svc.ValidateConfig()
	if len(errs) == 0 {
		t.Fatal("expected validation errors, got none")
	}
	msg := errs[0].Error()
	if !strings.Contains(msg, "Linux") && !strings.Contains(msg, "RPIO") {
		t.Errorf("expected error mentioning Linux or RPIO, got: %q", msg)
	}
}

// TestService_Initialize_Stub verifies Initialize returns an error on non-Linux.
func TestService_Initialize_Stub(t *testing.T) {
	svc := &Service{Deps: newTestDeps(), Cfg: newTestCfg("sw")}
	err := svc.Initialize()
	if err == nil {
		t.Fatal("expected error from Initialize, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Linux") && !strings.Contains(msg, "RPIO") {
		t.Errorf("expected error mentioning Linux or RPIO, got: %q", msg)
	}
}

// TestService_Check_Stub verifies Check returns nil (no-op) on the stub.
func TestService_Check_Stub(t *testing.T) {
	svc := &Service{Deps: newTestDeps(), Cfg: newTestCfg("sw")}
	if err := svc.Check(); err != nil {
		t.Errorf("expected nil from Check, got %v", err)
	}
}

// TestEvent_PayloadType verifies the value type returns the correct payload type string.
func TestEvent_PayloadType(t *testing.T) {
	e := Event{DeviceName: "light", State: "ON"}
	if got := e.PayloadType(); got != "switch.event.v1" {
		t.Errorf("expected %q, got %q", "switch.event.v1", got)
	}
}

// TestEvent_PayloadType_Pointer verifies the pointer type also returns the correct payload type.
func TestEvent_PayloadType_Pointer(t *testing.T) {
	e := &Event{DeviceName: "light", State: "OFF"}
	if got := e.PayloadType(); got != "switch.event.v1" {
		t.Errorf("expected %q, got %q", "switch.event.v1", got)
	}
}

// mockPayloadRegistry records Register calls for assertion.
type mockPayloadRegistry struct {
	registered []string
	returnErr  error
}

func (m *mockPayloadRegistry) Register(typeName string, _ core.PayloadFactory) error {
	m.registered = append(m.registered, typeName)
	return m.returnErr
}

func (m *mockPayloadRegistry) Decode(_ string, _ any) (any, error) {
	return nil, nil
}

func (m *mockPayloadRegistry) KnownTypes() []string {
	return m.registered
}

// TestService_RegisterPayloads verifies that RegisterPayloads registers "switch.event.v1".
func TestService_RegisterPayloads(t *testing.T) {
	svc := &Service{Deps: newTestDeps(), Cfg: newTestCfg("sw")}
	reg := &mockPayloadRegistry{}

	if err := svc.RegisterPayloads(reg); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	found := false
	for _, name := range reg.registered {
		if name == "switch.event.v1" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected switch.event.v1 to be registered, got: %v", reg.registered)
	}
}

// TestService_RegisterPayloads_Duplicate verifies that a duplicate registration error is ignored.
func TestService_RegisterPayloads_Duplicate(t *testing.T) {
	svc := &Service{Deps: newTestDeps(), Cfg: newTestCfg("sw")}
	reg := &mockPayloadRegistry{returnErr: fmt.Errorf("duplicate: %w", core.ErrPayloadTypeAlreadyRegistered)}

	if err := svc.RegisterPayloads(reg); err != nil {
		t.Errorf("expected duplicate error to be ignored, got: %v", err)
	}
}

// TestNewService verifies NewService returns a non-nil service with the correct name.
func TestNewService(t *testing.T) {
	cfg := newTestCfg("gate-switch")
	svc := NewService(newTestDeps(), cfg)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	type namer interface{ Name() string }
	if n, ok := svc.(namer); ok {
		if got := n.Name(); got != "gate-switch" {
			t.Errorf("expected %q, got %q", "gate-switch", got)
		}
	}
}
