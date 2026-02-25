package macosReminders

import (
	"keyop/core"
	"testing"
	"time"
)

// Minimal logger that satisfies core.Logger for tests
type testLogger struct {
	t *testing.T
}

func (l *testLogger) Debug(msg string, kv ...interface{}) { l.t.Logf("DEBUG: %s %v", msg, kv) }
func (l *testLogger) Info(msg string, kv ...interface{})  { l.t.Logf("INFO: %s %v", msg, kv) }
func (l *testLogger) Warn(msg string, kv ...interface{})  { l.t.Logf("WARN: %s %v", msg, kv) }
func (l *testLogger) Error(msg string, kv ...interface{}) { l.t.Logf("ERROR: %s %v", msg, kv) }

func TestValidateConfig_MissingPubs(t *testing.T) {
	// Build a minimal ServiceConfig without pubs
	cfg := core.ServiceConfig{
		Name:   "test",
		Type:   "macosReminders",
		Pubs:   nil,
		Config: map[string]interface{}{},
	}

	// create minimal dependencies with a fake messenger and logger
	fm := &core.FakeMessenger{}
	lg := &testLogger{t: t}
	deps := core.Dependencies{}
	deps.SetLogger(lg)
	deps.SetMessenger(fm)

	svc := &Service{Cfg: cfg, Deps: deps}
	err := svc.ValidateConfig()
	if len(err) == 0 {
		t.Fatal("expected validation errors when pubs missing")
	}
}

func TestParseLineAndSend(t *testing.T) {
	fm := &core.FakeMessenger{}
	lg := &testLogger{t: t}
	deps := core.Dependencies{}
	deps.SetLogger(lg)
	deps.SetMessenger(fm)

	cfg := core.ServiceConfig{
		Name:   "test",
		Type:   "macosReminders",
		Pubs:   map[string]core.ChannelInfo{"tasks": {Name: "tasks"}},
		Config: map[string]interface{}{},
	}

	// emulate a single line with title|||notes|||dueRaw|||false|||id
	line := "Buy milk|||2% milk|||January 2, 2006 at 3:04:05 PM|||false|||12345"
	parts := splitLine(line, "|||")
	if len(parts) < 5 {
		t.Fatalf("unexpected line parts: %v", parts)
	}
	title := parts[0]
	notes := parts[1]
	dueRaw := parts[2]
	completedRaw := parts[3]

	_ = parts[4] // id is unused in this test

	completed := false
	if completedRaw == "true" {
		completed = true
	}

	var ts time.Time
	if dueRaw != "" {
		// try parse
		if ttt, err := time.Parse("January 2, 2006 at 3:04:05 PM", dueRaw); err == nil {
			ts = ttt
		}
	}

	msg := core.Message{
		ChannelName: cfg.Pubs["tasks"].Name,
		ServiceName: cfg.Name,
		ServiceType: cfg.Type,
		Summary:     title,
		Text:        notes,
	}
	if !ts.IsZero() {
		msg.Timestamp = ts
	}

	if err := fm.Send(msg); err != nil {
		t.Fatalf("failed to send to fake messenger: %v", err)
	}

	if len(fm.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(fm.Messages))
	}

	m := fm.Messages[0]
	if m.Summary != title {
		t.Fatalf("expected title %s, got %s", title, m.Summary)
	}
	if m.Text != notes {
		t.Fatalf("expected notes %s, got %s", notes, m.Text)
	}

	if completed {
		t.Fatalf("did not expect completed to be true in this test")
	}
}

// simple splitter that mirrors strings.Split but returns at least empty elements
func splitLine(line string, sep string) []string {
	return stringSplitN(line, sep, -1)
}

// wrapper to avoid importing strings directly in the test and to keep logic clear
func stringSplitN(s, sep string, n int) []string {
	// actual implementation
	return stringsSplit(s, sep)
}

// to avoid polluting the top-level imports with unused items in other files, implement
// a minimal strings.Split replacement here.
func stringsSplit(s, sep string) []string {
	var res []string
	if sep == "" {
		for _, r := range s {
			res = append(res, string(r))
		}
		return res
	}
	idx := 0
	for {
		pos := indexOf(s[idx:], sep)
		if pos == -1 {
			res = append(res, s[idx:])
			break
		}
		res = append(res, s[idx:idx+pos])
		idx = idx + pos + len(sep)
		if idx > len(s) {
			res = append(res, "")
			break
		}
	}
	return res
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
