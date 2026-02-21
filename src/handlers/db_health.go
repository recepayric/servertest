package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"servertest/db"
)

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

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"database": "connected",
		"now":      now.UTC().Format(time.RFC3339Nano),
	})
}
