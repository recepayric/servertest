package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"servertest/db"
	"servertest/server"
)

func main() {
	// Get port from environment variable (Render sets this)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // Default port for local development
	}

	// Initialize DB pool (lives for life of the server)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.Init(ctx); err != nil {
		log.Fatalf("❌ Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Wire HTTP routes + static files
	mux := server.NewMux()

	// Start server
	addr := fmt.Sprintf(":%s", port)
	log.Printf("🚀 Server starting on port %s", port)
	log.Printf("🌐 Server running at http://localhost%s", addr)
	log.Printf("📡 API endpoints:")
	log.Printf("   - GET http://localhost%s/api/health", addr)
	log.Printf("   - GET http://localhost%s/api/db-health", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("❌ Server failed to start: %v", err)
	}
}

