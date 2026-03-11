//nolint:revive
package core

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// legacyEnvelope is used only for reading messages persisted in the old envelope
// format. It is not used for writing.
type legacyEnvelope struct {
	Version       string            `json:"v"`
	ID            string            `json:"id"`
	CorrelationID string            `json:"correlationId,omitempty"`
	Timestamp     time.Time         `json:"timestamp"`
	Source        string            `json:"source"`
	Topic         string            `json:"topic"`
	RetryCount    int               `json:"retryCount,omitempty"`
	Trace         []string          `json:"trace,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Payload       json.RawMessage   `json:"payload"`
}

// unmarshalLegacyEnvelope attempts to parse data as the old envelope format,
// returning the converted Message and true on success.
func unmarshalLegacyEnvelope(data []byte) (Message, bool) {
	var e legacyEnvelope
	if err := json.Unmarshal(data, &e); err != nil || e.Version == "" {
		return Message{}, false
	}
	var msg Message
	if len(e.Payload) > 0 {
		_ = json.Unmarshal(e.Payload, &msg)
	}
	if e.ID != "" {
		msg.Uuid = e.ID
	}
	if e.CorrelationID != "" {
		msg.Correlation = e.CorrelationID
	}
	if !e.Timestamp.IsZero() {
		msg.Timestamp = e.Timestamp
	}
	if e.Source != "" {
		msg.Hostname = e.Source
	}
	if e.Topic != "" {
		msg.ChannelName = e.Topic
	}
	if len(e.Trace) > 0 {
		msg.Route = e.Trace
	}
	if e.RetryCount > 0 {
		msg.RetryCount = e.RetryCount
	}
	if len(e.Headers) > 0 {
		if dt := e.Headers["payload-type"]; dt != "" && msg.DataType == "" {
			msg.DataType = dt
		}
	}
	return msg, true
}

// UnmarshalMessage parses a Message from JSON, handling both the current Message
// format and the legacy Envelope format for backward compatibility with older
// queue files.
func UnmarshalMessage(data []byte) (Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err == nil && (msg.Uuid != "" || msg.Event != "" || msg.ChannelName != "") {
		return msg, nil
	}
	if msg, ok := unmarshalLegacyEnvelope(data); ok {
		return msg, nil
	}
	var m Message
	return m, json.Unmarshal(data, &m)
}

type Message struct {
	Version     string      `json:"version,omitempty"`
	Timestamp   time.Time   `json:"timestamp,omitempty"`
	Uuid        string      `json:"uuid,omitempty"`
	Correlation string      `json:"correlation,omitempty"`
	Hostname    string      `json:"hostname,omitempty"`
	ChannelName string      `json:"channelName,omitempty"`
	ServiceType string      `json:"serviceType,omitempty"`
	ServiceName string      `json:"serviceName,omitempty"`
	Event       string      `json:"event,omitempty"`
	Status      string      `json:"status,omitempty"`
	Text        string      `json:"text,omitempty"`
	Body        string      `json:"body,omitempty"`
	Summary     string      `json:"summary,omitempty"`
	Metric      float64     `json:"metric,omitempty"`
	MetricName  string      `json:"metricName,omitempty"`
	State       string      `json:"state,omitempty"`
	Data        interface{} `json:"data,omitempty"`
	DataType    string      `json:"data-type,omitempty"`
	Route       []string    `json:"route,omitempty"`
	Log         []string    `json:"log,omitempty"`
	RetryCount  int         `json:"retryCount,omitempty"`
}

type MessengerApi interface {
	Send(msg Message) error
	Subscribe(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(Message) error) error
	SubscribeExtended(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(Message, string, int64) error) error
	SetReaderState(channelName string, readerName string, fileName string, offset int64) error
	SeekToEnd(channelName string, readerName string) error
	SetDataDir(dir string)
	SetHostname(hostname string)
	GetStats() MessengerStats
	GetPayloadRegistry() PayloadRegistry
	SetPayloadRegistry(reg PayloadRegistry)
}

func NewMessenger(logger Logger, osProvider OsProviderApi) *Messenger {
	if logger == nil {
		panic("logger not properly initialized")
	}
	if osProvider == nil {
		panic("osProvider not properly initialized")
	}

	home, err := osProvider.UserHomeDir()
	if err != nil {
		logger.Error("Failed to get user home directory, using current directory as fallback", "error", err)
		home = "."
	}
	m := &Messenger{
		subscriptions:        make(map[string][]func(Message) error),
		queues:               make(map[string]*PersistentQueue),
		logger:               logger,
		osProvider:           osProvider,
		dataDir:              filepath.Join(home, ".keyop", "data"),
		channelMessageCounts: make(map[string]int64),
		maxRetryAttempts:     5,
		retryBackoffBase:     time.Second,
		retryBackoffMax:      5 * time.Minute,
		payloadRegistry:      GetPayloadRegistry(),
	}

	if host, err := osProvider.Hostname(); err == nil {
		// get short hostname
		if idx := strings.Index(host, "."); idx != -1 {
			host = host[:idx]
		}
		m.hostname = host
	} else {
		logger.Error("Failed to determine hostname during initialization", "error", err)
	}

	return m
}

type Messenger struct {
	subscriptions map[string][]func(Message) error
	mutex         sync.RWMutex
	logger        Logger
	osProvider    OsProviderApi
	hostname      string
	queues        map[string]*PersistentQueue
	dataDir       string

	// retry config
	maxRetryAttempts int
	retryBackoffBase time.Duration
	retryBackoffMax  time.Duration

	// stats
	channelMessageCounts map[string]int64
	totalMessageCount    int64
	totalFailureCount    int64
	totalRetryCount      int64
	statsMutex           sync.RWMutex

	payloadRegistry PayloadRegistry
}

type MessengerStats struct {
	ChannelMessageCounts map[string]int64 `json:"channelMessageCounts"`
	TotalMessageCount    int64            `json:"totalMessageCount"`
	TotalFailureCount    int64            `json:"totalFailureCount"`
	TotalRetryCount      int64            `json:"totalRetryCount"`
}

//goland:noinspection GoVetCopyLock
func (m *Messenger) Send(msg Message) error {
	logger := m.logger
	if msg.ChannelName == "" {
		return fmt.Errorf("message must have a ChannelName")
	}
	channelName := msg.ChannelName

	if msg.Version == "" {
		msg.Version = "1.0"
	}
	if msg.Uuid == "" {
		msg.Uuid = uuid.NewString()
	}

	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	if msg.Hostname == "" {
		msg.Hostname = m.hostname
	}

	// prevent routing loops
	addRoute := fmt.Sprintf("%s:%s", m.hostname, channelName)
	for _, route := range msg.Route {
		if route == addRoute {
			m.logger.Debug("Discarding message already sent to this channel", "channel", channelName, "route", addRoute, "message", msg)
			return nil
		}
	}

	if msg.DataType == "" {
		if tp, ok := msg.Data.(TypedPayload); ok {
			msg.DataType = tp.PayloadType()
		}
	}
	msg.Route = append(msg.Route, addRoute)

	err := m.initializePersistentQueue(channelName)
	if err != nil {
		logger.Error("Failed to initialize queue", "error", err, "channel", channelName)
		return err
	}

	m.mutex.RLock()
	queue := m.queues[channelName]
	m.mutex.RUnlock()

	logger.Info("SEND",
		"time", msg.Timestamp.Format("2006-01-02 15:04:05"),
		"host", msg.Hostname,
		"channel", channelName,
		"event", msg.Event,
		"id", msg.Uuid,
		"payload", msg.Data,
	)
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		logger.Error("Failed to marshal message", "error", err)
		m.statsMutex.Lock()
		m.totalFailureCount++
		m.statsMutex.Unlock()
		return err
	}

	err = queue.Enqueue(string(msgBytes))
	if err != nil {
		logger.Error("Failed to enqueue message", "error", err)
		m.statsMutex.Lock()
		m.totalFailureCount++
		m.statsMutex.Unlock()
		return err
	}

	m.statsMutex.Lock()
	m.channelMessageCounts[channelName]++
	m.totalMessageCount++
	m.statsMutex.Unlock()

	return nil
}

//goland:noinspection GoVetCopyLock
func (m *Messenger) Subscribe(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(Message) error) error {
	return m.SubscribeExtended(ctx, source, channelName, serviceType, serviceName, maxAge, func(msg Message, fileName string, offset int64) error {
		return messageHandler(msg)
	})
}

func (m *Messenger) SubscribeExtended(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(Message, string, int64) error) error {
	logger := m.logger

	err := m.initializePersistentQueue(channelName)
	if err != nil {
		return err
	}

	m.mutex.RLock()
	queue := m.queues[channelName]
	m.mutex.RUnlock()

	logger.Info("Subscribing to channel", "channel", channelName, "source", source, "maxAge", maxAge)

	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info("Subscription cancelled", "channel", channelName, "source", source)
				return
			default:
			}

			msgStr, fileName, offset, err := queue.Dequeue(ctx, source)
			if err != nil {
				if err == context.Canceled || err == context.DeadlineExceeded {
					return
				}
				logger.Error("Failed to dequeue message", "error", err, "channel", channelName)

				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
				continue
			}

			msg, err := UnmarshalMessage([]byte(msgStr))
			if err != nil {
				logger.Error("Failed to unmarshal message",
					"error", err,
					"message", msgStr,
					"channel", channelName)
				if ackErr := queue.Ack(source); ackErr != nil {
					logger.Error("Failed to ack unparseable message", "error", ackErr, "channel", channelName)
				}
				continue
			}

			// Decode typed payload if data-type is set.
			payloadType := msg.DataType
			if payloadType != "" {
				reg := m.GetPayloadRegistry()
				if reg != nil {
					if typed, decodeErr := reg.Decode(payloadType, msg.Data); decodeErr == nil {
						msg.Data = typed
						msg.DataType = payloadType
					} else {
						logger.Error("Failed to decode typed payload", "type", payloadType, "error", decodeErr)
					}
				}
			}

			if maxAge > 0 && !msg.Timestamp.IsZero() && time.Since(msg.Timestamp) > maxAge {
				logger.Debug("Skipping old message", "channel", channelName, "source", source, "timestamp", msg.Timestamp, "maxAge", maxAge)
				if err := queue.Ack(source); err != nil {
					logger.Error("Failed to ack skipped message", "error", err, "channel", channelName)
				}
				continue
			}

			// Route for loop detection
			addRoute := fmt.Sprintf("%s:%s:%s", m.hostname, serviceType, serviceName)

			// Check if we should discard based on route
			alreadySent := false
			for _, r := range msg.Route {
				if r == addRoute {
					alreadySent = true
					break
				}
			}
			if alreadySent {
				logger.Debug("Discarding message already sent to this channel", "channel", channelName, "route", addRoute, "id", msg.Uuid)
				if err := queue.Ack(source); err != nil {
					logger.Error("Failed to ack discarded message", "error", err, "channel", channelName)
				}
				continue
			}

			// Add route
			msg.Route = append(msg.Route, addRoute)

			for {
				select {
				case <-ctx.Done():
					logger.Info("Subscription cancelled during processing", "channel", channelName, "source", source)
					return
				default:
				}

				if err := messageHandler(msg, fileName, offset); err != nil {
					m.statsMutex.Lock()
					m.totalRetryCount++
					m.statsMutex.Unlock()

					msg.RetryCount++
					logger.Error("Message handler returned error", "error", err, "id", msg.Uuid, "retryCount", msg.RetryCount)

					if msg.RetryCount > m.maxRetryAttempts {
						logger.Error("Max retry attempts reached, moving to DLQ",
							"id", msg.Uuid,
							"channel", channelName,
							"attempts", msg.RetryCount)
						dlqChannel := "_dlq." + channelName
						for {
							if dlqErr := m.SendToDLQ(dlqChannel, msg, err.Error()); dlqErr != nil {
								logger.Error("Failed to send to DLQ; original message NOT acked. Retrying DLQ write...",
									"error", dlqErr,
									"id", msg.Uuid,
									"channel", channelName)

								// Retry DLQ write with backoff to avoid tight loop
								// Use a smaller wait in tests
								dlqRetryWait := 5 * time.Second
								if strings.Contains(source, "test") || strings.Contains(channelName, "test") {
									dlqRetryWait = 100 * time.Millisecond
								}

								select {
								case <-ctx.Done():
									return
								case <-time.After(dlqRetryWait):
									continue
								}
							}
							break
						}
						if ackErr := queue.Ack(source); ackErr != nil {
							logger.Error("Failed to ack DLQed message", "error", ackErr, "channel", channelName)
						}
						break
					}

					backoff := m.retryBackoffBase
					for i := 1; i < msg.RetryCount && backoff < m.retryBackoffMax/2; i++ {
						backoff *= 2
					}
					if backoff > m.retryBackoffMax {
						backoff = m.retryBackoffMax
					}

					// full jitter: [0, backoff)
					sleepTime := time.Duration(0)
					if backoff > 0 {
						n, err := rand.Int(rand.Reader, big.NewInt(int64(backoff)))
						if err != nil {
							sleepTime = backoff / 2
						} else {
							sleepTime = time.Duration(n.Int64())
						}
					}

					// Use a very small sleep time during tests to avoid hanging
					if strings.Contains(source, "test") || strings.Contains(channelName, "test") {
						sleepTime = 10 * time.Millisecond
					}

					logger.Info("Sleeping before retry", "sleepTime", sleepTime, "channel", channelName, "id", msg.Uuid)

					select {
					case <-ctx.Done():
						return
					case <-time.After(sleepTime):
					}
					continue
				}

				if err := queue.Ack(source); err != nil {
					logger.Error("Failed to ack message", "error", err, "channel", channelName)
				}

				m.statsMutex.Lock()
				m.channelMessageCounts[channelName]++
				m.totalMessageCount++
				m.statsMutex.Unlock()

				break
			}
		}
	}()

	return nil
}

func (m *Messenger) SendToDLQ(dlqChannel string, msg Message, reason string) error {
	msg.Log = append(msg.Log, "dlq-reason: "+reason)

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	err = m.initializePersistentQueue(dlqChannel)
	if err != nil {
		return err
	}

	m.mutex.RLock()
	queue := m.queues[dlqChannel]
	m.mutex.RUnlock()

	return queue.Enqueue(string(msgBytes))
}

func (m *Messenger) SetReaderState(channelName string, readerName string, fileName string, offset int64) error {
	err := m.initializePersistentQueue(channelName)
	if err != nil {
		return err
	}
	m.mutex.RLock()
	queue := m.queues[channelName]
	m.mutex.RUnlock()
	return queue.SetState(readerName, fileName, offset)
}

func (m *Messenger) SeekToEnd(channelName string, readerName string) error {
	err := m.initializePersistentQueue(channelName)
	if err != nil {
		return err
	}
	m.mutex.RLock()
	queue := m.queues[channelName]
	m.mutex.RUnlock()
	return queue.SeekToEnd(readerName)
}

func (m *Messenger) SetDataDir(dir string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.dataDir = dir
}

func (m *Messenger) SetHostname(hostname string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.hostname = hostname
}

func (m *Messenger) GetStats() MessengerStats {
	m.statsMutex.RLock()
	defer m.statsMutex.RUnlock()

	channelCounts := make(map[string]int64)
	for k, v := range m.channelMessageCounts {
		channelCounts[k] = v
	}

	return MessengerStats{
		ChannelMessageCounts: channelCounts,
		TotalMessageCount:    m.totalMessageCount,
		TotalFailureCount:    m.totalFailureCount,
		TotalRetryCount:      m.totalRetryCount,
	}
}

func (m *Messenger) GetPayloadRegistry() PayloadRegistry {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.payloadRegistry
}

func (m *Messenger) SetPayloadRegistry(reg PayloadRegistry) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.payloadRegistry = reg
}

type FakeMessenger struct {
	Messages []Message
	mu       sync.RWMutex
}

func (f *FakeMessenger) Send(msg Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Messages = append(f.Messages, msg)
	return nil
}

func (f *FakeMessenger) Subscribe(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(Message) error) error {
	return nil
}

func (f *FakeMessenger) SubscribeExtended(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(Message, string, int64) error) error {
	return nil
}

func (f *FakeMessenger) SetReaderState(channelName string, readerName string, fileName string, offset int64) error {
	return nil
}

func (f *FakeMessenger) SeekToEnd(channelName string, readerName string) error {
	return nil
}

func (f *FakeMessenger) SetDataDir(dir string) {}

func (f *FakeMessenger) SetHostname(hostname string) {}

func (f *FakeMessenger) GetStats() MessengerStats {
	return MessengerStats{}
}

func (f *FakeMessenger) GetPayloadRegistry() PayloadRegistry {
	return nil
}

func (f *FakeMessenger) SetPayloadRegistry(reg PayloadRegistry) {}

func (m *Messenger) initializePersistentQueue(channelName string) error {
	// initialize persistent queue for source and channel
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.queues == nil {
		m.queues = make(map[string]*PersistentQueue)
	}
	_, queueExists := m.queues[channelName]
	if !queueExists {
		pq, err := NewPersistentQueue(channelName, m.dataDir, m.osProvider, m.logger)
		if err != nil {
			return err
		}
		m.queues[channelName] = pq
		m.logger.Info("Initialized persistent queue for channel", "channel", channelName)
	}
	return nil
}
