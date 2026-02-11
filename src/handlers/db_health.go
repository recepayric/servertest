package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"servertest/db"
)

type dbHealthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
	Now      string `json:"now"`
}

// DBHealth checks that the server can reach Postgres and returns current DB time.
// This is the endpoint your Unity app can call to verify DB connectivity.
func DBHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	var now time.Time
	if err := db.Pool.QueryRow(ctx, "SELECT NOW()").Scan(&now); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   "error",
			"database": "unreachable",
			"error":    err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(dbHealthResponse{
		Status:   "ok",
		Database: "connected",
		Now:      now.UTC().Format(time.RFC3339Nano),
	})
}

