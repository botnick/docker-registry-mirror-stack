package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const catalogSyncInterval = 10 * minute

type catalogPage struct {
	Repositories []string `json:"repositories"`
}

type repositoryTags struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

type manifestMetadata struct {
	Digest    string
	MediaType string
	SizeBytes int64
}

func (a *App) catalogSyncLoop(ctx context.Context) {
	a.runCatalogSync(context.Background(), "startup")

	ticker := time.NewTicker(catalogSyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.runCatalogSync(context.Background(), "scheduled")
		}
	}
}

func (a *App) runCatalogSync(ctx context.Context, trigger string) map[string]any {
	targetSummaries := make([]map[string]any, 0, len(a.cfg.Upstreams))
	totalRepositories := 0
	totalArtifactsSeen := 0
	totalErrors := 0

	for _, target := range a.cfg.Upstreams {
		imported, repos, errs := a.syncTargetCatalog(ctx, target)
		targetSummaries = append(targetSummaries, map[string]any{
			"host":           target.Host,
			"display_name":   target.DisplayName,
			"repositories":   repos,
			"artifacts_seen": imported,
			"errors":         errs,
		})
		totalRepositories += repos
		totalArtifactsSeen += imported
		totalErrors += len(errs)
	}

	summary := map[string]any{
		"trigger":         trigger,
		"targets":         targetSummaries,
		"repositories":    totalRepositories,
		"artifacts_seen":  totalArtifactsSeen,
		"error_count":     totalErrors,
		"completed_at":    nowRFC3339(),
		"sync_interval_s": int(catalogSyncInterval.Seconds()),
	}
	a.logSystem(ctx, "info", "catalog", "", "catalog sync completed", summary)
	return summary
}

func (a *App) syncTargetCatalog(ctx context.Context, target UpstreamTarget) (int, int, []string) {
	repos, err := a.listCatalogRepositories(ctx, target)
	if err != nil {
		return 0, 0, []string{err.Error()}
	}

	imported := 0
	var issues []string
	for _, repo := range repos {
		tags, err := a.listRepositoryTags(ctx, target, repo)
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s: %v", repo, err))
			continue
		}
		if len(tags) == 0 {
			continue
		}

		tx, err := a.db.BeginTx(ctx, nil)
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s: begin tx failed: %v", repo, err))
			continue
		}

		now := nowRFC3339()
		repoImported := 0
		repoErr := false
		canonicalRepo := a.cfg.CanonicalRepo(target.Host, repo)
		for _, tag := range tags {
			meta, err := a.fetchManifestMetadata(ctx, target, repo, tag)
			if err != nil {
				issues = append(issues, fmt.Sprintf("%s:%s: %v", canonicalRepo, tag, err))
				repoErr = true
				break
			}
			if meta.Digest == "" {
				continue
			}
			if err := a.upsertCatalogArtifact(ctx, tx, canonicalRepo, tag, meta, now); err != nil {
				issues = append(issues, fmt.Sprintf("%s:%s: %v", canonicalRepo, tag, err))
				repoErr = true
				break
			}
			repoImported++
		}

		if repoErr {
			_ = tx.Rollback()
			continue
		}
		if err := tx.Commit(); err != nil {
			issues = append(issues, fmt.Sprintf("%s: commit failed: %v", canonicalRepo, err))
			continue
		}
		imported += repoImported
	}

	return imported, len(repos), issues
}

func (a *App) listCatalogRepositories(ctx context.Context, target UpstreamTarget) ([]string, error) {
	var repositories []string
	nextURL := target.BackendURL + "/v2/_catalog?n=100"
	for nextURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := a.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("catalog status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		var page catalogPage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		repositories = append(repositories, page.Repositories...)
		nextURL = parseNextLink(resp.Header.Get("Link"), target.BackendURL)
	}
	return repositories, nil
}

func (a *App) listRepositoryTags(ctx context.Context, target UpstreamTarget, repo string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/v2/%s/tags/list", target.BackendURL, repo), nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tags status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload repositoryTags
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload.Tags, nil
}

