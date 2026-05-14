package util

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/testutil"
)

// ── EnsureGitRepo ────────────────────────────────────────────────────────────

func TestEnsureGitRepo_AlreadyExists(t *testing.T) {
	var called []string
	fos := &testutil.FakeOsProvider{
		StatFunc: func(name string) (os.FileInfo, error) {
			called = append(called, name)
			return nil, nil // stat succeeds → repo present
		},
	}
	err := EnsureGitRepo(fos, "/some/dir")
	require.NoError(t, err)
	assert.Len(t, called, 1)
	assert.Contains(t, called[0], ".git")
}

func TestEnsureGitRepo_InitOnNotExist(t *testing.T) {
	var cmds [][]string
	fos := &testutil.FakeOsProvider{
		StatFunc: func(name string) (os.FileInfo, error) {
			return nil, fmt.Errorf("not found: %w", fs.ErrNotExist)
		},
		CommandFunc: func(name string, args ...string) core.CommandApi {
			cmds = append(cmds, append([]string{name}, args...))
			return &testutil.FakeCommand{}
		},
	}
	err := EnsureGitRepo(fos, "/repo")
	require.NoError(t, err)
	require.Len(t, cmds, 1)
	assert.Equal(t, []string{"git", "-C", "/repo", "init"}, cmds[0])
}

func TestEnsureGitRepo_StatError_NotErrNotExist(t *testing.T) {
	statErr := errors.New("I/O error")
	fos := &testutil.FakeOsProvider{
		StatFunc: func(name string) (os.FileInfo, error) {
			return nil, statErr
		},
	}
	err := EnsureGitRepo(fos, "/repo")
	require.Error(t, err)
	assert.ErrorIs(t, err, statErr)
}

func TestEnsureGitRepo_InitFails(t *testing.T) {
	initErr := errors.New("permission denied")
	fos := &testutil.FakeOsProvider{
		StatFunc: func(name string) (os.FileInfo, error) {
			return nil, fmt.Errorf("%w", fs.ErrNotExist)
		},
		CommandFunc: func(name string, args ...string) core.CommandApi {
			return &testutil.FakeCommand{
				CombinedOutputFunc: func() ([]byte, error) {
					return []byte("fatal: cannot create"), initErr
				},
			}
		},
	}
	err := EnsureGitRepo(fos, "/repo")
	require.Error(t, err)
	assert.ErrorIs(t, err, initErr)
}

// ── GitCommitAll ─────────────────────────────────────────────────────────────

func TestGitCommitAll_Success(t *testing.T) {
	var cmds [][]string
	fos := &testutil.FakeOsProvider{
		CommandFunc: func(name string, args ...string) core.CommandApi {
			cmds = append(cmds, append([]string{name}, args...))
			return &testutil.FakeCommand{}
		},
	}
	err := GitCommitAll(fos, "/repo", "test commit")
	require.NoError(t, err)
	require.Len(t, cmds, 2)
	assert.Equal(t, []string{"git", "-C", "/repo", "add", "-A"}, cmds[0])
	assert.Equal(t, []string{"git", "-C", "/repo", "commit", "-m", "test commit"}, cmds[1])
}

func TestGitCommitAll_NothingToCommit(t *testing.T) {
	callCount := 0
	fos := &testutil.FakeOsProvider{
		CommandFunc: func(name string, args ...string) core.CommandApi {
			callCount++
			if callCount == 2 { // commit call
				return &testutil.FakeCommand{
					CombinedOutputFunc: func() ([]byte, error) {
						return []byte("nothing to commit, working tree clean"), errors.New("exit status 1")
					},
				}
			}
			return &testutil.FakeCommand{}
		},
	}
	err := GitCommitAll(fos, "/repo", "msg")
	require.NoError(t, err) // "nothing to commit" is not an error
}

func TestGitCommitAll_NothingAddedToCommit(t *testing.T) {
	callCount := 0
	fos := &testutil.FakeOsProvider{
		CommandFunc: func(name string, args ...string) core.CommandApi {
			callCount++
			if callCount == 2 {
				return &testutil.FakeCommand{
					CombinedOutputFunc: func() ([]byte, error) {
						return []byte("nothing added to commit but untracked files present"), errors.New("exit status 1")
					},
				}
			}
			return &testutil.FakeCommand{}
		},
	}
	err := GitCommitAll(fos, "/repo", "msg")
	require.NoError(t, err)
}

func TestGitCommitAll_AddFails(t *testing.T) {
	addErr := errors.New("exit status 128")
	fos := &testutil.FakeOsProvider{
		CommandFunc: func(name string, args ...string) core.CommandApi {
			return &testutil.FakeCommand{
				CombinedOutputFunc: func() ([]byte, error) {
					return []byte("fatal: not a git repository"), addErr
				},
			}
		},
	}
	err := GitCommitAll(fos, "/repo", "msg")
	require.Error(t, err)
	assert.ErrorIs(t, err, addErr)
}

func TestGitCommitAll_CommitFails(t *testing.T) {
	commitErr := errors.New("exit status 1")
	callCount := 0
	fos := &testutil.FakeOsProvider{
		CommandFunc: func(name string, args ...string) core.CommandApi {
			callCount++
			if callCount == 2 {
				return &testutil.FakeCommand{
					CombinedOutputFunc: func() ([]byte, error) {
						return []byte("Author identity unknown"), commitErr
					},
				}
			}
			return &testutil.FakeCommand{}
		},
	}
	err := GitCommitAll(fos, "/repo", "msg")
	require.Error(t, err)
	assert.ErrorIs(t, err, commitErr)
}

// ── SetGitIdentity ───────────────────────────────────────────────────────────

func TestSetGitIdentity_Success(t *testing.T) {
	var cmds [][]string
	fos := &testutil.FakeOsProvider{
		CommandFunc: func(name string, args ...string) core.CommandApi {
			cmds = append(cmds, append([]string{name}, args...))
			return &testutil.FakeCommand{}
		},
	}
	err := SetGitIdentity(fos, "/repo", "keyop", "keyop@localhost")
	require.NoError(t, err)
	require.Len(t, cmds, 2)
	assert.Equal(t, []string{"git", "-C", "/repo", "config", "user.name", "keyop"}, cmds[0])
	assert.Equal(t, []string{"git", "-C", "/repo", "config", "user.email", "keyop@localhost"}, cmds[1])
}

func TestSetGitIdentity_NameFails(t *testing.T) {
	nameErr := errors.New("exit status 1")
	fos := &testutil.FakeOsProvider{
		CommandFunc: func(name string, args ...string) core.CommandApi {
			return &testutil.FakeCommand{
				CombinedOutputFunc: func() ([]byte, error) {
					return []byte("fatal: not a git repository"), nameErr
				},
			}
		},
	}
	err := SetGitIdentity(fos, "/repo", "keyop", "keyop@localhost")
	require.Error(t, err)
	assert.ErrorIs(t, err, nameErr)
}

func TestSetGitIdentity_EmailFails(t *testing.T) {
	emailErr := errors.New("exit status 1")
	callCount := 0
	fos := &testutil.FakeOsProvider{
		CommandFunc: func(name string, args ...string) core.CommandApi {
			callCount++
			if callCount == 2 {
				return &testutil.FakeCommand{
					CombinedOutputFunc: func() ([]byte, error) {
						return []byte("fatal: not a git repository"), emailErr
					},
				}
			}
			return &testutil.FakeCommand{}
		},
	}
	err := SetGitIdentity(fos, "/repo", "keyop", "keyop@localhost")
	require.Error(t, err)
	assert.ErrorIs(t, err, emailErr)
}
