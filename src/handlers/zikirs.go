package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"servertest/db"
)

// ZikirDTO matches the JSON your app already uses, plus version fields.
type ZikirDTO struct {
	ID          string   `json:"id"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Arabic      string   `json:"arabic"`
	Translations struct {
		TR string `json:"tr"`
		EN string `json:"en"`
	} `json:"translations"`
	TargetCount    int    `json:"targetCount"`
	Description    string `json:"description"`
	CreatedVersion int    `json:"createdVersion"`
	UpdatedVersion int    `json:"updatedVersion"`
}

type zikirListResponse struct {
	Metadata struct {
		Version int `json:"version"`
	} `json:"metadata"`
	Zikirs []ZikirDTO `json:"zikirs"`
}

// Zikirs returns all zikirs, or only those updated after a given version.
// GET /api/zikirs?sinceVersion=1
func Zikirs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	sinceVersion := 0
	if sv := r.URL.Query().Get("sinceVersion"); sv != "" {
		if v, err := strconv.Atoi(sv); err == nil && v > 0 {
			sinceVersion = v
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Get current content version (max updated_version)
	var currentVersion int
	if err := db.Pool.QueryRow(ctx, `SELECT COALESCE(MAX(updated_version), 1) FROM zikirs`).Scan(&currentVersion); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "failed to read current version",
		})
		return
	}

	// Fetch zikirs, optionally filtered by sinceVersion
	var rows pgxRows
	var err error
	if sinceVersion > 0 {
		rows, err = db.Pool.Query(ctx, `
			SELECT id, category, arabic, tr_text, en_text,
			       target_count, description,
			       created_version, updated_version
			FROM zikirs
			WHERE updated_version > $1
			ORDER BY id
		`, sinceVersion)
	} else {
		rows, err = db.Pool.Query(ctx, `
			SELECT id, category, arabic, tr_text, en_text,
			       target_count, description,
			       created_version, updated_version
			FROM zikirs
			ORDER BY id
		`)
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "failed to query zikirs",
		})
		return
	}
	defer rows.Close()

	var result zikirListResponse
	result.Metadata.Version = currentVersion

	for rows.Next() {
		var z ZikirDTO
		if err := rows.Scan(
			&z.ID,
			&z.Category,
			&z.Arabic,
			&z.Translations.TR,
			&z.Translations.EN,
			&z.TargetCount,
			&z.Description,
			&z.CreatedVersion,
			&z.UpdatedVersion,
		); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "failed to scan zikir row",
			})
			return
		}

		// Load tags for this zikir
		tagRows, err := db.Pool.Query(ctx, `SELECT tag FROM zikir_tags WHERE zikir_id = $1 ORDER BY id`, z.ID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "failed to query zikir tags",
			})
			return
		}
		for tagRows.Next() {
			var t string
			if err := tagRows.Scan(&t); err != nil {
				tagRows.Close()
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": "failed to scan zikir tag",
				})
				return
			}
			z.Tags = append(z.Tags, t)
		}
		tagRows.Close()

		result.Zikirs = append(result.Zikirs, z)
	}

	if err := rows.Err(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "error reading zikirs",
		})
		return
	}

	_ = json.NewEncoder(w).Encode(result)
}

