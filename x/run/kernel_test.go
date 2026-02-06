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

//goland:noinspection GoUnusedParameter
func (m *mockMessenger) Subscribe(sourceName string, channelName string, messageHandler func(core.Message) error) error {
	return nil
}
