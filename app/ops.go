package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

var errOperationBusy = errors.New("another maintenance operation is already running")

type GCRequest struct {
	TriggerSource string `json:"trigger_source"`
	RequestedAt   string `json:"requested_at"`
	Force         bool   `json:"force"`
}

func (a *App) buildOverview(ctx context.Context) (map[string]any, error) {
	fallback, _ := a.refreshFallbackStatus(ctx)
	maintenance, _ := a.getMaintenanceState(ctx)
	activeCount, err := a.countQuery(ctx, "SELECT COUNT(*) FROM artifacts WHERE deleted_at IS NULL")
	if err != nil {
		return nil, err
	}
	deletedCount, err := a.countQuery(ctx, "SELECT COUNT(*) FROM artifacts WHERE deleted_at IS NOT NULL")
	if err != nil {
		return nil, err
	}
	pinnedCount, err := a.countQuery(ctx, "SELECT COUNT(*) FROM artifacts WHERE pinned = 1")
	if err != nil {
		return nil, err
	}
	protectedCount, err := a.countQuery(ctx, "SELECT COUNT(*) FROM artifacts WHERE explicit_protected = 1")
	if err != nil {
		return nil, err
	}
	eventCount, err := a.countQuery(ctx, "SELECT COUNT(*) FROM events")
	if err != nil {
		return nil, err
	}
	logCount, err := a.countQuery(ctx, "SELECT COUNT(*) FROM system_logs")
	if err != nil {
		return nil, err
	}
	candidates, err := a.listCandidates(ctx, false)
	if err != nil {
		return nil, err
	}

	lastJanitor, _ := a.getLastJob(ctx, "janitor")
	lastGC, _ := a.getLastJob(ctx, "gc")

	return map[string]any{
		"generated_at":   nowRFC3339(),
		"uptime_seconds": int64(time.Since(a.startedAt).Seconds()),
		"runtime": map[string]any{
			"go_version": runtime.Version(),
			"go_os":      runtime.GOOS,
			"go_arch":    runtime.GOARCH,
			"app_mode":   a.cfg.AppMode,
		},
		"registry":    fallback.Registry,
		"upstream":    fallback.Upstream,
		"fallback":    fallback,
		"maintenance": maintenance,
		"storage":     fallback.Storage,
		"policy":      a.policySnapshot(),
		"signals":     a.operationSnapshot(),
		"gc_pending":  a.gcRequested(),
		"counts":      map[string]any{"active_artifacts": activeCount, "deleted_artifacts": deletedCount, "pinned_artifacts": pinnedCount, "explicit_protected_artifacts": protectedCount, "events_total": eventCount, "logs_total": logCount, "eligible_candidates": len(candidates)},
		"last_runs":   map[string]any{"janitor": lastJanitor, "gc": lastGC},
		"public_base": a.cfg.PublicBaseURL,
		"security": map[string]any{
			"cookie_secure":          a.cfg.CookieSecure,
			"session_ttl_hours":      int(a.cfg.SessionTTL.Hours()),
			"notifications_username": a.cfg.NotificationsUsername,
			"trust_proxy_headers":    a.cfg.TrustProxyHeaders,
		},
	}, nil
}

func (a *App) buildCacheOverview(ctx context.Context) (map[string]any, error) {
	fallback, _ := a.refreshFallbackStatus(ctx)
	candidates, err := a.listCandidates(ctx, false)
	if err != nil {
		return nil, err
	}

	largestRows, err := a.db.QueryContext(ctx, `
		SELECT repo, tag, digest, media_type, COALESCE(size_bytes, 0), first_seen_at, last_used_at, COALESCE(use_count, 0), pinned, explicit_protected, deleted_at, delete_reason
		FROM artifacts
		WHERE deleted_at IS NULL
		ORDER BY COALESCE(size_bytes, 0) DESC, last_used_at DESC
		LIMIT 10
	`)
	if err != nil {
		return nil, err
	}
	defer largestRows.Close()

	var largest []Artifact
	for largestRows.Next() {
		item, err := a.scanArtifact(largestRows)
		if err != nil {
			return nil, err
		}
		largest = append(largest, a.decorateArtifact(item, false))
	}

	return map[string]any{
		"generated_at": nowRFC3339(),
		"storage":      fallback.Storage,
		"fallback":     fallback,
		"largest":      largest,
		"candidates":   candidates,
	}, nil
}

