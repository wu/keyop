package core

import (
	"encoding/json"
	"sync"
	"time"
)

type Message struct {
	Timestamp   time.Time
	Text        string
	ServiceName string
	ServiceType string
	Value       float64
	Hostname    string
	Data        string
}

type MessengerApi interface {
	Send(channelName string, msg Message, data interface{}) error
	Subscribe(channelName string) chan Message
}

func NewMessenger(logger Logger, osProvider OsProviderApi) *Messenger {
	if logger == nil {
		panic("logger not properly initialized")
	}
	if osProvider == nil {
		panic("osProvider not properly initialized")
	}

	m := &Messenger{
		subscriptions: make(map[string][]chan Message),
		logger:        logger,
	}

	if host, err := osProvider.Hostname(); err == nil {
		m.hostname = host
	} else {
		logger.Error("Failed to determine hostname during initialization", "error", err)
	}

	return m
}

type Messenger struct {
	subscriptions map[string][]chan Message
	mutex         sync.RWMutex
	logger        Logger
	hostname      string
}

func (m Messenger) Send(channelName string, msg Message, data interface{}) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Populate required fields
	msg.Timestamp = time.Now()
	msg.Hostname = m.hostname

	if data != nil {
		dataBytes, err := json.Marshal(data)
		if err == nil {
			msg.Data = string(dataBytes)
		} else {
			m.logger.Error("Failed to serialize data", "error", err)
		}
	}

	m.logger.Info("Sending message", "channel", channelName, "message", msg)
	if subscribers, subscribersExists := m.subscriptions[channelName]; subscribersExists {
		for _, ch := range subscribers {
			ch <- msg
		}
	}

	return nil
}

func (m Messenger) Subscribe(channelName string) chan Message {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.logger.Info("Subscribing to channel", "channel", channelName)
	channel := make(chan Message)
	if _, subscriptionsExist := m.subscriptions[channelName]; !subscriptionsExist {
		m.subscriptions[channelName] = []chan Message{}
	}
	m.subscriptions[channelName] = append(m.subscriptions[channelName], channel)
	return channel
}
