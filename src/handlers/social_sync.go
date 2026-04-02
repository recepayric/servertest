package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"servertest/db"
)

type socialSyncRequest struct {
	FriendsRev       int64 `json:"friends_rev"`
	FriendPendingRev int64 `json:"friend_pending_rev"`
	FriendSentRev    int64 `json:"friend_sent_rev"`
	GroupsRev        int64 `json:"groups_rev"`
	GroupPendingRev  int64 `json:"group_pending_rev"`
	GroupSentRev     int64 `json:"group_sent_rev"`
}

type socialSyncResponse struct {
	FriendsRev       int64 `json:"friends_rev"`
	FriendPendingRev int64 `json:"friend_pending_rev"`
	FriendSentRev    int64 `json:"friend_sent_rev"`
	GroupsRev        int64 `json:"groups_rev"`
	GroupPendingRev  int64 `json:"group_pending_rev"`
	GroupSentRev     int64 `json:"group_sent_rev"`

	FriendsChanged       bool `json:"friends_changed"`
	FriendPendingChanged bool `json:"friend_pending_changed"`
	FriendSentChanged    bool `json:"friend_sent_changed"`
	GroupsChanged        bool `json:"groups_changed"`
	GroupPendingChanged  bool `json:"group_pending_changed"`
	GroupSentChanged     bool `json:"group_sent_changed"`

	Friends       []friendEntry `json:"friends,omitempty"`
	FriendPending []struct {
		RequestID       string `json:"request_id"`
		FromUserID      string `json:"from_user_id"`
		FromFriendCode  string `json:"from_friend_code"`
		FromDisplayName string `json:"from_display_name"`
		CreatedAt       string `json:"created_at"`
	} `json:"friend_pending,omitempty"`
	FriendSent []struct {
		RequestID   string `json:"request_id"`
		FriendID    string `json:"friend_id"`
		FriendCode  string `json:"friend_code"`
		DisplayName string `json:"display_name"`
	} `json:"friend_sent,omitempty"`
	Groups       []groupEntry `json:"groups,omitempty"`
	GroupPending []struct {
		RequestID       string `json:"request_id"`
		GroupID         string `json:"group_id"`
		GroupName       string `json:"group_name"`
		GroupIconIndex  int    `json:"group_icon_index"`
		GroupIconKey    string `json:"group_icon_key"`
		FromUserID      string `json:"from_user_id"`
		FromFriendCode  string `json:"from_friend_code"`
		FromDisplayName string `json:"from_display_name"`
		CreatedAt       string `json:"created_at"`
	} `json:"group_pending,omitempty"`
	GroupSent []struct {
		RequestID      string `json:"request_id"`
		GroupID        string `json:"group_id"`
		GroupName      string `json:"group_name"`
		GroupIconIndex int    `json:"group_icon_index"`
		GroupIconKey   string `json:"group_icon_key"`
		UserID         string `json:"user_id"`
		FriendCode     string `json:"friend_code"`
		DisplayName    string `json:"display_name"`
	} `json:"group_sent,omitempty"`
}

