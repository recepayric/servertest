package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"

	"servertest/db"
	"servertest/metrics"
	"servertest/server"
	"servertest/ws"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.Init(ctx); err != nil {
		log.Fatalf("❌ Failed to connect to database: %v", err)
	}
	defer db.Close()

	mux := server.NewMux()

	addr := fmt.Sprintf(":%s", port)
	log.Printf("🚀 Server starting on port %s", port)
	log.Printf("🌐 Server running at http://localhost%s", addr)
	log.Printf("📡 API endpoints:")
	log.Printf("   - GET  /api/health")
	log.Printf("   - GET  /api/db-health")
	log.Printf("   - GET  /api/zikirs")
	log.Printf("   - POST /api/guest/register")
	log.Printf("   - GET  /api/friends")
	log.Printf("   - POST /api/friends/request")
	log.Printf("   - POST /api/friends/request/accept")
	log.Printf("   - POST /api/friends/request/refuse")
	log.Printf("   - GET  /api/friends/requests")
	log.Printf("   - GET  /api/friends/requests/sent")
	log.Printf("   - POST /api/friends/remove")
	log.Printf("   - WS   /ws, /ws/echo")
	log.Printf("   - GET  /api/debug (test: open in browser)")
	log.Printf("")
	log.Printf("💡 Every request will be logged. If you don't see 'POST /api/friends/request' in logs, the request isn't reaching this server.")

	go statusReporter(5 * time.Second)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("❌ Server failed to start: %v", err)
	}
}

func statusReporter(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		conns := ws.Hub.ConnectionCount()
		reqs := server.GetAndResetRequestCount()
		bytesOut := metrics.GetAndResetBytesOut()

		allocMB := float64(m.Alloc) / 1024 / 1024
		sysMB := float64(m.Sys) / 1024 / 1024
		goroutines := runtime.NumGoroutine()

		bytesKB := float64(bytesOut) / 1024
		bytesStr := fmt.Sprintf("%.0fB", float64(bytesOut))
		if bytesOut >= 1024 {
			bytesStr = fmt.Sprintf("%.1fKB", bytesKB)
		}
		if bytesOut >= 1024*1024 {
			bytesStr = fmt.Sprintf("%.1fMB", float64(bytesOut)/1024/1024)
		}

		log.Printf("📊 WS:%d reqs:%d out:%s mem:%.1fMB sys:%.1fMB goroutines:%d",
			conns, reqs, bytesStr, allocMB, sysMB, goroutines)
	}
}
