package util

import (
	"fmt"
	"keyop/core"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateConfig(t *testing.T) {
	type args struct {
		pubSubType  string
		chanInfoMap map[string]core.ChannelInfo
		required    []string
	}
	tests := []struct {
		name string
		args args
		want []error
	}{
		{
			name: "nil chanInfoMap",
			args: args{
				pubSubType:  "pub",
				chanInfoMap: nil,
				required:    []string{"foo"},
			},
			want: []error{fmt.Errorf("required field '%s' is empty", "pub")},
		},
		{
			name: "missing required channel",
			args: args{
				pubSubType:  "sub",
				chanInfoMap: map[string]core.ChannelInfo{},
				required:    []string{"bar"},
			},
			want: []error{fmt.Errorf("required %s channel '%s' is missing", "sub", "bar")},
		},
		{
			name: "required channel missing name",
			args: args{
				pubSubType: "pub",
				chanInfoMap: map[string]core.ChannelInfo{
					"baz": {Name: ""},
				},
				required: []string{"baz"},
			},
			want: []error{fmt.Errorf("required %s channel '%s' is missing a name", "pub", "baz")},
		},
		{
			name: "all required channels present and valid",
			args: args{
				pubSubType: "pub",
				chanInfoMap: map[string]core.ChannelInfo{
					"foo": {Name: "foo-chan"},
					"bar": {Name: "bar-chan"},
				},
				required: []string{"foo", "bar"},
			},
			want: nil,
		},
		{
			name: "some required channels missing, some missing name",
			args: args{
				pubSubType: "pub",
				chanInfoMap: map[string]core.ChannelInfo{
					"foo": {Name: ""},
				},
				required: []string{"foo", "bar"},
			},
			want: []error{
				fmt.Errorf("required %s channel '%s' is missing a name", "pub", "foo"),
				fmt.Errorf("required %s channel '%s' is missing", "pub", "bar"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateConfig(tt.args.pubSubType, tt.args.chanInfoMap, tt.args.required)
			assert.Equalf(t, len(tt.want), len(got), "error count mismatch")
			for i := range tt.want {
				assert.EqualError(t, got[i], tt.want[i].Error())
			}
		})
	}
}
