package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"servertest/db"

	"github.com/jackc/pgx/v5"
)

type cloudEntitlement struct {
	IsPremium     bool    `json:"is_premium"`
	Plan          string  `json:"plan"`
	ExpiresAt     *string `json:"expires_at,omitempty"`
	HasNoAds      bool    `json:"has_no_ads"`
	PremiumActive bool    `json:"premium_active"`
}

type cloudProfile struct {
	TotalReads       int64   `json:"total_reads"`
	Level            int     `json:"level"`
	XP               int64   `json:"xp"`
	StreakDays       int     `json:"streak_days"`
	BestStreakDays   int     `json:"best_streak_days"`
	DailyTarget      int     `json:"daily_target"`
	DailyReads       int     `json:"daily_reads"`
	LastReadAt       *string `json:"last_read_at,omitempty"`
	LastDailyResetAt *string `json:"last_daily_reset_at,omitempty"`
	UpdatedAt        string  `json:"updated_at"`
}

func getCloudEntitlement(ctx context.Context, userID string) (cloudEntitlement, error) {
	e := cloudEntitlement{
		IsPremium:     false,
		Plan:          "normal",
		HasNoAds:      false,
		PremiumActive: false,
	}

	var expiresAt *time.Time
	err := db.Pool.QueryRow(ctx, `
		SELECT is_premium, COALESCE(plan, 'normal'), expires_at, COALESCE(has_no_ads, false)
		FROM user_entitlements
		WHERE user_id::text = $1
	`, userID).Scan(&e.IsPremium, &e.Plan, &expiresAt, &e.HasNoAds)
	if err != nil {
		if err == pgx.ErrNoRows {
			return e, nil
		}
		return e, err
	}

	now := time.Now().UTC()
	if expiresAt != nil {
		ts := expiresAt.UTC().Format(time.RFC3339)
		e.ExpiresAt = &ts
	}
	e.PremiumActive = e.IsPremium && (expiresAt == nil || expiresAt.After(now))
	return e, nil
}

func requirePremiumForCloud(ctx context.Context, userID string) (cloudEntitlement, bool, int, string) {
	e, err := getCloudEntitlement(ctx, userID)
	if err != nil {
		return e, false, http.StatusInternalServerError, "failed to load entitlement"
	}
	if !e.PremiumActive {
		return e, false, http.StatusPaymentRequired, "premium_required"
	}
	return e, true, 0, ""
}

func ensureUserProfileRow(ctx context.Context, userID string) {
	_, _ = db.Pool.Exec(ctx, `
		INSERT INTO user_profile_stats (user_id)
		VALUES ($1::uuid)
		ON CONFLICT (user_id) DO NOTHING
	`, userID)
}

func readUserProfile(ctx context.Context, userID string) (cloudProfile, error) {
	ensureUserProfileRow(ctx, userID)

	var p cloudProfile
	var lastReadAt *time.Time
	var lastDailyResetAt *time.Time
	var updatedAt time.Time
	err := db.Pool.QueryRow(ctx, `
		SELECT total_reads, level, xp, streak_days, best_streak_days,
		       daily_target, daily_reads, last_read_at, last_daily_reset_at, updated_at
		FROM user_profile_stats
		WHERE user_id::text = $1
	`, userID).Scan(
		&p.TotalReads, &p.Level, &p.XP, &p.StreakDays, &p.BestStreakDays,
		&p.DailyTarget, &p.DailyReads, &lastReadAt, &lastDailyResetAt, &updatedAt,
	)
	if err != nil {
		return p, err
	}

	if lastReadAt != nil {
		s := lastReadAt.UTC().Format(time.RFC3339)
		p.LastReadAt = &s
	}
	if lastDailyResetAt != nil {
		s := lastDailyResetAt.UTC().Format(time.RFC3339)
		p.LastDailyResetAt = &s
	}
	p.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return p, nil
}

// CloudProfileGet returns premium cloud profile snapshot.
// GET /api/cloud/profile
func CloudProfileGet(w http.ResponseWriter, r *http.Request) {
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

	ent, allowed, status, msg := requirePremiumForCloud(ctx, userID)
	if !allowed {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": msg, "entitlement": ent})
		return
	}

	profile, err := readUserProfile(ctx, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to load profile"})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"entitlement": ent,
		"profile":     profile,
	})
}

