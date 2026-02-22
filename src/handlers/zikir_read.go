package handlers

import (
	"context"
	"encoding/json"
	"log"
	"regexp"
	"time"

	"servertest/db"
	"servertest/ws"
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Sentinel UUID for pooled group progress (all reads share one row).
var pooledUserID = "00000000-0000-0000-0000-000000000000"

// HandleZikirRead processes a zikir read from WebSocket. Updates DB and broadcasts.
// payload: { "target": "group"|"friend", "group_zikir_id"?: "...", "friend_zikir_id"?: "...", "count": 1 }
func HandleZikirRead(userID string, payload json.RawMessage) {
	var body struct {
		Target        string `json:"target"`
		GroupZikirID  string `json:"group_zikir_id"`
		FriendZikirID string `json:"friend_zikir_id"`
		Count         int    `json:"count"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		log.Printf("zikir_read: invalid payload: %v", err)
		return
	}
	if body.Count <= 0 {
		body.Count = 1
	}
	if body.Count > 100 {
		body.Count = 100
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch body.Target {
	case "group":
		handleGroupZikirRead(ctx, userID, body.GroupZikirID, body.Count)
	case "friend":
		handleFriendZikirRead(ctx, userID, body.FriendZikirID, body.Count)
	default:
		log.Printf("zikir_read: unknown target %q", body.Target)
	}
}

func handleGroupZikirRead(ctx context.Context, userID, groupZikirID string, count int) {
	if groupZikirID == "" {
		log.Printf("zikir_read: group_zikir_id required for target=group")
		return
	}
	if !uuidRe.MatchString(groupZikirID) {
		log.Printf("zikir_read: invalid group_zikir_id")
		return
	}

	var groupID, mode string
	err := db.Pool.QueryRow(ctx, `
		SELECT gz.group_id::text, COALESCE(gz.mode, 'pooled')
		FROM group_zikirs gz
		JOIN group_members gm ON gm.group_id = gz.group_id
		WHERE gz.id::text = $1 AND gm.user_id::text = $2
	`, groupZikirID, userID).Scan(&groupID, &mode)
	if err != nil {
		log.Printf("zikir_read: group zikir not found or not member: %v", err)
		return
	}

	progressUserID := userID
	if mode == "pooled" {
		progressUserID = pooledUserID
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO group_zikir_progress (group_zikir_id, user_id, reads, updated_at)
		VALUES ($1, $2::uuid, $3, now())
		ON CONFLICT (group_zikir_id, user_id) DO UPDATE
		SET reads = group_zikir_progress.reads + $3, updated_at = now()
	`, groupZikirID, progressUserID, count)
	if err != nil {
		log.Printf("zikir_read: update progress: %v", err)
		return
	}

	// Get member user IDs to broadcast
	rows, err := db.Pool.Query(ctx, `SELECT user_id::text FROM group_members WHERE group_id = $1`, groupID)
	if err != nil {
		return
	}
	defer rows.Close()
	var userIDs []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			continue
		}
		userIDs = append(userIDs, u)
	}

	// Get current total for pooled, or per-user for individual
	var progressPayload interface{}
	if mode == "pooled" {
		var total int
		_ = db.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(reads), 0) FROM group_zikir_progress WHERE group_zikir_id = $1`, groupZikirID).Scan(&total)
		progressPayload = map[string]interface{}{
			"group_zikir_id": groupZikirID,
			"mode":           "pooled",
			"total":          total,
		}
	} else {
		rows2, _ := db.Pool.Query(ctx, `SELECT user_id::text, reads FROM group_zikir_progress WHERE group_zikir_id = $1`, groupZikirID)
		perUser := make(map[string]int)
		if rows2 != nil {
			defer rows2.Close()
			for rows2.Next() {
				var uid string
				var r int
				if err := rows2.Scan(&uid, &r); err == nil {
					perUser[uid] = r
				}
			}
		}
		progressPayload = map[string]interface{}{
			"group_zikir_id": groupZikirID,
			"mode":           "individual",
			"per_user":       perUser,
		}
	}

	ws.Hub.PushToMany(userIDs, map[string]interface{}{
		"type": "zikir_read_update",
		"payload": map[string]interface{}{
			"target":   "group",
			"progress": progressPayload,
		},
	})
}

func handleFriendZikirRead(ctx context.Context, userID, friendZikirID string, count int) {
	if friendZikirID == "" {
		log.Printf("zikir_read: friend_zikir_id required for target=friend")
		return
	}
	if !uuidRe.MatchString(friendZikirID) {
		log.Printf("zikir_read: invalid friend_zikir_id")
		return
	}

	var fromUserID string
	err := db.Pool.QueryRow(ctx, `
		SELECT from_user_id::text FROM friend_zikirs
		WHERE id::text = $1 AND to_user_id::text = $2
	`, friendZikirID, userID).Scan(&fromUserID)
	if err != nil {
		log.Printf("zikir_read: friend zikir not found or not receiver: %v", err)
		return
	}

	var reads int
	err = db.Pool.QueryRow(ctx, `
		UPDATE friend_zikirs SET reads = reads + $1 WHERE id::text = $2 RETURNING reads
	`, count, friendZikirID).Scan(&reads)
	if err != nil {
		log.Printf("zikir_read: update friend zikir: %v", err)
		return
	}

	payload := map[string]interface{}{
		"type": "zikir_read_update",
		"payload": map[string]interface{}{
			"target":         "friend",
			"friend_zikir_id": friendZikirID,
			"reads":          reads,
		},
	}

	ws.Hub.Push(userID, payload)
	ws.Hub.Push(fromUserID, payload)
}
