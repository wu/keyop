package core

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Compile-time checks that implementations satisfy the interface
var _ OsProviderIface = OsProvider{}
var _ OsProviderIface = FakeOsProvider{}

func TestOsProvider_Hostname_MatchesStdlib(t *testing.T) {
	want, wantErr := os.Hostname()

	got, err := (OsProvider{}).Hostname()

	// Both should succeed or fail the same way; in practice stdlib shouldn't error
	if wantErr != nil {
		assert.Error(t, err)
		return
	}

	assert.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestFakeOsProvider_Hostname_ReturnsProvidedHost(t *testing.T) {
	f := FakeOsProvider{Host: "example-host"}
	got, err := f.Hostname()
	assert.NoError(t, err)
	assert.Equal(t, "example-host", got)
}

func TestFakeOsProvider_Hostname_PropagatesError(t *testing.T) {
	testErr := assert.AnError
	f := FakeOsProvider{Host: "ignored-host", Err: testErr}
	got, err := f.Hostname()
	assert.ErrorIs(t, err, testErr)
	// ensure host value is returned alongside error (documented behavior in type)
	assert.Equal(t, "ignored-host", got)
}
