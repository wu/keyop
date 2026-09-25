package core_test

import (
	"reflect"
	"testing"

	"github.com/wu/keyop/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const taggedType = "test.tagged.v1"

// tagged stands in for a payload with a list field and string fields to read from,
// shaped like the mail service's email event.
type tagged struct {
	Mailbox   string   `json:"mailbox"`
	Recipient string   `json:"recipient"`
	Count     int      `json:"count"`
	Tags      []string `json:"tags,omitempty"`
}

func taggedLookup(payloadType string) (reflect.Type, bool) {
	if payloadType == taggedType {
		return reflect.TypeOf(tagged{}), true
	}
	return nil, false
}

func taggedRules(t *testing.T, specs ...core.RuleSpec) []core.Rule {
	t.Helper()
	rules, errs := core.ParseRules("test", specs)
	require.Empty(t, errs)
	return rules
}

// mailRules are the two rules the mail service is configured with: tag by folder,
// and tag by the alias in a foo-c@geekfarm.org recipient.
func mailRules(t *testing.T) []core.Rule {
	return taggedRules(t,
		core.RuleSpec{Type: taggedType, Add: map[string]any{"tags": []any{"${mailbox}"}}},
		core.RuleSpec{
			Type: taggedType,
			When: map[string]any{"recipient": map[string]any{"matches": `(?i)^(?P<alias>[^\s<>,@"]+)-c@geekfarm\.org$`}},
			Add:  map[string]any{"tags": []any{"${match.alias}"}},
		},
	)
}

func TestApplyRules_Add_TagsFromFieldAndCapture(t *testing.T) {
	out, changes, errs := core.ApplyRules(taggedType,
		tagged{Mailbox: "jobs", Recipient: "amazon-c@geekfarm.org"}, mailRules(t))

	require.Empty(t, errs)
	assert.Equal(t, []string{"jobs", "amazon"}, out.(tagged).Tags)
	require.Len(t, changes, 2)
	assert.True(t, changes[0].Added)
	assert.False(t, changes[0].Forced)
	assert.Equal(t, 0, changes[0].RuleIndex)
	assert.Equal(t, 1, changes[1].RuleIndex)
}

func TestApplyRules_Add_CaptureRuleSkippedWhenRecipientDoesNotMatch(t *testing.T) {
	for _, recipient := range []string{"", "someone@example.com", "amazon@geekfarm.org", "-c@geekfarm.org"} {
		out, _, errs := core.ApplyRules(taggedType, tagged{Mailbox: "inbox", Recipient: recipient}, mailRules(t))
		require.Empty(t, errs, recipient)
		assert.Equal(t, []string{"inbox"}, out.(tagged).Tags, "recipient %q", recipient)
	}
}

func TestApplyRules_Add_CaptureKeepsHyphensInAlias(t *testing.T) {
	out, _, errs := core.ApplyRules(taggedType,
		tagged{Mailbox: "ads", Recipient: "my-shop-c@geekfarm.org"}, mailRules(t))
	require.Empty(t, errs)
	assert.Equal(t, []string{"ads", "my-shop"}, out.(tagged).Tags)
}

func TestApplyRules_Add_SkipsValuesAlreadyPresent(t *testing.T) {
	out, changes, errs := core.ApplyRules(taggedType,
		tagged{Mailbox: "jobs", Tags: []string{"jobs"}}, mailRules(t))
	require.Empty(t, errs)
	assert.Empty(t, changes, "adding a tag that is already there is not a change")
	assert.Equal(t, []string{"jobs"}, out.(tagged).Tags)
}

func TestApplyRules_Add_PreservesExistingElements(t *testing.T) {
	out, _, errs := core.ApplyRules(taggedType,
		tagged{Mailbox: "jobs", Tags: []string{"starred"}}, mailRules(t))
	require.Empty(t, errs)
	assert.Equal(t, []string{"starred", "jobs"}, out.(tagged).Tags)
}

func TestApplyRules_Add_LiteralValuesAndScalarForm(t *testing.T) {
	rules := taggedRules(t,
		core.RuleSpec{Type: taggedType, Add: map[string]any{"tags": []any{"a", "b", "a"}}},
		core.RuleSpec{Type: taggedType, Add: map[string]any{"tags": "c"}},
	)
	out, _, errs := core.ApplyRules(taggedType, tagged{}, rules)
	require.Empty(t, errs)
	assert.Equal(t, []string{"a", "b", "c"}, out.(tagged).Tags)
}

func TestApplyRules_Add_DoesNotMutateCaller(t *testing.T) {
	// Spare capacity is the dangerous case: appending in place would write into
	// the caller's backing array without changing its length.
	backing := make([]string, 1, 4)
	backing[0] = "kept"
	in := &tagged{Mailbox: "jobs", Tags: backing}

	out, _, errs := core.ApplyRules(taggedType, in, mailRules(t))
	require.Empty(t, errs)

	assert.Equal(t, []string{"kept", "jobs"}, out.(*tagged).Tags)
	assert.Equal(t, []string{"kept"}, in.Tags)
	assert.Equal(t, "", backing[:2][1], "the caller's backing array was written to")
}

func TestApplyRules_Interpolation_EmptyExpansionSkipsTheWrite(t *testing.T) {
	// No mailbox: the tag would be empty, so nothing is added.
	out, changes, errs := core.ApplyRules(taggedType, tagged{}, mailRules(t))
	require.Empty(t, errs)
	assert.Empty(t, changes)
	assert.Empty(t, out.(tagged).Tags)
}

func TestApplyRules_Interpolation_EmptyItemsDroppedFromList(t *testing.T) {
	rules := taggedRules(t, core.RuleSpec{
		Type: taggedType,
		Add:  map[string]any{"tags": []any{"${mailbox}", "${recipient}", "fixed"}},
	})
	out, _, errs := core.ApplyRules(taggedType, tagged{Recipient: "who"}, rules)
	require.Empty(t, errs)
	assert.Equal(t, []string{"who", "fixed"}, out.(tagged).Tags)
}

func TestApplyRules_Interpolation_InSetAndForce(t *testing.T) {
	rules := taggedRules(t,
		core.RuleSpec{Type: taggedType, Set: map[string]any{"recipient": "${mailbox}@example.org"}},
		core.RuleSpec{Type: taggedType, Force: map[string]any{"mailbox": "moved-${count}"}},
	)
	out, _, errs := core.ApplyRules(taggedType, tagged{Mailbox: "jobs", Count: 3}, rules)
	require.Empty(t, errs)
	assert.Equal(t, "jobs@example.org", out.(tagged).Recipient, "references read the payload as published")
	assert.Equal(t, "moved-3", out.(tagged).Mailbox)
}

func TestApplyRules_Interpolation_LiteralDollarIsKept(t *testing.T) {
	rules := taggedRules(t, core.RuleSpec{Type: taggedType, Set: map[string]any{"recipient": "cost $5 or $ {x}"}})
	out, _, errs := core.ApplyRules(taggedType, tagged{}, rules)
	require.Empty(t, errs)
	assert.Equal(t, "cost $5 or $ {x}", out.(tagged).Recipient)
}

func TestApplyRules_Interpolation_MalformedTemplateIsReported(t *testing.T) {
	// Validation rejects these at startup; ApplyRules must still not panic if
	// handed one, and must leave the payload alone.
	for _, tmpl := range []string{"${mailbox", "${}"} {
		rules := taggedRules(t, core.RuleSpec{Type: taggedType, Add: map[string]any{"tags": tmpl}})
		out, changes, errs := core.ApplyRules(taggedType, tagged{Mailbox: "jobs"}, rules)
		require.Len(t, errs, 1, tmpl)
		assert.Empty(t, changes)
		assert.Empty(t, out.(tagged).Tags)
	}
}

func TestApplyRules_Add_ToNonListIsReported(t *testing.T) {
	rules := taggedRules(t, core.RuleSpec{Type: taggedType, Add: map[string]any{"mailbox": "x"}})
	_, changes, errs := core.ApplyRules(taggedType, tagged{}, rules)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "not a list")
	assert.Empty(t, changes)
}

