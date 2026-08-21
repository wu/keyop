package runtime

import (
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/adapter"
	"github.com/wu/keyop/core/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rulesTestDeps(t *testing.T, dir string) core.Dependencies {
	t.Helper()
	t.Setenv("KEYOP_CONF_DIR", dir)

	deps := core.Dependencies{}
	deps.SetLogger(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	deps.SetOsProvider(adapter.OsProvider{})
	return deps
}

func writeRuleConf(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
}

func Test_loadServices_rules_loaded(t *testing.T) {
	dir := t.TempDir()
	deps := rulesTestDeps(t, dir)

	writeRuleConf(t, dir, "speak.yaml", `service: speak
pub_rules:
  - type: core.alert.v1
    when:
      level: warning
    set:
      notify.audio: silent
      category: tide
sub_rules:
  - type: core.alert.v1
    when:
      level: [warning, critical]
      summary: {contains: "disk"}
    force:
      notify.audio: speak
`)

	svcs, err := loadServiceConfigs(deps)
	require.NoError(t, err)
	require.Len(t, svcs, 1)

	require.Len(t, svcs[0].PubRules, 1)
	pub := svcs[0].PubRules[0]
	assert.Equal(t, "core.alert.v1", pub.PayloadType)
	assert.Equal(t, core.OpEq, pub.When["level"].Op)
	assert.Equal(t, "silent", pub.Set["notify.audio"])
	assert.Equal(t, "tide", pub.Set["category"])
	assert.Empty(t, pub.Force)

	require.Len(t, svcs[0].SubRules, 1)
	sub := svcs[0].SubRules[0]
	assert.Equal(t, core.OpIn, sub.When["level"].Op)
	assert.Equal(t, core.OpContains, sub.When["summary"].Op)
	assert.Equal(t, "speak", sub.Force["notify.audio"])
	assert.Empty(t, sub.Set)
}

// A service with no rules must keep working exactly as before.
func Test_loadServices_no_rules(t *testing.T) {
	dir := t.TempDir()
	deps := rulesTestDeps(t, dir)
	writeRuleConf(t, dir, "speak.yaml", "service: speak\nfreq: 1s\n")

	svcs, err := loadServiceConfigs(deps)
	require.NoError(t, err)
	require.Len(t, svcs, 1)
	assert.Nil(t, svcs[0].PubRules)
	assert.Nil(t, svcs[0].SubRules)
}

// A condition that cannot be parsed stops startup at config load, the same way a
// bad max_age does. The previous implementation skipped such rules silently.
func Test_loadServices_bad_rule_fails_load(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "unknown operator",
			body: "service: speak\npub_rules:\n  - type: core.alert.v1\n    when:\n      level: {startsWith: crit}\n    set:\n      category: x\n",
		},
		{
			name: "invalid regexp",
			body: "service: speak\nsub_rules:\n  - type: core.alert.v1\n    when:\n      summary: {matches: \"([unclosed\"}\n    set:\n      category: x\n",
		},
		{
			name: "empty candidate list",
			body: "service: speak\npub_rules:\n  - type: core.alert.v1\n    when:\n      level: []\n    set:\n      category: x\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			deps := rulesTestDeps(t, dir)
			writeRuleConf(t, dir, "speak.yaml", tt.body)

			_, err := loadServiceConfigs(deps)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid rules in service config")
		})
	}
}

// --- validation -------------------------------------------------------------

func alertOnlyLookup(payloadType string) (reflect.Type, bool) {
	if payloadType == "core.alert.v1" {
		return reflect.TypeOf(core.AlertEvent{}), true
	}
	return nil, false
}

func wrapperWithRules(name string, pub, sub []core.Rule) ServiceWrapper {
	return ServiceWrapper{
		Service: &fakeService{},
		Config:  core.ServiceConfig{Name: name, Type: "speak", PubRules: pub, SubRules: sub},
	}
}

func Test_validateServiceConfig_rules(t *testing.T) {
	good := core.Rule{
		PayloadType: "core.alert.v1",
		Force:       map[string]any{"notify.audio": core.AudioSilent},
	}

	tests := []struct {
		name    string
		pub     []core.Rule
		sub     []core.Rule
		wantErr bool
	}{
		{name: "no rules", wantErr: false},
		{name: "valid pub rule", pub: []core.Rule{good}, wantErr: false},
		{name: "valid sub rule", sub: []core.Rule{good}, wantErr: false},
		{
			name:    "field that does not exist",
			pub:     []core.Rule{{PayloadType: "core.alert.v1", Force: map[string]any{"notify.audi": "speak"}}},
			wantErr: true,
		},
		{
			name:    "value the field cannot hold",
			pub:     []core.Rule{{PayloadType: "core.alert.v1", Force: map[string]any{"level": 3}}},
			wantErr: true,
		},
		{
			name:    "payload type nothing registers",
			sub:     []core.Rule{{PayloadType: "service.nope.v1", Force: map[string]any{"category": "x"}}},
			wantErr: true,
		},
		{
			name:    "rule that changes nothing",
			sub:     []core.Rule{{PayloadType: "core.alert.v1"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateServiceConfig(
				[]ServiceWrapper{wrapperWithRules("speak", tt.pub, tt.sub)},
				alertOnlyLookup,
				slog.New(slog.NewJSONHandler(os.Stdout, nil)))
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Without a messenger there is no registry to check against, but the structural
// checks must still run so an obviously broken rule is not accepted.
func Test_validateServiceConfig_rules_nilLookup(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	badPath := []core.Rule{{PayloadType: "core.alert.v1", Force: map[string]any{"nosuchfield": "x"}}}
	assert.NoError(t, validateServiceConfig([]ServiceWrapper{wrapperWithRules("speak", badPath, nil)}, nil, logger),
		"field paths cannot be checked without a registry")

	noWrites := []core.Rule{{PayloadType: "core.alert.v1"}}
	assert.Error(t, validateServiceConfig([]ServiceWrapper{wrapperWithRules("speak", noWrites, nil)}, nil, logger),
		"structural checks must still run")
}

// The lookup is the messenger's own registry, so a payload type registered by a
// service resolves for rules that name it.
func Test_payloadPrototypeLookup(t *testing.T) {
	deps := core.Dependencies{}
	assert.Nil(t, payloadPrototypeLookup(deps), "no messenger means no lookup")

	msgr := testutil.NewFakeMessenger()
	require.NoError(t, msgr.RegisterPayloadType("core.alert.v1", &core.AlertEvent{}))
	deps.SetMessenger(msgr)

	lookup := payloadPrototypeLookup(deps)
	require.NotNil(t, lookup)

	got, ok := lookup("core.alert.v1")
	require.True(t, ok)
	assert.Equal(t, reflect.TypeOf(core.AlertEvent{}), got, "a pointer registration resolves to the element type")

	_, ok = lookup("service.nope.v1")
	assert.False(t, ok)
}
