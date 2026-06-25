package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	km "github.com/wu/keyop-messenger"
)

// recordingMessenger records what Subscribe received. Only Subscribe is used by
// these tests, so the embedded MessengerApi is left nil.
type recordingMessenger struct {
	MessengerApi
	lastChannel string
	lastOptsLen int
	calls       int
}

func (r *recordingMessenger) Subscribe(_ context.Context, channel, _ string, _ km.HandlerFunc, opts ...km.SubscribeOption) error {
	r.calls++
	r.lastChannel = channel
	r.lastOptsLen = len(opts)
	return nil
}

func TestChannelInfo_SubscribeOptions(t *testing.T) {
	zero := 0
	three := 3
	cases := []struct {
		name string
		ci   ChannelInfo
		want int
	}{
		{"none", ChannelInfo{}, 0},
		{"maxAge", ChannelInfo{MaxAge: time.Minute}, 1},
		{"explicit zero retries counts", ChannelInfo{MaxRetries: &zero}, 1},
		{"backoff base only", ChannelInfo{RetryBackoffBase: time.Second}, 1},
		{"backoff max only", ChannelInfo{RetryBackoffMax: time.Second}, 1},
		{"all three", ChannelInfo{MaxAge: time.Minute, MaxRetries: &three, RetryBackoffBase: time.Second}, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Len(t, c.ci.SubscribeOptions(), c.want)
		})
	}
}

func TestConfigMessenger_AppliesConfiguredOptions(t *testing.T) {
	rec := &recordingMessenger{}
	n := 2
	subs := map[string]ChannelInfo{
		"events": {Name: "events-ch", MaxAge: time.Minute, MaxRetries: &n}, // 2 options
		"plain":  {Name: "plain-ch"},                                       // 0 options → not indexed
	}
	m := NewConfigMessenger(rec, subs)

	// Configured channel: 2 config options prepended to 1 caller option = 3.
	require.NoError(t, m.Subscribe(context.Background(), "events-ch", "sub", nil, km.WithMaxAge(time.Hour)))
	assert.Equal(t, "events-ch", rec.lastChannel)
	assert.Equal(t, 3, rec.lastOptsLen)

	// Configured channel with no caller options: just the 2 config options.
	require.NoError(t, m.Subscribe(context.Background(), "events-ch", "sub", nil))
	assert.Equal(t, 2, rec.lastOptsLen)

	// Channel configured but with no options: passes through unchanged.
	require.NoError(t, m.Subscribe(context.Background(), "plain-ch", "sub", nil))
	assert.Equal(t, 0, rec.lastOptsLen)

	// Channel not in the config at all: passes through unchanged.
	require.NoError(t, m.Subscribe(context.Background(), "unknown-ch", "sub", nil))
	assert.Equal(t, 0, rec.lastOptsLen)
}
