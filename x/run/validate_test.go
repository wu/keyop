package run

import (
	"bytes"
	"fmt"
	"keyop/core"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeService struct {
	validateErrs []error
}

func (f *fakeService) Initialize() error       { return nil }
func (f *fakeService) Check() error            { return nil }
func (f *fakeService) ValidateConfig() []error { return f.validateErrs }
func (f *fakeService) String() string          { return "fakeService" }

func Test_validateServiceConfig(t *testing.T) {

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	tests := []struct {
		name     string
		services []ServiceWrapper
		wantErr  assert.ErrorAssertionFunc
		logMsgs  []string
	}{
		{
			name: "missing name",
			services: []ServiceWrapper{{
				Service: &fakeService{},
				Config:  core.ServiceConfig{Type: "foo"},
			}},
			wantErr: assert.Error,
			logMsgs: []string{"service config is missing the required field 'name'"},
		},
		{
			name: "missing type",
			services: []ServiceWrapper{{
				Service: &fakeService{},
				Config:  core.ServiceConfig{Name: "svc"},
			}},
			wantErr: assert.Error,
			logMsgs: []string{"service config is missing the required field 'type'"},
		},
		{
			name: "validate config returns error",
			services: []ServiceWrapper{{
				Service: &fakeService{validateErrs: []error{fmt.Errorf("bad config")}},
				Config:  core.ServiceConfig{Name: "svc", Type: "foo"},
			}},
			wantErr: assert.NoError,
			logMsgs: []string{"service config validation error"},
		},
		{
			name: "valid config",
			services: []ServiceWrapper{{
				Service: &fakeService{},
				Config:  core.ServiceConfig{Name: "svc", Type: "foo"},
			}},
			wantErr: assert.NoError,
			logMsgs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			err := validateServiceConfig(tt.services, logger)
			tt.wantErr(t, err, fmt.Sprintf("validateServiceConfig(%v, %v)", tt.services, logger))
			logs := buf.String()
			for _, msg := range tt.logMsgs {
				assert.Contains(t, logs, msg)
			}
		})
	}
}
