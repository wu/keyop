package core

import (
	"sync"
	"time"
)

type Message struct {
	Time     time.Time
	Text     string
	Service  string
	Value    float64
	Hostname string
	Data     string
}

type MessengerApi interface {
	Send(channelName string, msg Message) error
	Subscribe(channelName string) chan Message
}

func NewMessenger(logger Logger) *Messenger {
	if logger == nil {
		panic("logger not properly initialized")
	}
	return &Messenger{
		subscriptions: make(map[string][]chan Message),
		logger:        logger,
	}
}

type Messenger struct {
	subscriptions map[string][]chan Message
	mutex         sync.RWMutex
	logger        Logger
}

func (m Messenger) Send(channelName string, msg Message) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	m.logger.Info("Sending message", "channel", channelName, "message", msg)
	if subscribers, ok := m.subscriptions[channelName]; ok {
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
	if _, ok := m.subscriptions[channelName]; !ok {
		m.subscriptions[channelName] = []chan Message{}
	}
	m.subscriptions[channelName] = append(m.subscriptions[channelName], channel)
	return channel
}
