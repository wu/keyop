package versionControlGit

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"keyop/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── helpers ────────────────────────────────────────────────────────────────

// testDepsWithFakeOs builds dependencies backed by the real filesystem under
// tempDir so tests can inspect written files on disk.
func testDepsWithFakeOs(t *testing.T, tempDir string) (core.Dependencies, *core.FakeMessenger, *core.FakeOsProvider, *core.FakeLogger) {
	t.Helper()
	logger := &core.FakeLogger{}
	fm := &core.FakeMessenger{}
	fOs := &core.FakeOsProvider{Home: tempDir}

	fOs.OpenFileFunc = func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
		if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
			return nil, err
		}
		return os.OpenFile(name, flag, perm) //nolint:gosec // test-only file open using variable path
	}
	fOs.MkdirAllFunc = func(path string, perm os.FileMode) error {
		return os.MkdirAll(path, perm)
	}
	fOs.StatFunc = func(name string) (os.FileInfo, error) {
		return os.Stat(name)
	}

	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetMessenger(fm)
	deps.SetOsProvider(fOs)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	deps.SetContext(ctx)
	return deps, fm, fOs, logger
}

// successGitCmdFunc returns a CommandFunc that always succeeds and records calls.
func successGitCmdFunc(commands *[]string) func(string, ...string) core.CommandApi {
	return func(_ string, arg ...string) core.CommandApi {
		*commands = append(*commands, strings.Join(arg, " "))
		return &core.FakeCommand{CombinedOutputFunc: func() ([]byte, error) {
			return []byte("ok"), nil
		}}
	}
}

// ─── ValidateConfig ──────────────────────────────────────────────────────────

func TestValidateConfig_valid_noOptional(t *testing.T) {
	deps, _, _, _ := testDepsWithFakeOs(t, t.TempDir())
	cfg := core.ServiceConfig{
		Name: "vcgit",
		Subs: map[string]core.ChannelInfo{
			"input": {Name: "some-channel"},
		},
		Config: map[string]interface{}{},
	}
	svc := NewService(deps, cfg).(*Service)
	errs := svc.ValidateConfig()
	assert.Empty(t, errs, "valid minimal config should produce no errors")
}

func TestValidateConfig_valid_withOptionals(t *testing.T) {
	deps, _, _, _ := testDepsWithFakeOs(t, t.TempDir())
	cfg := core.ServiceConfig{
		Name: "vcgit",
		Subs: map[string]core.ChannelInfo{
			"input": {Name: "some-channel"},
		},
		Config: map[string]interface{}{
			"dir":       "/tmp/repo",
			"data_path": "new.content",
		},
	}
	svc := NewService(deps, cfg).(*Service)
	errs := svc.ValidateConfig()
	assert.Empty(t, errs)
}

func TestValidateConfig_missingInputSub(t *testing.T) {
	deps, _, _, _ := testDepsWithFakeOs(t, t.TempDir())
	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Subs:   map[string]core.ChannelInfo{},
		Config: map[string]interface{}{},
	}
	svc := NewService(deps, cfg).(*Service)
	errs := svc.ValidateConfig()
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "input")
}

func TestValidateConfig_nilSubs(t *testing.T) {
	deps, _, _, _ := testDepsWithFakeOs(t, t.TempDir())
	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{},
	}
	svc := NewService(deps, cfg).(*Service)
	errs := svc.ValidateConfig()
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "subs")
}

func TestValidateConfig_inputSubMissingName(t *testing.T) {
	deps, _, _, _ := testDepsWithFakeOs(t, t.TempDir())
	cfg := core.ServiceConfig{
		Name: "vcgit",
		Subs: map[string]core.ChannelInfo{
			"input": {Name: ""},
		},
		Config: map[string]interface{}{},
	}
	svc := NewService(deps, cfg).(*Service)
	errs := svc.ValidateConfig()
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "input")
}

func TestValidateConfig_dirWrongType(t *testing.T) {
	deps, _, _, _ := testDepsWithFakeOs(t, t.TempDir())
	cfg := core.ServiceConfig{
		Name: "vcgit",
		Subs: map[string]core.ChannelInfo{"input": {Name: "ch"}},
		Config: map[string]interface{}{
			"dir": 42, // wrong type
		},
	}
	svc := NewService(deps, cfg).(*Service)
	errs := svc.ValidateConfig()
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "dir")
}

