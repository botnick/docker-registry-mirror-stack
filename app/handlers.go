package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func (a *App) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	fallback := a.currentFallbackStatus()
	a.writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"time":           nowRFC3339(),
		"ui":             "/dashboard",
		"fallback_state": fallback.State,
	})
}

func (a *App) handleNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		a.writeError(w, http.StatusBadRequest, "อ่าน request body ไม่สำเร็จ")
		return
	}

	var envelope NotificationEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		a.writeError(w, http.StatusBadRequest, "payload ไม่ใช่ JSON ที่รองรับ")
		return
	}
	events := envelope.Events
	if len(events) == 0 {
		events = []json.RawMessage{body}
	}

	stored := 0
	failed := 0
	for _, raw := range events {
		if err := a.recordEvent(r.Context(), raw); err != nil {
			failed++
			a.logger.Warn("record event failed", "error", err)
			continue
		}
		stored++
	}
	a.writeJSON(w, http.StatusOK, map[string]any{"stored": stored, "failed": failed})
}

func (a *App) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	overview, err := a.buildOverview(r.Context())
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, overview)
}

func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	maintenance, _ := a.getMaintenanceState(r.Context())
	a.writeJSON(w, http.StatusOK, map[string]any{
		"registry_url":                    a.cfg.RegistryURL,
		"registry_container_name":         a.cfg.RegistryContainerName,
		"registry_data_path":              a.cfg.RegistryDataPath,
		"registry_config_path":            a.cfg.RegistryConfigPath,
		"sqlite_path":                     a.cfg.SQLitePath,
		"gc_request_flag":                 a.cfg.GCRequestFlag,
		"listen_host":                     a.cfg.ListenHost,
		"listen_port":                     a.cfg.ListenPort,
		"public_base_url":                 a.cfg.PublicBaseURL,
		"cookie_secure":                   a.cfg.CookieSecure,
		"session_ttl_hours":               int(a.cfg.SessionTTL.Hours()),
		"session_refresh_minutes":         safeDurationMinutes(a.cfg.SessionRefreshInterval),
		"bootstrap_username":              a.cfg.BootstrapUsername,
		"bootstrap_force_password_change": a.cfg.BootstrapForcePasswordChange,
		"dry_run":                         a.cfg.DryRun,
		"janitor_interval_seconds":        int(a.cfg.JanitorInterval.Seconds()),
		"max_delete_batch":                a.cfg.MaxDeleteBatch,
		"unused_days":                     a.cfg.UnusedDays,
		"min_cache_age_days":              a.cfg.MinCacheAgeDays,
		"low_watermark_pct":               a.cfg.LowWatermarkPct,
		"target_free_pct":                 a.cfg.TargetFreePct,
		"emergency_free_pct":              a.cfg.EmergencyFreePct,
		"gc_hour_utc":                     a.cfg.GCHourUTC,
		"protected_repos_regex":           a.cfg.ProtectedReposPattern,
		"protected_tags_regex":            a.cfg.ProtectedTagsPattern,
		"upstream_health_url":             a.cfg.UpstreamURL,
		"health_check_interval_seconds":   int(a.cfg.HealthCheckInterval.Seconds()),
		"upstream_timeout_seconds":        int(a.cfg.UpstreamTimeout.Seconds()),
		"log_retention_days":              a.cfg.LogRetentionDays,
		"event_retention_days":            a.cfg.EventRetentionDays,
		"job_retention_days":              a.cfg.JobRetentionDays,
		"login_max_attempts":              a.cfg.LoginMaxAttempts,
		"login_lock_minutes":              safeDurationMinutes(a.cfg.LoginLockDuration),
		"maintenance":                     maintenance,
	})
}

func (a *App) handleFallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	status, err := a.refreshFallbackStatus(r.Context())
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, status)
}

func (a *App) handleCacheOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	overview, err := a.buildCacheOverview(r.Context())
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, overview)
}

func (a *App) handleCandidates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	limit := clamp(queryInt(r.URL.Query().Get("limit"), 50), 1, 250)
	emergencyMode := parseBool(r.URL.Query().Get("emergency"))
	items, err := a.listCandidates(r.Context(), emergencyMode)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	total := len(items)
	if total > limit {
		items = items[:limit]
	}
	a.writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"total":    total,
		"limit":    limit,
		"has_more": total > limit,
	})
}

