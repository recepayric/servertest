package handlers

import (
	"context"
	"log"
	"net/http"
	"time"

	"servertest/db"
	"servertest/ws"

	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

const wsBufferSize = 256

// WebSocket handles WS connections. Requires ?token=guest_token. Registers for push.
func WebSocket(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	var userID string
	err := db.Pool.QueryRow(ctx, `SELECT id::text FROM users WHERE guest_token = $1`, token).Scan(&userID)
	cancel()
	if err != nil || userID == "" {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	defer conn.Close()

	send := make(chan []byte, wsBufferSize)
	connID := ws.Hub.Register(userID, send)
	defer ws.Hub.Unregister(userID, connID)

	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetWriteDeadline(time.Now().Add(60 * time.Second))

	go func() {
		for msg := range send {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		// Echo back via send channel (same writer as push); enables WsTest
		if len(msg) > 0 {
			select {
			case send <- msg:
			default:
				// channel full, skip echo
			}
		}
	}
}
