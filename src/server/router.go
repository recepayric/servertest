package server

import (
	"log"
	"net/http"
	"os"

	"servertest/handlers"
)

// NewMux wires all HTTP routes and static file serving.
func NewMux() http.Handler {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/health", handlers.Health)
	mux.HandleFunc("/api/db-health", handlers.DBHealth)
	mux.HandleFunc("/api/zikirs", handlers.Zikirs)

	// Serve static files from the build directory (or root)
	staticDir := "../build"
	if _, err := os.Stat(staticDir); os.IsNotExist(err) {
		staticDir = ".." // Fallback to repo root if build/ doesn't exist
	}

	log.Printf("📁 Serving static files from: %s", staticDir)

	fileServer := http.FileServer(http.Dir(staticDir))
	mux.Handle("/", http.StripPrefix("/", fileServer))

	return mux
}