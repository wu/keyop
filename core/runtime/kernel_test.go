package runtime

import (
	"context"
	"fmt"
	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/testutil"
	"log/slog"
	"os"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
)

func getDefaultTestDeps() core.Dependencies {

	deps := core.Dependencies{}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps.SetLogger(logger)

	ctx, cancel := context.WithCancel(context.Background())
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	osProvider := testutil.FakeOsProvider{Host: "test-host"}
	deps.SetOsProvider(osProvider)
	deps.SetStateStore(&testutil.NoOpStateStore{})

	return deps
}

func TestStartKernelRunOneTask(t *testing.T) {

	deps := getDefaultTestDeps()
	logger := deps.MustGetLogger()
	ctx := deps.MustGetContext()

	svcCtx, svcCancel := context.WithCancel(ctx)
	logger.Info("initialized dependencies for test")

	taskCounter := 0
	tasks := []Task{{
		Name: "simple task that only runs one time",
		Run: func() error {
			taskCounter++
			logger.Info("fake task running", "taskCounter", taskCounter)
			return nil
		},
		Cancel: svcCancel,
		Ctx:    svcCtx,
	}}

	err := StartKernel(deps, tasks)
	assert.NoError(t, err)

	assert.Equal(t, 1, taskCounter, "task should have run one time")
}

func TestStartKernelGlobalCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {

		deps := getDefaultTestDeps()
		logger := deps.MustGetLogger()
		ctx := deps.MustGetContext()
		cancel := deps.MustGetCancel()

		svcCtx, svcCancel := context.WithCancel(ctx)
		logger.Info("initialized dependencies for test")

		loopCounter := 0
		tasks := []Task{{
			Name:     "simple task that runs a few times and then calls global cancel",
			Interval: time.Minute,
			Run: func() error {
				loopCounter++
				logger.Info("Fake task", "loopCounter", loopCounter)

				// shut it down after running 3 times
				if loopCounter >= 3 {
					cancel()
				}

				return nil
			},
			Cancel: svcCancel,
			Ctx:    svcCtx,
		}}

		err := StartKernel(deps, tasks)
		assert.NoError(t, err)

		assert.Equal(t, 3, loopCounter, "task should have run 5 times")
	})
}

func TestStartKernelErrorChannel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		deps := getDefaultTestDeps()
		ctx := deps.MustGetContext()
		cancel := deps.MustGetCancel()

		messenger := testutil.NewFakeMessenger()
		deps.SetMessenger(messenger)

		svcCtx, svcCancel := context.WithCancel(ctx)
		tasks := []Task{{
			Name: "error task",
			Run: func() error {
				cancel()
				return fmt.Errorf("task failed")
			},
			Cancel: svcCancel,
			Ctx:    svcCtx,
		}}

		err := StartKernel(deps, tasks)
		assert.NoError(t, err)

		// Check that the error was published to the new messenger
		assert.Len(t, messenger.PublishedMessages, 1)
		assert.Equal(t, "errors", messenger.PublishedMessages[0].Channel)
		assert.Equal(t, "core.error.v1", messenger.PublishedMessages[0].PayloadType)

		// Verify the payload contains the error
		payload := messenger.PublishedMessages[0].Payload.(*core.ErrorEvent)
		assert.NotNil(t, payload)
		assert.Contains(t, payload.Text, "task failed")
	})
}

type mockStateStore struct {
	data map[string]interface{}
}

func (m *mockStateStore) Save(key string, value interface{}) error {
	if m.data == nil {
		m.data = make(map[string]interface{})
	}
	m.data[key] = value
	return nil
}

func (m *mockStateStore) Load(key string, value interface{}) error {
	if m.data == nil {
		return nil
	}
	val, ok := m.data[key]
	if !ok {
		return nil
	}

	// Simple simulation of JSON decoding
	v := value.(*time.Time)
	*v = val.(time.Time)
	return nil
}

func TestStartKernelStateCache(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		deps := getDefaultTestDeps()
		cancel := deps.MustGetCancel()

		stateStore := &mockStateStore{
			data: make(map[string]interface{}),
		}
		deps.SetStateStore(stateStore)

		// Set last run time to 30 seconds ago
		lastRun := time.Now().Add(-30 * time.Second)
		assert.NoError(t, stateStore.Save("last_check_test-service", lastRun))

		runCount := 0
		interval := 60 * time.Second

		tasks := []Task{{
			Name:     "test-service",
			Interval: interval,
			Run: func() error {
				runCount++
				if runCount >= 1 {
					cancel()
				}
				return nil
			},
			Ctx:    context.Background(),
			Cancel: func() {},
		}}

		// Start kernel in a goroutine because it blocks
		go func() {
			if err := StartKernel(deps, tasks); err != nil {
				t.Logf("StartKernel error: %v", err)
			}
		}()

		// It should NOT run immediately.
		// Since last run was 30s ago and interval is 60s, it should run in about 30s (+ jitter).
		time.Sleep(10 * time.Second)
		assert.Equal(t, 0, runCount, "Task should not have run yet")

		time.Sleep(25 * time.Second) // Total 35s, should have run by now (30s + small jitter)
		assert.Equal(t, 1, runCount, "Task should have run by now")

		// Check if state was updated
		var updatedRun time.Time
		assert.NoError(t, stateStore.Load("last_check_test-service", &updatedRun))
		assert.True(t, updatedRun.After(lastRun), "State should have been updated")
	})
}
