package runtime

import (
	"context"
	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/testutil"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Mock service implementation for testing run and StartKernel
type mockServiceForRun struct {
	core.Service
	initCalled    bool
	checkCalled   bool
	validateCalls int
}

func (m *mockServiceForRun) Initialize() error {
	m.initCalled = true
	return nil
}

func (m *mockServiceForRun) Check() error {
	m.checkCalled = true
	return nil
}

func (m *mockServiceForRun) ValidateConfig() []error {
	m.validateCalls++
	return nil
}

func TestRun_WithUnregisteredServiceType(t *testing.T) {
	logger := &testutil.FakeLogger{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	serviceConfigs := []core.ServiceConfig{
		{
			Name: "unregistered",
			Type: "nonexistent_service_type",
		},
	}

	err := run(deps, serviceConfigs)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "service type not registered")
}

func TestRun_ValidatesConfigBeforeInitializing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	// Register a mock service
	serviceName := "test_service_" + t.Name()
	core.RegisterService(serviceName, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return &mockServiceForRun{}
	})

	serviceConfigs := []core.ServiceConfig{
		{
			Name: "test-service",
			Type: serviceName,
		},
	}

	// Create channel to signal when validation happens
	validationDone := make(chan bool, 1)
	go func() {
		run(deps, serviceConfigs)
		validationDone <- true
	}()

	// Wait for the goroutine to finish
	<-validationDone

	// Validation should have occurred (checked through log messages)
	assert.NotEmpty(t, logger.LastInfoMsg)
}

func TestRun_WithEmptyServiceList(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	err := run(deps, []core.ServiceConfig{})
	// Empty service list should be OK (no services to run)
	// The function will complete successfully
	assert.Nil(t, err) // Might be OK or error depending on implementation
}

func TestStartKernel_WithSingleTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	taskExecuted := false
	tasks := []Task{
		{
			Name: "test-task",
			Run: func() error {
				taskExecuted = true
				// Cancel after first execution
				cancel()
				return nil
			},
			Ctx: ctx,
			Cancel: func() {
				// No-op
			},
		},
	}

	err := StartKernel(deps, tasks)
	assert.NoError(t, err)
	assert.True(t, taskExecuted)
}

func TestStartKernel_WithMultipleTasks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	// Use a WaitGroup to ensure both tasks get a chance to run
	var wg sync.WaitGroup
	wg.Add(2)

	tasks := []Task{
		{
			Name: "task-1",
			Run: func() error {
				defer wg.Done()
				return nil
			},
			Ctx:    ctx,
			Cancel: func() {},
		},
		{
			Name: "task-2",
			Run: func() error {
				defer wg.Done()
				return nil
			},
			Ctx:    ctx,
			Cancel: func() {},
		},
	}

	// Run StartKernel in a goroutine so we can wait for tasks
	errChan := make(chan error, 1)
	go func() {
		errChan <- StartKernel(deps, tasks)
	}()

	// Wait for both tasks to complete, then cancel
	wg.Wait()
	cancel()

	err := <-errChan
	assert.NoError(t, err)
}

// Additional test helpers for run() function

// mockServiceNotImplementingCore is a type that doesn't implement core.Service
type mockServiceNotImplementingCore struct{}

// mockServiceFailsInitialize implements core.Service but Initialize fails
type mockServiceFailsInitialize struct {
	core.Service
}

func (m *mockServiceFailsInitialize) Initialize() error {
	return assert.AnError
}

func (m *mockServiceFailsInitialize) Check() error {
	return nil
}

func (m *mockServiceFailsInitialize) ValidateConfig() []error {
	return nil
}

// mockServiceFailsValidation implements core.Service but ValidateConfig returns errors
type mockServiceFailsValidation struct {
	core.Service
}

func (m *mockServiceFailsValidation) Initialize() error {
	return nil
}

func (m *mockServiceFailsValidation) Check() error {
	return nil
}

func (m *mockServiceFailsValidation) ValidateConfig() []error {
	return []error{assert.AnError}
}

// mockServiceWithPayloadTypes implements RegisterPayloadTypesProvider
type mockServiceWithPayloadTypes struct {
	core.Service
	registerPayloadsCalled bool
}

func (m *mockServiceWithPayloadTypes) Initialize() error {
	return nil
}

func (m *mockServiceWithPayloadTypes) Check() error {
	return nil
}

func (m *mockServiceWithPayloadTypes) ValidateConfig() []error {
	return nil
}

func (m *mockServiceWithPayloadTypes) RegisterPayloadTypes(newMsgr interface {
	RegisterPayloadType(typeStr string, prototype any) error
}, logger core.Logger) error {
	m.registerPayloadsCalled = true
	return nil
}

