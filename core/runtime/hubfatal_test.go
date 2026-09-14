package runtime

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wu/keyop/core/testutil"
)

func TestHubFatalShutdown_NoFailure(t *testing.T) {
	h := newHubFatalShutdown(&testutil.FakeLogger{}, func() { t.Fatal("cancel must not be called") })
	assert.NoError(t, h.err())
}

func TestHubFatalShutdown_RecordsFirstFailureAndCancelsOnce(t *testing.T) {
	cancels := 0
	h := newHubFatalShutdown(&testutil.FakeLogger{}, func() { cancels++ })

	first := errors.New("rpc error: code = PermissionDenied desc = not in allowlist")
	h.handle("khub.example.org:7740", first)
	h.handle("other.example.org:7740", errors.New("second failure"))

	assert.Equal(t, 1, cancels, "shutdown is triggered once")
	err := h.err()
	require.Error(t, err)
	assert.ErrorIs(t, err, first, "the first failure is kept")
	assert.Contains(t, err.Error(), "khub.example.org:7740")
}