func TestValidateConfig_dirEmptyString(t *testing.T) {
	deps, _, _, _ := testDepsWithFakeOs(t, t.TempDir())
	cfg := core.ServiceConfig{
		Name: "vcgit",
		Subs: map[string]core.ChannelInfo{"input": {Name: "ch"}},
		Config: map[string]interface{}{
			"dir": "",
		},
	}
	svc := NewService(deps, cfg).(*Service)
	errs := svc.ValidateConfig()
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "dir")
}

func TestValidateConfig_dataPathWrongType(t *testing.T) {
	deps, _, _, _ := testDepsWithFakeOs(t, t.TempDir())
	cfg := core.ServiceConfig{
		Name: "vcgit",
		Subs: map[string]core.ChannelInfo{"input": {Name: "ch"}},
		Config: map[string]interface{}{
			"data_path": true, // wrong type
		},
	}
	svc := NewService(deps, cfg).(*Service)
	errs := svc.ValidateConfig()
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "data_path")
}

func TestValidateConfig_dataPathEmptyString(t *testing.T) {
	deps, _, _, _ := testDepsWithFakeOs(t, t.TempDir())
	cfg := core.ServiceConfig{
		Name: "vcgit",
		Subs: map[string]core.ChannelInfo{"input": {Name: "ch"}},
		Config: map[string]interface{}{
			"data_path": "",
		},
	}
	svc := NewService(deps, cfg).(*Service)
	errs := svc.ValidateConfig()
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "data_path")
}

func TestValidateConfig_multipleErrors(t *testing.T) {
	deps, _, _, _ := testDepsWithFakeOs(t, t.TempDir())
	cfg := core.ServiceConfig{
		Name: "vcgit",
		// nil subs → missing input
		Config: map[string]interface{}{
			"dir":       "",  // bad
			"data_path": 123, // bad
		},
	}
	svc := NewService(deps, cfg).(*Service)
	errs := svc.ValidateConfig()
	assert.GreaterOrEqual(t, len(errs), 3, "expected errors for missing sub, bad dir, and bad data_path")
}

// ─── Initialize ──────────────────────────────────────────────────────────────

func TestInitialize_defaultDir(t *testing.T) {
	tmp := t.TempDir()
	deps, _, fOs, _ := testDepsWithFakeOs(t, tmp)
	fOs.CommandFunc = successGitCmdFunc(new([]string))

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())
	assert.Equal(t, "./version_files", svc.dir)
}

func TestInitialize_customDir(t *testing.T) {
	tmp := t.TempDir()
	deps, _, fOs, _ := testDepsWithFakeOs(t, tmp)
	fOs.CommandFunc = successGitCmdFunc(new([]string))

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())
	assert.Equal(t, tmp, svc.dir)
}

func TestInitialize_dataPath(t *testing.T) {
	tmp := t.TempDir()
	deps, _, fOs, _ := testDepsWithFakeOs(t, tmp)
	fOs.CommandFunc = successGitCmdFunc(new([]string))

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp, "data_path": "a.b.c"},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())
	assert.Equal(t, "a.b.c", svc.dataPath)
}

func TestInitialize_mkdirFailure_doesNotReturnError(t *testing.T) {
	// MkdirAll failure should NOT prevent Initialize from returning nil;
	// the service emits an error event but continues.
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)
	mkdirErr := errors.New("permission denied")
	fOs.MkdirAllFunc = func(_ string, _ os.FileMode) error {
		return mkdirErr
	}
	fOs.CommandFunc = successGitCmdFunc(new([]string))

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	assert.NoError(t, err, "Initialize should not propagate MkdirAll errors")
	// an error event should have been emitted
	assert.Len(t, fm.Messages, 1)
	assert.Equal(t, "error", fm.Messages[0].Event)
}

func TestInitialize_subscribeFailure_returnsError(t *testing.T) {
	tmp := t.TempDir()
	_, _, fOs, _ := testDepsWithFakeOs(t, tmp)
	fOs.CommandFunc = successGitCmdFunc(new([]string))

	subErr := errors.New("broker unavailable")
	realDeps, _, _, _ := testDepsWithFakeOs(t, tmp)
	customMessenger := &errSubscribeMessenger{err: subErr}
	realDeps.SetMessenger(customMessenger)
	realDeps.SetOsProvider(fOs)

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(realDeps, cfg).(*Service)
	err := svc.Initialize()
	assert.ErrorIs(t, err, subErr)
}

// errSubscribeMessenger is a FakeMessenger that returns an error from Subscribe.
type errSubscribeMessenger struct {
	core.FakeMessenger
	err error
}

func (e *errSubscribeMessenger) Subscribe(_ context.Context, _, _, _, _ string, _ time.Duration, _ func(core.Message) error) error {
	return e.err
}

