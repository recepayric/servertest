package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"servertest/db"
)

const guestTokenHeader = "X-Guest-Token"

// Me returns the current user's profile (user_id, friend_code, display_name).
func Me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	userID, ok := getUserIDFromRequest(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing or invalid X-Guest-Token"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var friendCode, displayName string
	err := db.Pool.QueryRow(ctx, `SELECT friend_code, COALESCE(display_name, '') FROM users WHERE id::text = $1`, userID).Scan(&friendCode, &displayName)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "user not found"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"user_id":      userID,
		"friend_code":  friendCode,
		"display_name": displayName,
	})
}

// UpdateDisplayName lets the current user change their display_name.
// POST /api/me/display-name
// Body: { "display_name": "New Name" }
func UpdateDisplayName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	userID, ok := getUserIDFromRequest(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing or invalid X-Guest-Token"})
		return
	}

	var body struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	name := strings.TrimSpace(body.DisplayName)
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "display_name required"})
		return
	}
	if len(name) > 64 {
		name = name[:64]
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err := db.Pool.Exec(ctx, `UPDATE users SET display_name = $1 WHERE id::text = $2`, name, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to update display name"})
		return
	}

	// Return same shape as GET /api/me so client can reuse MeResponse.
	var friendCode, displayName string
	err = db.Pool.QueryRow(ctx, `SELECT friend_code, COALESCE(display_name, '') FROM users WHERE id::text = $1`, userID).
		Scan(&friendCode, &displayName)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to load user"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"user_id":      userID,
		"friend_code":  friendCode,
		"display_name": displayName,
	})
}

// getUserIDFromRequest returns the user's ID if X-Guest-Token is valid, or empty string.
func getUserIDFromRequest(r *http.Request) (string, bool) {
	token := r.Header.Get(guestTokenHeader)
	if token == "" {
		return "", false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	var userID string
	err := db.Pool.QueryRow(ctx, `SELECT id::text FROM users WHERE guest_token = $1`, token).Scan(&userID)
	if err != nil {
		return "", false
	}
	return userID, true
}