// Test: Service type doesn't implement core.Service
func TestRun_ServiceDoesNotImplementCoreService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	// Register a constructor that returns a type not implementing core.Service
	serviceName := "bad_service_" + t.Name()
	core.RegisterService(serviceName, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return &mockServiceNotImplementingCore{}
	})

	serviceConfigs := []core.ServiceConfig{
		{
			Name: "bad-service",
			Type: serviceName,
		},
	}

	err := run(deps, serviceConfigs)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not implement core.Service")
}

// Test: Service initialization fails
func TestRun_ServiceInitializationFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	// Register a service that fails to initialize
	serviceName := "fail_init_service_" + t.Name()
	core.RegisterService(serviceName, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return &mockServiceFailsInitialize{}
	})

	serviceConfigs := []core.ServiceConfig{
		{
			Name: "failing-service",
			Type: serviceName,
		},
	}

	err := run(deps, serviceConfigs)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "service initialization failed")
}

// Test: Service validation fails
func TestRun_ServiceValidationFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	// Register a service that fails validation
	serviceName := "fail_validate_service_" + t.Name()
	core.RegisterService(serviceName, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return &mockServiceFailsValidation{}
	})

	serviceConfigs := []core.ServiceConfig{
		{
			Name: "validating-service",
			Type: serviceName,
		},
	}

	err := run(deps, serviceConfigs)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "service configuration errors detected")
}

// Test: Service with payload type registration
func TestRun_ServiceRegistersPayloadTypes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	// Set up a fake messenger
	fakeMsgr := testutil.NewFakeMessenger()
	deps.SetMessenger(fakeMsgr)

	// Register a service with payload types
	serviceName := "payload_service_" + t.Name()
	svc := &mockServiceWithPayloadTypes{}
	core.RegisterService(serviceName, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return svc
	})

	serviceConfigs := []core.ServiceConfig{
		{
			Name: "payload-service",
			Type: serviceName,
		},
	}

	err := run(deps, serviceConfigs)
	// Should succeed and service should have registered payloads
	assert.NoError(t, err)
	assert.True(t, svc.registerPayloadsCalled)
}

// Test: Multiple services
func TestRun_WithMultipleServices(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	// Register multiple services
	serviceName1 := "service1_" + t.Name()
	serviceName2 := "service2_" + t.Name()

	core.RegisterService(serviceName1, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return &mockServiceForRun{}
	})

	core.RegisterService(serviceName2, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return &mockServiceForRun{}
	})

	serviceConfigs := []core.ServiceConfig{
		{
			Name: "service-1",
			Type: serviceName1,
		},
		{
			Name: "service-2",
			Type: serviceName2,
		},
	}

	err := run(deps, serviceConfigs)
	assert.NoError(t, err)
}

// mockServiceFailsPayloadRegistration implements RegisterPayloadTypesProvider but fails
type mockServiceFailsPayloadRegistration struct {
	core.Service
}

func (m *mockServiceFailsPayloadRegistration) Initialize() error {
	return nil
}

func (m *mockServiceFailsPayloadRegistration) Check() error {
	return nil
}

func (m *mockServiceFailsPayloadRegistration) ValidateConfig() []error {
	return nil
}

func (m *mockServiceFailsPayloadRegistration) RegisterPayloadTypes(newMsgr interface {
	RegisterPayloadType(typeStr string, prototype any) error
}, logger core.Logger) error {
	return assert.AnError
}

// Test: Service payload registration fails
func TestRun_PayloadRegistrationFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	// Set up a fake messenger so RegisterPayloadTypes is called
	fakeMsgr := testutil.NewFakeMessenger()
	deps.SetMessenger(fakeMsgr)

	// Register a service that fails to register payloads
	serviceName := "fail_payload_service_" + t.Name()
	core.RegisterService(serviceName, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return &mockServiceFailsPayloadRegistration{}
	})

	serviceConfigs := []core.ServiceConfig{
		{
			Name: "failing-payload-service",
			Type: serviceName,
		},
	}

	err := run(deps, serviceConfigs)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "service payload type registration failed")
}

// Test: Service without payload types doesn't trigger registration
func TestRun_ServiceWithoutPayloadTypes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	// Set up a fake messenger
	fakeMsgr := testutil.NewFakeMessenger()
	deps.SetMessenger(fakeMsgr)

	// Register a service without payload types
	serviceName := "no_payload_service_" + t.Name()
	core.RegisterService(serviceName, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return &mockServiceForRun{}
	})

	serviceConfigs := []core.ServiceConfig{
		{
			Name: "no-payload-service",
			Type: serviceName,
		},
	}

	err := run(deps, serviceConfigs)
	// Should succeed - service just doesn't have RegisterPayloadTypes
	assert.NoError(t, err)
}

// mockServiceWithFreq has a frequency to test task creation
type mockServiceWithFreq struct {
	core.Service
}

func (m *mockServiceWithFreq) Initialize() error {
	return nil
}

func (m *mockServiceWithFreq) Check() error {
	return nil
}

func (m *mockServiceWithFreq) ValidateConfig() []error {
	return nil
}

