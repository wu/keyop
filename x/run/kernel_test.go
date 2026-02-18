package run

import (
	"context"
	"fmt"
	"keyop/core"
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

	osProvider := core.FakeOsProvider{Host: "test-host"}
	deps.SetOsProvider(osProvider)

	deps.SetMessenger(core.NewMessenger(logger, osProvider))

	deps.SetStateStore(&core.NoOpStateStore{})

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

		messenger := &mockMessenger{}
		deps.SetMessenger(messenger)

		svcCtx, svcCancel := context.WithCancel(ctx)
		tasks := []Task{{
			Name:             "error task",
			ErrorChannelName: "errors",
			Run: func() error {
				cancel()
				return fmt.Errorf("task failed")
			},
			Cancel: svcCancel,
			Ctx:    svcCtx,
		}}

		err := StartKernel(deps, tasks)
		assert.NoError(t, err)

		assert.Len(t, messenger.messages, 1)
		assert.Equal(t, "errors", messenger.messages[0].ChannelName)
		assert.Contains(t, messenger.messages[0].Text, "task failed")
	})
}

type mockMessenger struct {
	messages []core.Message
}

func (m *mockMessenger) Send(msg core.Message) error {
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockMessenger) Subscribe(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	return nil
}

func (m *mockMessenger) SubscribeExtended(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message, string, int64) error) error {
	return nil
}

func (m *mockMessenger) SetReaderState(channelName string, readerName string, fileName string, offset int64) error {
	return nil
}

func (m *mockMessenger) SeekToEnd(channelName string, readerName string) error {
	return nil
}

func (m *mockMessenger) SetDataDir(dir string) {}

func (m *mockMessenger) GetStats() core.MessengerStats {
	return core.MessengerStats{}
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
		stateStore.Save("last_check_test-service", lastRun)

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
		go StartKernel(deps, tasks)

		// It should NOT run immediately.
		// Since last run was 30s ago and interval is 60s, it should run in about 30s (+ jitter).
		time.Sleep(10 * time.Second)
		assert.Equal(t, 0, runCount, "Task should not have run yet")

		time.Sleep(25 * time.Second) // Total 35s, should have run by now (30s + small jitter)
		assert.Equal(t, 1, runCount, "Task should have run by now")

		// Check if state was updated
		var updatedRun time.Time
		stateStore.Load("last_check_test-service", &updatedRun)
		assert.True(t, updatedRun.After(lastRun), "State should have been updated")
	})
}
