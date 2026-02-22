package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"servertest/db"
	"servertest/ws"
)

// GroupsLeave leaves a group. Cannot leave if owner (or transfer ownership first).
// POST /api/groups/leave
func GroupsLeave(w http.ResponseWriter, r *http.Request) {
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
		GroupID string `json:"group_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.GroupID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "group_id required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var ownerID string
	err := db.Pool.QueryRow(ctx, `SELECT owner_id::text FROM groups WHERE id::text = $1`, body.GroupID).Scan(&ownerID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "group not found"})
		return
	}
	if ownerID == userID {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "owner cannot leave; transfer ownership or delete group"})
		return
	}

	_, err = db.Pool.Exec(ctx, `DELETE FROM group_members WHERE group_id = $1 AND user_id = $2`, body.GroupID, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to leave"})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// GroupsZikirsList returns zikirs in a group.
// GET /api/groups/zikirs?group_id=xxx
func GroupsZikirsList(w http.ResponseWriter, r *http.Request) {
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

	groupID := r.URL.Query().Get("group_id")
	if groupID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "group_id required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var isMember bool
	_ = db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_members WHERE group_id = $1 AND user_id = $2)`, groupID, userID).Scan(&isMember)
	if !isMember {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not a group member"})
		return
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT gz.id::text, gz.zikir_type, gz.zikir_ref, gz.mode, gz.target_count, gz.added_by_user_id::text, gz.created_at::text
		FROM group_zikirs gz
		WHERE gz.group_id = $1
		ORDER BY gz.created_at ASC
	`, groupID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to list zikirs"})
		return
	}
	defer rows.Close()

	type zikirRef struct {
		ID            string         `json:"id"`
		ZikirType     string         `json:"zikir_type"`
		ZikirRef      string         `json:"zikir_ref"`
		Mode          string         `json:"mode"`
		TargetCount   int            `json:"target_count"`
		AddedByUserID string         `json:"added_by_user_id"`
		CreatedAt     string         `json:"created_at"`
		Progress      map[string]int `json:"progress"`
	}

	var list []zikirRef
	for rows.Next() {
		var z zikirRef
		var mode string
		var targetCount int
		if err := rows.Scan(&z.ID, &z.ZikirType, &z.ZikirRef, &mode, &targetCount, &z.AddedByUserID, &z.CreatedAt); err != nil {
			continue
		}
		z.Mode = mode
		if z.Mode == "" {
			z.Mode = "pooled"
		}
		z.TargetCount = targetCount
		if z.TargetCount <= 0 {
			z.TargetCount = 100
		}
		z.Progress = make(map[string]int)
		if z.Mode == "pooled" {
			var total int
			_ = db.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(reads), 0) FROM group_zikir_progress WHERE group_zikir_id = $1`, z.ID).Scan(&total)
			z.Progress["total"] = total
		} else {
			rows2, _ := db.Pool.Query(ctx, `SELECT user_id::text, reads FROM group_zikir_progress WHERE group_zikir_id = $1`, z.ID)
			if rows2 != nil {
				for rows2.Next() {
					var uid string
					var r int
					if err := rows2.Scan(&uid, &r); err == nil {
						z.Progress[uid] = r
					}
				}
				rows2.Close()
			}
		}
		list = append(list, z)
	}
	if list == nil {
		list = []zikirRef{}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"zikirs": list})
}

