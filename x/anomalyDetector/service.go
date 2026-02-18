package anomalyDetector

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"math"
	"math/rand"
	"sync"
)

type Service struct {
	Deps         core.Dependencies
	Cfg          core.ServiceConfig
	AE           *Autoencoder
	WindowSize   int
	Threshold    float64
	MetricBuffer map[string][]float64
	mu           sync.Mutex
	Training     bool
	MinTrainSize int
	SkipServices []string
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	windowSize := 10
	if ws, ok := cfg.Config["window_size"].(float64); ok {
		windowSize = int(ws)
	} else if ws, ok := cfg.Config["window_size"].(int); ok {
		windowSize = ws
	}

	threshold := 0.1
	if t, ok := cfg.Config["threshold"].(float64); ok {
		threshold = t
	}

	minTrainSize := 100
	if mts, ok := cfg.Config["min_train_size"].(float64); ok {
		minTrainSize = int(mts)
	}

	var skipServices []string
	if ss, ok := cfg.Config["skip_services"].([]interface{}); ok {
		for _, s := range ss {
			if str, ok := s.(string); ok {
				skipServices = append(skipServices, str)
			}
		}
	} else if ss, ok := cfg.Config["skip_services"].([]string); ok {
		skipServices = ss
	}

	return &Service{
		Deps:         deps,
		Cfg:          cfg,
		AE:           NewAutoencoder(windowSize, windowSize/2),
		WindowSize:   windowSize,
		Threshold:    threshold,
		MetricBuffer: make(map[string][]float64),
		Training:     true,
		MinTrainSize: minTrainSize,
		SkipServices: skipServices,
	}
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"metrics"}, logger)
	if _, ok := svc.Cfg.Pubs["status"]; !ok {
		errs = append(errs, fmt.Errorf("anomalyDetector: required pubs channel 'status' is missing"))
	}
	return errs
}

func (svc *Service) Initialize() error {
	messenger := svc.Deps.MustGetMessenger()
	return messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name, svc.Cfg.Subs["metrics"].Name, svc.Cfg.Type, svc.Cfg.Name, svc.Cfg.Subs["metrics"].MaxAge, svc.messageHandler)
}

func (svc *Service) messageHandler(msg core.Message) error {

	for _, skipService := range svc.SkipServices {
		if msg.ServiceName == skipService {
			return nil
		}
	}

	svc.mu.Lock()
	defer svc.mu.Unlock()

	// TODO: min/max normalization based on historical data or config
	// Normalize metric to [0, 1] range for sigmoid.
	// This is a naive normalization, in real world we'd need min/max.
	// Assuming metrics are roughly 0-100 (like CPU/Mem)
	val := msg.Metric / 100.0
	if val < 0 {
		val = 0
	}
	if val > 1 {
		val = 1
	}

	svc.MetricBuffer[msg.MetricName] = append(svc.MetricBuffer[msg.MetricName], val)

	if len(svc.MetricBuffer[msg.MetricName]) < svc.WindowSize {
		return nil
	}

	// Keep only the last WindowSize metrics
	window := svc.MetricBuffer[msg.MetricName][len(svc.MetricBuffer[msg.MetricName])-svc.WindowSize:]
	svc.MetricBuffer[msg.MetricName] = window

	if svc.Training {
		mse := svc.AE.Train(window)
		// If we've seen enough data, we can stop "pure" training or just keep training slowly
		// For simplicity, we just keep training and check error
		if len(svc.MetricBuffer[msg.MetricName]) > svc.MinTrainSize {
			// Maybe reduce learning rate?
		}

		if mse > svc.Threshold {
			return svc.reportAnomalyStatus(msg, mse, "warning")
		} else {
			return svc.reportAnomalyStatus(msg, mse, "ok")
		}
	} else {
		_, reconstructed := svc.AE.Forward(window)
		mse := 0.0
		for i := 0; i < svc.WindowSize; i++ {
			err := reconstructed[i] - window[i]
			mse += err * err
		}
		mse /= float64(svc.WindowSize)

		if mse > svc.Threshold {
			return svc.reportAnomalyStatus(msg, mse, "warning")
		} else {
			return svc.reportAnomalyStatus(msg, mse, "ok")
		}
	}

	return nil
}

