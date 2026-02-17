package anomalyDetector

import (
	"context"
	"keyop/core"
	"testing"
	"time"
)

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

func TestAnomalyDetection(t *testing.T) {
	messenger := &mockMessenger{}
	deps := core.Dependencies{}
	deps.SetMessenger(messenger)
	deps.SetLogger(&core.FakeLogger{})

	cfg := core.ServiceConfig{
		Name: "anomaly_test",
		Subs: map[string]core.ChannelInfo{
			"metrics": {Name: "metrics_channel"},
		},
		Pubs: map[string]core.ChannelInfo{
			"status": {Name: "status_channel"},
		},
		Config: map[string]interface{}{
			"window_size":    5.0,
			"threshold":      0.001,
			"min_train_size": 10.0,
		},
	}

	svc := NewService(deps, cfg).(*Service)

	// Train with constant data
	for i := 0; i < 20; i++ {
		err := svc.messageHandler(core.Message{
			MetricName: "test.metric",
			Metric:     50.0,
		})
		if err != nil {
			t.Fatalf("messageHandler failed: %v", err)
		}
	}

	// Now send an anomaly
	err := svc.messageHandler(core.Message{
		MetricName: "test.metric",
		Metric:     99.0,
	})
	if err != nil {
		t.Fatalf("messageHandler failed: %v", err)
	}

	// Check if anomaly was reported
	foundAnomaly := false
	for _, msg := range messenger.messages {
		if msg.Status == "warning" {
			foundAnomaly = true
			break
		}
	}

	if !foundAnomaly {
		t.Errorf("Anomaly was not detected")
	}
}

func TestSkipServices(t *testing.T) {
	messenger := &mockMessenger{}
	deps := core.Dependencies{}
	deps.SetMessenger(messenger)
	deps.SetLogger(&core.FakeLogger{})

	cfg := core.ServiceConfig{
		Name: "anomaly_test",
		Subs: map[string]core.ChannelInfo{
			"metrics": {Name: "metrics_channel"},
		},
		Pubs: map[string]core.ChannelInfo{
			"status": {Name: "status_channel"},
		},
		Config: map[string]interface{}{
			"window_size":    5.0,
			"threshold":      0.001,
			"min_train_size": 10.0,
			"skip_services":  []interface{}{"skipped-service"},
		},
	}

	svc := NewService(deps, cfg).(*Service)

	// Send a message from a skipped service
	err := svc.messageHandler(core.Message{
		ServiceName: "skipped-service",
		MetricName:  "test.metric",
		Metric:      50.0,
	})
	if err != nil {
		t.Fatalf("messageHandler failed: %v", err)
	}

	// It should NOT be in the MetricBuffer if it's skipped
	svc.mu.Lock()
	if _, ok := svc.MetricBuffer["test.metric"]; ok {
		t.Errorf("Message from skipped service was not skipped")
	}
	svc.mu.Unlock()

	// Send a message from a non-skipped service
	err = svc.messageHandler(core.Message{
		ServiceName: "other-service",
		MetricName:  "other.metric",
		Metric:      50.0,
	})
	if err != nil {
		t.Fatalf("messageHandler failed: %v", err)
	}

	// It SHOULD be in the MetricBuffer
	svc.mu.Lock()
	if _, ok := svc.MetricBuffer["other.metric"]; !ok {
		t.Errorf("Message from non-skipped service was skipped")
	}
	svc.mu.Unlock()
}

func TestAutoencoder_Train(t *testing.T) {
	ae := NewAutoencoder(4, 2)
	input := []float64{0.1, 0.2, 0.3, 0.4}

	initialMSE := 0.0
	_, initialOutput := ae.Forward(input)
	for i := range input {
		err := initialOutput[i] - input[i]
		initialMSE += err * err
	}

	// Train several times
	for i := 0; i < 1000; i++ {
		ae.Train(input)
	}

	finalMSE := 0.0
	_, finalOutput := ae.Forward(input)
	for i := range input {
		err := finalOutput[i] - input[i]
		finalMSE += err * err
	}

	if finalMSE >= initialMSE {
		t.Errorf("MSE did not decrease: initial=%f, final=%f", initialMSE, finalMSE)
	}
}
