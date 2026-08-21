package core

import (
	"fmt"
	"regexp"
	"sort"
)

// Audio modes for NotifyHints.Audio.
//
// The zero value ("") means the sender expressed no opinion and the consumer
// applies its own default policy.
const (
	// AudioSpeak requests that the alert be read aloud (text-to-speech).
	AudioSpeak = "speak"
	// AudioSound requests that a named sound be played instead of speech.
	AudioSound = "sound"
	// AudioSilent requests that the alert produce no audio at all.
	AudioSilent = "silent"
)

// DefaultSoundsDir is the directory NotifyHints.Sound names are resolved against.
// Consumers expand it with ExpandHome and append the sound name plus whatever
// extension they support.
const DefaultSoundsDir = "~/.keyop/sounds"

// soundNamePattern restricts sound names to a bare identifier so a hint can never
// reference a path outside DefaultSoundsDir.
var soundNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// notifyColors is the fixed colour vocabulary for NotifyHints.Color. Colours are
// semantic names, not hex values: each consumer maps them to whatever its display
// can render. Extending the vocabulary means adding an entry here and teaching the
// consumers about it; an unrecognised colour is a configuration error, not a
// silent fallback.
var notifyColors = map[string]bool{
	"red":     true,
	"orange":  true,
	"yellow":  true,
	"green":   true,
	"cyan":    true,
	"blue":    true,
	"purple":  true,
	"magenta": true,
	"white":   true,
	"gray":    true,
}

// NotifyColors returns the fixed colour vocabulary, sorted. Intended for error
// messages, documentation and config validation.
func NotifyColors() []string {
	names := make([]string, 0, len(notifyColors))
	for name := range notifyColors {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidNotifyColor reports whether name is part of the fixed colour vocabulary.
func ValidNotifyColor(name string) bool { return notifyColors[name] }

// ValidAudioMode reports whether mode is one of the recognised audio modes.
func ValidAudioMode(mode string) bool {
	switch mode {
	case AudioSpeak, AudioSound, AudioSilent:
		return true
	}
	return false
}

// NotifyHints expresses how an alert should be delivered, separately from how
// severe it is. Severity stays in AlertEvent.Level; everything about presentation
// lives here.
//
// Every field's zero value means "no opinion": the consumer falls back to its own
// default policy for that one field. A nil *NotifyHints means no opinion about
// anything. This is what lets a sender state an opinion about a single aspect of
// delivery -- "never speak this" -- without having to specify the rest.
//
// Hints are set either by the sending service or by pub_rules/sub_rules in a
// service's YAML config, so producers do not have to hard-code delivery decisions.
type NotifyHints struct {
	// Audio selects between speech, a sound, and silence. One field rather than
	// separate flags because the options are alternatives: two independent
	// booleans could contradict each other.
	Audio string `json:"audio,omitempty"`
	// Sound names the sound to play when Audio is AudioSound. It is a bare name
	// resolved against DefaultSoundsDir by the consumer, never a path. Setting
	// Sound while Audio is unset implies AudioSound -- see AudioMode.
	Sound string `json:"sound,omitempty"`
	// SMS requests (true) or suppresses (false) delivery as a text message.
	// nil leaves the decision to the consumer.
	SMS *bool `json:"sms,omitempty"`
	// Persist requests a persistent notification that must be dismissed (true)
	// rather than a transient banner (false). nil leaves it to the consumer.
	Persist *bool `json:"persist,omitempty"`
	// Color is a semantic name from the fixed vocabulary (see NotifyColors), not
	// a hex value: a literal colour would bake one display's palette into an
	// event that fans out to several hosts.
	Color string `json:"color,omitempty"`
}

// AudioMode returns the effective audio mode for h, resolving the implication
// that naming a sound means playing it. Returns "" when the hints express no
// opinion about audio, including when h is nil.
func (h *NotifyHints) AudioMode() string {
	if h == nil {
		return ""
	}
	if h.Audio == "" && h.Sound != "" {
		return AudioSound
	}
	return h.Audio
}

// Validate reports every problem with the hints. A nil *NotifyHints is valid and
// returns no errors, as is a hints struct with no fields set.
func (h *NotifyHints) Validate() []error {
	if h == nil {
		return nil
	}
	var errs []error

	if h.Audio != "" && !ValidAudioMode(h.Audio) {
		errs = append(errs, fmt.Errorf("notify: invalid audio mode %q (valid: %s, %s, %s)",
			h.Audio, AudioSpeak, AudioSound, AudioSilent))
	}

	if h.Sound != "" {
		if !soundNamePattern.MatchString(h.Sound) {
			errs = append(errs, fmt.Errorf("notify: invalid sound name %q: must match %s",
				h.Sound, soundNamePattern))
		}
		// Sound with no audio mode implies AudioSound (see AudioMode), but an
		// explicit speak/silent alongside a sound name is a contradiction the
		// sender needs to resolve.
		if h.Audio != "" && h.Audio != AudioSound {
			errs = append(errs, fmt.Errorf("notify: sound %q set but audio is %q, not %q",
				h.Sound, h.Audio, AudioSound))
		}
	}

	if h.Color != "" && !ValidNotifyColor(h.Color) {
		errs = append(errs, fmt.Errorf("notify: invalid color %q (valid: %v)",
			h.Color, NotifyColors()))
	}

	return errs
}

// BoolPtr returns a pointer to b. It exists so the tri-state hint fields can be
// set inline: &NotifyHints{SMS: core.BoolPtr(false)}.
func BoolPtr(b bool) *bool { return &b }