// --- validation ------------------------------------------------------------

func TestValidateRules_Add(t *testing.T) {
	tests := []struct {
		name string
		spec core.RuleSpec
		want string // empty: valid
	}{
		{"add alone is enough", core.RuleSpec{Type: taggedType, Add: map[string]any{"tags": []any{"x"}}}, ""},
		{"field reference", core.RuleSpec{Type: taggedType, Add: map[string]any{"tags": []any{"${mailbox}"}}}, ""},
		{"numeric field reference", core.RuleSpec{Type: taggedType, Add: map[string]any{"tags": []any{"n${count}"}}}, ""},
		{"add to a string field", core.RuleSpec{Type: taggedType, Add: map[string]any{"mailbox": "x"}}, "not a list"},
		{"add to an unknown field", core.RuleSpec{Type: taggedType, Add: map[string]any{"nope": "x"}}, "no such field"},
		{"unknown field reference", core.RuleSpec{Type: taggedType, Add: map[string]any{"tags": "${nope}"}}, "not a field of"},
		{"list field reference", core.RuleSpec{Type: taggedType, Add: map[string]any{"tags": "${tags}"}}, "only strings, numbers and bools"},
		{"unterminated reference", core.RuleSpec{Type: taggedType, Add: map[string]any{"tags": "${mailbox"}}, "unterminated"},
		{"empty reference", core.RuleSpec{Type: taggedType, Add: map[string]any{"tags": "${}"}}, "empty ${}"},
		{"group reference without a matches condition", core.RuleSpec{Type: taggedType, Add: map[string]any{"tags": "${match.alias}"}}, `no named group "alias"`},
		{
			"group reference to an unnamed group",
			core.RuleSpec{
				Type: taggedType,
				When: map[string]any{"recipient": map[string]any{"matches": `^(x)`}},
				Add:  map[string]any{"tags": "${match.alias}"},
			},
			`no named group "alias"`,
		},
		{
			"group reference resolves",
			core.RuleSpec{
				Type: taggedType,
				When: map[string]any{"recipient": map[string]any{"matches": `^(?P<alias>x)`}},
				Add:  map[string]any{"tags": "${match.alias}"},
			},
			"",
		},
		{
			"same group name in two conditions",
			core.RuleSpec{
				Type: taggedType,
				When: map[string]any{
					"recipient": map[string]any{"matches": `^(?P<alias>x)`},
					"mailbox":   map[string]any{"matches": `^(?P<alias>y)`},
				},
				Add: map[string]any{"tags": "${match.alias}"},
			},
			`named group "alias" is defined by more than one`,
		},
		{
			"same path in set and add",
			core.RuleSpec{Type: taggedType, Set: map[string]any{"tags": []any{"a"}}, Add: map[string]any{"tags": []any{"b"}}},
			"appears in both 'set' and 'add'",
		},
		{
			"same path in force and add",
			core.RuleSpec{Type: taggedType, Force: map[string]any{"tags": []any{"a"}}, Add: map[string]any{"tags": []any{"b"}}},
			"appears in both 'force' and 'add'",
		},
		{"nothing to do", core.RuleSpec{Type: taggedType}, "would change nothing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules, parseErrs := core.ParseRules("test", []core.RuleSpec{tt.spec})
			require.Empty(t, parseErrs)
			errs := core.ValidateRules("test", rules, taggedLookup)
			if tt.want == "" {
				assert.Empty(t, errs)
				return
			}
			require.NotEmpty(t, errs)
			assert.Contains(t, errs[0].Error(), tt.want)
		})
	}
}

func TestValidateRules_MailRulesAreValid(t *testing.T) {
	assert.Empty(t, core.ValidateRules("test", mailRules(t), taggedLookup))
}
