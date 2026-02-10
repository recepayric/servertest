package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	// Get port from environment variable (Render sets this)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // Default port for local development
	}

	// Create a new HTTP mux
	mux := http.NewServeMux()

	// API endpoint
	mux.HandleFunc("/api/health", healthHandler)
	mux.HandleFunc("/api/hello", helloHandler)

	// Serve static files from the build directory (or root)
	staticDir := "../build"
	if _, err := os.Stat(staticDir); os.IsNotExist(err) {
		staticDir = ".." // Fallback to root if build doesn't exist
	}

	// Serve static files
	fileServer := http.FileServer(http.Dir(staticDir))
	mux.Handle("/", http.StripPrefix("/", fileServer))

	// Start server
	addr := fmt.Sprintf(":%s", port)
	log.Printf("🚀 Server starting on port %s", port)
	log.Printf("📁 Serving static files from: %s", staticDir)
	log.Printf("🌐 Server running at http://localhost%s", addr)
	
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("❌ Server failed to start: %v", err)
	}
}

// Health check endpoint
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok","service":"go-server"}`)
}

// Hello endpoint
func helloHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"message":"Hello from Go server!","language":"Go"}`)
}
