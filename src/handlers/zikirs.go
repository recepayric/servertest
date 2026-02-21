package handlers

import (
	"encoding/json"
	"net/http"
)

// Zikirs returns empty content for now (no DB read yet).
func Zikirs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"metadata": map[string]any{"version": 0},
		"zikirs":   []any{},
	})
}