func (svc *Service) reportAnomalyStatus(msg core.Message, mse float64, status string) error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	newMessage := core.Message{
		ChannelName: svc.Cfg.Pubs["status"].Name,
		Uuid:        msg.Uuid,
		MetricName:  msg.MetricName,
		Metric:      msg.Metric,
		Timestamp:   msg.Timestamp,
		ServiceName: msg.ServiceName + "-anomaly",
		ServiceType: msg.ServiceType,
		Status:      status,
		Data: map[string]interface{}{
			"mse":       mse,
			"threshold": svc.Threshold,
		},
	}

	if status != "ok" {
		newMessage.Text = fmt.Sprintf("Anomaly detected in %s: MSE=%.4f exceeds threshold %.4f", msg.MetricName, mse, svc.Threshold)
		logger.Warn(newMessage.Text)
	} else {
		newMessage.Text = fmt.Sprintf("%s is normal: MSE=%.4f within threshold %.4f", msg.MetricName, mse, svc.Threshold)
	}

	return messenger.Send(newMessage)
}

func (svc *Service) Check() error {
	return nil
}

// Autoencoder is a simple linear autoencoder (basically PCA-like if linear,
// but we'll add a bit more structure)
type Autoencoder struct {
	InputSize    int
	HiddenSize   int
	W1           [][]float64
	B1           []float64
	W2           [][]float64
	B2           []float64
	LearningRate float64
}

func NewAutoencoder(inputSize, hiddenSize int) *Autoencoder {
	ae := &Autoencoder{
		InputSize:    inputSize,
		HiddenSize:   hiddenSize,
		W1:           make([][]float64, hiddenSize),
		B1:           make([]float64, hiddenSize),
		W2:           make([][]float64, inputSize),
		B2:           make([]float64, inputSize),
		LearningRate: 0.01,
	}

	for i := range ae.W1 {
		ae.W1[i] = make([]float64, inputSize)
		for j := range ae.W1[i] {
			ae.W1[i][j] = rand.NormFloat64() * 0.1
		}
	}
	for i := range ae.W2 {
		ae.W2[i] = make([]float64, hiddenSize)
		for j := range ae.W2[i] {
			ae.W2[i][j] = rand.NormFloat64() * 0.1
		}
	}
	return ae
}

func sigmoid(x float64) float64 {
	return 1.0 / (1.0 + math.Exp(-x))
}

func sigmoidDeriv(x float64) float64 {
	sx := sigmoid(x)
	return sx * (1.0 - sx)
}

func (ae *Autoencoder) Forward(input []float64) ([]float64, []float64) {
	hidden := make([]float64, ae.HiddenSize)
	for i := 0; i < ae.HiddenSize; i++ {
		sum := ae.B1[i]
		for j := 0; j < ae.InputSize; j++ {
			sum += input[j] * ae.W1[i][j]
		}
		hidden[i] = sigmoid(sum)
	}

	output := make([]float64, ae.InputSize)
	for i := 0; i < ae.InputSize; i++ {
		sum := ae.B2[i]
		for j := 0; j < ae.HiddenSize; j++ {
			sum += hidden[j] * ae.W2[i][j]
		}
		output[i] = sigmoid(sum) // reconstructed input
	}
	return hidden, output
}

func (ae *Autoencoder) Train(input []float64) float64 {
	hidden, output := ae.Forward(input)

	// Output error
	outputDelta := make([]float64, ae.InputSize)
	mse := 0.0
	for i := 0; i < ae.InputSize; i++ {
		err := output[i] - input[i]
		mse += err * err
		outputDelta[i] = err * sigmoidDeriv(output[i]) // Simplified, using sigmoid as output too
	}
	mse /= float64(ae.InputSize)

	// Hidden error
	hiddenDelta := make([]float64, ae.HiddenSize)
	for i := 0; i < ae.HiddenSize; i++ {
		err := 0.0
		for j := 0; j < ae.InputSize; j++ {
			err += outputDelta[j] * ae.W2[j][i]
		}
		hiddenDelta[i] = err * sigmoidDeriv(hidden[i])
	}

	// Update W2, B2
	for i := 0; i < ae.InputSize; i++ {
		for j := 0; j < ae.HiddenSize; j++ {
			ae.W2[i][j] -= ae.LearningRate * outputDelta[i] * hidden[j]
		}
		ae.B2[i] -= ae.LearningRate * outputDelta[i]
	}

	// Update W1, B1
	for i := 0; i < ae.HiddenSize; i++ {
		for j := 0; j < ae.InputSize; j++ {
			ae.W1[i][j] -= ae.LearningRate * hiddenDelta[i] * input[j]
		}
		ae.B1[i] -= ae.LearningRate * hiddenDelta[i]
	}

	return mse
}
