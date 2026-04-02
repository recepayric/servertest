package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"servertest/db"
)

const (
	friendCodeChars = "23456789abcdefghjkmnpqrstuvwxyz"
	friendCodeLen   = 6
	maxRetries      = 5
)

type guestRegisterResponse struct {
	GuestToken string `json:"guest_token"`
	FriendCode string `json:"friend_code"`
	UserID     string `json:"user_id"`
}

type accountInfo struct {
	UserID      string `json:"user_id"`
	GuestToken  string `json:"guest_token,omitempty"`
	FriendCode  string `json:"friend_code"`
	DisplayName string `json:"display_name"`
}

type linkIdentityRequest struct {
	AuthProvider     string `json:"auth_provider"`
	UnityPlayerID    string `json:"unity_player_id"`
	UnityAccessToken string `json:"unity_access_token"` // accepted for forward compatibility
	ConflictPolicy   string `json:"conflict_policy"`    // "", "switch", "keep"
}

// GuestRegister creates a new guest user and returns guest_token + friend_code.
func GuestRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	handleIdentityAuth(w, r)
}

// LinkIdentity is a dedicated endpoint for linking provider identities.
// It shares behavior with GuestRegister, including conflict handling:
// - conflict_policy = "switch" => switch device account to already linked account
// - conflict_policy = "keep"   => move identity mapping to current account
func LinkIdentity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	handleIdentityAuth(w, r)
}

func handleIdentityAuth(w http.ResponseWriter, r *http.Request) {
	body := parseLinkIdentityRequest(r)
	provider := normalizeProvider(body.AuthProvider)
	externalID := strings.TrimSpace(body.UnityPlayerID)
	conflictPolicy := strings.ToLower(strings.TrimSpace(body.ConflictPolicy))
	currentGuestToken := strings.TrimSpace(r.Header.Get(guestTokenHeader))

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	// Optional current account (from existing X-Guest-Token)
	var current *accountInfo
	if currentGuestToken != "" {
		acc, err := getAccountByGuestToken(ctx, currentGuestToken)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid X-Guest-Token"})
			return
		}
		current = &acc
	}

	// Identity-based logic (link/sign-in with provider identity)
	if externalID != "" {
		linked, linkedFound := getAccountByIdentity(ctx, provider, externalID)

		if current != nil {
			// We have both a currently signed-in account and an identity we want to link/sign-in with.
			if linkedFound && linked.UserID != current.UserID {
				switch conflictPolicy {
				case "switch":
					// Keep identity on linked account and switch this device/account to it.
					_ = json.NewEncoder(w).Encode(guestRegisterResponse{
						GuestToken: linked.GuestToken,
						FriendCode: linked.FriendCode,
						UserID:     linked.UserID,
					})
					return
				case "keep":
					// Move this identity to current account.
					if err := upsertIdentity(ctx, provider, externalID, current.UserID); err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to link identity"})
						return
					}
					_ = json.NewEncoder(w).Encode(guestRegisterResponse{
						GuestToken: current.GuestToken,
						FriendCode: current.FriendCode,
						UserID:     current.UserID,
					})
					return
				default:
					// Ask client to choose.
					w.WriteHeader(http.StatusConflict)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"error":   "identity_conflict",
						"code":    "identity_conflict",
						"message": "This identity is already linked to another account. Choose conflict_policy=keep or switch.",
						"current_account": map[string]string{
							"user_id":      current.UserID,
							"friend_code":  current.FriendCode,
							"display_name": current.DisplayName,
						},
						"linked_account": map[string]string{
							"user_id":      linked.UserID,
							"friend_code":  linked.FriendCode,
							"display_name": linked.DisplayName,
						},
					})
					return
				}
			}

			// No conflict (either identity missing, or already linked to current account): ensure mapping to current.
			if err := upsertIdentity(ctx, provider, externalID, current.UserID); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to link identity"})
				return
			}
			_ = json.NewEncoder(w).Encode(guestRegisterResponse{
				GuestToken: current.GuestToken,
				FriendCode: current.FriendCode,
				UserID:     current.UserID,
			})
			return
		}

		// No current guest token: identity sign-in path.
		if linkedFound {
			_ = json.NewEncoder(w).Encode(guestRegisterResponse{
				GuestToken: linked.GuestToken,
				FriendCode: linked.FriendCode,
				UserID:     linked.UserID,
			})
			return
		}
	}

	// If current guest account exists and no identity external id provided, just return current.
	if current != nil {
		_ = json.NewEncoder(w).Encode(guestRegisterResponse{
			GuestToken: current.GuestToken,
			FriendCode: current.FriendCode,
			UserID:     current.UserID,
		})
		return
	}

	// No current account and identity lookup missed -> create a fresh guest.
	created, err := createGuestUser(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// If identity provided, map it to the newly created user.
	if externalID != "" {
		if err := upsertIdentity(ctx, provider, externalID, created.UserID); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to link identity"})
			return
		}
	}

	_ = json.NewEncoder(w).Encode(guestRegisterResponse{
		GuestToken: created.GuestToken,
		FriendCode: created.FriendCode,
		UserID:     created.UserID,
	})
}

