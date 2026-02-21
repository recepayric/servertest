package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	mode := flag.String("mode", "client", "server | client")
	url := flag.String("url", "ws://localhost:8081/ws", "WebSocket URL (client only)")
	n := flag.Int("n", 1000, "number of connections to open (client only)")
	batch := flag.Int("batch", 100, "connections per batch, with 10ms delay (client only)")
	port := flag.Int("port", 8081, "server port (server only)")
	flag.Parse()

	switch *mode {
	case "server":
		runServer(*port)
	case "client":
		runClient(*url, *n, *batch)
	default:
		log.Fatalf("mode must be 'server' or 'client'")
	}
}

func runServer(port int) {
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	addr := fmt.Sprintf(":%d", port)
	log.Printf("WebSocket server on ws://localhost%s/ws", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func runClient(url string, total, batchSize int) {
	log.Printf("Target: %d connections | batch: %d | URL: %s", total, batchSize, url)
	log.Printf("OS: %s | GOMAXPROCS: %d", runtime.GOOS, runtime.GOMAXPROCS(0))

	var conns []*websocket.Conn
	var mu sync.Mutex
	var successCount atomic.Int64
	var failCount atomic.Int64
	var lastErr string
	var lastErrMu sync.Mutex

	for i := 0; i < total; i += batchSize {
		batch := batchSize
		if i+batch > total {
			batch = total - i
		}

		for j := 0; j < batch; j++ {
			go func() {
				conn, _, err := websocket.DefaultDialer.Dial(url, nil)
				if err != nil {
					failCount.Add(1)
					lastErrMu.Lock()
					lastErr = err.Error()
					lastErrMu.Unlock()
					return
				}
				successCount.Add(1)
				mu.Lock()
				conns = append(conns, conn)
				mu.Unlock()
			}()
		}

		time.Sleep(10 * time.Millisecond)
		succ := successCount.Load()
		fail := failCount.Load()
		if fail > 0 {
			lastErrMu.Lock()
			e := lastErr
			lastErrMu.Unlock()
			log.Printf("Progress: %d OK, %d failed (last: %s)", succ, fail, e)
			if fail > int64(batch) {
				break
			}
		} else if (i+batch)%500 == 0 || i+batch == total {
			log.Printf("Progress: %d connections open", succ)
		}
	}

	time.Sleep(500 * time.Millisecond)
	succ := successCount.Load()
	fail := failCount.Load()

	log.Printf("\n--- Result ---")
	log.Printf("Connected: %d", succ)
	log.Printf("Failed:    %d", fail)
	log.Printf("Total:     %d", succ+fail)

	if fail > 0 {
		lastErrMu.Lock()
		log.Printf("Last error: %s", lastErr)
		lastErrMu.Unlock()
		log.Printf("\nCommon causes:")
		log.Printf("  - 'too many open files' / FD limit: ulimit -n (Linux/Mac)")
		log.Printf("  - Windows: per-process handle limit (often ~512 default)")
		log.Printf("  - Raise: ulimit -n 65535 (Linux) or increase handle quota")
	}

	mu.Lock()
	log.Printf("\nKeeping %d connections open for 5s (check netstat/ss)", len(conns))
	mu.Unlock()

	time.Sleep(5 * time.Second)

	mu.Lock()
	for _, c := range conns {
		c.Close()
	}
	mu.Unlock()

	log.Printf("Closed all. Done.")
}