func (a *App) policySnapshot() map[string]any {
	return map[string]any{
		"dry_run":                   a.cfg.DryRun,
		"unused_days":               a.cfg.UnusedDays,
		"min_cache_age_days":        a.cfg.MinCacheAgeDays,
		"max_delete_batch":          a.cfg.MaxDeleteBatch,
		"janitor_interval_seconds":  int(a.cfg.JanitorInterval.Seconds()),
		"low_watermark_pct":         a.cfg.LowWatermarkPct,
		"target_free_pct":           a.cfg.TargetFreePct,
		"emergency_free_pct":        a.cfg.EmergencyFreePct,
		"gc_hour_utc":               a.cfg.GCHourUTC,
		"gc_worker_poll_seconds":    int(a.cfg.GCWorkerPollInterval.Seconds()),
		"protected_repos_regex":     a.cfg.ProtectedReposPattern,
		"protected_tags_regex":      a.cfg.ProtectedTagsPattern,
		"health_check_interval_sec": int(a.cfg.HealthCheckInterval.Seconds()),
		"upstream_health_url":       a.cfg.UpstreamURL,
		"log_retention_days":        a.cfg.LogRetentionDays,
		"event_retention_days":      a.cfg.EventRetentionDays,
		"job_retention_days":        a.cfg.JobRetentionDays,
	}
}

func (a *App) refreshFallbackStatus(ctx context.Context) (FallbackStatus, error) {
	registryProbe := a.probeHTTP(ctx, "registry", a.cfg.RegistryURL+"/v2/", a.cfg.UpstreamTimeout)
	upstreamProbe := a.probeHTTP(ctx, "upstream", a.cfg.UpstreamURL, a.cfg.UpstreamTimeout)
	diskStats, diskErr := getDiskStats(a.cfg.RegistryDataPath)
	maintenance, _ := a.getMaintenanceState(ctx)
	signals := a.operationSnapshot()

	storage := map[string]any{
		"path":               a.cfg.RegistryDataPath,
		"gc_request_pending": a.gcRequested(),
	}
	if diskErr != nil {
		storage["disk_error"] = diskErr.Error()
	} else {
		storage["total_bytes"] = diskStats.TotalBytes
		storage["used_bytes"] = diskStats.UsedBytes
		storage["free_bytes"] = diskStats.FreeBytes
		storage["used_pct"] = roundFloat(diskStats.UsedPercent)
		storage["free_pct"] = roundFloat(diskStats.FreePercent)
		storage["pressure"] = diskStats.FreePercent < float64(a.cfg.LowWatermarkPct)
		storage["emergency"] = diskStats.FreePercent < float64(a.cfg.EmergencyFreePct)
		storage["bytes_to_target"] = bytesToTarget(diskStats.TotalBytes, diskStats.FreeBytes, a.cfg.TargetFreePct)
	}

	state := "normal"
	summary := "ready"
	details := "mirror and upstream are reachable"
	cachedModeUsable := registryProbe.Healthy
	destructivePaused := false

	switch {
	case maintenance.MaintenanceMode || signals["gc_running"]:
		state = "maintenance"
		summary = "maintenance mode active"
		details = "destructive operations are paused while maintenance or GC is active"
		destructivePaused = true
	case !registryProbe.Healthy:
		state = "mirror-degraded"
		summary = "registry mirror is unhealthy"
		details = "check the registry service before continuing with cleanup or GC"
		destructivePaused = true
	case !upstreamProbe.Healthy:
		state = "upstream-degraded"
		summary = "upstream registry is degraded"
		details = "cached artifacts can still be served, but cache misses may fail"
		destructivePaused = true
	case storageBool(storage, "emergency"):
		state = "storage-emergency"
		summary = "storage is critically low"
		details = "review cleanup candidates or extend capacity immediately"
	case storageBool(storage, "pressure"):
		state = "storage-pressure"
		summary = "storage is below the low watermark"
		details = "cleanup should be reviewed soon"
	}

	if maintenance.JanitorPaused || maintenance.GCPaused {
		destructivePaused = true
	}

	status := FallbackStatus{
		State:             state,
		Summary:           summary,
		Details:           details,
		Since:             nowRFC3339(),
		LastCheckAt:       nowRFC3339(),
		CachedModeUsable:  cachedModeUsable,
		DestructivePaused: destructivePaused,
		Registry:          registryProbe,
		Upstream:          upstreamProbe,
		Storage:           storage,
		Maintenance: map[string]any{
			"maintenance_mode": maintenance.MaintenanceMode,
			"janitor_paused":   maintenance.JanitorPaused,
			"gc_paused":        maintenance.GCPaused,
			"note":             maintenance.Note,
			"gc_running":       signals["gc_running"],
			"janitor_running":  signals["janitor_running"],
		},
	}

	a.fallbackMu.Lock()
	previous := a.fallbackState
	if previous.State == status.State && previous.Summary == status.Summary {
		status.Since = previous.Since
	}
	a.fallbackState = status
	a.fallbackMu.Unlock()

	if previous.State != "" && previous.State != status.State {
		a.logSystem(ctx, "warn", "health", "", "fallback state changed", map[string]any{
			"from":    previous.State,
			"to":      status.State,
			"summary": status.Summary,
		})
	}

	return status, nil
}