func parseLinkIdentityRequest(r *http.Request) linkIdentityRequest {
	var body linkIdentityRequest
	if r.Body == nil {
		return body
	}
	b, _ := io.ReadAll(r.Body)
	if len(b) == 0 {
		return body
	}
	_ = json.Unmarshal(b, &body)
	return body
}

func normalizeProvider(provider string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	if p == "" {
		return "guest"
	}
	return p
}

func getAccountByGuestToken(ctx context.Context, guestToken string) (accountInfo, error) {
	var acc accountInfo
	err := db.Pool.QueryRow(ctx, `
		SELECT id::text, guest_token, friend_code, COALESCE(display_name, '')
		FROM users WHERE guest_token = $1
	`, guestToken).Scan(&acc.UserID, &acc.GuestToken, &acc.FriendCode, &acc.DisplayName)
	return acc, err
}

func getAccountByIdentity(ctx context.Context, provider, externalID string) (accountInfo, bool) {
	var acc accountInfo
	err := db.Pool.QueryRow(ctx, `
		SELECT u.id::text, u.guest_token, u.friend_code, COALESCE(u.display_name, '')
		FROM user_identities ui
		JOIN users u ON u.id = ui.user_id
		WHERE ui.provider = $1 AND ui.external_id = $2
	`, provider, externalID).Scan(&acc.UserID, &acc.GuestToken, &acc.FriendCode, &acc.DisplayName)
	if err != nil {
		return accountInfo{}, false
	}
	return acc, true
}

func upsertIdentity(ctx context.Context, provider, externalID, userID string) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO user_identities (provider, external_id, user_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (provider, external_id)
		DO UPDATE SET user_id = EXCLUDED.user_id
	`, provider, externalID, userID)
	return err
}

func createGuestUser(ctx context.Context) (accountInfo, error) {
	guestToken, err := generateGuestToken()
	if err != nil {
		return accountInfo{}, fmt.Errorf("failed to generate token")
	}
	friendCode, err := generateUniqueFriendCode(ctx)
	if err != nil {
		return accountInfo{}, fmt.Errorf("failed to generate friend code")
	}

	displayName := "User#" + friendCode
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO users (id, guest_token, friend_code, display_name, created_at)
		VALUES (gen_random_uuid(), $1, $2, $3, now())
	`, guestToken, friendCode, displayName)
	if err != nil {
		return accountInfo{}, fmt.Errorf("failed to create user")
	}

	var userID string
	if err := db.Pool.QueryRow(ctx, `SELECT id::text FROM users WHERE guest_token = $1`, guestToken).Scan(&userID); err != nil {
		return accountInfo{}, fmt.Errorf("failed to load created user")
	}
	return accountInfo{
		UserID:      userID,
		GuestToken:  guestToken,
		FriendCode:  friendCode,
		DisplayName: displayName,
	}, nil
}

func generateUniqueFriendCode(ctx context.Context) (string, error) {
	for i := 0; i < maxRetries; i++ {
		code, err := generateFriendCode()
		if err != nil {
			return "", err
		}
		var exists bool
		err = db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE friend_code = $1)`, code).Scan(&exists)
		if err != nil {
			return "", err
		}
		if !exists {
			return code, nil
		}
	}
	return "", fmt.Errorf("could not generate unique friend code after %d retries", maxRetries)
}

func generateGuestToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func generateFriendCode() (string, error) {
	b := make([]byte, friendCodeLen)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(friendCodeChars))))
		if err != nil {
			return "", err
		}
		b[i] = friendCodeChars[n.Int64()]
	}
	return string(b), nil
}