func (a *App) handleCleanupHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	limit := clamp(queryInt(r.URL.Query().Get("limit"), 20), 1, 100)
	offset := maxInt(queryInt(r.URL.Query().Get("offset"), 0), 0)
	statusFilter := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("status")))
	items, total, err := a.queryJobs(r.Context(), "janitor", statusFilter, limit, offset)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, PaginatedResponse[JobRun]{
		Items:   items,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasMore: offset+limit < total,
	})
}

func (a *App) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	limit := clamp(queryInt(r.URL.Query().Get("limit"), 40), 1, 250)
	offset := maxInt(queryInt(r.URL.Query().Get("offset"), 0), 0)
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	state := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("state")))
	pinnedFilter := parseOptionalBool(r.URL.Query().Get("pinned"))
	protectedFilter := parseOptionalBool(r.URL.Query().Get("protected"))

	items, total, err := a.queryArtifacts(r.Context(), search, state, pinnedFilter, protectedFilter, limit, offset)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, PaginatedResponse[Artifact]{
		Items:   items,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasMore: offset+limit < total,
	})
}

func (a *App) handleArtifactDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	digest := strings.TrimSpace(r.URL.Query().Get("digest"))
	if repo == "" || digest == "" {
		a.writeError(w, http.StatusBadRequest, "ต้องระบุ repo และ digest")
		return
	}
	detail, err := a.queryArtifactDetail(r.Context(), repo, digest)
	if err != nil {
		if err == sql.ErrNoRows {
			a.writeError(w, http.StatusNotFound, "ไม่พบ artifact ที่ต้องการ")
			return
		}
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, detail)
}

func (a *App) handleArtifactHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	digest := strings.TrimSpace(r.URL.Query().Get("digest"))
	if repo == "" || digest == "" {
		a.writeError(w, http.StatusBadRequest, "ต้องระบุ repo และ digest")
		return
	}
	events, err := a.queryArtifactEvents(r.Context(), repo, digest, 30)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	logs, err := a.queryArtifactLogs(r.Context(), repo, digest, 30)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"logs":   logs,
	})
}

func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	limit := clamp(queryInt(r.URL.Query().Get("limit"), 40), 1, 250)
	offset := maxInt(queryInt(r.URL.Query().Get("offset"), 0), 0)
	includeRaw := parseBool(r.URL.Query().Get("include_raw"))

	items, total, err := a.queryEvents(r.Context(), limit, offset, includeRaw)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, PaginatedResponse[EventRecord]{
		Items:   items,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasMore: offset+limit < total,
	})
}

func (a *App) handleJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	limit := clamp(queryInt(r.URL.Query().Get("limit"), 20), 1, 100)
	offset := maxInt(queryInt(r.URL.Query().Get("offset"), 0), 0)
	jobType := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("job_type")))
	statusFilter := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("status")))

	items, total, err := a.queryJobs(r.Context(), jobType, statusFilter, limit, offset)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, PaginatedResponse[JobRun]{
		Items:   items,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasMore: offset+limit < total,
	})
}

func (a *App) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	limit := clamp(queryInt(r.URL.Query().Get("limit"), 50), 1, 250)
	offset := maxInt(queryInt(r.URL.Query().Get("offset"), 0), 0)
	level := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("level")))
	scope := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("scope")))
	actor := strings.TrimSpace(r.URL.Query().Get("actor"))
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	afterID, _ := parseInt64(r.URL.Query().Get("after_id"))

	items, total, err := a.queryLogs(r.Context(), level, scope, actor, search, afterID, limit, offset)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, PaginatedResponse[LogRecord]{
		Items:   items,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasMore: offset+limit < total,
	})
}

func (a *App) handleRunJanitor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var payload struct {
		DryRun bool `json:"dry_run"`
		Force  bool `json:"force"`
	}
	if r.ContentLength > 0 {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload)
	}
	result, err := a.runJanitor(r.Context(), "manual", payload.DryRun, payload.Force)
	if err != nil {
		status := http.StatusInternalServerError
		if err == errOperationBusy {
			status = http.StatusConflict
		}
		a.writeError(w, status, err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, result)
}

func (a *App) handleRunGC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var payload struct {
		Force bool `json:"force"`
	}
	if r.ContentLength > 0 {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload)
	}
	result, err := a.runGC(r.Context(), "manual", payload.Force)
	if err != nil {
		status := http.StatusInternalServerError
		if err == errOperationBusy {
			status = http.StatusConflict
		}
		a.writeError(w, status, err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, result)
}