func (a *App) currentFallbackStatus() FallbackStatus {
	a.fallbackMu.RLock()
	defer a.fallbackMu.RUnlock()
	return a.fallbackState
}

func (a *App) probeHTTP(ctx context.Context, name, url string, timeout time.Duration) HealthProbe {
	started := time.Now()
	probe := HealthProbe{Name: name, LastCheckAt: nowRFC3339()}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		probe.Message = err.Error()
		return probe
	}
	resp, err := a.httpClient.Do(req)
	probe.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		probe.Message = err.Error()
		return probe
	}
	defer resp.Body.Close()
	probe.StatusCode = resp.StatusCode
	probe.Healthy = resp.StatusCode < 500 && resp.StatusCode > 0
	if probe.Healthy {
		probe.Message = "reachable"
	} else {
		probe.Message = fmt.Sprintf("status %d", resp.StatusCode)
	}
	return probe
}

func storageBool(storage map[string]any, key string) bool {
	value, ok := storage[key]
	if !ok {
		return false
	}
	result, ok := value.(bool)
	return ok && result
}

func (a *App) runJanitor(ctx context.Context, trigger string, requestDryRun, force bool) (JanitorResult, error) {
	if err := a.beginOperation("janitor"); err != nil {
		return JanitorResult{}, err
	}
	defer a.endOperation("janitor")

	startedAt := nowRFC3339()
	jobID, err := a.createJobRun(ctx, "janitor", trigger, startedAt)
	if err != nil {
		return JanitorResult{}, err
	}

	result := JanitorResult{
		TriggerSource:    trigger,
		StartedAt:        startedAt,
		DryRun:           a.cfg.DryRun || requestDryRun,
		Forced:           force,
		LowWatermarkPct:  a.cfg.LowWatermarkPct,
		TargetFreePct:    a.cfg.TargetFreePct,
		EmergencyFreePct: a.cfg.EmergencyFreePct,
		BatchLimit:       a.cfg.MaxDeleteBatch,
	}

	fail := func(runErr error) (JanitorResult, error) {
		result.FinishedAt = nowRFC3339()
		details, _ := json.Marshal(result)
		_ = a.finishJobRun(context.Background(), jobID, "error", result.FinishedAt, details)
		a.logSystem(context.Background(), "error", "janitor", "", "janitor failed", map[string]any{
			"error":   runErr.Error(),
			"trigger": trigger,
		})
		return result, runErr
	}

	fallback, _ := a.refreshFallbackStatus(ctx)
	result.FallbackState = fallback.State
	maintenance, _ := a.getMaintenanceState(ctx)

	if maintenance.MaintenanceMode && !force {
		result.Skipped = true
		result.SkipReason = "maintenance mode is active"
		result.FinishedAt = nowRFC3339()
		return a.finishJanitorJob(context.Background(), jobID, result, "skipped")
	}
	if maintenance.JanitorPaused && !force {
		result.Skipped = true
		result.SkipReason = "janitor is paused"
		result.FinishedAt = nowRFC3339()
		return a.finishJanitorJob(context.Background(), jobID, result, "skipped")
	}
	if fallback.State == "mirror-degraded" && !force {
		result.Skipped = true
		result.SkipReason = "registry mirror is degraded"
		result.FinishedAt = nowRFC3339()
		return a.finishJanitorJob(context.Background(), jobID, result, "skipped")
	}

	diskBefore, err := getDiskStats(a.cfg.RegistryDataPath)
	if err != nil {
		return fail(fmt.Errorf("read disk stats failed: %w", err))
	}
	result.FreePctBefore = roundFloat(diskBefore.FreePercent)
	result.MustFree = diskBefore.FreePercent < float64(a.cfg.LowWatermarkPct)
	result.EmergencyMode = diskBefore.FreePercent < float64(a.cfg.EmergencyFreePct)
	if result.MustFree {
		result.RequiredBytes = bytesToTarget(diskBefore.TotalBytes, diskBefore.FreeBytes, a.cfg.TargetFreePct)
	}

	if fallback.State == "upstream-degraded" && !result.EmergencyMode && !force {
		result.Skipped = true
		result.SkipReason = "upstream is degraded and cleanup is paused to preserve cache"
		result.FinishedAt = nowRFC3339()
		return a.finishJanitorJob(context.Background(), jobID, result, "skipped")
	}

	candidates, err := a.listCandidates(ctx, result.EmergencyMode)
	if err != nil {
		return fail(err)
	}
	result.CandidateCount = len(candidates)

	recoveredEstimate := int64(0)
	for _, candidate := range candidates {
		if len(result.Results) >= a.cfg.MaxDeleteBatch {
			break
		}

		item := JanitorItemResult{
			Repo:      candidate.Repo,
			Tag:       candidate.Tag,
			Digest:    candidate.Digest,
			SizeBytes: candidate.SizeBytes,
		}

		if result.DryRun {
			item.Status = "planned"
			result.PlannedCount++
			recoveredEstimate += candidate.SizeBytes
			result.Results = append(result.Results, item)
			if result.MustFree && recoveredEstimate >= result.RequiredBytes {
				break
			}
			continue
		}

		ok, code, detail := a.deleteManifest(ctx, candidate.Repo, candidate.Digest)
		item.StatusCode = code
		if ok {
			if err := a.markDeleted(ctx, candidate.Repo, candidate.Digest, trigger); err != nil {
				item.Status = "error"
				item.Error = err.Error()
				result.ErrorCount++
			} else {
				item.Status = "deleted"
				result.DeletedCount++
				recoveredEstimate += candidate.SizeBytes
			}
		} else {
			item.Status = "error"
			item.Error = detail
			result.ErrorCount++
		}
		result.Results = append(result.Results, item)
		if result.MustFree && recoveredEstimate >= result.RequiredBytes {
			break
		}
	}

	result.EstimatedRecoveredBytes = recoveredEstimate
	if !result.DryRun && result.DeletedCount > 0 {
		if _, err := a.requestGC(trigger, false); err == nil {
			result.GCRequested = true
		}
	}

	if diskCurrent, err := getDiskStats(a.cfg.RegistryDataPath); err == nil {
		result.FreePctCurrent = roundFloat(diskCurrent.FreePercent)
	} else {
		result.FreePctCurrent = result.FreePctBefore
	}
	result.FinishedAt = nowRFC3339()

	status := "success"
	switch {
	case result.Skipped:
		status = "skipped"
	case result.DryRun:
		status = "dry-run"
	case result.ErrorCount > 0 && result.DeletedCount == 0:
		status = "error"
	case result.ErrorCount > 0:
		status = "partial"
	}

	details, _ := json.Marshal(result)
	if err := a.finishJobRun(context.Background(), jobID, status, result.FinishedAt, details); err != nil {
		return result, err
	}
	a.logSystem(context.Background(), "info", "janitor", "", "janitor completed", map[string]any{
		"trigger":                   trigger,
		"dry_run":                   result.DryRun,
		"forced":                    force,
		"deleted_count":             result.DeletedCount,
		"planned_count":             result.PlannedCount,
		"error_count":               result.ErrorCount,
		"estimated_recovered_bytes": result.EstimatedRecoveredBytes,
		"gc_requested":              result.GCRequested,
		"fallback_state":            result.FallbackState,
	})
	return result, nil
}

