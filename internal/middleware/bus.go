package middleware

import (
	"sync"

	"airport-baggage/internal/domain"
)

// Handler processes one middleware envelope. Handlers must not retain the
// payload map after returning if they mutate it.
type Handler func(*domain.Message)

// Bus is the in-process representation of the airport's baggage message
// middleware. Topics correspond to the operational systems:
// check-in, BHS/sorter, loading, departure and transfer/rebooking.
type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
}

func NewBus() *Bus {
	return &Bus{handlers: map[string][]Handler{}}
}

func (b *Bus) Subscribe(topic string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[topic] = append(b.handlers[topic], h)
}

func (b *Bus) Publish(msg *domain.Message) {
	b.mu.RLock()
	hs := append([]Handler{}, b.handlers[msg.Topic]...)
	wild := append([]Handler{}, b.handlers["*"]...)
	b.mu.RUnlock()
	for _, h := range hs {
		h(msg)
	}
	for _, h := range wild {
		h(msg)
	}
}
