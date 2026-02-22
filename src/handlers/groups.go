package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"servertest/db"
	"servertest/ws"
)

// GroupsCreate creates a new group. Creator becomes owner and first member.
func GroupsCreate(w http.ResponseWriter, r *http.Request) {
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
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "name required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var groupID string
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO groups (name, owner_id) VALUES ($1, $2)
		RETURNING id::text
	`, body.Name, userID).Scan(&groupID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to create group"})
		return
	}

	_, _ = db.Pool.Exec(ctx, `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, groupID, userID)

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"group_id": groupID,
		"name":     body.Name,
		"owner_id": userID,
	})
}

type groupEntry struct {
	GroupID string `json:"group_id"`
	Name    string `json:"name"`
	OwnerID string `json:"owner_id"`
}

// GroupsList returns groups the user is a member of.
func GroupsList(w http.ResponseWriter, r *http.Request) {
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
		SELECT g.id::text, g.name, g.owner_id::text
		FROM groups g
		JOIN group_members gm ON gm.group_id = g.id
		WHERE gm.user_id = $1
		ORDER BY g.created_at
	`, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to list groups"})
		return
	}
	defer rows.Close()

	var groups []groupEntry
	for rows.Next() {
		var e groupEntry
		if err := rows.Scan(&e.GroupID, &e.Name, &e.OwnerID); err != nil {
			continue
		}
		groups = append(groups, e)
	}
	if groups == nil {
		groups = []groupEntry{}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"groups": groups})
}

// GroupsMembers returns members of a group. Caller must be a member.
func GroupsMembers(w http.ResponseWriter, r *http.Request) {
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
		SELECT u.id::text, u.friend_code, COALESCE(u.display_name, ''), (g.owner_id = u.id)
		FROM group_members gm
		JOIN users u ON u.id = gm.user_id
		JOIN groups g ON g.id = gm.group_id
		WHERE gm.group_id = $1
		ORDER BY (g.owner_id = u.id) DESC, gm.created_at
	`, groupID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to list members"})
		return
	}
	defer rows.Close()

	type memberEntry struct {
		UserID     string `json:"user_id"`
		FriendCode string `json:"friend_code"`
		DisplayName string `json:"display_name"`
		IsOwner    bool   `json:"is_owner"`
	}

	var members []memberEntry
	for rows.Next() {
		var e memberEntry
		if err := rows.Scan(&e.UserID, &e.FriendCode, &e.DisplayName, &e.IsOwner); err != nil {
			continue
		}
		members = append(members, e)
	}
	if members == nil {
		members = []memberEntry{}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"members": members})
}

