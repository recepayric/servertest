package handlers

import (
	"context"
	"net/http"
	"time"

	"servertest/db"
)

const guestTokenHeader = "X-Guest-Token"

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