func SocialSync(w http.ResponseWriter, r *http.Request) {
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
	var req socialSyncRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx, cancel := context.WithTimeout(r.Context(), 7*time.Second)
	defer cancel()
	if err := ensureSocialMetaRow(ctx, userID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to init social meta"})
		return
	}

	var resp socialSyncResponse
	if err := db.Pool.QueryRow(ctx, `
		SELECT friends_rev, friend_pending_rev, friend_sent_rev, groups_rev, group_pending_rev, group_sent_rev
		FROM user_social_meta
		WHERE user_id = $1::uuid
	`, userID).Scan(&resp.FriendsRev, &resp.FriendPendingRev, &resp.FriendSentRev, &resp.GroupsRev, &resp.GroupPendingRev, &resp.GroupSentRev); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to read social revisions"})
		return
	}

	resp.FriendsChanged = req.FriendsRev != resp.FriendsRev
	resp.FriendPendingChanged = req.FriendPendingRev != resp.FriendPendingRev
	resp.FriendSentChanged = req.FriendSentRev != resp.FriendSentRev
	resp.GroupsChanged = req.GroupsRev != resp.GroupsRev
	resp.GroupPendingChanged = req.GroupPendingRev != resp.GroupPendingRev
	resp.GroupSentChanged = req.GroupSentRev != resp.GroupSentRev

	if resp.FriendsChanged {
		rows, err := db.Pool.Query(ctx, `
			SELECT u.id::text, u.friend_code, COALESCE(u.display_name, '')
			FROM friendships f
			JOIN users u ON u.id = f.friend_id
			WHERE f.user_id = $1::uuid
			ORDER BY f.created_at
		`, userID)
		if err == nil {
			for rows.Next() {
				var e friendEntry
				if rows.Scan(&e.UserID, &e.FriendCode, &e.DisplayName) == nil {
					resp.Friends = append(resp.Friends, e)
				}
			}
			rows.Close()
		}
		if resp.Friends == nil {
			resp.Friends = []friendEntry{}
		}
	}

	if resp.FriendPendingChanged {
		rows, err := db.Pool.Query(ctx, `
			SELECT fr.id::text, u.id::text, u.friend_code, COALESCE(u.display_name, ''), fr.created_at::text
			FROM friendship_requests fr
			JOIN users u ON u.id = fr.from_user_id
			WHERE fr.to_user_id = $1::uuid AND fr.status = 'pending'
			ORDER BY fr.created_at DESC
		`, userID)
		if err == nil {
			for rows.Next() {
				var e struct {
					RequestID       string `json:"request_id"`
					FromUserID      string `json:"from_user_id"`
					FromFriendCode  string `json:"from_friend_code"`
					FromDisplayName string `json:"from_display_name"`
					CreatedAt       string `json:"created_at"`
				}
				if rows.Scan(&e.RequestID, &e.FromUserID, &e.FromFriendCode, &e.FromDisplayName, &e.CreatedAt) == nil {
					resp.FriendPending = append(resp.FriendPending, e)
				}
			}
			rows.Close()
		}
		if resp.FriendPending == nil {
			resp.FriendPending = []struct {
				RequestID       string `json:"request_id"`
				FromUserID      string `json:"from_user_id"`
				FromFriendCode  string `json:"from_friend_code"`
				FromDisplayName string `json:"from_display_name"`
				CreatedAt       string `json:"created_at"`
			}{}
		}
	}

	if resp.FriendSentChanged {
		rows, err := db.Pool.Query(ctx, `
			SELECT fr.id::text, u.id::text, u.friend_code, COALESCE(u.display_name, '')
			FROM friendship_requests fr
			JOIN users u ON u.id = fr.to_user_id
			WHERE fr.from_user_id = $1::uuid AND fr.status = 'pending'
			ORDER BY fr.created_at DESC
		`, userID)
		if err == nil {
			for rows.Next() {
				var e struct {
					RequestID   string `json:"request_id"`
					FriendID    string `json:"friend_id"`
					FriendCode  string `json:"friend_code"`
					DisplayName string `json:"display_name"`
				}
				if rows.Scan(&e.RequestID, &e.FriendID, &e.FriendCode, &e.DisplayName) == nil {
					resp.FriendSent = append(resp.FriendSent, e)
				}
			}
			rows.Close()
		}
		if resp.FriendSent == nil {
			resp.FriendSent = []struct {
				RequestID   string `json:"request_id"`
				FriendID    string `json:"friend_id"`
				FriendCode  string `json:"friend_code"`
				DisplayName string `json:"display_name"`
			}{}
		}
	}

	if resp.GroupsChanged {
		rows, err := db.Pool.Query(ctx, `
			SELECT g.id::text, g.name, g.owner_id::text, g.icon_index, g.icon_key
			FROM groups g
			JOIN group_members gm ON gm.group_id = g.id
			WHERE gm.user_id = $1::uuid
			ORDER BY g.created_at
		`, userID)
		if err == nil {
			for rows.Next() {
				var e groupEntry
				if rows.Scan(&e.GroupID, &e.Name, &e.OwnerID, &e.GroupIconIndex, &e.GroupIconKey) == nil {
					resp.Groups = append(resp.Groups, e)
				}
			}
			rows.Close()
		}
		if resp.Groups == nil {
			resp.Groups = []groupEntry{}
		}
	}

	if resp.GroupPendingChanged {
		rows, err := db.Pool.Query(ctx, `
			SELECT gir.id::text, gir.group_id::text, g.name, g.icon_index, g.icon_key,
			       u.id::text, u.friend_code, COALESCE(u.display_name, ''), gir.created_at::text
			FROM group_invite_requests gir
			JOIN groups g ON g.id = gir.group_id
			JOIN users u ON u.id = gir.from_user_id
			WHERE gir.to_user_id = $1::uuid AND gir.status = 'pending'
			ORDER BY gir.created_at DESC
		`, userID)
		if err == nil {
			for rows.Next() {
				var e struct {
					RequestID       string `json:"request_id"`
					GroupID         string `json:"group_id"`
					GroupName       string `json:"group_name"`
					GroupIconIndex  int    `json:"group_icon_index"`
					GroupIconKey    string `json:"group_icon_key"`
					FromUserID      string `json:"from_user_id"`
					FromFriendCode  string `json:"from_friend_code"`
					FromDisplayName string `json:"from_display_name"`
					CreatedAt       string `json:"created_at"`
				}
				if rows.Scan(&e.RequestID, &e.GroupID, &e.GroupName, &e.GroupIconIndex, &e.GroupIconKey, &e.FromUserID, &e.FromFriendCode, &e.FromDisplayName, &e.CreatedAt) == nil {
					resp.GroupPending = append(resp.GroupPending, e)
				}
			}
			rows.Close()
		}
		if resp.GroupPending == nil {
			resp.GroupPending = []struct {
				RequestID       string `json:"request_id"`
				GroupID         string `json:"group_id"`
				GroupName       string `json:"group_name"`
				GroupIconIndex  int    `json:"group_icon_index"`
				GroupIconKey    string `json:"group_icon_key"`
				FromUserID      string `json:"from_user_id"`
				FromFriendCode  string `json:"from_friend_code"`
				FromDisplayName string `json:"from_display_name"`
				CreatedAt       string `json:"created_at"`
			}{}
		}
	}

	if resp.GroupSentChanged {
		rows, err := db.Pool.Query(ctx, `
			SELECT gir.id::text, gir.group_id::text, g.name, g.icon_index, g.icon_key,
			       u.id::text, u.friend_code, COALESCE(u.display_name, '')
			FROM group_invite_requests gir
			JOIN groups g ON g.id = gir.group_id
			JOIN users u ON u.id = gir.to_user_id
			WHERE gir.from_user_id = $1::uuid AND gir.status = 'pending'
			ORDER BY gir.created_at DESC
		`, userID)
		if err == nil {
			for rows.Next() {
				var e struct {
					RequestID      string `json:"request_id"`
					GroupID        string `json:"group_id"`
					GroupName      string `json:"group_name"`
					GroupIconIndex int    `json:"group_icon_index"`
					GroupIconKey   string `json:"group_icon_key"`
					UserID         string `json:"user_id"`
					FriendCode     string `json:"friend_code"`
					DisplayName    string `json:"display_name"`
				}
				if rows.Scan(&e.RequestID, &e.GroupID, &e.GroupName, &e.GroupIconIndex, &e.GroupIconKey, &e.UserID, &e.FriendCode, &e.DisplayName) == nil {
					resp.GroupSent = append(resp.GroupSent, e)
				}
			}
			rows.Close()
		}
		if resp.GroupSent == nil {
			resp.GroupSent = []struct {
				RequestID      string `json:"request_id"`
				GroupID        string `json:"group_id"`
				GroupName      string `json:"group_name"`
				GroupIconIndex int    `json:"group_icon_index"`
				GroupIconKey   string `json:"group_icon_key"`
				UserID         string `json:"user_id"`
				FriendCode     string `json:"friend_code"`
				DisplayName    string `json:"display_name"`
			}{}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