// GroupsInvite invites a friend to a group. Owner only. Invitee must be a friend.
func GroupsInvite(w http.ResponseWriter, r *http.Request) {
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
		GroupID    string `json:"group_id"`
		FriendCode string `json:"friend_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.GroupID == "" || body.FriendCode == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "group_id and friend_code required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Must be owner
	var ownerID string
	err := db.Pool.QueryRow(ctx, `SELECT owner_id::text FROM groups WHERE id::text = $1`, body.GroupID).Scan(&ownerID)
	if err != nil || ownerID != userID {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not group owner or group not found"})
		return
	}

	var toID string
	err = db.Pool.QueryRow(ctx, `SELECT id::text FROM users WHERE friend_code = $1`, body.FriendCode).Scan(&toID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "friend code not found"})
		return
	}

	// Must be friends
	var friendExists bool
	_ = db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM friendships WHERE user_id = $1 AND friend_id = $2)`, userID, toID).Scan(&friendExists)
	if !friendExists {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "can only invite friends"})
		return
	}

	// Already member?
	var memberExists bool
	_ = db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_members WHERE group_id = $1 AND user_id = $2)`, body.GroupID, toID).Scan(&memberExists)
	if memberExists {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "already in group"})
		return
	}

	// Already invited?
	var existingStatus string
	err = db.Pool.QueryRow(ctx, `SELECT status FROM group_invite_requests WHERE group_id = $1 AND to_user_id = $2`, body.GroupID, toID).Scan(&existingStatus)
	if err == nil {
		if existingStatus == "pending" {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "already_sent", "error": "invite already sent"})
			return
		}
		// refused/accepted - can delete and re-insert or just allow new invite
		_, _ = db.Pool.Exec(ctx, `DELETE FROM group_invite_requests WHERE group_id = $1 AND to_user_id = $2`, body.GroupID, toID)
	}

	var requestID string
	err = db.Pool.QueryRow(ctx, `
		INSERT INTO group_invite_requests (group_id, from_user_id, to_user_id, status)
		VALUES ($1, $2, $3, 'pending')
		RETURNING id::text
	`, body.GroupID, userID, toID).Scan(&requestID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to create invite"})
		return
	}

	var groupName, fromCode, fromName string
	_ = db.Pool.QueryRow(ctx, `SELECT name FROM groups WHERE id::text = $1`, body.GroupID).Scan(&groupName)
	_ = db.Pool.QueryRow(ctx, `SELECT friend_code, COALESCE(display_name, '') FROM users WHERE id = $1`, userID).Scan(&fromCode, &fromName)

	ws.Hub.Push(toID, map[string]interface{}{
		"type": "group_invite",
		"payload": map[string]interface{}{
			"request_id":       requestID,
			"group_id":         body.GroupID,
			"group_name":       groupName,
			"from_user_id":     userID,
			"from_friend_code": fromCode,
			"from_display_name": fromName,
		},
	})

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"request_id": requestID,
		"group_id":   body.GroupID,
		"group_name": groupName,
	})
}

// GroupsInviteAccept accepts a group invite.
func GroupsInviteAccept(w http.ResponseWriter, r *http.Request) {
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

	var groupID, toID string
	err := db.Pool.QueryRow(ctx, `SELECT group_id::text, to_user_id::text FROM group_invite_requests WHERE id::text = $1 AND status = 'pending'`, body.RequestID).Scan(&groupID, &toID)
	if err != nil || toID != userID {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request not found or already handled"})
		return
	}

	_, _ = db.Pool.Exec(ctx, `UPDATE group_invite_requests SET status = 'accepted' WHERE id::text = $1`, body.RequestID)
	_, _ = db.Pool.Exec(ctx, `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, groupID, userID)

	var groupName, friendCode, displayName string
	_ = db.Pool.QueryRow(ctx, `SELECT name FROM groups WHERE id::text = $1`, groupID).Scan(&groupName)
	_ = db.Pool.QueryRow(ctx, `SELECT friend_code, COALESCE(display_name, '') FROM users WHERE id = $1`, userID).Scan(&friendCode, &displayName)

	// Push to all other members so they update without manual fetch
	memberRows, _ := db.Pool.Query(ctx, `SELECT user_id::text FROM group_members WHERE group_id = $1 AND user_id != $2`, groupID, userID)
	if memberRows != nil {
		payload := map[string]interface{}{
			"type": "group_member_joined",
			"payload": map[string]interface{}{
				"group_id":     groupID,
				"user_id":      userID,
				"friend_code":  friendCode,
				"display_name": displayName,
				"is_owner":     false,
			},
		}
		for memberRows.Next() {
			var otherID string
			if err := memberRows.Scan(&otherID); err == nil {
				ws.Hub.Push(otherID, payload)
			}
		}
		memberRows.Close()
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "ok",
		"group_id":     groupID,
		"group_name":   groupName,
		"friend_id":    userID,
		"friend_code":  friendCode,
		"display_name": displayName,
	})
}