// ─── handleMessage ───────────────────────────────────────────────────────────

func Test_handleMessage_storeFullMessageAndGitCommit(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	var commands []string
	fOs.CommandFunc = successGitCmdFunc(&commands)

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	msg := core.Message{
		ChannelName: "vcgit",
		ServiceName: "vcgit",
		ServiceType: "versionControlGit",
		Summary:     "My Test Subject",
		Text:        "body text",
		Data:        map[string]interface{}{"k": "v"},
	}

	require.NoError(t, svc.handleMessage(msg))

	filename := sanitizeFilename(msg.Summary) + ".txt"
	p := filepath.Join(tmp, filename)
	b, err := os.ReadFile(p) //nolint:gosec // test-only file read
	require.NoError(t, err)
	var parsed core.Message
	require.NoError(t, json.Unmarshal(b, &parsed))
	assert.Equal(t, msg.Summary, parsed.Summary)
	assert.Empty(t, fm.Messages, "no error events expected")
}

func Test_handleMessage_fallbackSubjectFromText(t *testing.T) {
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
		Summary: "",
		Text:    "first line\nsecond line",
	}
	require.NoError(t, svc.handleMessage(msg))

	filename := sanitizeFilename("first line") + ".txt"
	_, err := os.ReadFile(filepath.Join(tmp, filename)) //nolint:gosec // test-only file read
	require.NoError(t, err, "file should be named after the first line of Text")
	assert.Empty(t, fm.Messages)
}

func Test_handleMessage_fallbackSubjectTimestamp(t *testing.T) {
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

	msg := core.Message{Summary: "", Text: ""}
	require.NoError(t, svc.handleMessage(msg))

	// There should be exactly one .txt file in tmp (timestamp-named)
	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	var txts []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".txt") {
			txts = append(txts, e.Name())
		}
	}
	assert.Len(t, txts, 1, "expected exactly one timestamped file")
	assert.Empty(t, fm.Messages)
}

func Test_handleMessage_dataPath_stringNode(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)
	fOs.CommandFunc = successGitCmdFunc(new([]string))

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp, "data_path": "new.content"},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	msg := core.Message{
		Summary: "subject-1",
		Data:    map[string]interface{}{"new": map[string]interface{}{"content": "hello world"}},
	}
	require.NoError(t, svc.handleMessage(msg))

	b, err := os.ReadFile(filepath.Join(tmp, sanitizeFilename("subject-1")+".txt")) //nolint:gosec // test-only file read
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(b))
	assert.Empty(t, fm.Messages)
}

func Test_handleMessage_dataPath_objectNode(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)
	fOs.CommandFunc = successGitCmdFunc(new([]string))

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp, "data_path": "new.content"},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	msg := core.Message{
		Summary: "subject-obj",
		Data:    map[string]interface{}{"new": map[string]interface{}{"content": map[string]interface{}{"a": float64(1)}}},
	}
	require.NoError(t, svc.handleMessage(msg))

	b, err := os.ReadFile(filepath.Join(tmp, sanitizeFilename("subject-obj")+".txt")) //nolint:gosec // test-only file read
	require.NoError(t, err)
	assert.Contains(t, string(b), `"a": 1`)
	assert.Empty(t, fm.Messages)
}

func Test_handleMessage_dataPath_extractFailure_fallsBackToFullMessage(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)
	fOs.CommandFunc = successGitCmdFunc(new([]string))

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp, "data_path": "nonexistent.path"},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	msg := core.Message{
		Summary: "fallback-test",
		Data:    map[string]interface{}{"x": "y"},
	}
	require.NoError(t, svc.handleMessage(msg))

	// file should exist and contain the full JSON-marshaled message
	b, err := os.ReadFile(filepath.Join(tmp, sanitizeFilename("fallback-test")+".txt")) //nolint:gosec // test-only file read
	require.NoError(t, err)
	var parsed core.Message
	require.NoError(t, json.Unmarshal(b, &parsed))
	assert.Equal(t, msg.Summary, parsed.Summary)

	// an error event should have been emitted for the failed extraction
	require.Len(t, fm.Messages, 1)
	assert.Equal(t, "error", fm.Messages[0].Event)
	assert.Equal(t, "extract-data-node", fm.Messages[0].Data.(map[string]string)["op"])
}

