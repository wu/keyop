//nolint:revive
package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"keyop/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── handleRename ────────────────────────────────────────────────────────────

func TestHandleRename_Success(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	var cmds []string
	fOs.CommandFunc = successGitCmdFunc(&cmds)

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	// Create the old file so Stat succeeds.
	oldFilename := sanitizeFilename("old note") + ".txt"
	require.NoError(t, os.WriteFile(filepath.Join(tmp, oldFilename), []byte("content"), 0o600))

	msg := core.Message{
		DataType: "notes.content_rename.v1",
		Data:     ContentRenameEvent{OldName: "old note", NewName: "new note"},
	}
	require.NoError(t, svc.handleRename(msg))

	assert.Empty(t, fm.Messages, "no error events expected")

	var mvFound, commitFound bool
	for _, c := range cmds {
		if strings.Contains(c, "mv") {
			mvFound = true
		}
		if strings.Contains(c, "commit") {
			commitFound = true
		}
	}
	assert.True(t, mvFound, "expected git mv call")
	assert.True(t, commitFound, "expected git commit call")
}

func TestHandleRename_SameName(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	var cmds []string
	fOs.CommandFunc = successGitCmdFunc(&cmds)

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	msg := core.Message{
		DataType: "notes.content_rename.v1",
		Data:     ContentRenameEvent{OldName: "same", NewName: "same"},
	}
	require.NoError(t, svc.handleRename(msg))

	assert.Empty(t, fm.Messages, "no error events expected")
	// No git commands should have been issued after Initialize
	for _, c := range cmds {
		assert.False(t, strings.Contains(c, "mv"), "unexpected git mv for same-name rename")
		assert.False(t, strings.Contains(c, "commit"), "unexpected git commit for same-name rename")
	}
}

func TestHandleRename_FileNotExist(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	var cmds []string
	fOs.CommandFunc = successGitCmdFunc(&cmds)

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	// Do NOT create the old file — Stat should return ErrNotExist.
	msg := core.Message{
		DataType: "notes.content_rename.v1",
		Data:     ContentRenameEvent{OldName: "missing", NewName: "other"},
	}
	require.NoError(t, svc.handleRename(msg))

	assert.Empty(t, fm.Messages, "no error events expected when old file is absent")
	for _, c := range cmds {
		assert.False(t, strings.Contains(c, "mv"), "git mv should not be called when old file is absent")
	}
}

func TestHandleRename_FromRawMap(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	var cmds []string
	fOs.CommandFunc = successGitCmdFunc(&cmds)

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	// Create the old file.
	oldFilename := sanitizeFilename("raw old") + ".txt"
	require.NoError(t, os.WriteFile(filepath.Join(tmp, oldFilename), []byte("hi"), 0o600))

	// No DataType → registry path is skipped; raw map fallback is used.
	msg := core.Message{
		Data: map[string]any{"old_name": "raw old", "new_name": "raw new"},
	}
	require.NoError(t, svc.handleRename(msg))

	assert.Empty(t, fm.Messages, "no error events expected")
	var mvFound bool
	for _, c := range cmds {
		if strings.Contains(c, "mv") {
			mvFound = true
		}
	}
	assert.True(t, mvFound, "expected git mv call from raw-map path")
}

func TestHandleRename_GitMvError(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	fOs.CommandFunc = func(_ string, arg ...string) core.CommandApi {
		cmd := strings.Join(arg, " ")
		if strings.Contains(cmd, "mv") {
			return &core.FakeCommand{CombinedOutputFunc: func() ([]byte, error) {
				return []byte("fatal: not a git repo"), errors.New("git mv failed")
			}}
		}
		return &core.FakeCommand{CombinedOutputFunc: func() ([]byte, error) { return []byte("ok"), nil }}
	}

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	oldFilename := sanitizeFilename("mv-old") + ".txt"
	require.NoError(t, os.WriteFile(filepath.Join(tmp, oldFilename), []byte("x"), 0o600))

	msg := core.Message{
		DataType: "notes.content_rename.v1",
		Data:     ContentRenameEvent{OldName: "mv-old", NewName: "mv-new"},
	}
	require.NoError(t, svc.handleRename(msg))

	require.Len(t, fm.Messages, 1)
	assert.Equal(t, "error", fm.Messages[0].Event)
	assert.Equal(t, "git-mv", fm.Messages[0].Data.(map[string]string)["op"])
}

// ─── handleRemove ────────────────────────────────────────────────────────────

func TestHandleRemove_Success(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	var cmds []string
	fOs.CommandFunc = successGitCmdFunc(&cmds)

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	filename := sanitizeFilename("remove-me") + ".txt"
	require.NoError(t, os.WriteFile(filepath.Join(tmp, filename), []byte("data"), 0o600))

	msg := core.Message{
		DataType: "notes.content_remove.v1",
		Data:     ContentRemoveEvent{Name: "remove-me"},
	}
	require.NoError(t, svc.handleRemove(msg))

	assert.Empty(t, fm.Messages, "no error events expected")

	var rmFound, commitFound bool
	for _, c := range cmds {
		if strings.Contains(c, " rm ") {
			rmFound = true
		}
		if strings.Contains(c, "commit") {
			commitFound = true
		}
	}
	assert.True(t, rmFound, "expected git rm call")
	assert.True(t, commitFound, "expected git commit call")
}

func TestHandleRemove_FileNotExist(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	var cmds []string
	fOs.CommandFunc = successGitCmdFunc(&cmds)

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	// File does not exist.
	msg := core.Message{
		DataType: "notes.content_remove.v1",
		Data:     ContentRemoveEvent{Name: "ghost"},
	}
	require.NoError(t, svc.handleRemove(msg))

	assert.Empty(t, fm.Messages, "no error events expected when file is absent")
	for _, c := range cmds {
		assert.False(t, strings.Contains(c, " rm "), "git rm should not be called when file is absent")
	}
}