// GroupsInviteRefuse refuses a group invite.
func GroupsInviteRefuse(w http.ResponseWriter, r *http.Request) {
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

	var toID string
	err := db.Pool.QueryRow(ctx, `SELECT to_user_id::text FROM group_invite_requests WHERE id::text = $1 AND status = 'pending'`, body.RequestID).Scan(&toID)
	if err != nil || toID != userID {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request not found or already handled"})
		return
	}

	_, _ = db.Pool.Exec(ctx, `UPDATE group_invite_requests SET status = 'refused' WHERE id::text = $1`, body.RequestID)

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// GroupsInviteList returns pending group invites (incoming).
func GroupsInviteList(w http.ResponseWriter, r *http.Request) {
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
		SELECT gir.id::text, gir.group_id::text, g.name, u.id::text, u.friend_code, COALESCE(u.display_name, ''), gir.created_at::text
		FROM group_invite_requests gir
		JOIN groups g ON g.id = gir.group_id
		JOIN users u ON u.id = gir.from_user_id
		WHERE gir.to_user_id = $1 AND gir.status = 'pending'
		ORDER BY gir.created_at DESC
	`, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to list invites"})
		return
	}
	defer rows.Close()

	type inviteItem struct {
		RequestID       string `json:"request_id"`
		GroupID         string `json:"group_id"`
		GroupName       string `json:"group_name"`
		FromUserID      string `json:"from_user_id"`
		FromFriendCode  string `json:"from_friend_code"`
		FromDisplayName string `json:"from_display_name"`
		CreatedAt       string `json:"created_at"`
	}

	var list []inviteItem
	for rows.Next() {
		var e inviteItem
		if err := rows.Scan(&e.RequestID, &e.GroupID, &e.GroupName, &e.FromUserID, &e.FromFriendCode, &e.FromDisplayName, &e.CreatedAt); err != nil {
			continue
		}
		list = append(list, e)
	}
	if list == nil {
		list = []inviteItem{}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"invites": list})
}

// GroupsInviteListSent returns group invites the user (owner) has sent, pending.
func GroupsInviteListSent(w http.ResponseWriter, r *http.Request) {
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
		SELECT gir.id::text, gir.group_id::text, g.name, u.id::text, u.friend_code, COALESCE(u.display_name, '')
		FROM group_invite_requests gir
		JOIN groups g ON g.id = gir.group_id
		JOIN users u ON u.id = gir.to_user_id
		WHERE gir.from_user_id = $1 AND gir.status = 'pending'
		ORDER BY gir.created_at DESC
	`, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to list sent invites"})
		return
	}
	defer rows.Close()

	type sentItem struct {
		RequestID  string `json:"request_id"`
		GroupID    string `json:"group_id"`
		GroupName  string `json:"group_name"`
		UserID     string `json:"user_id"`
		FriendCode string `json:"friend_code"`
		DisplayName string `json:"display_name"`
	}

	var list []sentItem
	for rows.Next() {
		var e sentItem
		if err := rows.Scan(&e.RequestID, &e.GroupID, &e.GroupName, &e.UserID, &e.FriendCode, &e.DisplayName); err != nil {
			continue
		}
		list = append(list, e)
	}
	if list == nil {
		list = []sentItem{}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"invites": list})
}

// GroupsKick removes a member from the group. Owner only. Cannot kick self.
func GroupsKick(w http.ResponseWriter, r *http.Request) {
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
		GroupID    string `json:"group_id"`
		UserID     string `json:"user_id"`
		FriendCode string `json:"friend_code"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if body.GroupID == "" || (body.UserID == "" && body.FriendCode == "") {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "group_id and user_id or friend_code required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var ownerID string
	err := db.Pool.QueryRow(ctx, `SELECT owner_id::text FROM groups WHERE id::text = $1`, body.GroupID).Scan(&ownerID)
	if err != nil || ownerID != userID {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not group owner or group not found"})
		return
	}

	var targetID string
	if body.UserID != "" {
		targetID = body.UserID
	} else {
		err = db.Pool.QueryRow(ctx, `SELECT id::text FROM users WHERE friend_code = $1`, body.FriendCode).Scan(&targetID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "user not found"})
			return
		}
	}

	if targetID == userID {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "cannot kick yourself"})
		return
	}

	_, err = db.Pool.Exec(ctx, `DELETE FROM group_members WHERE group_id = $1 AND user_id = $2`, body.GroupID, targetID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to kick"})
		return
	}

	// Push to remaining members so they update without manual fetch
	memberRows, _ := db.Pool.Query(ctx, `SELECT user_id::text FROM group_members WHERE group_id = $1`, body.GroupID)
	if memberRows != nil {
		payload := map[string]interface{}{
			"type": "group_member_left",
			"payload": map[string]interface{}{
				"group_id": body.GroupID,
				"user_id":  targetID,
			},
		}
		for memberRows.Next() {
			var otherID string
			if err := memberRows.Scan(&otherID); err == nil {
				ws.Hub.Push(otherID, payload)
			}
		}
		memberRows.Close()
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
