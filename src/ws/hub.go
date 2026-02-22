package ws

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"sync"

	"servertest/metrics"
)

// Hub tracks WebSocket connections by user ID for push notifications.
// Supports multiple connections per user (e.g. ParrelSync clones with same token).
var Hub = &hub{conns: make(map[string]map[string]chan []byte)}

type hub struct {
	mu    sync.RWMutex
	conns map[string]map[string]chan []byte // userID -> connID -> send
}

func genConnID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "0"
	}
	return hex.EncodeToString(b)
}

// Register adds a connection for the given userID. Returns connID for Unregister.
func (h *hub) Register(userID string, send chan []byte) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	connID := genConnID()
	if h.conns[userID] == nil {
		h.conns[userID] = make(map[string]chan []byte)
	}
	h.conns[userID][connID] = send
	return connID
}

// Unregister removes the connection for userID+connID.
func (h *hub) Unregister(userID string, connID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if channels, ok := h.conns[userID]; ok {
		if send, ok := channels[connID]; ok {
			close(send)
			delete(channels, connID)
		}
		if len(channels) == 0 {
			delete(h.conns, userID)
		}
	}
}

// ConnectionCount returns the total number of active WebSocket connections.
func (h *hub) ConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, chs := range h.conns {
		n += len(chs)
	}
	return n
}

// Push sends a JSON message to all connections for the given user.
func (h *hub) Push(userID string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("ws hub: marshal: %v", err)
		return
	}
	metrics.AddBytesOut(uint64(len(data)))
	h.mu.RLock()
	channels := h.conns[userID]
	h.mu.RUnlock()
	for _, send := range channels {
		select {
		case send <- data:
		default:
			// Buffer full, skip
		}
	}
}

// PushToMany sends a JSON message to all connections for each of the given user IDs.
func (h *hub) PushToMany(userIDs []string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("ws hub: marshal: %v", err)
		return
	}
	metrics.AddBytesOut(uint64(len(data) * len(userIDs)))
	h.mu.RLock()
	defer h.mu.RUnlock()
	seen := make(map[string]bool)
	for _, userID := range userIDs {
		if seen[userID] {
			continue
		}
		seen[userID] = true
		for _, send := range h.conns[userID] {
			select {
			case send <- data:
			default:
			}
		}
	}
}
