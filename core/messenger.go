package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Message struct {
	Timestamp   time.Time
	Hostname    string
	ServiceType string
	ServiceName string
	ChannelName string
	Text        string
	Metric      float64
	State       string
	Data        interface{}
	Route       []string
}

type MessengerApi interface {
	Send(msg Message) error
	Subscribe(sourceName string, channelName string, messageHandler func(Message) error) error
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
	channelName := msg.ChannelName
	m.logger.Debug("Send message called", "channel", channelName, "message", msg)

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
		msg.Timestamp = time.Now()
	}
	if msg.Hostname == "" {
		msg.Hostname = m.hostname
	}

	m.logger.Info("SEND", "channel", channelName, "message", msg)
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	err = m.queues[channelName].Enqueue(string(msgBytes))
	if err != nil {
		m.logger.Error("Failed to enqueue message", "error", err)
		return err
	}

	return nil
}

//goland:noinspection GoVetCopyLock
func (m *Messenger) Subscribe(source string, channelName string, messageHandler func(Message) error) error {

	err := m.initializePersistentQueue(channelName)
	if err != nil {
		return err
	}

	m.mutex.RLock()
	queue := m.queues[channelName]
	m.mutex.RUnlock()

	m.logger.Info("Subscribing to channel", "channel", channelName, "source", source)

	go func() {
		for {
			msgStr, err := queue.Dequeue(source)
			if err != nil {
				m.logger.Error("Failed to dequeue message", "error", err, "channel", channelName)
				time.Sleep(1 * time.Second)
				continue
			}

			var msg Message
			if err := json.Unmarshal([]byte(msgStr), &msg); err != nil {
				m.logger.Error("Failed to unmarshal dequeued message", "error", err, "message", msgStr)
				continue
			}

			if err := messageHandler(msg); err != nil {
				m.logger.Error("Message handler returned error", "error", err, "message", msg)
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
