package run

import (
	"context"
	"fmt"
	"keyop/core"
	"math/rand"
	"sync"
	"time"
)

type Task struct {
	Name             string
	Interval         time.Duration
	Run              func() error
	Cancel           func()
	Ctx              context.Context
	ErrorChannelName string
}

func StartKernel(deps core.Dependencies, tasks []Task) error {
	logger := deps.MustGetLogger()
	globalCtx := deps.MustGetContext()
	stateStore := deps.MustGetStateStore()

	logger.Info("kernel started")
	logger.Info("Tasks: ", "count", len(tasks))

	var wg sync.WaitGroup
	for _, task := range tasks {

		wg.Add(1)

		// create a goroutine for each task
		go func(task Task) {
			defer wg.Done()

			stateKey := fmt.Sprintf("last_check_%s", task.Name)
			var lastRun time.Time
			if err := stateStore.Load(stateKey, &lastRun); err != nil {
				logger.Error("failed to load state", "service", task.Name, "error", err)
			}

			if !lastRun.IsZero() && task.Interval > 0 {
				nextRun := lastRun.Add(task.Interval)
				wait := time.Until(nextRun)
				if wait > 0 {
					logger.Info("Scheduled next run based on cached state", "service", task.Name, "wait", wait)
					timer := time.NewTimer(wait)
					select {
					case <-globalCtx.Done():
						if !timer.Stop() {
							<-timer.C
						}
						return
					case <-timer.C:
					}
				}
			}

			for {
				select {
				case <-globalCtx.Done():
					logger.Error("task: global context done, exiting check loop", "service", task.Name)
					return
				default:
				}

				done := make(chan struct{})
				go func() {
					defer close(done)
					logger.Debug("Starting task run", "service", task.Name)
					err := task.Run()
					if err == nil {
						logger.Debug("Task run completed", "service", task.Name)
					} else {
						logger.Error("Task run completed with error", "service", task.Name, "error", err)
						if task.ErrorChannelName != "" {
							messenger := deps.MustGetMessenger()
							_ = messenger.Send(core.Message{
								ChannelName: task.ErrorChannelName,
								ServiceName: task.Name,
								Text:        fmt.Sprintf("Task %s failed: %v", task.Name, err),
								Data:        err.Error(),
							})
						}
					}
					// Always save last check time
					if err := stateStore.Save(stateKey, time.Now()); err != nil {
						logger.Error("failed to save state", "service", task.Name, "error", err)
					}
				}()

				// Wait for one of the following:
				// - global context done: cancel run and exit worker
				// - run context cancelled (task called runCancel)
				// - task finished on its own (done channel closes)
				select {
				case <-globalCtx.Done():
					// Ensure the task run is cancelled and fully stopped
					// is this really needed, though?
					logger.Error("task: global context done, exiting check loop", "service", task.Name)
					task.Cancel()
					<-done
					return
				case <-task.Ctx.Done():
					// Task asked to cancel/restart OR inherited cancel from parent
					logger.Error("task: context done, exiting check loop", "service", task.Name)
					<-done
				case <-done:
					// Task returned without explicit cancel; treat as a normal completion
					// and restart after interval
					logger.Debug("Task completed normally", "service", task.Name)
				}

				if task.Interval <= 0 {
					logger.Info("Task has non-positive interval, not restarting", "service", task.Name)
					return
				}

				// Delay before restart, unless shutting down
				jitter := time.Duration(rand.Int63n(int64(task.Interval) / 20)) // up to 5% jitter
				timer := time.NewTimer(task.Interval + jitter)
				select {
				case <-globalCtx.Done():
					logger.Error("task: global context done during interval wait, exiting check loop", "service", task.Name)
					if !timer.Stop() {
						<-timer.C
					}
					return
				case <-timer.C:
				}
			}
		}(task)
	}

	logger.Info("kernel: blocking until all tasks complete")
	wg.Wait()
	logger.Info("kernel: all tasks stopped")

	return nil
}
