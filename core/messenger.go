package core

import (
	"encoding/json"
	"sync"
	"time"
)

type Message struct {
	Timestamp   time.Time
	Hostname    string
	ServiceType string
	ServiceName string
	Text        string
	Metric      float64
	State       string
	Data        string
}

type MessengerApi interface {
	Send(channelName string, msg Message, data interface{}) error
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
	subscriptions map[string][]func(Message) error
	mutex         sync.RWMutex
	logger        Logger
	hostname      string
}

//goland:noinspection GoVetCopyLock
func (m Messenger) Send(channelName string, msg Message, data interface{}) error {
	m.logger.Debug("Send message called", "channel", channelName, "message", msg, "data", data)

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

	m.logger.Info("SEND", "channel", channelName, "message", msg)
	if subscribers, subscribersExists := m.subscriptions[channelName]; subscribersExists {
		for _, ch := range subscribers {
			err := ch(msg)
			if err != nil {
				m.logger.Error("Failed to send message to subscriber", "error", err)
			}
		}
	} else {
		m.logger.Debug("No subscribers for channel", "channel", channelName)
	}

	return nil
}

//goland:noinspection GoVetCopyLock
func (m Messenger) Subscribe(source string, channelName string, messageHandler func(Message) error) error {

	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.logger.Info("Subscribing to channel", "channel", channelName, "source", source)

	if _, subscriptionsExist := m.subscriptions[channelName]; !subscriptionsExist {
		m.subscriptions[channelName] = []func(Message) error{}
	}
	m.subscriptions[channelName] = append(m.subscriptions[channelName], messageHandler)
	return nil
}
