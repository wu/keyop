package util

import (
	"keyop/core"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_GetShortHostname_TrimsDomain(t *testing.T) {
	short, err := GetShortHostname(core.FakeOsProvider{Host: "host.example.com"})
	assert.NoError(t, err, "GetShortHostname should not return an error")
	assert.Equal(t, "host", short)
}

func Test_GetShortHostname_NoDomain(t *testing.T) {
	short, err := GetShortHostname(core.FakeOsProvider{Host: "myhost"})
	assert.NoError(t, err)
	assert.Equal(t, "myhost", short)
}

func Test_GetShortHostname_PropagatesError(t *testing.T) {
	_, err := GetShortHostname(core.FakeOsProvider{Host: "", Err: assert.AnError})
	assert.Error(t, err)
}