// GroupsZikirAdd adds a zikir to a group. Any member can add.
// POST /api/groups/zikirs/add
func GroupsZikirAdd(w http.ResponseWriter, r *http.Request) {
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
		GroupID     string `json:"group_id"`
		ZikirType   string `json:"zikir_type"`
		ZikirRef    string `json:"zikir_ref"`
		Mode        string `json:"mode"`
		TargetCount int    `json:"target_count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}
	if body.GroupID == "" || body.ZikirRef == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "group_id and zikir_ref required"})
		return
	}
	if body.ZikirType != "builtin" && body.ZikirType != "custom" {
		body.ZikirType = "builtin"
	}
	if body.Mode != "pooled" && body.Mode != "individual" {
		body.Mode = "pooled"
	}
	if body.TargetCount <= 0 {
		body.TargetCount = 100
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var isMember bool
	_ = db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_members WHERE group_id = $1 AND user_id = $2)`, body.GroupID, userID).Scan(&isMember)
	if !isMember {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not a group member"})
		return
	}

	if body.ZikirType == "custom" {
		var ownerID string
		err := db.Pool.QueryRow(ctx, `SELECT user_id::text FROM custom_zikirs WHERE id::text = $1`, body.ZikirRef).Scan(&ownerID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "custom zikir not found"})
			return
		}
		var isFriendOrOwner bool
		_ = db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_members WHERE group_id = $1 AND user_id = $2)`, body.GroupID, ownerID).Scan(&isFriendOrOwner)
		if ownerID != userID && !isFriendOrOwner {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "can only add your own custom zikirs or from group members"})
			return
		}
	}

	res, err := db.Pool.Exec(ctx, `
		INSERT INTO group_zikirs (group_id, zikir_type, zikir_ref, mode, target_count, added_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (group_id, zikir_type, zikir_ref) DO NOTHING
	`, body.GroupID, body.ZikirType, body.ZikirRef, body.Mode, body.TargetCount, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to add"})
		return
	}
	if res.RowsAffected() == 0 {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "zikir already in group"})
		return
	}

	rows, _ := db.Pool.Query(ctx, `SELECT user_id::text FROM group_members WHERE group_id = $1`, body.GroupID)
	var userIDs []string
	if rows != nil {
		for rows.Next() {
			var u string
			if err := rows.Scan(&u); err == nil {
				userIDs = append(userIDs, u)
			}
		}
		rows.Close()
	}
	if len(userIDs) > 0 {
		ws.Hub.PushToMany(userIDs, map[string]interface{}{
			"type": "group_zikir_added",
			"payload": map[string]interface{}{"group_id": body.GroupID},
		})
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// GroupsZikirRemove removes a zikir from a group. Owner or who added can remove.
// POST /api/groups/zikirs/remove
func GroupsZikirRemove(w http.ResponseWriter, r *http.Request) {
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
		GroupID string `json:"group_id"`
		ZikirID string `json:"zikir_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}
	if body.GroupID == "" || body.ZikirID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "group_id and zikir_id required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	res, err := db.Pool.Exec(ctx, `
		DELETE FROM group_zikirs
		WHERE id::text = $1 AND group_id = $2
		AND (added_by_user_id = $3 OR EXISTS (SELECT 1 FROM groups WHERE id::text = $2 AND owner_id = $3))
	`, body.ZikirID, body.GroupID, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to remove"})
		return
	}
	if res.RowsAffected() == 0 {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "zikir not found or no permission"})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// GroupsZikirRequest creates a request to add a zikir to a group (owner approves).
// POST /api/groups/zikirs/request
func GroupsZikirRequest(w http.ResponseWriter, r *http.Request) {
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
		GroupID     string `json:"group_id"`
		ZikirType   string `json:"zikir_type"`
		ZikirRef    string `json:"zikir_ref"`
		Mode        string `json:"mode"`
		TargetCount int    `json:"target_count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}
	if body.GroupID == "" || body.ZikirRef == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "group_id and zikir_ref required"})
		return
	}
	if body.ZikirType != "builtin" && body.ZikirType != "custom" {
		body.ZikirType = "builtin"
	}
	if body.Mode != "pooled" && body.Mode != "individual" {
		body.Mode = "pooled"
	}
	if body.TargetCount <= 0 {
		body.TargetCount = 100
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var isMember bool
	_ = db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_members WHERE group_id = $1 AND user_id = $2)`, body.GroupID, userID).Scan(&isMember)
	if !isMember {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not a group member"})
		return
	}

	var ownerID string
	_ = db.Pool.QueryRow(ctx, `SELECT owner_id::text FROM groups WHERE id::text = $1`, body.GroupID).Scan(&ownerID)
	if ownerID == userID {
		// Owner can add directly
		_, _ = db.Pool.Exec(ctx, `
			INSERT INTO group_zikirs (group_id, zikir_type, zikir_ref, mode, target_count, added_by_user_id)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (group_id, zikir_type, zikir_ref) DO NOTHING
		`, body.GroupID, body.ZikirType, body.ZikirRef, body.Mode, body.TargetCount, userID)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "added": "true"})
		return
	}

	var requestID string
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO group_zikir_requests (group_id, from_user_id, zikir_type, zikir_ref, mode, target_count)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text
	`, body.GroupID, userID, body.ZikirType, body.ZikirRef, body.Mode, body.TargetCount).Scan(&requestID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to create request"})
		return
	}

	ws.Hub.Push(ownerID, map[string]interface{}{
		"type": "group_zikir_request",
		"payload": map[string]interface{}{
			"request_id":   requestID,
			"group_id":     body.GroupID,
			"zikir_type":   body.ZikirType,
			"zikir_ref":    body.ZikirRef,
			"mode":         body.Mode,
			"target_count": body.TargetCount,
			"from_user_id": userID,
		},
	})

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"request_id": requestID,
	})
}