// CloudProfileDelta applies batched counters for premium users.
// POST /api/cloud/profile/delta
func CloudProfileDelta(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		ReadsDelta      int    `json:"reads_delta"`
		XPDelta         int    `json:"xp_delta"`
		DailyReadsDelta int    `json:"daily_reads_delta"`
		DailyTarget     int    `json:"daily_target"`
		LastReadAt      string `json:"last_read_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	// Defensive limits against accidental huge payloads.
	if body.ReadsDelta > 10000 {
		body.ReadsDelta = 10000
	}
	if body.ReadsDelta < -10000 {
		body.ReadsDelta = -10000
	}
	if body.XPDelta > 100000 {
		body.XPDelta = 100000
	}
	if body.XPDelta < -100000 {
		body.XPDelta = -100000
	}
	if body.DailyReadsDelta > 10000 {
		body.DailyReadsDelta = 10000
	}
	if body.DailyReadsDelta < -10000 {
		body.DailyReadsDelta = -10000
	}
	if body.DailyTarget <= 0 {
		body.DailyTarget = 33
	}
	if body.DailyTarget > 100000 {
		body.DailyTarget = 100000
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	ent, allowed, status, msg := requirePremiumForCloud(ctx, userID)
	if !allowed {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": msg, "entitlement": ent})
		return
	}

	ensureUserProfileRow(ctx, userID)

	var lastReadAtParam interface{} = nil
	if body.LastReadAt != "" {
		if ts, err := time.Parse(time.RFC3339, body.LastReadAt); err == nil {
			lastReadAtParam = ts.UTC()
		}
	}

	_, err := db.Pool.Exec(ctx, `
		UPDATE user_profile_stats
		SET
			total_reads = GREATEST(0, total_reads + $2),
			xp = GREATEST(0, xp + $3),
			daily_reads = GREATEST(0, daily_reads + $4),
			daily_target = $5,
			last_read_at = COALESCE($6::timestamptz, last_read_at),
			updated_at = now()
		WHERE user_id::text = $1
	`, userID, body.ReadsDelta, body.XPDelta, body.DailyReadsDelta, body.DailyTarget, lastReadAtParam)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to apply profile delta"})
		return
	}

	if body.DailyReadsDelta != 0 {
		_, _ = db.Pool.Exec(ctx, `
			INSERT INTO user_daily_stats (user_id, day, reads)
			VALUES ($1::uuid, (now() AT TIME ZONE 'UTC')::date, GREATEST($2, 0))
			ON CONFLICT (user_id, day)
			DO UPDATE SET reads = GREATEST(0, user_daily_stats.reads + EXCLUDED.reads)
		`, userID, body.DailyReadsDelta)
	}

	profile, err := readUserProfile(ctx, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to load updated profile"})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "ok",
		"entitlement": ent,
		"profile":     profile,
	})
}

type cloudRoutine struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	BackgroundColor string   `json:"background_color"`
	IconColor       string   `json:"icon_color"`
	StyleKey        string   `json:"style_key"`
	ThemeJSON       string   `json:"theme_json"`
	IconKey         string   `json:"icon_key"`
	IconFile        string   `json:"icon_file"`
	SortOrder       int      `json:"sort_order"`
	BaseZikirIDs    []string `json:"base_zikir_ids"`
	AddedZikirIDs   []string `json:"added_zikir_ids"`
	UpdatedAt       string   `json:"updated_at"`
}

// CloudRoutinesList returns all custom routines for premium users.
// GET /api/cloud/routines
func CloudRoutinesList(w http.ResponseWriter, r *http.Request) {
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

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	ent, allowed, status, msg := requirePremiumForCloud(ctx, userID)
	if !allowed {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": msg, "entitlement": ent})
		return
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT id::text, name, description, background_color, icon_color, style_key, theme_json::text, icon_key, icon_file, sort_order, updated_at::text
		FROM user_routines
		WHERE user_id::text = $1
		ORDER BY sort_order ASC, created_at ASC
	`, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to load routines"})
		return
	}
	defer rows.Close()

	var list []cloudRoutine
	for rows.Next() {
		var e cloudRoutine
		if err := rows.Scan(&e.ID, &e.Name, &e.Description, &e.BackgroundColor, &e.IconColor, &e.StyleKey, &e.ThemeJSON, &e.IconKey, &e.IconFile, &e.SortOrder, &e.UpdatedAt); err != nil {
			continue
		}
		e.BaseZikirIDs = []string{}
		e.AddedZikirIDs = []string{}
		list = append(list, e)
	}
	if list == nil {
		list = []cloudRoutine{}
	}

	// Fill items per routine.
	for i := range list {
		itemRows, err := db.Pool.Query(ctx, `
			SELECT zikir_id, source
			FROM user_routine_items
			WHERE routine_id::text = $1
			ORDER BY sort_order ASC, created_at ASC
		`, list[i].ID)
		if err != nil {
			continue
		}
		for itemRows.Next() {
			var zikirID, source string
			if err := itemRows.Scan(&zikirID, &source); err != nil {
				continue
			}
			if source == "base" {
				list[i].BaseZikirIDs = append(list[i].BaseZikirIDs, zikirID)
			} else {
				list[i].AddedZikirIDs = append(list[i].AddedZikirIDs, zikirID)
			}
		}
		itemRows.Close()
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"entitlement": ent,
		"routines":    list,
	})
}

