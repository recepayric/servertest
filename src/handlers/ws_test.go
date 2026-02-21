package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(WebSocket))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	// httptest gives http://..., so wsURL becomes ws://127.0.0.1:port/
	// But the handler is mounted at "/" in httptest, so the path is /
	// We need to hit the same path. Server serves WebSocket at "/" in this test.
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("expected 101, got %d", resp.StatusCode)
	}

	// Send and receive
	msg := []byte("hello")
	if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, recv, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(recv) != string(msg) {
		t.Errorf("expected %q, got %q", msg, recv)
	}
}

func TestWebSocketConcurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(WebSocket))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				t.Errorf("dial %d: %v", id, err)
				return
			}
			defer conn.Close()
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			msg := []byte("ping")
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				t.Errorf("write %d: %v", id, err)
				return
			}
			_, recv, err := conn.ReadMessage()
			if err != nil {
				t.Errorf("read %d: %v", id, err)
				return
			}
			if string(recv) != "ping" {
				t.Errorf("conn %d: expected ping, got %q", id, recv)
			}
		}(i)
	}
	wg.Wait()
}
