package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"servertest/db"
	"servertest/ws"
)

// FriendsRequest sends a friend request (recipient must accept).
func FriendsRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	fromID, ok := getUserIDFromRequest(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing or invalid X-Guest-Token"})
		return
	}

	var body struct {
		FriendCode string `json:"friend_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.FriendCode == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "friend_code required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var toID string
	err := db.Pool.QueryRow(ctx, `SELECT id::text FROM users WHERE friend_code = $1`, body.FriendCode).Scan(&toID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "friend code not found"})
		return
	}
	if toID == fromID {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "cannot request yourself"})
		return
	}

	// Check if already friends
	var exists bool
	_ = db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM friendships WHERE user_id = $1 AND friend_id = $2)`, fromID, toID).Scan(&exists)
	if exists {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "already friends"})
		return
	}

	// Check if reverse request already pending (mutual add - auto-accept both)
	var reverseRequestID string
	err = db.Pool.QueryRow(ctx, `SELECT id::text FROM friendship_requests WHERE from_user_id = $2 AND to_user_id = $1 AND status = 'pending'`, fromID, toID).Scan(&reverseRequestID)
	if err == nil {
		// B already sent to A - auto-accept and create friendship
		_, _ = db.Pool.Exec(ctx, `UPDATE friendship_requests SET status = 'accepted' WHERE id::text = $1`, reverseRequestID)
		_, _ = db.Pool.Exec(ctx, `INSERT INTO friendships (user_id, friend_id) VALUES ($1, $2), ($2, $1) ON CONFLICT (user_id, friend_id) DO NOTHING`, fromID, toID)
		_ = bumpSocialRevs(ctx, fromID, revFriends, revFriendPending, revFriendSent)
		_ = bumpSocialRevs(ctx, toID, revFriends, revFriendPending, revFriendSent)
		var fromCode, fromName string
		_ = db.Pool.QueryRow(ctx, `SELECT friend_code, COALESCE(display_name, '') FROM users WHERE id = $1`, fromID).Scan(&fromCode, &fromName)
		ws.Hub.Push(toID, map[string]interface{}{
			"type": "friend_request_accepted",
			"payload": map[string]string{
				"request_id":   reverseRequestID,
				"friend_id":    fromID,
				"friend_code":  fromCode,
				"display_name": fromName,
			},
		})
		var toCode, toName string
		_ = db.Pool.QueryRow(ctx, `SELECT friend_code, COALESCE(display_name, '') FROM users WHERE id = $1`, toID).Scan(&toCode, &toName)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "ok",
			"accepted":     "mutual",
			"friend_id":    toID,
			"friend_code":  toCode,
			"display_name": toName,
		})
		return
	}

	// Check if request already pending (we already sent to them)
	_ = db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM friendship_requests WHERE from_user_id = $1 AND to_user_id = $2 AND status = 'pending')`, fromID, toID).Scan(&exists)
	if exists {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "already_sent", "error": "request already sent"})
		return
	}

	var requestID string
	err = db.Pool.QueryRow(ctx, `
		INSERT INTO friendship_requests (from_user_id, to_user_id, status)
		VALUES ($1, $2, 'pending')
		RETURNING id::text
	`, fromID, toID).Scan(&requestID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to create request"})
		return
	}
	_ = bumpSocialRevs(ctx, fromID, revFriendSent)
	_ = bumpSocialRevs(ctx, toID, revFriendPending)

	// Push to recipient via WebSocket
	var fromCode, fromName string
	_ = db.Pool.QueryRow(ctx, `SELECT friend_code, COALESCE(display_name, '') FROM users WHERE id = $1`, fromID).Scan(&fromCode, &fromName)
	ws.Hub.Push(toID, map[string]interface{}{
		"type": "friend_request",
		"payload": map[string]string{
			"request_id":        requestID,
			"from_user_id":      fromID,
			"from_friend_code":  fromCode,
			"from_display_name": fromName,
		},
	})

	// Return recipient info so client can show "Pending: [name]"
	var toCode, toName string
	_ = db.Pool.QueryRow(ctx, `SELECT friend_code, COALESCE(display_name, '') FROM users WHERE id = $1`, toID).Scan(&toCode, &toName)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "ok",
		"request_id":  requestID,
		"friend_id":   toID,
		"friend_code": toCode,
		"display_name": toName,
	})
}

func FriendsRequestAccept(w http.ResponseWriter, r *http.Request) {
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

	// Read body once so we can log it on failure
	rawBody, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(rawBody))
	log.Printf("📥 FriendsRequestAccept body: %s | Content-Type: %s | user: %s", string(rawBody), r.Header.Get("Content-Type"), userID)

	var body struct {
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(bytes.NewReader(rawBody)).Decode(&body); err != nil || body.RequestID == "" {
		log.Printf("❌ FriendsRequestAccept 400 — decode_err=%v request_id=%q raw=%s", err, body.RequestID, string(rawBody))
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request_id required"})
		return
	}
	log.Printf("✅ FriendsRequestAccept — request_id=%s user=%s", body.RequestID, userID)

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var fromID string
	err := db.Pool.QueryRow(ctx, `
		UPDATE friendship_requests 
		SET status = 'accepted'
		WHERE id::text = $1 AND to_user_id = $2 AND status = 'pending'
		RETURNING from_user_id::text
	`, body.RequestID, userID).Scan(&fromID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request not found or already handled"})
		return
	}

	// Create friendship both ways
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO friendships (user_id, friend_id)
		VALUES ($1, $2), ($2, $1)
		ON CONFLICT (user_id, friend_id) DO NOTHING
	`, userID, fromID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to add friend"})
		return
	}
	_ = bumpSocialRevs(ctx, userID, revFriends, revFriendPending)
	_ = bumpSocialRevs(ctx, fromID, revFriends, revFriendSent)

	var friendCode, friendName string
	_ = db.Pool.QueryRow(ctx, `SELECT friend_code, COALESCE(display_name, '') FROM users WHERE id = $1`, fromID).Scan(&friendCode, &friendName)
	ws.Hub.Push(fromID, map[string]interface{}{
		"type": "friend_request_accepted",
		"payload": map[string]string{
			"request_id":   body.RequestID,
			"friend_id":    userID,
			"friend_code":  friendCode,
			"display_name": friendName,
		},
	})

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "ok",
		"friend_id":    fromID,
		"friend_code":  friendCode,
		"display_name": friendName,
	})
}

