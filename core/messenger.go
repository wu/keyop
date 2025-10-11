package core

import "sync"

type Message struct {
	Content interface{}
}

type MessengerApi interface {
	Send(channelName string, msg Message) error
	Subscribe(channelName string) chan Message
}

func NewMessenger() *Messenger {
	return &Messenger{
		subscriptions: make(map[string][]chan Message),
	}
}

type Messenger struct {
	subscriptions map[string][]chan Message
	mutex         sync.RWMutex
}

func (m *Messenger) Send(channelName string, msg Message) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if subscribers, ok := m.subscriptions[channelName]; ok {
		for _, ch := range subscribers {
			ch <- msg
		}
	}

	return nil
}

func (m *Messenger) Subscribe(channelName string) chan Message {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	channel := make(chan Message)
	if _, ok := m.subscriptions[channelName]; !ok {
		m.subscriptions[channelName] = []chan Message{}
	}
	m.subscriptions[channelName] = append(m.subscriptions[channelName], channel)
	return channel
}
