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

	// Resolve human-readable name to include in the push
	var zikirName string
	if body.ZikirType == "custom" {
		_ = db.Pool.QueryRow(ctx, `SELECT COALESCE(name_tr, $1) FROM custom_zikirs WHERE id::text = $1`, body.ZikirRef).Scan(&zikirName)
	}
	if zikirName == "" {
		zikirName = body.ZikirRef
	}

	// Notify friend
	var fromCode, fromName string
	_ = db.Pool.QueryRow(ctx, `SELECT friend_code, COALESCE(display_name, '') FROM users WHERE id = $1`, fromID).Scan(&fromCode, &fromName)
	ws.Hub.Push(body.ToUserID, map[string]interface{}{
		"type": "friend_zikir_request",
		"payload": map[string]interface{}{
			"request_id":   requestID,
			"from_user_id": fromID,
			"zikir_type":   body.ZikirType,
			"zikir_ref":    body.ZikirRef,
			"zikir_name":   zikirName,
			"target_count": body.TargetCount,
			"friend_code":  fromCode,
			"display_name": fromName,
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
		SELECT fzr.id::text, fzr.from_user_id::text, fzr.zikir_type, fzr.zikir_ref,
		       COALESCE(cz.name_tr, fzr.zikir_ref, '?') AS zikir_name,
		       fzr.target_count, fzr.created_at::text,
		       u.friend_code, COALESCE(u.display_name, '') as display_name
		FROM friend_zikir_requests fzr
		JOIN users u ON u.id = fzr.from_user_id
		LEFT JOIN custom_zikirs cz ON cz.id::text = fzr.zikir_ref AND fzr.zikir_type = 'custom'
		WHERE fzr.to_user_id::text = $1 AND fzr.status = 'pending'
		AND fzr.created_at > now() - interval '24 hours'
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
		ZikirName   string `json:"zikir_name"`
		TargetCount int    `json:"target_count"`
		CreatedAt   string `json:"created_at"`
		FriendCode  string `json:"friend_code"`
		DisplayName string `json:"display_name"`
	}

	var list []reqEntry
	for rows.Next() {
		var e reqEntry
		if err := rows.Scan(&e.RequestID, &e.FromUserID, &e.ZikirType, &e.ZikirRef, &e.ZikirName, &e.TargetCount, &e.CreatedAt, &e.FriendCode, &e.DisplayName); err != nil {
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
		WHERE id::text = $1 AND to_user_id::text = $2 AND status = 'pending'
		AND created_at > now() - interval '24 hours'
	`, body.RequestID, userID).Scan(&fromUserID, &zikirType, &zikirRef, &targetCount)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request not found, already handled, or expired (24h)"})
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
		WHERE id::text = $1 AND to_user_id::text = $2 AND status = 'pending'
		AND created_at > now() - interval '24 hours'
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

// FriendZikirSentList returns outgoing friend zikir requests sent by the current user.
// Includes live read count when the recipient has accepted.
// GET /api/zikirs/friend/sent
func FriendZikirSentList(w http.ResponseWriter, r *http.Request) {
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
		SELECT
			fzr.id::text,
			fzr.to_user_id::text,
			fzr.zikir_type,
			fzr.zikir_ref,
			COALESCE(cz.name_tr, fzr.zikir_ref, '?') AS zikir_name,
			fzr.target_count,
			fzr.status,
			fzr.created_at::text,
			u.friend_code AS to_friend_code,
			COALESCE(u.display_name, '') AS to_display_name,
			COALESCE(fz.id::text, '') AS friend_zikir_id,
			COALESCE(fz.reads, 0) AS reads,
			(fz.id IS NOT NULL AND fz.reads >= fzr.target_count) AS is_completed,
			COALESCE(fz.updated_at::text, '') AS updated_at,
			COALESCE(fz.completed_at::text, '') AS completed_at
		FROM friend_zikir_requests fzr
		JOIN users u ON u.id = fzr.to_user_id
		LEFT JOIN friend_zikirs fz ON fz.request_id = fzr.id
		LEFT JOIN custom_zikirs cz
			ON cz.id::text = fzr.zikir_ref AND fzr.zikir_type = 'custom'
		WHERE fzr.from_user_id::text = $1
		ORDER BY fzr.created_at DESC
		LIMIT 50
	`, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to list sent zikirs"})
		return
	}
	defer rows.Close()

	type sentEntry struct {
		RequestID     string `json:"request_id"`
		ToUserID      string `json:"to_user_id"`
		ZikirType     string `json:"zikir_type"`
		ZikirRef      string `json:"zikir_ref"`
		ZikirName     string `json:"zikir_name"`
		TargetCount   int    `json:"target_count"`
		Status        string `json:"status"`
		CreatedAt     string `json:"created_at"`
		ToFriendCode  string `json:"to_friend_code"`
		ToDisplayName string `json:"to_display_name"`
		FriendZikirID string `json:"friend_zikir_id"`
		Reads         int    `json:"reads"`
		IsCompleted   bool   `json:"is_completed"`
		UpdatedAt     string `json:"updated_at"`
		CompletedAt   string `json:"completed_at"`
	}

	var list []sentEntry
	for rows.Next() {
		var e sentEntry
		if err := rows.Scan(
			&e.RequestID, &e.ToUserID, &e.ZikirType, &e.ZikirRef, &e.ZikirName,
			&e.TargetCount, &e.Status, &e.CreatedAt,
			&e.ToFriendCode, &e.ToDisplayName,
			&e.FriendZikirID, &e.Reads, &e.IsCompleted,
			&e.UpdatedAt, &e.CompletedAt,
		); err != nil {
			continue
		}
		list = append(list, e)
	}
	if list == nil {
		list = []sentEntry{}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"sent": list})
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
		       fz.updated_at::text, COALESCE(fz.completed_at::text, '') AS completed_at,
		       u.friend_code, COALESCE(u.display_name, '') as display_name
		FROM friend_zikirs fz
		JOIN friend_zikir_requests fzr ON fzr.id = fz.request_id
		JOIN users u ON u.id = fz.from_user_id
		WHERE fz.to_user_id::text = $1
		  AND fz.reads < fzr.target_count
		ORDER BY fz.updated_at DESC
		LIMIT 50
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
		UpdatedAt   string `json:"updated_at"`
		CompletedAt string `json:"completed_at"`
		FriendCode  string `json:"friend_code"`
		DisplayName string `json:"display_name"`
	}

	var list []zikirEntry
	for rows.Next() {
		var e zikirEntry
		if err := rows.Scan(&e.ID, &e.FromUserID, &e.ZikirType, &e.ZikirRef, &e.TargetCount, &e.Reads, &e.CreatedAt, &e.UpdatedAt, &e.CompletedAt, &e.FriendCode, &e.DisplayName); err != nil {
			continue
		}
		list = append(list, e)
	}
	if list == nil {
		list = []zikirEntry{}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"zikirs": list})
}
