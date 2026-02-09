package core

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

type Message struct {
	Timestamp   time.Time   `json:"timestamp,omitempty"`
	Hostname    string      `json:"hostname,omitempty"`
	ServiceType string      `json:"serviceType,omitempty"`
	ServiceName string      `json:"serviceName,omitempty"`
	ChannelName string      `json:"channelName,omitempty"`
	Text        string      `json:"text,omitempty"`
	Metric      float64     `json:"metric,omitempty"`
	MetricName  string      `json:"metricName,omitempty"`
	Status      string      `json:"status,omitempty"`
	State       string      `json:"state,omitempty"`
	Data        interface{} `json:"data,omitempty"`
	Route       []string    `json:"route,omitempty"`
}

type MessengerApi interface {
	Send(msg Message) error
	Subscribe(sourceName string, channelName string, maxAge time.Duration, messageHandler func(Message) error) error
}

func NewMessenger(logger Logger, osProvider OsProviderApi) *Messenger {
	if logger == nil {
		panic("logger not properly initialized")
	}
	if osProvider == nil {
		panic("osProvider not properly initialized")
	}

	m := &Messenger{
		subscriptions: make(map[string][]func(Message) error),
		queues:        make(map[string]*PersistentQueue),
		logger:        logger,
		osProvider:    osProvider,
		dataDir:       "data",
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
}

//goland:noinspection GoVetCopyLock
func (m *Messenger) Send(msg Message) error {
	logger := m.logger
	if msg.ChannelName == "" {
		return fmt.Errorf("message must have a ChannelName")
	}
	channelName := msg.ChannelName
	logger.Debug("Send message called", "channel", channelName, "message", msg)

	addRoute := fmt.Sprintf("%s:%s", m.hostname, channelName)

	// Check if addRoute already exists in the route array
	for _, route := range msg.Route {
		if route == addRoute {
			m.logger.Debug("Discarding message already sent to this channel", "channel", channelName, "route", addRoute, "message", msg)
			return nil
		}
	}

	msg.Route = append(msg.Route, addRoute)

	err := m.initializePersistentQueue(channelName)
	if err != nil {
		return err
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Populate required fields
	if msg.Timestamp.IsZero() {
		logger.Warn("Timestamp is zero, setting timestamp to now", "message", msg)
		msg.Timestamp = time.Now()
	}
	if msg.Hostname == "" {
		msg.Hostname = m.hostname
	}

	logger.Info("SEND", "channel", channelName, "message", msg)
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	err = m.queues[channelName].Enqueue(string(msgBytes))
	if err != nil {
		logger.Error("Failed to enqueue message", "error", err)
		return err
	}

	return nil
}

//goland:noinspection GoVetCopyLock
func (m *Messenger) Subscribe(source string, channelName string, maxAge time.Duration, messageHandler func(Message) error) error {
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
		const (
			minBackoff = time.Second
			maxBackoff = 5 * time.Minute
		)
		retryCount := 0

		for {
			msgStr, err := queue.Dequeue(source)
			if err != nil {
				logger.Error("Failed to dequeue message", "error", err, "channel", channelName)
				time.Sleep(1 * time.Second)
				continue
			}

			var msg Message
			if err := json.Unmarshal([]byte(msgStr), &msg); err != nil {
				logger.Error("Failed to unmarshal dequeued message", "error", err, "message", msgStr)
				continue
			}

			if maxAge > 0 && !msg.Timestamp.IsZero() && time.Since(msg.Timestamp) > maxAge {
				logger.Debug("Skipping old message", "channel", channelName, "source", source, "timestamp", msg.Timestamp, "maxAge", maxAge)
				if err := queue.Ack(source); err != nil {
					logger.Error("Failed to ack skipped message", "error", err, "channel", channelName)
				}
				continue
			}

			for {
				if err := messageHandler(msg); err != nil {
					retryCount++
					logger.Error("Message handler returned error, retrying", "error", err, "message", msg, "retryCount", retryCount)

					// Truncated exponential backoff with jitter
					backoff := minBackoff * time.Duration(1<<uint(retryCount-1))
					if backoff > maxBackoff || backoff < minBackoff { // overflow check
						backoff = maxBackoff
					}

					// Apply jitter: [0.5 * backoff, 1.5 * backoff]
					jitter := time.Duration(rand.Float64() * float64(backoff))
					sleepTime := (backoff / 2) + jitter
					if sleepTime > maxBackoff {
						sleepTime = maxBackoff
					}

					logger.Info("Sleeping before retry", "sleepTime", sleepTime, "channel", channelName, "source", source, "sleep", sleepTime)
					time.Sleep(sleepTime)
					continue
				}

				retryCount = 0
				if err := queue.Ack(source); err != nil {
					logger.Error("Failed to ack message", "error", err, "channel", channelName)
				}
				break
			}
		}
	}()

	return nil
}

func (m *Messenger) SetDataDir(dir string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.dataDir = dir
}

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
