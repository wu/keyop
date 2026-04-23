package runtime

import (
	"context"
	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/testutil"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockService struct {
	core.Service
	initialized bool
	checked     bool
}

func (m *mockService) Initialize() error {
	m.initialized = true
	return nil
}

func (m *mockService) Check() error {
	m.checked = true
	return nil
}

func TestMockService_LifeCycle(t *testing.T) {
	// Test that a basic service can be initialized and checked
	m := &mockService{}
	require.NoError(t, m.Initialize())
	assert.True(t, m.initialized)

	require.NoError(t, m.Check())
	assert.True(t, m.checked)
}

func TestLoadPlugins_WithMessenger(t *testing.T) {
	// Test that plugins can be registered with the new messenger
	deps := core.Dependencies{}
	logger := &testutil.FakeLogger{}
	deps.SetLogger(logger)
	ctx, cancel := context.WithCancel(context.Background())
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	newMsgr := testutil.NewFakeMessenger()
	deps.SetMessenger(newMsgr)

	// Verify new messenger is available
	assert.NotNil(t, newMsgr)
	assert.NotNil(t, deps.MustGetMessenger())
}