func (a *App) finishJanitorJob(ctx context.Context, jobID int64, result JanitorResult, status string) (JanitorResult, error) {
	details, _ := json.Marshal(result)
	if err := a.finishJobRun(ctx, jobID, status, result.FinishedAt, details); err != nil {
		return result, err
	}
	a.logSystem(ctx, "info", "janitor", "", "janitor skipped", map[string]any{
		"reason":         result.SkipReason,
		"fallback_state": result.FallbackState,
	})
	return result, nil
}

func (a *App) deleteManifest(ctx context.Context, repo, digest string) (bool, int, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("%s/v2/%s/manifests/%s", a.cfg.RegistryURL, repo, digest), nil)
	if err != nil {
		return false, 0, err.Error()
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return false, 0, err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	ok := resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNotFound
	return ok, resp.StatusCode, string(body)
}

func (a *App) markDeleted(ctx context.Context, repo, digest, reason string) error {
	_, err := a.db.ExecContext(ctx, `
		UPDATE artifacts
		SET deleted_at = ?, delete_reason = ?
		WHERE repo = ? AND digest = ?
	`, nowRFC3339(), reason, repo, digest)
	return err
}

func (a *App) runGC(ctx context.Context, trigger string, force bool) (GCResult, error) {
	request, err := a.requestGC(trigger, force)
	if err != nil {
		return GCResult{}, err
	}
	result := GCResult{
		Queued:        true,
		RequestedAt:   request.RequestedAt,
		TriggerSource: request.TriggerSource,
		Forced:        request.Force,
		GCPending:     true,
	}
	a.logSystem(ctx, "info", "gc", "", "gc requested", map[string]any{
		"trigger":      trigger,
		"force":        force,
		"requested_at": request.RequestedAt,
	})
	return result, nil
}

