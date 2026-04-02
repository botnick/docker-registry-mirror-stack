package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
)

func (a *App) collectUpstreamStatuses(ctx context.Context) []UpstreamStatus {
	statuses := make([]UpstreamStatus, 0, len(a.cfg.Upstreams))
	for _, target := range a.cfg.Upstreams {
		registryProbe := a.probeHTTP(ctx, "registry:"+target.Host, target.BackendURL+"/v2/", a.cfg.UpstreamTimeout)
		upstreamProbe := a.probeHTTP(ctx, "upstream:"+target.Host, target.UpstreamHealthURL, a.cfg.UpstreamTimeout)
		storage := map[string]any{
			"path":               target.RegistryDataPath,
			"gc_request_pending": a.gcRequestedForTarget(target),
		}
		if diskStats, err := getDiskStats(target.RegistryDataPath); err != nil {
			storage["disk_error"] = err.Error()
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

		healthy := registryProbe.Healthy && upstreamProbe.Healthy
		summary := "ready"
		switch {
		case !registryProbe.Healthy:
			summary = "registry cache is unhealthy"
		case !upstreamProbe.Healthy:
			summary = "upstream registry is degraded"
		case storageBool(storage, "emergency"):
			summary = "storage is critically low"
		case storageBool(storage, "pressure"):
			summary = "storage is below the low watermark"
		}

		statuses = append(statuses, UpstreamStatus{
			Host:         target.Host,
			DisplayName:  target.DisplayName,
			Default:      target.Default,
			Registry:     registryProbe,
			Upstream:     upstreamProbe,
			Storage:      storage,
			GCPending:    a.gcRequestedForTarget(target),
			GCActive:     a.gcActiveForTarget(target),
			Healthy:      healthy,
			Summary:      summary,
			CanonicalRef: target.Host + "/example/repo:tag",
		})
	}
	return statuses
}

func aggregateStorage(statuses []UpstreamStatus, lowWatermarkPct, emergencyPct, targetFreePct int) map[string]any {
	storage := map[string]any{
		"path":    ternaryString(len(statuses) > 1, "multiple upstream caches", "-"),
		"targets": len(statuses),
	}
	var totalBytes uint64
	var usedBytes uint64
	var freeBytes uint64
	var pressure bool
	var emergency bool
	var pending bool

	for _, status := range statuses {
		if value, ok := status.Storage["total_bytes"].(uint64); ok {
			totalBytes += value
		}
		if value, ok := status.Storage["used_bytes"].(uint64); ok {
			usedBytes += value
		}
		if value, ok := status.Storage["free_bytes"].(uint64); ok {
			freeBytes += value
		}
		if storageBool(status.Storage, "pressure") {
			pressure = true
		}
		if storageBool(status.Storage, "emergency") {
			emergency = true
		}
		if status.GCPending {
			pending = true
		}
	}

	if totalBytes > 0 {
		storage["total_bytes"] = totalBytes
		storage["used_bytes"] = usedBytes
		storage["free_bytes"] = freeBytes
		storage["used_pct"] = roundFloat((float64(usedBytes) / float64(totalBytes)) * 100)
		storage["free_pct"] = roundFloat((float64(freeBytes) / float64(totalBytes)) * 100)
		storage["bytes_to_target"] = bytesToTarget(totalBytes, freeBytes, targetFreePct)
	} else {
		storage["total_bytes"] = uint64(0)
		storage["used_bytes"] = uint64(0)
		storage["free_bytes"] = uint64(0)
		storage["used_pct"] = float64(0)
		storage["free_pct"] = float64(0)
		storage["bytes_to_target"] = int64(0)
	}
	storage["pressure"] = pressure || valueAsFloat(storage["free_pct"]) < float64(lowWatermarkPct)
	storage["emergency"] = emergency || valueAsFloat(storage["free_pct"]) < float64(emergencyPct)
	storage["gc_request_pending"] = pending
	return storage
}

func valueAsFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int64:
		return float64(typed)
	case uint64:
		return float64(typed)
	default:
		return 0
	}
}

func (a *App) requestGCForTarget(target UpstreamTarget, trigger string, force bool) (GCRequest, error) {
	request := GCRequest{
		TriggerSource: firstNonEmpty(trigger, "manual"),
		RequestedAt:   nowRFC3339(),
		Force:         force,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return GCRequest{}, err
	}
	return request, os.WriteFile(target.GCRequestFlag, payload, 0o600)
}

func (a *App) readGCRequestForTarget(target UpstreamTarget) (GCRequest, bool, error) {
	data, err := os.ReadFile(target.GCRequestFlag)
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

func (a *App) gcRequestedForTarget(target UpstreamTarget) bool {
	_, err := os.Stat(target.GCRequestFlag)
	return err == nil
}

func (a *App) gcRequested() bool {
	for _, target := range a.cfg.Upstreams {
		if a.gcRequestedForTarget(target) {
			return true
		}
	}
	return false
}

func (a *App) clearGCRequestForTarget(target UpstreamTarget) error {
	if err := os.Remove(target.GCRequestFlag); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (a *App) clearGCRequest() error {
	for _, target := range a.cfg.Upstreams {
		if err := a.clearGCRequestForTarget(target); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) setGCActiveForTarget(target UpstreamTarget, active bool) error {
	if active {
		return os.WriteFile(target.GCActiveFlag, []byte(nowRFC3339()), 0o600)
	}
	if err := os.Remove(target.GCActiveFlag); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (a *App) gcActiveForTarget(target UpstreamTarget) bool {
	_, err := os.Stat(target.GCActiveFlag)
	return err == nil
}

func (a *App) gcActive() bool {
	for _, target := range a.cfg.Upstreams {
		if a.gcActiveForTarget(target) {
			return true
		}
	}
	return false
}

func firstUnhealthyRegistry(statuses []UpstreamStatus) UpstreamStatus {
	for _, status := range statuses {
		if !status.Registry.Healthy {
			return status
		}
	}
	return UpstreamStatus{}
}

func firstUnhealthyUpstream(statuses []UpstreamStatus) UpstreamStatus {
	for _, status := range statuses {
		if !status.Upstream.Healthy {
			return status
		}
	}
	return UpstreamStatus{}
}