// CloudRoutinesUpsert creates/updates one routine for premium users.
// POST /api/cloud/routines/upsert
func CloudRoutinesUpsert(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		RoutineID       string   `json:"routine_id"`
		Name            string   `json:"name"`
		Description     string   `json:"description"`
		BackgroundColor string   `json:"background_color"`
		IconColor       string   `json:"icon_color"`
		StyleKey        string   `json:"style_key"`
		ThemeJSON       string   `json:"theme_json"`
		IconKey         string   `json:"icon_key"`
		IconFile        string   `json:"icon_file"`
		SortOrder       int      `json:"sort_order"`
		BaseZikirIDs    []string `json:"base_zikir_ids"`
		AddedZikirIDs   []string `json:"added_zikir_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "name required"})
		return
	}

	if body.ThemeJSON == "" {
		body.ThemeJSON = "{}"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	ent, allowed, status, msg := requirePremiumForCloud(ctx, userID)
	if !allowed {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": msg, "entitlement": ent})
		return
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to begin tx"})
		return
	}
	defer tx.Rollback(ctx)

	routineID := body.RoutineID
	if routineID == "" {
		err = tx.QueryRow(ctx, `
			INSERT INTO user_routines (user_id, name, description, background_color, icon_color, style_key, theme_json, icon_key, icon_file, sort_order, updated_at)
			VALUES ($1::uuid, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, now())
			RETURNING id::text
		`, userID, body.Name, body.Description, body.BackgroundColor, body.IconColor, body.StyleKey, body.ThemeJSON, body.IconKey, body.IconFile, body.SortOrder).Scan(&routineID)
	} else {
		res, errExec := tx.Exec(ctx, `
			UPDATE user_routines
			SET name=$1, description=$2, background_color=$3, icon_color=$4, style_key=$5, theme_json=$6::jsonb, icon_key=$7, icon_file=$8, sort_order=$9, updated_at=now()
			WHERE id::text=$10 AND user_id::text=$11
		`, body.Name, body.Description, body.BackgroundColor, body.IconColor, body.StyleKey, body.ThemeJSON, body.IconKey, body.IconFile, body.SortOrder, routineID, userID)
		if errExec != nil {
			err = errExec
		} else if res.RowsAffected() == 0 {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "routine not found or not yours"})
			return
		}
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to upsert routine"})
		return
	}

	_, _ = tx.Exec(ctx, `DELETE FROM user_routine_items WHERE routine_id::text = $1`, routineID)

	sort := 0
	for _, z := range body.BaseZikirIDs {
		if z == "" {
			continue
		}
		_, _ = tx.Exec(ctx, `
			INSERT INTO user_routine_items (routine_id, zikir_id, source, sort_order)
			VALUES ($1::uuid, $2, 'base', $3)
		`, routineID, z, sort)
		sort++
	}
	sort = 0
	for _, z := range body.AddedZikirIDs {
		if z == "" {
			continue
		}
		_, _ = tx.Exec(ctx, `
			INSERT INTO user_routine_items (routine_id, zikir_id, source, sort_order)
			VALUES ($1::uuid, $2, 'added', $3)
		`, routineID, z, sort)
		sort++
	}

	if err := tx.Commit(ctx); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to commit routine"})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "ok",
		"entitlement":  ent,
		"routine_id":   routineID,
		"created":      body.RoutineID == "",
		"items_counts": map[string]int{"base": len(body.BaseZikirIDs), "added": len(body.AddedZikirIDs)},
	})
}

// CloudRoutinesDelete deletes one routine for premium users.
// POST /api/cloud/routines/delete
func CloudRoutinesDelete(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		RoutineID string `json:"routine_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RoutineID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "routine_id required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	ent, allowed, status, msg := requirePremiumForCloud(ctx, userID)
	if !allowed {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": msg, "entitlement": ent})
		return
	}

	res, err := db.Pool.Exec(ctx, `DELETE FROM user_routines WHERE id::text = $1 AND user_id::text = $2`, body.RoutineID, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to delete routine"})
		return
	}
	if res.RowsAffected() == 0 {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "routine not found or not yours"})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "entitlement": ent})
}

