package api

import (
	"fmt"
	"sync"
	"time"
)

// SSEClient represents a Server-Sent Events subscriber.
type SSEClient struct {
	ID      string
	Channel string
	send    chan string
	done    chan struct{}
}

// SSEHub manages all SSE clients and broadcasts events.
type SSEHub struct {
	mu        sync.RWMutex
	clients   map[string]*SSEClient
	broadcast chan SSEEvent
	done      chan struct{}
}

// SSEEvent is a typed event to broadcast.
type SSEEvent struct {
	Channel string
	Event   string
	Data    string
}

// NewSSEHub creates and starts a new SSE hub.
func NewSSEHub() *SSEHub {
	h := &SSEHub{
		clients:   make(map[string]*SSEClient),
		broadcast: make(chan SSEEvent, 256),
		done:      make(chan struct{}),
	}
	go h.run()
	return h
}

func (h *SSEHub) run() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case event := <-h.broadcast:
			h.dispatch(event)
		case <-ticker.C:
			h.ping()
		case <-h.done:
			return
		}
	}
}

func (h *SSEHub) dispatch(event SSEEvent) {
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event.Event, event.Data)
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, client := range h.clients {
		if client.Channel == "" || client.Channel == event.Channel {
			select {
			case client.send <- msg:
			default:
				// Slow client: drop event rather than block
			}
		}
	}
}

func (h *SSEHub) ping() {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, client := range h.clients {
		select {
		case client.send <- ": ping\n\n":
		default:
		}
	}
}

// Subscribe adds a new SSE client.
func (h *SSEHub) Subscribe(id, channel string) *SSEClient {
	client := &SSEClient{
		ID:      id,
		Channel: channel,
		send:    make(chan string, 64),
		done:    make(chan struct{}),
	}
	h.mu.Lock()
	h.clients[id] = client
	h.mu.Unlock()
	return client
}

// Unsubscribe removes a client and closes its channels.
func (h *SSEHub) Unsubscribe(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.clients[id]; ok {
		close(c.done)
		close(c.send) // signal SSE handler goroutine to exit
		delete(h.clients, id)
	}
}

// Publish sends an event to all subscribers of a channel.
func (h *SSEHub) Publish(channel, event, data string) {
	select {
	case h.broadcast <- SSEEvent{Channel: channel, Event: event, Data: data}:
	default:
		// Drop if broadcast channel is full; log in production
	}
}

// ClientCount returns the number of connected SSE clients.
func (h *SSEHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Stop shuts down the hub.
func (h *SSEHub) Stop() {
	close(h.done)
}
