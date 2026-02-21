package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
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

// GuestRegister creates a new guest user and returns guest_token + friend_code.
func GuestRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	guestToken, err := generateGuestToken()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to generate token"})
		return
	}
	friendCode, err := generateUniqueFriendCode(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to generate friend code"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	displayName := "User#" + friendCode

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO users (id, guest_token, friend_code, display_name, created_at)
		VALUES (gen_random_uuid(), $1, $2, $3, now())
	`, guestToken, friendCode, displayName)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to create user"})
		return
	}

	var userID string
	if err := db.Pool.QueryRow(ctx, `SELECT id::text FROM users WHERE guest_token = $1`, guestToken).Scan(&userID); err != nil {
		userID = ""
	}

	_ = json.NewEncoder(w).Encode(guestRegisterResponse{
		GuestToken: guestToken,
		FriendCode: friendCode,
		UserID:     userID,
	})
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