func (a *App) handleGCStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	fallback := a.currentFallbackStatus()
	lastGC, _ := a.getLastJob(r.Context(), "gc")
	a.writeJSON(w, http.StatusOK, map[string]any{
		"pending":        a.gcRequested(),
		"fallback_state": fallback.State,
		"last_gc":        lastGC,
	})
}

func (a *App) handleGCHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	limit := clamp(queryInt(r.URL.Query().Get("limit"), 20), 1, 100)
	offset := maxInt(queryInt(r.URL.Query().Get("offset"), 0), 0)
	statusFilter := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("status")))
	items, total, err := a.queryJobs(r.Context(), "gc", statusFilter, limit, offset)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, PaginatedResponse[JobRun]{
		Items:   items,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasMore: offset+limit < total,
	})
}

func (a *App) handleClearGCRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if err := a.clearGCRequest(); err != nil {
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.logSystem(r.Context(), "info", "gc", "", "gc request cleared", nil)
	a.writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handlePin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var payload struct {
		Repo   string `json:"repo"`
		Digest string `json:"digest"`
		Pinned *bool  `json:"pinned"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
		a.writeError(w, http.StatusBadRequest, "payload pin ไม่ถูกต้อง")
		return
	}
	if payload.Pinned == nil {
		value := true
		payload.Pinned = &value
	}
	if payload.Repo == "" || payload.Digest == "" {
		a.writeError(w, http.StatusBadRequest, "ต้องระบุ repo และ digest")
		return
	}
	result, err := a.db.ExecContext(r.Context(), "UPDATE artifacts SET pinned = ? WHERE repo = ? AND digest = ?", boolToInt(*payload.Pinned), payload.Repo, payload.Digest)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		a.writeError(w, http.StatusNotFound, "ไม่พบ artifact ที่ต้องการ")
		return
	}
	auth, _ := a.authSessionFromRequest(r)
	a.logSystem(r.Context(), "info", "artifact", auth.User.Username, "pin updated", map[string]any{
		"repo":   payload.Repo,
		"digest": payload.Digest,
		"pinned": *payload.Pinned,
	})
	a.writeJSON(w, http.StatusOK, map[string]any{"repo": payload.Repo, "digest": payload.Digest, "pinned": *payload.Pinned})
}

func (a *App) handleProtect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var payload struct {
		Repo      string `json:"repo"`
		Digest    string `json:"digest"`
		Protected *bool  `json:"protected"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
		a.writeError(w, http.StatusBadRequest, "payload protect ไม่ถูกต้อง")
		return
	}
	if payload.Protected == nil {
		value := true
		payload.Protected = &value
	}
	if payload.Repo == "" || payload.Digest == "" {
		a.writeError(w, http.StatusBadRequest, "ต้องระบุ repo และ digest")
		return
	}
	result, err := a.db.ExecContext(r.Context(), "UPDATE artifacts SET explicit_protected = ? WHERE repo = ? AND digest = ?", boolToInt(*payload.Protected), payload.Repo, payload.Digest)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		a.writeError(w, http.StatusNotFound, "ไม่พบ artifact ที่ต้องการ")
		return
	}
	auth, _ := a.authSessionFromRequest(r)
	a.logSystem(r.Context(), "info", "artifact", auth.User.Username, "protect updated", map[string]any{
		"repo":      payload.Repo,
		"digest":    payload.Digest,
		"protected": *payload.Protected,
	})
	a.writeJSON(w, http.StatusOK, map[string]any{"repo": payload.Repo, "digest": payload.Digest, "protected": *payload.Protected})
}

func (a *App) handleMaintenanceState(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		state, err := a.getMaintenanceState(r.Context())
		if err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.writeJSON(w, http.StatusOK, state)
	case http.MethodPost:
		var payload MaintenanceState
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
			a.writeError(w, http.StatusBadRequest, "payload maintenance ไม่ถูกต้อง")
			return
		}
		if err := a.updateMaintenanceState(r.Context(), payload); err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		auth, _ := a.authSessionFromRequest(r)
		a.logSystem(r.Context(), "info", "maintenance", auth.User.Username, "maintenance state updated", payload)
		state, _ := a.getMaintenanceState(r.Context())
		a.writeJSON(w, http.StatusOK, state)
	default:
		a.writeMethodNotAllowed(w, "GET, POST")
	}
}