func (a *App) executeGC(ctx context.Context, trigger string, force bool) (GCResult, error) {
	if err := a.beginOperation("gc"); err != nil {
		return GCResult{}, err
	}
	defer a.endOperation("gc")
	_ = a.setGCActive(true)
	defer func() {
		_ = a.setGCActive(false)
	}()

	startedAt := nowRFC3339()
	jobID, err := a.createJobRun(ctx, "gc", trigger, startedAt)
	if err != nil {
		return GCResult{}, err
	}

	result := GCResult{
		TriggerSource: trigger,
		StartedAt:     startedAt,
		Forced:        force,
		GCPending:     a.gcRequested(),
	}

	fail := func(runErr error) (GCResult, error) {
		result.FinishedAt = nowRFC3339()
		if result.LogsTail == "" {
			result.LogsTail = runErr.Error()
		}
		details, _ := json.Marshal(result)
		_ = a.finishJobRun(context.Background(), jobID, "error", result.FinishedAt, details)
		a.logSystem(context.Background(), "error", "gc", "", "gc failed", map[string]any{
			"error":   runErr.Error(),
			"trigger": trigger,
		})
		return result, runErr
	}

	fallback, _ := a.refreshFallbackStatus(ctx)
	result.FallbackState = fallback.State
	maintenance, _ := a.getMaintenanceState(ctx)
	if maintenance.MaintenanceMode && !force {
		result.Skipped = true
		result.SkipReason = "maintenance mode is active"
		result.FinishedAt = nowRFC3339()
		return a.finishGCJob(context.Background(), jobID, result, "skipped")
	}
	if maintenance.GCPaused && !force {
		result.Skipped = true
		result.SkipReason = "gc is paused"
		result.FinishedAt = nowRFC3339()
		return a.finishGCJob(context.Background(), jobID, result, "skipped")
	}
	if fallback.State == "upstream-degraded" && !force {
		result.Skipped = true
		result.SkipReason = "upstream is degraded"
		result.FinishedAt = nowRFC3339()
		return a.finishGCJob(context.Background(), jobID, result, "skipped")
	}
	if !result.GCPending && !force {
		result.Skipped = true
		result.SkipReason = "no gc request is pending"
		result.FinishedAt = nowRFC3339()
		return a.finishGCJob(context.Background(), jobID, result, "skipped")
	}

	cmd := exec.CommandContext(ctx, a.cfg.RegistryBinaryPath, "garbage-collect", a.cfg.RegistryConfigPath)
	output, execErr := cmd.CombinedOutput()
	result.RegistryImage = "local-registry-binary"
	result.LogsTail = trimTail(string(output), maxLogTailBytes)
	result.FinishedAt = nowRFC3339()
	if execErr != nil {
		if exitErr, ok := execErr.(*exec.ExitError); ok {
			result.StatusCode = exitErr.ExitCode()
		} else {
			result.StatusCode = 1
		}
		return fail(fmt.Errorf("registry garbage-collect failed: %w", execErr))
	}

	result.StatusCode = 0
	if err := a.clearGCRequest(); err == nil {
		result.GCPending = false
		result.GCFlagCleared = true
	}

	details, _ := json.Marshal(result)
	if err := a.finishJobRun(context.Background(), jobID, "success", result.FinishedAt, details); err != nil {
		return result, err
	}
	a.logSystem(context.Background(), "info", "gc", "", "gc completed", map[string]any{
		"trigger":         trigger,
		"forced":          force,
		"status_code":     result.StatusCode,
		"gc_flag_cleared": result.GCFlagCleared,
	})
	return result, nil
}

