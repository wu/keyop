package core_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/wu/keyop/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A nil Notify must not appear in the encoded alert at all: messages already on
// disk in ~/.keyop/msgs were written without these fields, and every producer that
// has not been updated yet still emits alerts without them.
func TestAlertEvent_RoundTrip_NotifyNil(t *testing.T) {
	ts := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	evt := core.AlertEvent{Timestamp: ts, Summary: "s", Text: "t", Level: "warning"}

	raw, err := json.Marshal(evt)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "notify")
	assert.NotContains(t, string(raw), "category")

	var got core.AlertEvent
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, evt, got)
	assert.Nil(t, got.Notify)
}

// An alert encoded before Notify and Category existed must still decode.
func TestAlertEvent_DecodesLegacyJSON(t *testing.T) {
	legacy := `{"timestamp":"2026-08-20T12:00:00Z","hostname":"host1","summary":"s","text":"t","level":"critical","link":"http://example.com"}`

	var got core.AlertEvent
	require.NoError(t, json.Unmarshal([]byte(legacy), &got))

	assert.Equal(t, "critical", got.Level)
	assert.Equal(t, "http://example.com", got.Link)
	assert.Empty(t, got.Category)
	assert.Nil(t, got.Notify)
}

// An empty (but non-nil) hints struct is meaningful: it says "I have no opinion",
// which is what a rule that has not fired yet leaves behind. It must survive the
// round trip without collapsing to nil.
func TestAlertEvent_RoundTrip_NotifyEmpty(t *testing.T) {
	evt := core.AlertEvent{Summary: "s", Text: "t", Notify: &core.NotifyHints{}}

	raw, err := json.Marshal(evt)
	require.NoError(t, err)

	var got core.AlertEvent
	require.NoError(t, json.Unmarshal(raw, &got))
	require.NotNil(t, got.Notify)
	assert.Equal(t, core.NotifyHints{}, *got.Notify)
}

func TestAlertEvent_RoundTrip_NotifyPopulated(t *testing.T) {
	evt := core.AlertEvent{
		Timestamp: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
		Hostname:  "host1",
		Summary:   "s",
		Text:      "t",
		Level:     "warning",
		Category:  "tide",
		Notify: &core.NotifyHints{
			Audio:   core.AudioSound,
			Sound:   "chime",
			SMS:     core.BoolPtr(false),
			Persist: core.BoolPtr(true),
			Color:   "orange",
		},
	}

	raw, err := json.Marshal(evt)
	require.NoError(t, err)

	var got core.AlertEvent
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, evt, got)
}

// false is not the same as unset: with omitempty on a *bool the pointer survives,
// which is the whole reason the tri-state fields are pointers.
func TestNotifyHints_FalseSurvivesRoundTrip(t *testing.T) {
	raw, err := json.Marshal(core.NotifyHints{SMS: core.BoolPtr(false)})
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"sms":false`)

	var got core.NotifyHints
	require.NoError(t, json.Unmarshal(raw, &got))
	require.NotNil(t, got.SMS)
	assert.False(t, *got.SMS)
}

func TestNotifyHints_AudioMode(t *testing.T) {
	tests := []struct {
		name  string
		hints *core.NotifyHints
		want  string
	}{
		{"nil hints", nil, ""},
		{"empty hints", &core.NotifyHints{}, ""},
		{"explicit speak", &core.NotifyHints{Audio: core.AudioSpeak}, core.AudioSpeak},
		{"explicit silent", &core.NotifyHints{Audio: core.AudioSilent}, core.AudioSilent},
		{"sound name implies sound mode", &core.NotifyHints{Sound: "chime"}, core.AudioSound},
		{"explicit mode wins over implication", &core.NotifyHints{Audio: core.AudioSpeak, Sound: "chime"}, core.AudioSpeak},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.hints.AudioMode())
		})
	}
}

func TestNotifyHints_Validate_Valid(t *testing.T) {
	tests := []struct {
		name  string
		hints *core.NotifyHints
	}{
		{"nil", nil},
		{"empty", &core.NotifyHints{}},
		{"speak only", &core.NotifyHints{Audio: core.AudioSpeak}},
		{"silent only", &core.NotifyHints{Audio: core.AudioSilent}},
		{"sound with mode", &core.NotifyHints{Audio: core.AudioSound, Sound: "front_door-2"}},
		{"sound without mode", &core.NotifyHints{Sound: "chime"}},
		{"every field", &core.NotifyHints{
			Audio:   core.AudioSound,
			Sound:   "chime",
			SMS:     core.BoolPtr(true),
			Persist: core.BoolPtr(false),
			Color:   "red",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Empty(t, tt.hints.Validate())
		})
	}
}

func TestNotifyHints_Validate_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		hints core.NotifyHints
		want  string
	}{
		{"unknown audio mode", core.NotifyHints{Audio: "shout"}, `invalid audio mode "shout"`},
		{"unknown color", core.NotifyHints{Color: "chartreuse"}, `invalid color "chartreuse"`},
		{"grey is not gray", core.NotifyHints{Color: "grey"}, `invalid color "grey"`},
		{"sound contradicts audio", core.NotifyHints{Audio: core.AudioSpeak, Sound: "chime"}, `not "sound"`},
		{"sound path traversal", core.NotifyHints{Sound: "../../etc/passwd"}, `invalid sound name`},
		{"sound with separator", core.NotifyHints{Sound: "sub/chime"}, `invalid sound name`},
		{"sound with extension", core.NotifyHints{Sound: "chime.wav"}, `invalid sound name`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.hints.Validate()
			require.NotEmpty(t, errs)
			assert.Contains(t, errs[0].Error(), tt.want)
		})
	}
}

// Validate reports every problem at once so a bad config surfaces in one pass.
func TestNotifyHints_Validate_ReportsAllErrors(t *testing.T) {
	hints := core.NotifyHints{Audio: "shout", Sound: "bad name", Color: "chartreuse"}
	assert.Len(t, hints.Validate(), 4)
}

func TestNotifyColors(t *testing.T) {
	colors := core.NotifyColors()
	require.NotEmpty(t, colors)
	assert.IsIncreasing(t, colors)
	for _, name := range colors {
		assert.True(t, core.ValidNotifyColor(name), "NotifyColors listed %q but ValidNotifyColor rejects it", name)
	}
	assert.False(t, core.ValidNotifyColor(""))
	assert.False(t, core.ValidNotifyColor("chartreuse"))

	// The returned slice is a copy: mutating it must not corrupt the vocabulary.
	colors[0] = "mutated"
	assert.False(t, core.ValidNotifyColor("mutated"))
	assert.Equal(t, core.NotifyColors(), core.NotifyColors())
}

func TestValidAudioMode(t *testing.T) {
	assert.True(t, core.ValidAudioMode(core.AudioSpeak))
	assert.True(t, core.ValidAudioMode(core.AudioSound))
	assert.True(t, core.ValidAudioMode(core.AudioSilent))
	assert.False(t, core.ValidAudioMode(""))
	assert.False(t, core.ValidAudioMode("Speak"))
}

func TestBoolPtr(t *testing.T) {
	assert.True(t, *core.BoolPtr(true))
	assert.False(t, *core.BoolPtr(false))
	// Each call must yield an independent pointer.
	a, b := core.BoolPtr(true), core.BoolPtr(true)
	assert.NotSame(t, a, b)
}