func Test_handleMessage_mkdirFailure_emitsErrorAndReturnsNil(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)
	fOs.CommandFunc = successGitCmdFunc(new([]string))

	mkdirCalls := 0
	fOs.MkdirAllFunc = func(path string, perm os.FileMode) error {
		mkdirCalls++
		if mkdirCalls >= 2 {
			// fail on the per-message mkdir (second call)
			return errors.New("disk full")
		}
		return os.MkdirAll(path, perm)
	}

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	msg := core.Message{Summary: "fail-mkdir"}
	require.NoError(t, svc.handleMessage(msg))

	require.Len(t, fm.Messages, 1)
	assert.Equal(t, "error", fm.Messages[0].Event)
	assert.Equal(t, "mkdir", fm.Messages[0].Data.(map[string]string)["op"])
}

func Test_handleMessage_writeFileFailure_emitsError(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)
	fOs.CommandFunc = successGitCmdFunc(new([]string))

	writeErr := errors.New("no space left on device")
	fOs.OpenFileFunc = func(_ string, _ int, _ os.FileMode) (core.FileApi, error) {
		return nil, writeErr
	}

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	msg := core.Message{Summary: "write-fail"}
	require.NoError(t, svc.handleMessage(msg))

	require.Len(t, fm.Messages, 1)
	assert.Equal(t, "error", fm.Messages[0].Event)
	assert.Equal(t, "write", fm.Messages[0].Data.(map[string]string)["op"])
}