// CloudDailyGet returns daily_zikir style payload for premium users.
// GET /api/cloud/daily
func CloudDailyGet(w http.ResponseWriter, r *http.Request) {
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

	ent, allowed, status, msg := requirePremiumForCloud(ctx, userID)
	if !allowed {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": msg, "entitlement": ent})
		return
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT day::text, reads, completed
		FROM user_daily_stats
		WHERE user_id::text = $1
		ORDER BY day ASC
	`, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to load daily stats"})
		return
	}
	defer rows.Close()

	type dayEntry struct {
		Date  string `json:"date"`
		Count int    `json:"count"`
	}
	var days []dayEntry
	var completedDays []string
	for rows.Next() {
		var d string
		var c int
		var completed bool
		if err := rows.Scan(&d, &c, &completed); err != nil {
			continue
		}
		days = append(days, dayEntry{Date: d, Count: c})
		if completed {
			completedDays = append(completedDays, d)
		}
	}
	if days == nil {
		days = []dayEntry{}
	}
	if completedDays == nil {
		completedDays = []string{}
	}

	var lastCompleted *string
	currentStreak := 0
	var lcd *time.Time
	_ = db.Pool.QueryRow(ctx, `
		SELECT last_completed_date, current_streak
		FROM user_daily_meta
		WHERE user_id::text = $1
	`, userID).Scan(&lcd, &currentStreak)
	if lcd != nil {
		s := lcd.UTC().Format("2006-01-02")
		lastCompleted = &s
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"entitlement":       ent,
		"days":              days,
		"completedDays":     completedDays,
		"lastCompletedDate": lastCompleted,
		"currentStreak":     currentStreak,
	})
}

// CloudDailyUpsert saves daily_zikir style payload for premium users.
// POST /api/cloud/daily/upsert
func CloudDailyUpsert(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		Days []struct {
			Date  string `json:"date"`
			Count int    `json:"count"`
		} `json:"days"`
		CompletedDays     []string `json:"completedDays"`
		LastCompletedDate string   `json:"lastCompletedDate"`
		CurrentStreak     int      `json:"currentStreak"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	ent, allowed, status, msg := requirePremiumForCloud(ctx, userID)
	if !allowed {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": msg, "entitlement": ent})
		return
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to begin tx"})
		return
	}
	defer tx.Rollback(ctx)

	_, _ = tx.Exec(ctx, `DELETE FROM user_daily_stats WHERE user_id::text = $1`, userID)
	completedSet := map[string]bool{}
	for _, d := range body.CompletedDays {
		if d != "" {
			completedSet[d] = true
		}
	}

	for _, d := range body.Days {
		if d.Date == "" {
			continue
		}
		if d.Count < 0 {
			d.Count = 0
		}
		_, _ = tx.Exec(ctx, `
			INSERT INTO user_daily_stats (user_id, day, reads, completed)
			VALUES ($1::uuid, $2::date, $3, $4)
		`, userID, d.Date, d.Count, completedSet[d.Date])
	}

	var lcd interface{} = nil
	if body.LastCompletedDate != "" {
		lcd = body.LastCompletedDate
	}
	if body.CurrentStreak < 0 {
		body.CurrentStreak = 0
	}
	_, _ = tx.Exec(ctx, `
		INSERT INTO user_daily_meta (user_id, last_completed_date, current_streak, updated_at)
		VALUES ($1::uuid, $2::date, $3, now())
		ON CONFLICT (user_id) DO UPDATE
		SET last_completed_date = EXCLUDED.last_completed_date,
		    current_streak = EXCLUDED.current_streak,
		    updated_at = now()
	`, userID, lcd, body.CurrentStreak)

	if err := tx.Commit(ctx); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to commit daily sync"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "entitlement": ent})
}

// CloudZikirProgressGet returns user_progress-style map for premium users.
// GET /api/cloud/zikir-progress
func CloudZikirProgressGet(w http.ResponseWriter, r *http.Request) {
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
	ent, allowed, status, msg := requirePremiumForCloud(ctx, userID)
	if !allowed {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": msg, "entitlement": ent})
		return
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT zikir_key, clicks, repeats, target_count, is_favourite
		FROM user_zikir_progress
		WHERE user_id::text = $1
	`, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to load zikir progress"})
		return
	}
	defer rows.Close()

	out := map[string]interface{}{}
	for rows.Next() {
		var key string
		var clicks, repeats, target int
		var fav bool
		if err := rows.Scan(&key, &clicks, &repeats, &target, &fav); err != nil {
			continue
		}
		out[key] = map[string]interface{}{
			"clicks":      clicks,
			"repeats":     repeats,
			"targetCount": target,
			"isFavourite": fav,
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"entitlement": ent, "progress": out})
}

// CloudZikirProgressUpsertBatch upserts user_progress entries for premium users.
// POST /api/cloud/zikir-progress/upsert-batch
func CloudZikirProgressUpsertBatch(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		ReplaceAll bool `json:"replace_all"`
		Items      []struct {
			Key         string `json:"key"`
			Clicks      int    `json:"clicks"`
			Repeats     int    `json:"repeats"`
			TargetCount int    `json:"targetCount"`
			IsFavourite bool   `json:"isFavourite"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	ent, allowed, status, msg := requirePremiumForCloud(ctx, userID)
	if !allowed {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": msg, "entitlement": ent})
		return
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to begin tx"})
		return
	}
	defer tx.Rollback(ctx)

	if body.ReplaceAll {
		_, _ = tx.Exec(ctx, `DELETE FROM user_zikir_progress WHERE user_id::text = $1`, userID)
	}
	for _, it := range body.Items {
		if it.Key == "" {
			continue
		}
		if it.Clicks < 0 {
			it.Clicks = 0
		}
		if it.Repeats < 0 {
			it.Repeats = 0
		}
		if it.TargetCount < 0 {
			it.TargetCount = 0
		}
		_, _ = tx.Exec(ctx, `
			INSERT INTO user_zikir_progress (user_id, zikir_key, clicks, repeats, target_count, is_favourite, updated_at)
			VALUES ($1::uuid, $2, $3, $4, $5, $6, now())
			ON CONFLICT (user_id, zikir_key) DO UPDATE
			SET clicks = EXCLUDED.clicks,
			    repeats = EXCLUDED.repeats,
			    target_count = EXCLUDED.target_count,
			    is_favourite = EXCLUDED.is_favourite,
			    updated_at = now()
		`, userID, it.Key, it.Clicks, it.Repeats, it.TargetCount, it.IsFavourite)
	}
	if err := tx.Commit(ctx); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to commit zikir progress"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "entitlement": ent, "count": len(body.Items)})
}

// CloudAchievementsGet returns achievements-style list for premium users.
// GET /api/cloud/achievements
func CloudAchievementsGet(w http.ResponseWriter, r *http.Request) {
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
	ent, allowed, status, msg := requirePremiumForCloud(ctx, userID)
	if !allowed {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": msg, "entitlement": ent})
		return
	}

	type item struct {
		ID       string `json:"id"`
		Current  int    `json:"current"`
		Unlocked bool   `json:"unlocked"`
	}
	var items []item
	rows, err := db.Pool.Query(ctx, `
		SELECT achievement_id, current_value, unlocked
		FROM user_achievements
		WHERE user_id::text = $1
		ORDER BY achievement_id ASC
	`, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to load achievements"})
		return
	}
	defer rows.Close()
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Current, &it.Unlocked); err == nil {
			items = append(items, it)
		}
	}
	if items == nil {
		items = []item{}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"entitlement": ent, "items": items})
}

