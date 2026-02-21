package ws

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"sync"
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

// Push sends a JSON message to all connections for the given user.
func (h *hub) Push(userID string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("ws hub: marshal: %v", err)
		return
	}
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
