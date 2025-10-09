package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_GetShortHostname(t *testing.T) {
	short, err := GetShortHostname()
	assert.NoError(t, err, "GetShortHostname should not return an error")
	assert.NotEmpty(t, short, "shortHostname should be present in heartbeat message")
	assert.NotContains(t, short, ".", "shortHostname should not be fully qualified")
}