// CloudAchievementsUpsertBatch upserts achievements for premium users.
// POST /api/cloud/achievements/upsert-batch
func CloudAchievementsUpsertBatch(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Content-Type", "application/json")
	var body struct {
		ReplaceAll bool `json:"replace_all"`
		Items      []struct {
			ID       string `json:"id"`
			Current  int    `json:"current"`
			Unlocked bool   `json:"unlocked"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	ent, allowed, status, msg := requirePremiumForCloud(ctx, userID)
	if !allowed {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": msg, "entitlement": ent})
		return
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to begin tx"})
		return
	}
	defer tx.Rollback(ctx)
	if body.ReplaceAll {
		_, _ = tx.Exec(ctx, `DELETE FROM user_achievements WHERE user_id::text = $1`, userID)
	}
	for _, it := range body.Items {
		if it.ID == "" {
			continue
		}
		if it.Current < 0 {
			it.Current = 0
		}
		_, _ = tx.Exec(ctx, `
			INSERT INTO user_achievements (user_id, achievement_id, current_value, unlocked, updated_at)
			VALUES ($1::uuid, $2, $3, $4, now())
			ON CONFLICT (user_id, achievement_id) DO UPDATE
			SET current_value = EXCLUDED.current_value,
			    unlocked = EXCLUDED.unlocked,
			    updated_at = now()
		`, userID, it.ID, it.Current, it.Unlocked)
	}
	if err := tx.Commit(ctx); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to commit achievements"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "entitlement": ent, "count": len(body.Items)})
}