func FriendsRequestRefuse(w http.ResponseWriter, r *http.Request) {
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

	rawBody, _ := io.ReadAll(r.Body)
	log.Printf("📥 FriendsRequestRefuse body: %s | user: %s", string(rawBody), userID)

	var body struct {
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(bytes.NewReader(rawBody)).Decode(&body); err != nil || body.RequestID == "" {
		log.Printf("❌ FriendsRequestRefuse 400 — decode_err=%v request_id=%q raw=%s", err, body.RequestID, string(rawBody))
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request_id required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	result, err := db.Pool.Exec(ctx, `
		UPDATE friendship_requests SET status = 'refused'
		WHERE id::text = $1 AND to_user_id = $2 AND status = 'pending'
	`, body.RequestID, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to refuse"})
		return
	}
	if result.RowsAffected() == 0 {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request not found or already handled"})
		return
	}
	var fromUserID string
	_ = db.Pool.QueryRow(ctx, `SELECT from_user_id::text FROM friendship_requests WHERE id::text = $1`, body.RequestID).Scan(&fromUserID)
	_ = bumpSocialRevs(ctx, userID, revFriendPending)
	_ = bumpSocialRevs(ctx, fromUserID, revFriendSent)
	ws.Hub.Push(fromUserID, map[string]interface{}{
		"type": "friend_request_refused",
		"payload": map[string]string{
			"request_id": body.RequestID,
		},
	})

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type friendRequestEntry struct {
	RequestID      string `json:"request_id"`
	FromUserID     string `json:"from_user_id"`
	FromFriendCode string `json:"from_friend_code"`
	FromDisplayName string `json:"from_display_name"`
	CreatedAt      string `json:"created_at"`
}

func FriendsRequestList(w http.ResponseWriter, r *http.Request) {
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
		SELECT fr.id::text, u.id::text, u.friend_code, COALESCE(u.display_name, ''), fr.created_at::text
		FROM friendship_requests fr
		JOIN users u ON u.id = fr.from_user_id
		WHERE fr.to_user_id = $1 AND fr.status = 'pending'
		ORDER BY fr.created_at DESC
	`, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to list requests"})
		return
	}
	defer rows.Close()

	var list []friendRequestEntry
	for rows.Next() {
		var e friendRequestEntry
		if err := rows.Scan(&e.RequestID, &e.FromUserID, &e.FromFriendCode, &e.FromDisplayName, &e.CreatedAt); err != nil {
			continue
		}
		list = append(list, e)
	}
	if list == nil {
		list = []friendRequestEntry{}
	}

	// Map to Unity-expected format
	type reqItem struct {
		RequestID       string `json:"request_id"`
		FromUserID      string `json:"from_user_id"`
		FromFriendCode  string `json:"from_friend_code"`
		FromDisplayName string `json:"from_display_name"`
		CreatedAt       string `json:"created_at"`
	}
	items := make([]reqItem, len(list))
	for i := range list {
		items[i] = reqItem(list[i])
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"requests": items})
}

// FriendsRequestListSent returns outgoing requests (pending, waiting for them to accept).
func FriendsRequestListSent(w http.ResponseWriter, r *http.Request) {
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
		SELECT fr.id::text, u.id::text, u.friend_code, COALESCE(u.display_name, '')
		FROM friendship_requests fr
		JOIN users u ON u.id = fr.to_user_id
		WHERE fr.from_user_id = $1 AND fr.status = 'pending'
		ORDER BY fr.created_at DESC
	`, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to list sent requests"})
		return
	}
	defer rows.Close()

	type sentItem struct {
		RequestID   string `json:"request_id"`
		FriendID    string `json:"friend_id"`
		FriendCode  string `json:"friend_code"`
		DisplayName string `json:"display_name"`
	}
	var list []sentItem
	for rows.Next() {
		var e sentItem
		if err := rows.Scan(&e.RequestID, &e.FriendID, &e.FriendCode, &e.DisplayName); err != nil {
			continue
		}
		list = append(list, e)
	}
	if list == nil {
		list = []sentItem{}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"requests": list})
}
