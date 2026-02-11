package handlers

import (
	"encoding/json"
	"net/http"
)

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

// Health is a simple server health check (no DB).
func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthResponse{
		Status:  "ok",
		Service: "go-server",
	})
}

