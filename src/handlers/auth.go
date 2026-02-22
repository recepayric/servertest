package handlers

import (
	"context"
	"encoding/json"
	"net/http"
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
