package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"servertest/db"
)

func FriendsRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
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
		FriendID   string `json:"friend_id"`
		FriendCode string `json:"friend_code"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if body.FriendID == "" && body.FriendCode == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "friend_id or friend_code required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var friendID string
	if body.FriendID != "" {
		friendID = body.FriendID
	} else {
		err := db.Pool.QueryRow(ctx, `SELECT id::text FROM users WHERE friend_code = $1`, body.FriendCode).Scan(&friendID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "friend code not found"})
			return
		}
	}

	// Remove both directions
	_, err := db.Pool.Exec(ctx, `
		DELETE FROM friendships WHERE (user_id = $1 AND friend_id = $2) OR (user_id = $2 AND friend_id = $1)
	`, userID, friendID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to remove friend"})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type friendEntry struct {
	UserID     string `json:"user_id"`
	FriendCode string `json:"friend_code"`
	DisplayName string `json:"display_name,omitempty"`
}

func FriendsList(w http.ResponseWriter, r *http.Request) {
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

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, err := db.Pool.Query(ctx, `
		SELECT u.id::text, u.friend_code, COALESCE(u.display_name, '') 
		FROM friendships f
		JOIN users u ON u.id = f.friend_id
		WHERE f.user_id = $1
		ORDER BY f.created_at
	`, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to list friends"})
		return
	}
	defer rows.Close()

	var friends []friendEntry
	for rows.Next() {
		var e friendEntry
		if err := rows.Scan(&e.UserID, &e.FriendCode, &e.DisplayName); err != nil {
			continue
		}
		friends = append(friends, e)
	}
	if friends == nil {
		friends = []friendEntry{}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"friends": friends})
}