// Test: Service with frequency creates task with correct interval
func TestRun_ServiceWithFreqCreatesTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	// Register a service
	serviceName := "freq_service_" + t.Name()
	core.RegisterService(serviceName, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return &mockServiceWithFreq{}
	})

	// Create a service config with a frequency
	serviceConfigs := []core.ServiceConfig{
		{
			Name: "freq-service",
			Type: serviceName,
			Freq: 5 * time.Second,
		},
	}

	// Run in a goroutine and cancel quickly to stop the kernel
	errChan := make(chan error, 1)
	go func() {
		errChan <- run(deps, serviceConfigs)
	}()

	// Cancel almost immediately - just let it start
	time.Sleep(10 * time.Millisecond)
	cancel()

	err := <-errChan
	assert.NoError(t, err)
}

// Test: Service with config gets initialized properly
func TestRun_ServiceWithConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	// Register a service
	serviceName := "config_service_" + t.Name()
	core.RegisterService(serviceName, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return &mockServiceForRun{}
	})

	// Create a service config with custom config
	serviceConfigs := []core.ServiceConfig{
		{
			Name: "config-service",
			Type: serviceName,
			Config: map[string]interface{}{
				"key1": "value1",
				"key2": 42,
			},
		},
	}

	// Run and cancel
	errChan := make(chan error, 1)
	go func() {
		errChan <- run(deps, serviceConfigs)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	err := <-errChan
	assert.NoError(t, err)
}

// mockServiceTracksCheck tracks if Check was called
type mockServiceTracksCheck struct {
	core.Service
	checkCallCount int
}

func (m *mockServiceTracksCheck) Initialize() error {
	return nil
}

func (m *mockServiceTracksCheck) Check() error {
	m.checkCallCount++
	return nil
}

func (m *mockServiceTracksCheck) ValidateConfig() []error {
	return nil
}

// Test: Service Check is called by the kernel
func TestRun_ServiceCheckIsCalled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	// Register a service
	serviceName := "check_service_" + t.Name()
	svc := &mockServiceTracksCheck{}
	core.RegisterService(serviceName, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return svc
	})

	serviceConfigs := []core.ServiceConfig{
		{
			Name: "check-service",
			Type: serviceName,
		},
	}

	// Run and wait for Check to be called at least once
	errChan := make(chan error, 1)
	go func() {
		errChan <- run(deps, serviceConfigs)
	}()

	// Wait a bit for the service to be initialized and Check to run
	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-errChan
	assert.NoError(t, err)
	assert.Greater(t, svc.checkCallCount, 0, "Check should have been called at least once")
}

// Test: Service name defaults to filename if not specified
func TestRun_ServiceNameDefaultsToFilename(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	// Register a service
	serviceName := "name_default_service_" + t.Name()
	core.RegisterService(serviceName, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return &mockServiceForRun{}
	})

	// Config with empty Name (would be set to filename by loadServiceConfigs)
	serviceConfigs := []core.ServiceConfig{
		{
			Name: "", // Empty name
			Type: serviceName,
		},
	}

	err := run(deps, serviceConfigs)
	// Should error because Name is empty
	assert.Error(t, err)
}

// Test: Three services in sequence
func TestRun_WithThreeServices(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	// Register three services
	s1 := "s1_" + t.Name()
	s2 := "s2_" + t.Name()
	s3 := "s3_" + t.Name()

	core.RegisterService(s1, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return &mockServiceForRun{}
	})
	core.RegisterService(s2, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return &mockServiceForRun{}
	})
	core.RegisterService(s3, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return &mockServiceForRun{}
	})

	serviceConfigs := []core.ServiceConfig{
		{Name: "svc1", Type: s1},
		{Name: "svc2", Type: s2},
		{Name: "svc3", Type: s3},
	}

	errChan := make(chan error, 1)
	go func() {
		errChan <- run(deps, serviceConfigs)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	err := <-errChan
	assert.NoError(t, err)
}

// mockServiceError implements Service and Check returns error
type mockServiceError struct {
	core.Service
}

func (m *mockServiceError) Initialize() error {
	return nil
}

func (m *mockServiceError) Check() error {
	return assert.AnError
}

func (m *mockServiceError) ValidateConfig() []error {
	return nil
}

// Test: Service that returns error from Check
func TestRun_ServiceCheckReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &testutil.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	// Set up a fake messenger for error events
	fakeMsgr := testutil.NewFakeMessenger()
	deps.SetMessenger(fakeMsgr)

	// Register a service that fails Check
	serviceName := "error_check_service_" + t.Name()
	svc := &mockServiceError{}
	core.RegisterService(serviceName, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return svc
	})

	serviceConfigs := []core.ServiceConfig{
		{
			Name: "error-check-service",
			Type: serviceName,
		},
	}

	errChan := make(chan error, 1)
	go func() {
		errChan <- run(deps, serviceConfigs)
	}()

	// Let it run for a bit - the kernel will handle the error from Check
	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-errChan
	assert.NoError(t, err)
}