func (a *App) fetchManifestMetadata(ctx context.Context, target UpstreamTarget, repo, reference string) (manifestMetadata, error) {
	requestMeta := func(method string) (manifestMetadata, int, error) {
		req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("%s/v2/%s/manifests/%s", target.BackendURL, repo, reference), nil)
		if err != nil {
			return manifestMetadata{}, 0, err
		}
		req.Header.Set("Accept", strings.Join([]string{
			"application/vnd.oci.image.index.v1+json",
			"application/vnd.oci.image.manifest.v1+json",
			"application/vnd.docker.distribution.manifest.list.v2+json",
			"application/vnd.docker.distribution.manifest.v2+json",
			"application/vnd.docker.distribution.manifest.v1+json",
		}, ", "))

		resp, err := a.httpClient.Do(req)
		if err != nil {
			return manifestMetadata{}, 0, err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			return manifestMetadata{}, resp.StatusCode, fmt.Errorf("manifest status %d", resp.StatusCode)
		}

		sizeBytes := int64(0)
		if contentLength := strings.TrimSpace(resp.Header.Get("Content-Length")); contentLength != "" {
			if parsed, err := strconv.ParseInt(contentLength, 10, 64); err == nil {
				sizeBytes = parsed
			}
		}
		return manifestMetadata{
			Digest:    strings.TrimSpace(resp.Header.Get("Docker-Content-Digest")),
			MediaType: strings.TrimSpace(resp.Header.Get("Content-Type")),
			SizeBytes: sizeBytes,
		}, resp.StatusCode, nil
	}

	meta, _, err := requestMeta(http.MethodHead)
	if err == nil && meta.Digest != "" {
		return meta, nil
	}
	meta, _, err = requestMeta(http.MethodGet)
	if err != nil {
		return manifestMetadata{}, err
	}
	if meta.Digest == "" {
		return manifestMetadata{}, fmt.Errorf("manifest digest is missing")
	}
	return meta, nil
}

func (a *App) upsertCatalogArtifact(ctx context.Context, tx *sql.Tx, repo, tag string, meta manifestMetadata, now string) error {
	firstSeenAt := now
	lastUsedAt := now
	useCount := int64(0)
	pinned := 0
	explicitProtected := 0

	row := tx.QueryRowContext(ctx, `
		SELECT first_seen_at, last_used_at, use_count, pinned, explicit_protected
		FROM artifacts
		WHERE repo = ? AND digest = ?
	`, repo, meta.Digest)
	switch err := row.Scan(&firstSeenAt, &lastUsedAt, &useCount, &pinned, &explicitProtected); err {
	case nil:
	case sql.ErrNoRows:
	default:
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifacts (repo, tag, digest, media_type, size_bytes, first_seen_at, last_used_at, use_count, pinned, explicit_protected, deleted_at, delete_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL)
		ON CONFLICT(repo, digest) DO UPDATE SET
			tag = COALESCE(excluded.tag, artifacts.tag),
			media_type = COALESCE(excluded.media_type, artifacts.media_type),
			size_bytes = CASE
				WHEN excluded.size_bytes IS NULL OR excluded.size_bytes = 0 THEN artifacts.size_bytes
				ELSE excluded.size_bytes
			END,
			deleted_at = NULL,
			delete_reason = NULL
	`, repo, nullString(tag), meta.Digest, nullString(meta.MediaType), nullableInt64(meta.SizeBytes), firstSeenAt, lastUsedAt, useCount, pinned, explicitProtected); err != nil {
		return err
	}

	if tag != "" {
		if err := a.upsertArtifactTag(ctx, tx, repo, meta.Digest, tag, now); err != nil {
			return err
		}
	}
	return nil
}

func parseNextLink(linkHeader, baseURL string) string {
	linkHeader = strings.TrimSpace(linkHeader)
	if linkHeader == "" {
		return ""
	}
	start := strings.Index(linkHeader, "<")
	end := strings.Index(linkHeader, ">")
	if start == -1 || end == -1 || end <= start+1 {
		return ""
	}
	next := linkHeader[start+1 : end]
	if strings.HasPrefix(next, "http://") || strings.HasPrefix(next, "https://") {
		return next
	}
	return strings.TrimRight(baseURL, "/") + next
}

func nullableInt64(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}
