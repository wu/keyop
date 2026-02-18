package systemctl

import (
	"bytes"
	"io"
	"keyop/core"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type mockFile struct {
	buf *bytes.Buffer
}

func (m *mockFile) Write(p []byte) (n int, err error)            { return m.buf.Write(p) }
func (m *mockFile) WriteString(s string) (n int, err error)      { return m.buf.WriteString(s) }
func (m *mockFile) Close() error                                 { return nil }
func (m *mockFile) Read(p []byte) (n int, err error)             { return 0, io.EOF }
func (m *mockFile) Seek(offset int64, whence int) (int64, error) { return 0, nil }

func TestInstallSystemd(t *testing.T) {
	fakeOs := &core.FakeOsProvider{}
	logger := &core.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(logger)

	buf := &bytes.Buffer{}
	fakeOs.OpenFileFunc = func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
		if name != "/etc/systemd/system/keyop.service" {
			t.Errorf("unexpected file path: %s", name)
		}
		return &mockFile{buf: buf}, nil
	}

	var commands []string
	fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
		commands = append(commands, name+" "+strings.Join(arg, " "))
		return &core.FakeCommand{}
	}

	err := installSystemd(deps, "root", "root")
	if err != nil {
		t.Fatalf("installSystemd failed: %v", err)
	}

	exe, _ := os.Executable()
	exe, _ = filepath.Abs(exe)

	writtenContent := buf.String()
	if !strings.Contains(writtenContent, "ExecStart="+exe+" run") {
		t.Errorf("service file missing correct ExecStart: %s", writtenContent)
	}
	if !strings.Contains(writtenContent, "User=root") {
		t.Errorf("service file missing User=root: %s", writtenContent)
	}
	if !strings.Contains(writtenContent, "Group=root") {
		t.Errorf("service file missing Group=root: %s", writtenContent)
	}
	if !strings.Contains(writtenContent, "Restart=always") {
		t.Errorf("service file missing Restart=always: %s", writtenContent)
	}

	expectedCommands := []string{
		"systemctl daemon-reload",
		"systemctl enable keyop.service",
		"systemctl start keyop.service",
	}

	if len(commands) != len(expectedCommands) {
		t.Fatalf("expected %d commands, got %d: %v", len(expectedCommands), len(commands), commands)
	}

	for i, cmd := range expectedCommands {
		if commands[i] != cmd {
			t.Errorf("expected command %d to be %s, got %s", i, cmd, commands[i])
		}
	}
}

func TestInstallSystemdCustomUserGroup(t *testing.T) {
	fakeOs := &core.FakeOsProvider{}
	logger := &core.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(logger)

	buf := &bytes.Buffer{}
	fakeOs.OpenFileFunc = func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
		return &mockFile{buf: buf}, nil
	}

	fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
		return &core.FakeCommand{}
	}

	err := installSystemd(deps, "customuser", "customgroup")
	if err != nil {
		t.Fatalf("installSystemd failed: %v", err)
	}

	writtenContent := buf.String()
	if !strings.Contains(writtenContent, "User=customuser") {
		t.Errorf("service file missing User=customuser: %s", writtenContent)
	}
	if !strings.Contains(writtenContent, "Group=customgroup") {
		t.Errorf("service file missing Group=customgroup: %s", writtenContent)
	}
}