// GroupsZikirRequestsList returns pending zikir requests for groups the user owns.
// GET /api/groups/zikirs/requests?group_id=xxx
func GroupsZikirRequestsList(w http.ResponseWriter, r *http.Request) {
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

	groupID := r.URL.Query().Get("group_id")
	if groupID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "group_id required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, err := db.Pool.Query(ctx, `
		SELECT gzr.id::text, gzr.group_id::text, gzr.from_user_id::text, gzr.zikir_type, gzr.zikir_ref,
		       COALESCE(gzr.mode, 'pooled'), COALESCE(gzr.target_count, 100), gzr.created_at::text,
		       u.friend_code, COALESCE(u.display_name, '')
		FROM group_zikir_requests gzr
		JOIN groups g ON g.id::text = gzr.group_id AND g.owner_id = $1
		JOIN users u ON u.id = gzr.from_user_id
		WHERE gzr.group_id = $2 AND gzr.status = 'pending'
		ORDER BY gzr.created_at DESC
	`, userID, groupID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to list requests"})
		return
	}
	defer rows.Close()

	type reqEntry struct {
		RequestID   string `json:"request_id"`
		GroupID     string `json:"group_id"`
		FromUserID  string `json:"from_user_id"`
		ZikirType   string `json:"zikir_type"`
		ZikirRef    string `json:"zikir_ref"`
		Mode        string `json:"mode"`
		TargetCount int    `json:"target_count"`
		CreatedAt   string `json:"created_at"`
		FriendCode  string `json:"friend_code"`
		DisplayName string `json:"display_name"`
	}

	var list []reqEntry
	for rows.Next() {
		var e reqEntry
		if err := rows.Scan(&e.RequestID, &e.GroupID, &e.FromUserID, &e.ZikirType, &e.ZikirRef, &e.Mode, &e.TargetCount, &e.CreatedAt, &e.FriendCode, &e.DisplayName); err != nil {
			continue
		}
		list = append(list, e)
	}
	if list == nil {
		list = []reqEntry{}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"requests": list})
}

// GroupsZikirRequestAccept accepts a zikir request (owner only).
// POST /api/groups/zikirs/requests/accept
func GroupsZikirRequestAccept(w http.ResponseWriter, r *http.Request) {
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

	var groupID, fromUserID, zikirType, zikirRef, mode string
	var targetCount int
	err := db.Pool.QueryRow(ctx, `
		SELECT gzr.group_id::text, gzr.from_user_id::text, gzr.zikir_type, gzr.zikir_ref, COALESCE(gzr.mode, 'pooled'), COALESCE(gzr.target_count, 100)
		FROM group_zikir_requests gzr
		JOIN groups g ON g.id = gzr.group_id AND g.owner_id = $1
		WHERE gzr.id::text = $2 AND gzr.status = 'pending'
	`, userID, body.RequestID).Scan(&groupID, &fromUserID, &zikirType, &zikirRef, &mode, &targetCount)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request not found or already handled"})
		return
	}

	_, _ = db.Pool.Exec(ctx, `UPDATE group_zikir_requests SET status = 'accepted' WHERE id::text = $1`, body.RequestID)
	_, _ = db.Pool.Exec(ctx, `
		INSERT INTO group_zikirs (group_id, zikir_type, zikir_ref, mode, target_count, added_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (group_id, zikir_type, zikir_ref) DO NOTHING
	`, groupID, zikirType, zikirRef, mode, targetCount, fromUserID)

	ws.Hub.Push(fromUserID, map[string]interface{}{
		"type": "group_zikir_request_accepted",
		"payload": map[string]interface{}{
			"group_id":   groupID,
			"zikir_type": zikirType,
			"zikir_ref":  zikirRef,
		},
	})

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// GroupsZikirRequestRefuse refuses a zikir request (owner only).
// POST /api/groups/zikirs/requests/refuse
func GroupsZikirRequestRefuse(w http.ResponseWriter, r *http.Request) {
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
		UPDATE group_zikir_requests SET status = 'refused'
		WHERE id::text = $1 AND status = 'pending'
		AND EXISTS (SELECT 1 FROM groups g WHERE g.id = group_zikir_requests.group_id AND g.owner_id = $2)
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