func (a *App) finishGCJob(ctx context.Context, jobID int64, result GCResult, status string) (GCResult, error) {
	details, _ := json.Marshal(result)
	if err := a.finishJobRun(ctx, jobID, status, result.FinishedAt, details); err != nil {
		return result, err
	}
	a.logSystem(ctx, "info", "gc", "", "gc skipped", map[string]any{
		"reason":         result.SkipReason,
		"fallback_state": result.FallbackState,
	})
	return result, nil
}

func (a *App) janitorLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.JanitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := a.runJanitor(context.Background(), "scheduled", false, false); err != nil && !errors.Is(err, errOperationBusy) {
				a.logger.Error("scheduled janitor failed", "error", err)
			}
		}
	}
}

func (a *App) gcWorkerLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.GCWorkerPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			request, ok, err := a.readGCRequest()
			if err != nil {
				a.logger.Error("gc request read failed", "error", err)
				continue
			}
			if ok {
				if _, err := a.executeGC(context.Background(), request.TriggerSource, request.Force); err != nil && !errors.Is(err, errOperationBusy) {
					a.logger.Error("requested gc failed", "error", err)
				}
				continue
			}

			now := time.Now().UTC()
			if !a.gcRequested() || now.Hour() < a.cfg.GCHourUTC {
				continue
			}
			alreadyRun, err := a.hasJobRunToday(context.Background(), "gc", "scheduled")
			if err != nil || alreadyRun {
				continue
			}
			if _, err := a.executeGC(context.Background(), "scheduled", false); err != nil && !errors.Is(err, errOperationBusy) {
				a.logger.Error("scheduled gc failed", "error", err)
			}
		}
	}
}

