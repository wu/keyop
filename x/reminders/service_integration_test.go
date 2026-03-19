//go:build darwin

//nolint:revive
package reminders

import (
	"os"
	"testing"
	"time"

	"keyop/core"
)

func TestIntegration_FetchAndPublish_WithSwiftHelper(t *testing.T) {
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping macOS integration test on CI")
	}

	// prefer helper packaged in output dir
	helperPath := "output/reminders_fetcher"
	if _, err := os.Stat(helperPath); os.IsNotExist(err) {
		// fallback to dev path
		helperPath = "x/macosReminders/cmd/reminders_fetcher/reminders_fetcher"
		if _, err := os.Stat(helperPath); os.IsNotExist(err) {
			t.Skipf("reminders_fetcher binary not found at expected locations; build it and re-run test")
		}
	}

	// Setup dependencies with FakeMessenger and FileStateStore in temp dir
	fm := &core.FakeMessenger{}
	lg := &testLogger{t: t}
	deps := core.Dependencies{}
	deps.SetLogger(lg)
	deps.SetMessenger(fm)
	// set a file state store in temp dir
	tmpDir := t.TempDir()
	osProvider := core.OsProvider{}
	stateStore := core.NewFileStateStore(tmpDir, osProvider)
	deps.SetStateStore(stateStore)

	cfg := core.ServiceConfig{
		Name:   "itest",
		Type:   "macosReminders",
		Pubs:   map[string]core.ChannelInfo{"task": {Name: "task"}},
		Config: map[string]interface{}{"inbox_name": "Inbox", "only_uncompleted": true, "helper_path": helperPath},
	}

	svc := &Service{Deps: deps, Cfg: cfg}

	// Run Initialize and Check (which invokes fetchAndPublish)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Give some time for the Swift helper to run and for messages to be sent
	// (Check() is synchronous, but dispatch in EventKit is async; the helper waits; still we give a small wait)
	time.Sleep(1 * time.Second)

	if err := svc.Check(); err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	// Allow time for message enqueueing
	time.Sleep(500 * time.Millisecond)

	if len(fm.Messages) == 0 {
		t.Log("No messages were sent; this may be because there are no uncompleted reminders in Inbox")
	}

	// Clean up
	if err := os.RemoveAll(tmpDir); err != nil {
		t.Logf("failed to remove %s: %v", tmpDir, err)
	}
}
