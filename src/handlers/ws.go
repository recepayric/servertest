package handlers

import (
	"context"
	"encoding/json"
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
		log.Printf(
			"ws request: remote=%s path=%s token_present=false upgrade=%q connection=%q sec-key-present=%t",
			r.RemoteAddr,
			r.URL.Path,
			r.Header.Get("Upgrade"),
			r.Header.Get("Connection"),
			r.Header.Get("Sec-WebSocket-Key") != "",
		)
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}
	log.Printf(
		"ws request: remote=%s path=%s token_present=true token_len=%d upgrade=%q connection=%q sec-key-present=%t",
		r.RemoteAddr,
		r.URL.Path,
		len(token),
		r.Header.Get("Upgrade"),
		r.Header.Get("Connection"),
		r.Header.Get("Sec-WebSocket-Key") != "",
	)

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	var userID string
	err := db.Pool.QueryRow(ctx, `SELECT id::text FROM users WHERE guest_token = $1`, token).Scan(&userID)
	cancel()
	if err != nil || userID == "" {
		log.Printf("ws auth failed: err=%v userID_empty=%t", err, userID == "")
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	log.Printf("ws auth ok: userID=%s", userID)
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
		if len(msg) == 0 {
			continue
		}
		// Handle structured messages
		var envelope struct {
			Type    string          `json:"type"`
			Payload json.RawMessage  `json:"payload"`
		}
		if err := json.Unmarshal(msg, &envelope); err == nil && envelope.Type == "zikir_read" {
			HandleZikirRead(userID, envelope.Payload)
			continue
		}
		// Echo back for other messages (enables WsTest)
		select {
		case send <- msg:
		default:
		}
	}
}
