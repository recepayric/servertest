package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"servertest/db"
)

// CustomZikirCreate creates a user zikir.
// POST /api/zikirs/custom
func CustomZikirCreate(w http.ResponseWriter, r *http.Request) {
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
		NameTr       string   `json:"name_tr"`
		NameEn       string   `json:"name_en"`
		ReadTr       string   `json:"read_tr"`
		Arabic       string   `json:"arabic"`
		TranslationTr string  `json:"translation_tr"`
		TranslationEn string  `json:"translation_en"`
		DescriptionTr string  `json:"description_tr"`
		DescriptionEn string  `json:"description_en"`
		TargetCount  int      `json:"target_count"`
		Category     string   `json:"category"`
		Tags         []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Arabic == "" || body.NameTr == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "arabic and name_tr required"})
		return
	}
	if body.TargetCount <= 0 {
		body.TargetCount = 33
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	tagsJSON, _ := json.Marshal(body.Tags)
	if tagsJSON == nil {
		tagsJSON = []byte("[]")
	}

	var zikirID string
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO custom_zikirs (user_id, name_tr, name_en, read_tr, arabic, translation_tr, translation_en, description_tr, description_en, target_count, category, tags)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id::text
	`, userID, body.NameTr, body.NameEn, body.ReadTr, body.Arabic, body.TranslationTr, body.TranslationEn, body.DescriptionTr, body.DescriptionEn, body.TargetCount, body.Category, tagsJSON).Scan(&zikirID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to create zikir"})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"zikir_id": zikirID,
	})
}

// CustomZikirList returns the user's custom zikirs. If ?id=xxx provided, returns single zikir (if accessible).
// GET /api/zikirs/custom
// GET /api/zikirs/custom?id=xxx
func CustomZikirList(w http.ResponseWriter, r *http.Request) {
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

	zikirID := r.URL.Query().Get("id")
	if zikirID != "" {
		var e struct {
			ID            string   `json:"id"`
			NameTr        string   `json:"name_tr"`
			NameEn        string   `json:"name_en"`
			ReadTr        string   `json:"read_tr"`
			Arabic        string   `json:"arabic"`
			TranslationTr  string `json:"translation_tr"`
			TranslationEn  string `json:"translation_en"`
			DescriptionTr string `json:"description_tr"`
			DescriptionEn string `json:"description_en"`
			TargetCount   int     `json:"target_count"`
			Category      string  `json:"category"`
			Tags          []string `json:"tags"`
			CreatedAt     string  `json:"created_at"`
		}
		var tagsJSON []byte
		err := db.Pool.QueryRow(ctx, `
			SELECT id::text, name_tr, name_en, read_tr, arabic, translation_tr, translation_en, description_tr, description_en, target_count, category, tags, created_at::text
			FROM custom_zikirs
			WHERE id::text = $1 AND user_id = $2
		`, zikirID, userID).Scan(&e.ID, &e.NameTr, &e.NameEn, &e.ReadTr, &e.Arabic, &e.TranslationTr, &e.TranslationEn, &e.DescriptionTr, &e.DescriptionEn, &e.TargetCount, &e.Category, &tagsJSON, &e.CreatedAt)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "zikir not found"})
			return
		}
		_ = json.Unmarshal(tagsJSON, &e.Tags)
		_ = json.NewEncoder(w).Encode(e)
		return
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT id::text, name_tr, name_en, read_tr, arabic, translation_tr, translation_en, description_tr, description_en, target_count, category, tags, created_at::text
		FROM custom_zikirs
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to list zikirs"})
		return
	}
	defer rows.Close()

	type zikirEntry struct {
		ID           string   `json:"id"`
		NameTr       string   `json:"name_tr"`
		NameEn       string   `json:"name_en"`
		ReadTr       string   `json:"read_tr"`
		Arabic       string   `json:"arabic"`
		TranslationTr  string `json:"translation_tr"`
		TranslationEn  string `json:"translation_en"`
		DescriptionTr string `json:"description_tr"`
		DescriptionEn string `json:"description_en"`
		TargetCount  int     `json:"target_count"`
		Category     string   `json:"category"`
		Tags         []string `json:"tags"`
		CreatedAt    string   `json:"created_at"`
	}

	var list []zikirEntry
	for rows.Next() {
		var e zikirEntry
		var tagsJSON []byte
		if err := rows.Scan(&e.ID, &e.NameTr, &e.NameEn, &e.ReadTr, &e.Arabic, &e.TranslationTr, &e.TranslationEn, &e.DescriptionTr, &e.DescriptionEn, &e.TargetCount, &e.Category, &tagsJSON, &e.CreatedAt); err != nil {
			continue
		}
		_ = json.Unmarshal(tagsJSON, &e.Tags)
		list = append(list, e)
	}
	if list == nil {
		list = []zikirEntry{}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"zikirs": list})
}

// CustomZikirGet returns a custom zikir by id. Access if: owner, or in a group that has this zikir.
// GET /api/zikirs/custom/get?ref=xxx
func CustomZikirGet(w http.ResponseWriter, r *http.Request) {
	log.Printf("[CustomZikirGet] request path=%s query=%s", r.URL.Path, r.URL.RawQuery)
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	userID, ok := getUserIDFromRequest(r)
	if !ok {
		log.Printf("[CustomZikirGet] 401: missing or invalid X-Guest-Token")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing or invalid X-Guest-Token"})
		return
	}

	ref := r.URL.Query().Get("ref")
	if ref == "" {
		log.Printf("[CustomZikirGet] 400: ref required")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "ref required"})
		return
	}

	log.Printf("[CustomZikirGet] ref=%s userID=%s", ref, userID)
	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var e struct {
		ID            string   `json:"id"`
		NameTr        string   `json:"name_tr"`
		NameEn        string   `json:"name_en"`
		ReadTr        string   `json:"read_tr"`
		Arabic        string   `json:"arabic"`
		TranslationTr  string `json:"translation_tr"`
		TranslationEn  string `json:"translation_en"`
		DescriptionTr string `json:"description_tr"`
		DescriptionEn string `json:"description_en"`
		TargetCount   int     `json:"target_count"`
		Category      string  `json:"category"`
		Tags          []string `json:"tags"`
		CreatedAt     string  `json:"created_at"`
	}
	var tagsJSON []byte
	err := db.Pool.QueryRow(ctx, `
		SELECT cz.id::text, cz.name_tr, cz.name_en, cz.read_tr, cz.arabic, cz.translation_tr, cz.translation_en, cz.description_tr, cz.description_en, cz.target_count, cz.category, cz.tags, cz.created_at::text
		FROM custom_zikirs cz
		WHERE cz.id::text = $1
		AND (
			cz.user_id::text = $2
			OR EXISTS (
				SELECT 1 FROM group_zikirs gz
				JOIN group_members gm ON gm.group_id = gz.group_id
				WHERE gz.zikir_type = 'custom' AND gz.zikir_ref::text = cz.id::text AND gm.user_id::text = $2
			)
			OR EXISTS (
				SELECT 1 FROM group_zikir_requests gzr
				JOIN group_members gm ON gm.group_id = gzr.group_id
				WHERE gzr.zikir_type = 'custom' AND gzr.zikir_ref::text = cz.id::text AND gm.user_id::text = $2
			)
			OR EXISTS (
				SELECT 1 FROM friend_zikirs fz
				WHERE fz.zikir_type = 'custom' AND fz.zikir_ref::text = cz.id::text AND fz.to_user_id::text = $2
			)
		)
	`, ref, userID).Scan(&e.ID, &e.NameTr, &e.NameEn, &e.ReadTr, &e.Arabic, &e.TranslationTr, &e.TranslationEn, &e.DescriptionTr, &e.DescriptionEn, &e.TargetCount, &e.Category, &tagsJSON, &e.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("[CustomZikirGet] 404: zikir not found ref=%s userID=%s (no rows - not owner and no group/friend access)", ref, userID)
		} else {
			log.Printf("[CustomZikirGet] 500: db error ref=%s err=%v", ref, err)
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "database error"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "zikir not found"})
		return
	}
	log.Printf("[CustomZikirGet] 200: found zikir id=%s name=%s", e.ID, e.NameTr)
	_ = json.Unmarshal(tagsJSON, &e.Tags)
	_ = json.NewEncoder(w).Encode(e)
}

// CustomZikirDelete deletes the user's custom zikir.
// DELETE /api/zikirs/custom?id=xxx
func CustomZikirDelete(w http.ResponseWriter, r *http.Request) {
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

	zikirID := r.URL.Query().Get("id")
	if zikirID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "id required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	res, err := db.Pool.Exec(ctx, `DELETE FROM custom_zikirs WHERE id::text = $1 AND user_id = $2`, zikirID, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to delete"})
		return
	}
	if res.RowsAffected() == 0 {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "zikir not found or not yours"})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
