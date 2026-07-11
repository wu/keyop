package launchd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/testutil"
)

type mockFile struct {
	buf *bytes.Buffer
}

func (m *mockFile) Write(p []byte) (n int, err error)       { return m.buf.Write(p) }
func (m *mockFile) WriteString(s string) (n int, err error) { return m.buf.WriteString(s) }
func (m *mockFile) Close() error                            { return nil }
func (m *mockFile) Read(_ []byte) (int, error)              { return 0, io.EOF }
func (m *mockFile) Seek(_ int64, _ int) (int64, error)      { return 0, nil }

func newTestDeps(fakeOs *testutil.FakeOsProvider) core.Dependencies {
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&testutil.FakeLogger{})
	return deps
}

func TestInstallLaunchd(t *testing.T) {
	buf := &bytes.Buffer{}
	var commands []string
	var mkdirs []string

	fakeOs := &testutil.FakeOsProvider{
		Home: "/Users/test",
		OpenFileFunc: func(name string, _ int, _ os.FileMode) (core.FileApi, error) {
			want := "/Users/test/Library/LaunchAgents/" + Label + ".plist"
			if name != want {
				t.Errorf("unexpected plist path: %s (want %s)", name, want)
			}
			return &mockFile{buf: buf}, nil
		},
		MkdirAllFunc: func(path string, _ os.FileMode) error {
			mkdirs = append(mkdirs, path)
			return nil
		},
		CommandFunc: func(name string, arg ...string) core.CommandApi {
			commands = append(commands, name+" "+strings.Join(arg, " "))
			return &testutil.FakeCommand{}
		},
	}

	if err := installLaunchd(newTestDeps(fakeOs)); err != nil {
		t.Fatalf("installLaunchd failed: %v", err)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("failed to get executable: %v", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		t.Fatalf("failed to get absolute path: %v", err)
	}

	plist := buf.String()
	for _, want := range []string{
		"<string>" + Label + "</string>",
		"<string>" + exe + "</string>",
		"<string>run</string>",
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
		"/Users/test/.keyop/logs/launchd-stdout.log",
		"/Users/test/.keyop/logs/launchd-stderr.log",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q:\n%s", want, plist)
		}
	}

	wantDirs := []string{"/Users/test/.keyop/logs", "/Users/test/Library/LaunchAgents"}
	if len(mkdirs) != len(wantDirs) {
		t.Fatalf("expected %d mkdirs, got %v", len(wantDirs), mkdirs)
	}
	for i, d := range wantDirs {
		if mkdirs[i] != d {
			t.Errorf("mkdir %d: want %s, got %s", i, d, mkdirs[i])
		}
	}

	wantCommands := []string{
		"launchctl bootout " + serviceTarget(),
		"launchctl enable " + serviceTarget(),
		"launchctl bootstrap " + guiDomain() + " /Users/test/Library/LaunchAgents/" + Label + ".plist",
	}
	if len(commands) != len(wantCommands) {
		t.Fatalf("expected %d commands, got %d: %v", len(wantCommands), len(commands), commands)
	}
	for i, cmd := range wantCommands {
		if commands[i] != cmd {
			t.Errorf("command %d: want %q, got %q", i, cmd, commands[i])
		}
	}
}

func TestInstallLaunchdBootstrapFails(t *testing.T) {
	fakeOs := &testutil.FakeOsProvider{
		Home: "/Users/test",
		OpenFileFunc: func(_ string, _ int, _ os.FileMode) (core.FileApi, error) {
			return &mockFile{buf: &bytes.Buffer{}}, nil
		},
		CommandFunc: func(_ string, arg ...string) core.CommandApi {
			if len(arg) > 0 && arg[0] == "bootstrap" {
				return &testutil.FakeCommand{RunFunc: func() error { return errors.New("Bootstrap failed: 5") }}
			}
			return &testutil.FakeCommand{}
		},
	}

	err := installLaunchd(newTestDeps(fakeOs))
	if err == nil {
		t.Fatal("expected error when bootstrap fails")
	}
	if !strings.Contains(err.Error(), "GUI session") {
		t.Errorf("error should hint at GUI session requirement: %v", err)
	}
}

func TestUninstallLaunchd(t *testing.T) {
	var commands []string
	var removed []string

	fakeOs := &testutil.FakeOsProvider{
		Home: "/Users/test",
		CommandFunc: func(name string, arg ...string) core.CommandApi {
			commands = append(commands, name+" "+strings.Join(arg, " "))
			return &testutil.FakeCommand{}
		},
		RemoveFunc: func(name string) error {
			removed = append(removed, name)
			return nil
		},
	}

	if err := uninstallLaunchd(newTestDeps(fakeOs)); err != nil {
		t.Fatalf("uninstallLaunchd failed: %v", err)
	}

	if len(commands) != 1 || commands[0] != "launchctl bootout "+serviceTarget() {
		t.Errorf("unexpected commands: %v", commands)
	}
	want := "/Users/test/Library/LaunchAgents/" + Label + ".plist"
	if len(removed) != 1 || removed[0] != want {
		t.Errorf("unexpected removals: %v (want %s)", removed, want)
	}
}

func TestStartLaunchdKickstartSucceeds(t *testing.T) {
	var commands []string
	fakeOs := &testutil.FakeOsProvider{
		Home: "/Users/test",
		CommandFunc: func(name string, arg ...string) core.CommandApi {
			commands = append(commands, name+" "+strings.Join(arg, " "))
			return &testutil.FakeCommand{}
		},
	}

	if err := startLaunchd(newTestDeps(fakeOs)); err != nil {
		t.Fatalf("startLaunchd failed: %v", err)
	}
	if len(commands) != 1 || commands[0] != "launchctl kickstart "+serviceTarget() {
		t.Errorf("unexpected commands: %v", commands)
	}
}

func TestStartLaunchdFallsBackToBootstrap(t *testing.T) {
	var commands []string
	fakeOs := &testutil.FakeOsProvider{
		Home: "/Users/test",
		CommandFunc: func(name string, arg ...string) core.CommandApi {
			commands = append(commands, name+" "+strings.Join(arg, " "))
			if arg[0] == "kickstart" {
				return &testutil.FakeCommand{RunFunc: func() error { return errors.New("not loaded") }}
			}
			return &testutil.FakeCommand{}
		},
	}

	if err := startLaunchd(newTestDeps(fakeOs)); err != nil {
		t.Fatalf("startLaunchd failed: %v", err)
	}
	want := []string{
		"launchctl kickstart " + serviceTarget(),
		"launchctl bootstrap " + guiDomain() + " /Users/test/Library/LaunchAgents/" + Label + ".plist",
	}
	if len(commands) != 2 || commands[0] != want[0] || commands[1] != want[1] {
		t.Errorf("unexpected commands: %v (want %v)", commands, want)
	}
}