func Test_handleMessage_gitAddError_emitsError(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	var cmds []string
	fOs.CommandFunc = func(_ string, arg ...string) core.CommandApi {
		cmd := strings.Join(arg, " ")
		cmds = append(cmds, cmd)
		if strings.Contains(cmd, " add ") {
			return &core.FakeCommand{CombinedOutputFunc: func() ([]byte, error) {
				return []byte("fatal: permission denied"), errors.New("git add failed")
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

	msg := core.Message{Summary: "add-err"}
	require.NoError(t, svc.handleMessage(msg))

	// file should still have been written
	_, err := os.ReadFile(filepath.Join(tmp, sanitizeFilename("add-err")+".txt")) //nolint:gosec // test-only file read
	require.NoError(t, err)

	require.Len(t, fm.Messages, 1)
	assert.Equal(t, "error", fm.Messages[0].Event)
	assert.Equal(t, "git-add", fm.Messages[0].Data.(map[string]string)["op"])
}

func Test_handleMessage_gitCommitError_emitsError(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	fOs.CommandFunc = func(_ string, arg ...string) core.CommandApi {
		cmd := strings.Join(arg, " ")
		if strings.Contains(cmd, " commit ") {
			return &core.FakeCommand{CombinedOutputFunc: func() ([]byte, error) {
				return []byte("fatal: broken repo"), errors.New("commit failed")
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

	msg := core.Message{Summary: "commit-err"}
	require.NoError(t, svc.handleMessage(msg))

	require.Len(t, fm.Messages, 1)
	assert.Equal(t, "error", fm.Messages[0].Event)
	assert.Equal(t, "git-commit", fm.Messages[0].Data.(map[string]string)["op"])
}

func Test_handleMessage_gitCommitNothingToCommit_noError(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	fOs.CommandFunc = func(_ string, arg ...string) core.CommandApi {
		cmd := strings.Join(arg, " ")
		if strings.Contains(cmd, " commit ") {
			return &core.FakeCommand{CombinedOutputFunc: func() ([]byte, error) {
				return []byte("nothing to commit, working tree clean"), errors.New("exit status 1")
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

	msg := core.Message{Summary: "no-change"}
	require.NoError(t, svc.handleMessage(msg))
	assert.Empty(t, fm.Messages, "nothing-to-commit should not generate an error event")
}

func Test_handleMessage_gitInitError_emitsError(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	// Override Stat so .git always appears absent, forcing git init every call
	fOs.StatFunc = func(name string) (os.FileInfo, error) {
		if strings.HasSuffix(name, ".git") {
			return nil, os.ErrNotExist
		}
		return os.Stat(name)
	}

	fOs.CommandFunc = func(_ string, arg ...string) core.CommandApi {
		cmd := strings.Join(arg, " ")
		if strings.Contains(cmd, " init") {
			return &core.FakeCommand{CombinedOutputFunc: func() ([]byte, error) {
				return []byte("fatal: cannot init"), errors.New("init failed")
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

	msg := core.Message{Summary: "init-err"}
	require.NoError(t, svc.handleMessage(msg))

	// should have emitted a git-init error event (continuing despite the error)
	var initErrFound bool
	for _, m := range fm.Messages {
		if m.Event == "error" {
			if d, ok := m.Data.(map[string]string); ok && d["op"] == "git-init" {
				initErrFound = true
			}
		}
	}
	assert.True(t, initErrFound, "expected a git-init error event")
}

func Test_handleMessage_gitStatError_emitsError(t *testing.T) {
	tmp := t.TempDir()
	deps, fm, fOs, _ := testDepsWithFakeOs(t, tmp)

	statErr := errors.New("I/O error")
	fOs.StatFunc = func(name string) (os.FileInfo, error) {
		if strings.HasSuffix(name, ".git") {
			return nil, statErr // not os.ErrNotExist
		}
		return os.Stat(name)
	}
	fOs.CommandFunc = successGitCmdFunc(new([]string))

	cfg := core.ServiceConfig{
		Name:   "vcgit",
		Config: map[string]interface{}{"dir": tmp},
		Subs:   map[string]core.ChannelInfo{"input": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	require.NoError(t, svc.Initialize())

	msg := core.Message{Summary: "stat-err"}
	require.NoError(t, svc.handleMessage(msg))

	var statErrFound bool
	for _, m := range fm.Messages {
		if m.Event == "error" {
			if d, ok := m.Data.(map[string]string); ok && d["op"] == "git-stat" {
				statErrFound = true
			}
		}
	}
	assert.True(t, statErrFound, "expected a git-stat error event")
}

// ─── sendErrorEvent ───────────────────────────────────────────────────────────

func Test_sendErrorEvent_appendsMessage(t *testing.T) {
	fm := &core.FakeMessenger{}
	cfg := core.ServiceConfig{Name: "vcgit", Type: "versionControlGit"}
	sendErrorEvent(fm, cfg, "test-op", errors.New("boom"), []byte("out"))
	require.Len(t, fm.Messages, 1)
	m := fm.Messages[0]
	assert.Equal(t, "error", m.Event)
	d, ok := m.Data.(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "test-op", d["op"])
	assert.Equal(t, "boom", d["error"])
	assert.Equal(t, "out", d["output"])
}

func Test_sendErrorEvent_nilErrorAndNilOutput(t *testing.T) {
	fm := &core.FakeMessenger{}
	cfg := core.ServiceConfig{Name: "vcgit", Type: "versionControlGit"}
	sendErrorEvent(fm, cfg, "op-only", nil, nil)
	require.Len(t, fm.Messages, 1)
	d := fm.Messages[0].Data.(map[string]string)
	assert.Equal(t, "op-only", d["op"])
	_, hasErr := d["error"]
	assert.False(t, hasErr, "error key should not be set when err is nil")
	_, hasOut := d["output"]
	assert.False(t, hasOut, "output key should not be set when output is nil")
}

// ─── sanitizeFilename ─────────────────────────────────────────────────────────

func Test_sanitizeFilename(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"hello world", "hello_world"},
		{"hello/world", "hello_world"},
		{"  spaces  ", "spaces"},
		{"", "message"},
		{"a:b*c?d\"e<f>g|h", "a_b_c_d_e_f_g_h"},
		{strings.Repeat("a", 120), strings.Repeat("a", 100)},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := sanitizeFilename(tc.input)
			assert.Equal(t, tc.expected, got)
		})
	}
}

// ─── extractDataNode ─────────────────────────────────────────────────────────

func Test_extractDataNode_nilData(t *testing.T) {
	_, err := extractDataNode(nil, "a.b")
	assert.Error(t, err)
}

func Test_extractDataNode_nonExistentPath(t *testing.T) {
	data := map[string]interface{}{"x": map[string]interface{}{"y": "z"}}
	_, err := extractDataNode(data, "x.z")
	assert.Error(t, err)
}

func Test_extractDataNode_pathThroughNonMap(t *testing.T) {
	data := map[string]interface{}{"x": "not a map"}
	_, err := extractDataNode(data, "x.y")
	assert.Error(t, err)
}

func Test_extractDataNode_stringValue(t *testing.T) {
	data := map[string]interface{}{"a": map[string]interface{}{"b": "my string"}}
	got, err := extractDataNode(data, "a.b")
	require.NoError(t, err)
	assert.Equal(t, "my string", string(got))
}

func Test_extractDataNode_numericValue(t *testing.T) {
	data := map[string]interface{}{"val": float64(42)}
	got, err := extractDataNode(data, "val")
	require.NoError(t, err)
	assert.Equal(t, "42", string(got))
}

func Test_extractDataNode_nestedObject(t *testing.T) {
	data := map[string]interface{}{"a": map[string]interface{}{"b": map[string]interface{}{"c": "deep"}}}
	got, err := extractDataNode(data, "a.b")
	require.NoError(t, err)
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(got, &parsed))
	assert.Equal(t, "deep", parsed["c"])
}