func (a *App) healthLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.HealthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = a.refreshFallbackStatus(context.Background())
		}
	}
}

func (a *App) housekeepingLoop(ctx context.Context) {
	ticker := time.NewTicker(6 * hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.pruneExpiredSessions(context.Background()); err != nil {
				a.logger.Error("session prune failed", "error", err)
			}
			if err := a.pruneMetadata(context.Background()); err != nil {
				a.logger.Error("metadata prune failed", "error", err)
			}
		}
	}
}

func (a *App) requestGC(trigger string, force bool) (GCRequest, error) {
	request := GCRequest{
		TriggerSource: firstNonEmpty(trigger, "manual"),
		RequestedAt:   nowRFC3339(),
		Force:         force,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return GCRequest{}, err
	}
	return request, os.WriteFile(a.cfg.GCRequestFlag, payload, 0o600)
}

func (a *App) readGCRequest() (GCRequest, bool, error) {
	data, err := os.ReadFile(a.cfg.GCRequestFlag)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return GCRequest{}, false, nil
		}
		return GCRequest{}, false, err
	}
	if len(data) == 0 {
		return GCRequest{TriggerSource: "manual", RequestedAt: nowRFC3339()}, true, nil
	}

	var request GCRequest
	if err := json.Unmarshal(data, &request); err != nil {
		request = GCRequest{
			TriggerSource: "legacy",
			RequestedAt:   strings.TrimSpace(string(data)),
		}
	}
	if request.TriggerSource == "" {
		request.TriggerSource = "manual"
	}
	if request.RequestedAt == "" {
		request.RequestedAt = nowRFC3339()
	}
	return request, true, nil
}

func (a *App) gcRequested() bool {
	_, err := os.Stat(a.cfg.GCRequestFlag)
	return err == nil
}

func (a *App) clearGCRequest() error {
	if err := os.Remove(a.cfg.GCRequestFlag); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (a *App) setGCActive(active bool) error {
	if active {
		return os.WriteFile(a.cfg.GCActiveFlag, []byte(nowRFC3339()), 0o600)
	}
	if err := os.Remove(a.cfg.GCActiveFlag); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (a *App) gcActive() bool {
	_, err := os.Stat(a.cfg.GCActiveFlag)
	return err == nil
}

func (a *App) beginOperation(kind string) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	if a.janitorRunning || a.gcRunning {
		return errOperationBusy
	}
	switch kind {
	case "janitor":
		a.janitorRunning = true
	case "gc":
		a.gcRunning = true
	}
	return nil
}

func (a *App) endOperation(kind string) {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	switch kind {
	case "janitor":
		a.janitorRunning = false
	case "gc":
		a.gcRunning = false
	}
}

func (a *App) operationSnapshot() map[string]bool {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	return map[string]bool{
		"janitor_running": a.janitorRunning,
		"gc_running":      a.gcRunning || a.gcActive(),
	}
}

func bytesToTarget(totalBytes, freeBytes uint64, targetFreePct int) int64 {
	targetFreeBytes := uint64(float64(totalBytes) * (float64(targetFreePct) / 100.0))
	if targetFreeBytes <= freeBytes {
		return 0
	}
	return int64(targetFreeBytes - freeBytes)
}
