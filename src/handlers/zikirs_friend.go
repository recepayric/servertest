package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"servertest/db"
	"servertest/ws"
)

// FriendZikirSend sends a zikir to a friend. Friend must accept before they can read.
// POST /api/zikirs/friend/send
func FriendZikirSend(w http.ResponseWriter, r *http.Request) {
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
		ToUserID     string `json:"to_user_id"`
		ZikirType    string `json:"zikir_type"`
		ZikirRef     string `json:"zikir_ref"`
		TargetCount  int    `json:"target_count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}
	if body.ToUserID == "" || body.ZikirRef == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "to_user_id and zikir_ref required"})
		return
	}
	if body.ZikirType != "builtin" && body.ZikirType != "custom" {
		body.ZikirType = "builtin"
	}
	if body.TargetCount <= 0 {
		body.TargetCount = 33
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Must be friends
	var areFriends bool
	_ = db.Pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM friendships WHERE (user_id = $1 AND friend_id = $2) OR (user_id = $2 AND friend_id = $1)
		)
	`, fromID, body.ToUserID).Scan(&areFriends)
	if !areFriends {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "can only send to friends"})
		return
	}

	var requestID string
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO friend_zikir_requests (from_user_id, to_user_id, zikir_type, zikir_ref, target_count)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text
	`, fromID, body.ToUserID, body.ZikirType, body.ZikirRef, body.TargetCount).Scan(&requestID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to send"})
		return
	}

	// Notify friend
	ws.Hub.Push(body.ToUserID, map[string]interface{}{
		"type": "friend_zikir_request",
		"payload": map[string]interface{}{
			"request_id":   requestID,
			"from_user_id": fromID,
			"zikir_type":   body.ZikirType,
			"zikir_ref":    body.ZikirRef,
			"target_count": body.TargetCount,
		},
	})

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"request_id": requestID,
	})
}

// FriendZikirRequestsList returns incoming zikir requests for the user.
// GET /api/zikirs/friend/requests
func FriendZikirRequestsList(w http.ResponseWriter, r *http.Request) {
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
		SELECT fzr.id::text, fzr.from_user_id::text, fzr.zikir_type, fzr.zikir_ref, fzr.target_count, fzr.created_at::text,
		       u.friend_code, COALESCE(u.display_name, '') as display_name
		FROM friend_zikir_requests fzr
		JOIN users u ON u.id = fzr.from_user_id
		WHERE fzr.to_user_id = $1 AND fzr.status = 'pending'
		ORDER BY fzr.created_at DESC
	`, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to list requests"})
		return
	}
	defer rows.Close()

	type reqEntry struct {
		RequestID   string `json:"request_id"`
		FromUserID  string `json:"from_user_id"`
		ZikirType   string `json:"zikir_type"`
		ZikirRef    string `json:"zikir_ref"`
		TargetCount int    `json:"target_count"`
		CreatedAt   string `json:"created_at"`
		FriendCode  string `json:"friend_code"`
		DisplayName string `json:"display_name"`
	}

	var list []reqEntry
	for rows.Next() {
		var e reqEntry
		if err := rows.Scan(&e.RequestID, &e.FromUserID, &e.ZikirType, &e.ZikirRef, &e.TargetCount, &e.CreatedAt, &e.FriendCode, &e.DisplayName); err != nil {
			continue
		}
		list = append(list, e)
	}
	if list == nil {
		list = []reqEntry{}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"requests": list})
}

// FriendZikirAccept accepts a friend zikir request.
// POST /api/zikirs/friend/accept
func FriendZikirAccept(w http.ResponseWriter, r *http.Request) {
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
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RequestID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request_id required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var fromUserID, zikirType, zikirRef string
	var targetCount int
	err := db.Pool.QueryRow(ctx, `
		SELECT from_user_id::text, zikir_type, zikir_ref, target_count
		FROM friend_zikir_requests
		WHERE id::text = $1 AND to_user_id = $2 AND status = 'pending'
	`, body.RequestID, userID).Scan(&fromUserID, &zikirType, &zikirRef, &targetCount)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request not found or already handled"})
		return
	}

	_, _ = db.Pool.Exec(ctx, `UPDATE friend_zikir_requests SET status = 'accepted' WHERE id::text = $1`, body.RequestID)

	var friendZikirID string
	err = db.Pool.QueryRow(ctx, `
		INSERT INTO friend_zikirs (request_id, to_user_id, from_user_id, zikir_type, zikir_ref, target_count)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text
	`, body.RequestID, userID, fromUserID, zikirType, zikirRef, targetCount).Scan(&friendZikirID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to accept"})
		return
	}

	ws.Hub.Push(fromUserID, map[string]interface{}{
		"type": "friend_zikir_accepted",
		"payload": map[string]interface{}{
			"friend_zikir_id": friendZikirID,
			"to_user_id":      userID,
		},
	})

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "ok",
		"friend_zikir_id": friendZikirID,
	})
}

// FriendZikirRefuse refuses a friend zikir request.
// POST /api/zikirs/friend/refuse
func FriendZikirRefuse(w http.ResponseWriter, r *http.Request) {
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
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RequestID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request_id required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	res, err := db.Pool.Exec(ctx, `
		UPDATE friend_zikir_requests SET status = 'refused'
		WHERE id::text = $1 AND to_user_id = $2 AND status = 'pending'
	`, body.RequestID, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to refuse"})
		return
	}
	if res.RowsAffected() == 0 {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request not found or already handled"})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// FriendZikirList returns accepted friend zikirs (that user can read).
// GET /api/zikirs/friend
func FriendZikirList(w http.ResponseWriter, r *http.Request) {
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
		SELECT fz.id::text, fz.from_user_id::text, fz.zikir_type, fz.zikir_ref, fz.target_count, fz.reads, fz.created_at::text,
		       u.friend_code, COALESCE(u.display_name, '') as display_name
		FROM friend_zikirs fz
		JOIN users u ON u.id = fz.from_user_id
		WHERE fz.to_user_id = $1
		ORDER BY fz.created_at DESC
	`, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to list"})
		return
	}
	defer rows.Close()

	type zikirEntry struct {
		ID          string `json:"id"`
		FromUserID  string `json:"from_user_id"`
		ZikirType   string `json:"zikir_type"`
		ZikirRef    string `json:"zikir_ref"`
		TargetCount int    `json:"target_count"`
		Reads       int    `json:"reads"`
		CreatedAt   string `json:"created_at"`
		FriendCode  string `json:"friend_code"`
		DisplayName string `json:"display_name"`
	}

	var list []zikirEntry
	for rows.Next() {
		var e zikirEntry
		if err := rows.Scan(&e.ID, &e.FromUserID, &e.ZikirType, &e.ZikirRef, &e.TargetCount, &e.Reads, &e.CreatedAt, &e.FriendCode, &e.DisplayName); err != nil {
			continue
		}
		list = append(list, e)
	}
	if list == nil {
		list = []zikirEntry{}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"zikirs": list})
}