func TestHandleRemove_FromRawMap(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	var cmds []string
	fOs.CommandFunc = successGitCmdFunc(&cmds)

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	filename := sanitizeFilename("map-remove") + ".txt"
	require.NoError(t, os.WriteFile(filepath.Join(tmp, filename), []byte("d"), 0o600))

	// No DataType → registry skipped; raw map path used.
	msg := core.Message{
		Data: map[string]any{"name": "map-remove"},
	}
	require.NoError(t, svc.handleRemove(msg))

	assert.Empty(t, fm.Messages, "no error events expected")
	var rmFound bool
	for _, c := range cmds {
		if strings.Contains(c, " rm ") {
			rmFound = true
		}
	}
	assert.True(t, rmFound, "expected git rm call from raw-map path")
}

func TestHandleRemove_FromSummary(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	var cmds []string
	fOs.CommandFunc = successGitCmdFunc(&cmds)

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	filename := sanitizeFilename("summary-note") + ".txt"
	require.NoError(t, os.WriteFile(filepath.Join(tmp, filename), []byte("d"), 0o600))

	// No Data, no DataType — name falls back to msg.Summary.
	msg := core.Message{
		Summary: "summary-note",
	}
	require.NoError(t, svc.handleRemove(msg))

	assert.Empty(t, fm.Messages, "no error events expected")
	var rmFound bool
	for _, c := range cmds {
		if strings.Contains(c, " rm ") {
			rmFound = true
		}
	}
	assert.True(t, rmFound, "expected git rm call from Summary fallback path")
}

func TestHandleRemove_GitRmError(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	fOs.CommandFunc = func(_ string, arg ...string) core.CommandApi {
		cmd := strings.Join(arg, " ")
		if strings.Contains(cmd, " rm ") {
			return &core.FakeCommand{CombinedOutputFunc: func() ([]byte, error) {
				return []byte("fatal: pathspec did not match"), errors.New("git rm failed")
			}}
		}
		return &core.FakeCommand{CombinedOutputFunc: func() ([]byte, error) { return []byte("ok"), nil }}
	}

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	filename := sanitizeFilename("rm-err") + ".txt"
	require.NoError(t, os.WriteFile(filepath.Join(tmp, filename), []byte("x"), 0o600))

	msg := core.Message{
		DataType: "notes.content_remove.v1",
		Data:     ContentRemoveEvent{Name: "rm-err"},
	}
	require.NoError(t, svc.handleRemove(msg))

	require.Len(t, fm.Messages, 1)
	assert.Equal(t, "error", fm.Messages[0].Event)
	assert.Equal(t, "git-rm", fm.Messages[0].Data.(map[string]string)["op"])
}

// ─── handleMessage — ContentChange paths ─────────────────────────────────────

func TestHandleMessage_ContentChange_TypedPayload(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)
	fOs.CommandFunc = successGitCmdFunc(new([]string))

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	msg := core.Message{
		Summary:  "typed-note",
		DataType: "notes.content_change.v1",
		Data:     ContentChangeEvent{Name: "typed-note", New: "hello typed world"},
	}
	require.NoError(t, svc.handleMessage(msg))

	assert.Empty(t, fm.Messages, "no error events expected")

	filename := sanitizeFilename("typed-note") + ".txt"
	content, err := os.ReadFile(filepath.Join(tmp, filename)) //nolint:gosec // test-only
	require.NoError(t, err)
	assert.Equal(t, "hello typed world", string(content))
}

func TestHandleMessage_ContentChange_MapFallback(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)
	fOs.CommandFunc = successGitCmdFunc(new([]string))

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	msg := core.Message{
		Summary:  "map-note",
		DataType: "notes.content_change.v1",
		Data:     map[string]any{"new": "hello from map"},
	}
	require.NoError(t, svc.handleMessage(msg))

	assert.Empty(t, fm.Messages, "no error events expected")

	filename := sanitizeFilename("map-note") + ".txt"
	content, err := os.ReadFile(filepath.Join(tmp, filename)) //nolint:gosec // test-only
	require.NoError(t, err)
	assert.Equal(t, "hello from map", string(content))
}

func TestHandleMessage_NothingAddedToCommit(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	fOs.CommandFunc = func(_ string, arg ...string) core.CommandApi {
		cmd := strings.Join(arg, " ")
		if strings.Contains(cmd, " commit ") {
			return &core.FakeCommand{CombinedOutputFunc: func() ([]byte, error) {
				// "nothing added to commit" is the second variant ignored by the service.
				return []byte("nothing added to commit but untracked files present"), errors.New("exit status 1")
			}}
		}
		return &core.FakeCommand{CombinedOutputFunc: func() ([]byte, error) { return []byte("ok"), nil }}
	}

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	msg := core.Message{Summary: "no-add"}
	require.NoError(t, svc.handleMessage(msg))
	assert.Empty(t, fm.Messages, "nothing-added-to-commit should not generate an error event")
}

// ─── PayloadType methods ──────────────────────────────────────────────────────

func TestContentChangeEvent_PayloadType(t *testing.T) {
	e := ContentChangeEvent{}
	assert.Equal(t, "notes.content_change.v1", e.PayloadType())
}

func TestContentRenameEvent_PayloadType(t *testing.T) {
	e := ContentRenameEvent{}
	assert.Equal(t, "notes.content_rename.v1", e.PayloadType())
}

func TestContentRemoveEvent_PayloadType(t *testing.T) {
	e := ContentRemoveEvent{}
	assert.Equal(t, "notes.content_remove.v1", e.PayloadType())
}
