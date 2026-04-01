package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type rowScanner interface {
	Scan(dest ...any) error
}

func (a *App) setSettingIfMissing(ctx context.Context, key, value string) error {
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO NOTHING
	`, key, value, nowRFC3339())
	return err
}

func (a *App) setSetting(ctx context.Context, key, value string) error {
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at
	`, key, value, nowRFC3339())
	return err
}

func (a *App) getSetting(ctx context.Context, key, fallback string) (string, error) {
	var value string
	err := a.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return fallback, nil
	}
	return value, err
}

func (a *App) getMaintenanceState(ctx context.Context) (MaintenanceState, error) {
	modeValue, err := a.getSetting(ctx, "maintenance_mode", "false")
	if err != nil {
		return MaintenanceState{}, err
	}
	janitorValue, err := a.getSetting(ctx, "janitor_paused", "false")
	if err != nil {
		return MaintenanceState{}, err
	}
	gcValue, err := a.getSetting(ctx, "gc_paused", "false")
	if err != nil {
		return MaintenanceState{}, err
	}
	noteValue, err := a.getSetting(ctx, "maintenance_note", "")
	if err != nil {
		return MaintenanceState{}, err
	}

	var updatedAt sql.NullString
	if err := a.db.QueryRowContext(ctx, `
		SELECT MAX(updated_at)
		FROM settings
		WHERE key IN ('maintenance_mode', 'janitor_paused', 'gc_paused', 'maintenance_note')
	`).Scan(&updatedAt); err != nil {
		return MaintenanceState{}, err
	}

	return MaintenanceState{
		MaintenanceMode: parseBool(modeValue),
		JanitorPaused:   parseBool(janitorValue),
		GCPaused:        parseBool(gcValue),
		Note:            noteValue,
		UpdatedAt:       toPointer(updatedAt),
	}, nil
}

func (a *App) updateMaintenanceState(ctx context.Context, state MaintenanceState) error {
	if err := a.setSetting(ctx, "maintenance_mode", fmt.Sprintf("%t", state.MaintenanceMode)); err != nil {
		return err
	}
	if err := a.setSetting(ctx, "janitor_paused", fmt.Sprintf("%t", state.JanitorPaused)); err != nil {
		return err
	}
	if err := a.setSetting(ctx, "gc_paused", fmt.Sprintf("%t", state.GCPaused)); err != nil {
		return err
	}
	if err := a.setSetting(ctx, "maintenance_note", state.Note); err != nil {
		return err
	}
	return nil
}

func (a *App) findUserByUsername(ctx context.Context, username string) (User, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, must_change_password, failed_attempts, locked_until, last_login_at, last_password_change_at, created_at, updated_at
		FROM users
		WHERE username = ?
	`, strings.TrimSpace(username))
	return scanUser(row)
}

func (a *App) findUserByID(ctx context.Context, userID int64) (User, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, must_change_password, failed_attempts, locked_until, last_login_at, last_password_change_at, created_at, updated_at
		FROM users
		WHERE id = ?
	`, userID)
	return scanUser(row)
}

func scanUser(row rowScanner) (User, error) {
	var user User
	var mustChange int
	var lockedUntil sql.NullString
	var lastLoginAt sql.NullString
	var lastPasswordChangeAt sql.NullString
	if err := row.Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&mustChange,
		&user.FailedAttempts,
		&lockedUntil,
		&lastLoginAt,
		&lastPasswordChangeAt,
		&user.CreatedAt,
		&user.UpdatedAt,
	); err != nil {
		return User{}, err
	}
	user.MustChangePassword = mustChange == 1
	user.LockedUntil = toPointer(lockedUntil)
	user.LastLoginAt = toPointer(lastLoginAt)
	user.LastPasswordChangeAt = toPointer(lastPasswordChangeAt)
	return user, nil
}

func (a *App) recordFailedLogin(ctx context.Context, user User) error {
	now := time.Now().UTC()
	nextAttempts := user.FailedAttempts + 1
	var lockUntil *string
	if nextAttempts >= a.cfg.LoginMaxAttempts {
		lockUntilValue := now.Add(a.cfg.LoginLockDuration).Format(time.RFC3339)
		lockUntil = &lockUntilValue
		nextAttempts = 0
	}
	_, err := a.db.ExecContext(ctx, `
		UPDATE users
		SET failed_attempts = ?, locked_until = ?, updated_at = ?
		WHERE id = ?
	`, nextAttempts, mustRFC3339OrZero(lockUntil), now.Format(time.RFC3339), user.ID)
	return err
}

func (a *App) recordSuccessfulLogin(ctx context.Context, userID int64) error {
	now := nowRFC3339()
	_, err := a.db.ExecContext(ctx, `
		UPDATE users
		SET failed_attempts = 0, locked_until = NULL, last_login_at = ?, updated_at = ?
		WHERE id = ?
	`, now, now, userID)
	return err
}

func (a *App) updateUserPassword(ctx context.Context, userID int64, passwordHash string) error {
	now := nowRFC3339()
	_, err := a.db.ExecContext(ctx, `
		UPDATE users
		SET password_hash = ?, must_change_password = 0, failed_attempts = 0, locked_until = NULL, updated_at = ?, last_password_change_at = ?
		WHERE id = ?
	`, passwordHash, now, now, userID)
	return err
}

func (a *App) createSession(ctx context.Context, userID int64, remoteAddr, userAgent string) (string, Session, error) {
	token, err := tokenString(32)
	if err != nil {
		return "", Session{}, err
	}
	csrfToken, err := tokenString(24)
	if err != nil {
		return "", Session{}, err
	}

	now := time.Now().UTC()
	expiresAt := now.Add(a.cfg.SessionTTL)
	res, err := a.db.ExecContext(ctx, `
		INSERT INTO sessions (user_id, token_hash, csrf_token, created_at, expires_at, last_seen_at, remote_addr, user_agent)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, sha256Hex(token), csrfToken, now.Format(time.RFC3339), expiresAt.Format(time.RFC3339), now.Format(time.RFC3339), remoteAddr, userAgent)
	if err != nil {
		return "", Session{}, err
	}
	sessionID, _ := res.LastInsertId()
	return token, Session{
		ID:         sessionID,
		UserID:     userID,
		TokenHash:  sha256Hex(token),
		CSRFToken:  csrfToken,
		CreatedAt:  now.Format(time.RFC3339),
		ExpiresAt:  expiresAt.Format(time.RFC3339),
		LastSeenAt: now.Format(time.RFC3339),
		RemoteAddr: nullableString(remoteAddr),
		UserAgent:  nullableString(userAgent),
	}, nil
}

func (a *App) findAuthSession(ctx context.Context, sessionToken string) (AuthSession, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT
			s.id, s.user_id, s.token_hash, s.csrf_token, s.created_at, s.expires_at, s.last_seen_at, s.remote_addr, s.user_agent, s.revoked_at,
			u.id, u.username, u.password_hash, u.must_change_password, u.failed_attempts, u.locked_until, u.last_login_at, u.last_password_change_at, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ?
		  AND s.revoked_at IS NULL
		  AND s.expires_at > ?
	`, sha256Hex(sessionToken), nowRFC3339())

	var auth AuthSession
	var remoteAddr sql.NullString
	var userAgent sql.NullString
	var revokedAt sql.NullString
	var mustChange int
	var lockedUntil sql.NullString
	var lastLoginAt sql.NullString
	var lastPasswordChangeAt sql.NullString
	if err := row.Scan(
		&auth.Session.ID,
		&auth.Session.UserID,
		&auth.Session.TokenHash,
		&auth.Session.CSRFToken,
		&auth.Session.CreatedAt,
		&auth.Session.ExpiresAt,
		&auth.Session.LastSeenAt,
		&remoteAddr,
		&userAgent,
		&revokedAt,
		&auth.User.ID,
		&auth.User.Username,
		&auth.User.PasswordHash,
		&mustChange,
		&auth.User.FailedAttempts,
		&lockedUntil,
		&lastLoginAt,
		&lastPasswordChangeAt,
		&auth.User.CreatedAt,
		&auth.User.UpdatedAt,
	); err != nil {
		return AuthSession{}, err
	}
	auth.Session.RemoteAddr = toPointer(remoteAddr)
	auth.Session.UserAgent = toPointer(userAgent)
	auth.Session.RevokedAt = toPointer(revokedAt)
	auth.User.MustChangePassword = mustChange == 1
	auth.User.LockedUntil = toPointer(lockedUntil)
	auth.User.LastLoginAt = toPointer(lastLoginAt)
	auth.User.LastPasswordChangeAt = toPointer(lastPasswordChangeAt)
	return auth, nil
}

func (a *App) findSessionByID(ctx context.Context, sessionID int64) (Session, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, csrf_token, created_at, expires_at, last_seen_at, remote_addr, user_agent, revoked_at
		FROM sessions
		WHERE id = ?
	`, sessionID)
	return scanSession(row)
}

func scanSession(row rowScanner) (Session, error) {
	var session Session
	var remoteAddr sql.NullString
	var userAgent sql.NullString
	var revokedAt sql.NullString
	if err := row.Scan(
		&session.ID,
		&session.UserID,
		&session.TokenHash,
		&session.CSRFToken,
		&session.CreatedAt,
		&session.ExpiresAt,
		&session.LastSeenAt,
		&remoteAddr,
		&userAgent,
		&revokedAt,
	); err != nil {
		return Session{}, err
	}
	session.RemoteAddr = toPointer(remoteAddr)
	session.UserAgent = toPointer(userAgent)
	session.RevokedAt = toPointer(revokedAt)
	return session, nil
}

func (a *App) touchSession(ctx context.Context, sessionID int64) error {
	session, err := a.findSessionByID(ctx, sessionID)
	if err != nil {
		return err
	}
	lastSeenAt, err := parseRFC3339(session.LastSeenAt)
	if err != nil {
		return nil
	}
	if time.Since(lastSeenAt) < a.cfg.SessionRefreshInterval {
		return nil
	}

	now := time.Now().UTC()
	_, err = a.db.ExecContext(ctx, `
		UPDATE sessions
		SET last_seen_at = ?, expires_at = ?
		WHERE id = ? AND revoked_at IS NULL
	`, now.Format(time.RFC3339), now.Add(a.cfg.SessionTTL).Format(time.RFC3339), sessionID)
	return err
}

func (a *App) revokeSessionByID(ctx context.Context, sessionID int64) error {
	_, err := a.db.ExecContext(ctx, "UPDATE sessions SET revoked_at = ? WHERE id = ?", nowRFC3339(), sessionID)
	return err
}

func (a *App) revokeAllSessionsForUser(ctx context.Context, userID int64) error {
	_, err := a.db.ExecContext(ctx, "UPDATE sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL", nowRFC3339(), userID)
	return err
}

func (a *App) pruneExpiredSessions(ctx context.Context) error {
	_, err := a.db.ExecContext(ctx, "DELETE FROM sessions WHERE revoked_at IS NOT NULL OR expires_at < ?", nowRFC3339())
	return err
}

func (a *App) recordEvent(ctx context.Context, raw json.RawMessage) error {
	var event NotificationEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return fmt.Errorf("decode event: %w", err)
	}

	repo := firstNonEmpty(event.Target.Repository, event.Repository)
	tag := strings.TrimSpace(event.Target.Tag)
	digest := strings.TrimSpace(event.Target.Digest)
	action := strings.TrimSpace(event.Action)
	mediaType := strings.TrimSpace(event.Target.MediaType)
	now := nowRFC3339()

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO events (received_at, action, repo, tag, digest, raw_json)
		VALUES (?, ?, ?, ?, ?, ?)
	`, now, nullString(action), nullString(repo), nullString(tag), nullString(digest), string(raw)); err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	if repo != "" && digest != "" {
		firstSeenAt := now
		pinned := 0
		explicitProtected := 0
		useCount := int64(0)
		row := tx.QueryRowContext(ctx, "SELECT pinned, explicit_protected, first_seen_at, use_count FROM artifacts WHERE repo = ? AND digest = ?", repo, digest)
		switch err := row.Scan(&pinned, &explicitProtected, &firstSeenAt, &useCount); err {
		case nil:
		case sql.ErrNoRows:
			firstSeenAt = now
			pinned = 0
			explicitProtected = 0
			useCount = 0
		default:
			return fmt.Errorf("lookup artifact: %w", err)
		}

		var sizeValue any
		if event.Target.Length > 0 {
			sizeValue = event.Target.Length
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO artifacts (repo, tag, digest, media_type, size_bytes, first_seen_at, last_used_at, use_count, pinned, explicit_protected, deleted_at, delete_reason)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL)
			ON CONFLICT(repo, digest) DO UPDATE SET
				tag = COALESCE(excluded.tag, artifacts.tag),
				media_type = COALESCE(excluded.media_type, artifacts.media_type),
				size_bytes = COALESCE(excluded.size_bytes, artifacts.size_bytes),
				last_used_at = excluded.last_used_at,
				use_count = artifacts.use_count + 1,
				deleted_at = NULL,
				delete_reason = NULL
		`, repo, nullString(tag), digest, nullString(mediaType), sizeValue, firstSeenAt, now, useCount+1, pinned, explicitProtected); err != nil {
			return fmt.Errorf("upsert artifact: %w", err)
		}

		if tag != "" {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO artifact_tags (repo, digest, tag, first_seen_at, last_seen_at)
				VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(repo, digest, tag) DO UPDATE SET
					last_seen_at = excluded.last_seen_at
			`, repo, digest, tag, now, now); err != nil {
				return fmt.Errorf("upsert artifact tag: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit event: %w", err)
	}

	a.logSystem(ctx, "info", "webhook", "", "notification stored", map[string]any{
		"repo":   repo,
		"tag":    tag,
		"digest": digest,
		"action": action,
	})
	return nil
}

func (a *App) queryArtifacts(ctx context.Context, search, state string, pinnedFilter, protectedFilter *bool, limit, offset int) ([]Artifact, int, error) {
	var where []string
	var args []any

	switch state {
	case "", "all":
	case "active":
		where = append(where, "deleted_at IS NULL")
	case "deleted":
		where = append(where, "deleted_at IS NOT NULL")
	default:
		return nil, 0, fmt.Errorf("state ต้องเป็น all, active หรือ deleted")
	}

	if pinnedFilter != nil {
		where = append(where, "pinned = ?")
		args = append(args, boolToInt(*pinnedFilter))
	}
	if protectedFilter != nil {
		where = append(where, "explicit_protected = ?")
		args = append(args, boolToInt(*protectedFilter))
	}
	if search != "" {
		pattern := "%" + search + "%"
		where = append(where, "(repo LIKE ? OR COALESCE(tag, '') LIKE ? OR digest LIKE ?)")
		args = append(args, pattern, pattern, pattern)
	}

	query := `
		SELECT repo, tag, digest, media_type, COALESCE(size_bytes, 0), first_seen_at, last_used_at, COALESCE(use_count, 0), pinned, explicit_protected, deleted_at, delete_reason
		FROM artifacts
	`
	countQuery := `SELECT COUNT(*) FROM artifacts`
	if len(where) > 0 {
		clause := " WHERE " + strings.Join(where, " AND ")
		query += clause
		countQuery += clause
	}
	query += " ORDER BY last_used_at DESC LIMIT ? OFFSET ?"

	countArgs := append([]any(nil), args...)
	args = append(args, limit, offset)
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []Artifact
	for rows.Next() {
		item, err := a.scanArtifact(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, a.decorateArtifact(item, false))
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	total, err := a.countDynamicQuery(ctx, countQuery, countArgs...)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (a *App) queryArtifactDetail(ctx context.Context, repo, digest string) (ArtifactDetail, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT repo, tag, digest, media_type, COALESCE(size_bytes, 0), first_seen_at, last_used_at, COALESCE(use_count, 0), pinned, explicit_protected, deleted_at, delete_reason
		FROM artifacts
		WHERE repo = ? AND digest = ?
	`, repo, digest)
	item, err := a.scanArtifact(row)
	if err != nil {
		return ArtifactDetail{}, err
	}
	item = a.decorateArtifact(item, false)

	tags, err := a.queryArtifactTags(ctx, repo, digest)
	if err != nil {
		return ArtifactDetail{}, err
	}
	recentEvents, err := a.queryArtifactEvents(ctx, repo, digest, 20)
	if err != nil {
		return ArtifactDetail{}, err
	}
	recentLogs, err := a.queryArtifactLogs(ctx, repo, digest, 20)
	if err != nil {
		return ArtifactDetail{}, err
	}

	return ArtifactDetail{
		Artifact:       item,
		Tags:           tags,
		RecentEvents:   recentEvents,
		RecentActivity: recentLogs,
	}, nil
}

func (a *App) queryArtifactTags(ctx context.Context, repo, digest string) ([]string, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT tag
		FROM artifact_tags
		WHERE repo = ? AND digest = ?
		ORDER BY last_seen_at DESC
	`, repo, digest)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (a *App) queryArtifactEvents(ctx context.Context, repo, digest string, limit int) ([]EventRecord, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT id, received_at, action, repo, tag, digest, raw_json
		FROM events
		WHERE repo = ? AND digest = ?
		ORDER BY received_at DESC, id DESC
		LIMIT ?
	`, repo, digest, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []EventRecord
	for rows.Next() {
		record, err := scanEvent(rows, true)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func (a *App) queryArtifactLogs(ctx context.Context, repo, digest string, limit int) ([]LogRecord, error) {
	pattern := "%" + repo + "%"
	digestPattern := "%" + digest + "%"
	rows, err := a.db.QueryContext(ctx, `
		SELECT id, created_at, level, scope, actor, message, details_json
		FROM system_logs
		WHERE details_json LIKE ?
		  AND details_json LIKE ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, pattern, digestPattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []LogRecord
	for rows.Next() {
		record, err := scanLog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func (a *App) listCandidates(ctx context.Context, emergencyMode bool) ([]Artifact, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -a.cfg.UnusedDays).Format(time.RFC3339)
	minAgeCutoff := time.Now().UTC().AddDate(0, 0, -a.cfg.MinCacheAgeDays).Format(time.RFC3339)
	query := `
		SELECT repo, tag, digest, media_type, COALESCE(size_bytes, 0), first_seen_at, last_used_at, COALESCE(use_count, 0), pinned, explicit_protected, deleted_at, delete_reason
		FROM artifacts
		WHERE deleted_at IS NULL
		  AND last_used_at < ?
	`
	args := []any{cutoff}
	if !emergencyMode {
		query += " AND first_seen_at < ?"
		args = append(args, minAgeCutoff)
	}
	query += " ORDER BY last_used_at ASC, use_count ASC, first_seen_at ASC"

	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Artifact
	for rows.Next() {
		item, err := a.scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		decorated := a.decorateArtifact(item, emergencyMode)
		if decorated.Candidate {
			items = append(items, decorated)
		}
	}
	return items, rows.Err()
}

func (a *App) decorateArtifact(item Artifact, emergencyMode bool) Artifact {
	item.RegexProtected = a.isProtected(item.Repo, valueOrEmpty(item.Tag))
	item.Protected = item.RegexProtected || item.ExplicitProtected

	var reasons []string
	if item.DeletedAt != nil {
		reasons = append(reasons, "ถูกลบเชิงตรรกะแล้ว")
	}
	if item.Pinned {
		reasons = append(reasons, "ถูกปักหมุดไว้")
	}
	if item.Protected {
		reasons = append(reasons, "ถูกป้องกันจากการลบ")
	}

	firstSeenAt, _ := parseRFC3339(item.FirstSeenAt)
	lastUsedAt, _ := parseRFC3339(item.LastUsedAt)
	if !lastUsedAt.IsZero() && time.Since(lastUsedAt) < time.Duration(a.cfg.UnusedDays)*24*time.Hour {
		reasons = append(reasons, "ยังไม่เก่าพอสำหรับนโยบายลบ")
	}
	if !emergencyMode && !firstSeenAt.IsZero() && time.Since(firstSeenAt) < time.Duration(a.cfg.MinCacheAgeDays)*24*time.Hour {
		reasons = append(reasons, "อายุแคชยังไม่ถึงขั้นต่ำ")
	}

	item.BlockedReasons = reasons
	item.Candidate = len(reasons) == 0
	return item
}

func (a *App) scanArtifact(scanner rowScanner) (Artifact, error) {
	var item Artifact
	var tag sql.NullString
	var mediaType sql.NullString
	var deletedAt sql.NullString
	var deleteReason sql.NullString
	var pinned int
	var explicitProtected int
	if err := scanner.Scan(
		&item.Repo,
		&tag,
		&item.Digest,
		&mediaType,
		&item.SizeBytes,
		&item.FirstSeenAt,
		&item.LastUsedAt,
		&item.UseCount,
		&pinned,
		&explicitProtected,
		&deletedAt,
		&deleteReason,
	); err != nil {
		return Artifact{}, err
	}
	item.Tag = toPointer(tag)
	item.MediaType = toPointer(mediaType)
	item.Pinned = pinned == 1
	item.ExplicitProtected = explicitProtected == 1
	item.DeletedAt = toPointer(deletedAt)
	item.DeleteReason = toPointer(deleteReason)
	return item, nil
}

func (a *App) queryEvents(ctx context.Context, limit, offset int, includeRaw bool) ([]EventRecord, int, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT id, received_at, action, repo, tag, digest, raw_json
		FROM events
		ORDER BY received_at DESC, id DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []EventRecord
	for rows.Next() {
		record, err := scanEvent(rows, includeRaw)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, record)
	}

	total, err := a.countQuery(ctx, "SELECT COUNT(*) FROM events")
	if err != nil {
		return nil, 0, err
	}
	return items, total, rows.Err()
}

func scanEvent(scanner rowScanner, includeRaw bool) (EventRecord, error) {
	var item EventRecord
	var action sql.NullString
	var repo sql.NullString
	var tag sql.NullString
	var digest sql.NullString
	var raw string
	if err := scanner.Scan(&item.ID, &item.ReceivedAt, &action, &repo, &tag, &digest, &raw); err != nil {
		return EventRecord{}, err
	}
	item.Action = action.String
	item.Repo = toPointer(repo)
	item.Tag = toPointer(tag)
	item.Digest = toPointer(digest)
	if includeRaw {
		item.Raw = json.RawMessage(raw)
	}
	return item, nil
}

func (a *App) queryJobs(ctx context.Context, jobType, statusFilter string, limit, offset int) ([]JobRun, int, error) {
	var where []string
	var args []any
	if jobType != "" && jobType != "all" {
		where = append(where, "job_type = ?")
		args = append(args, jobType)
	}
	if statusFilter != "" && statusFilter != "all" {
		where = append(where, "status = ?")
		args = append(args, statusFilter)
	}

	query := `
		SELECT id, job_type, trigger_source, started_at, finished_at, status, details_json
		FROM job_runs
	`
	countQuery := `SELECT COUNT(*) FROM job_runs`
	if len(where) > 0 {
		clause := " WHERE " + strings.Join(where, " AND ")
		query += clause
		countQuery += clause
	}
	query += " ORDER BY started_at DESC, id DESC LIMIT ? OFFSET ?"

	countArgs := append([]any(nil), args...)
	args = append(args, limit, offset)
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []JobRun
	for rows.Next() {
		var run JobRun
		var finishedAt sql.NullString
		var details string
		if err := rows.Scan(&run.ID, &run.JobType, &run.TriggerSource, &run.StartedAt, &finishedAt, &run.Status, &details); err != nil {
			return nil, 0, err
		}
		run.FinishedAt = toPointer(finishedAt)
		run.Details = json.RawMessage(details)
		items = append(items, run)
	}
	total, err := a.countDynamicQuery(ctx, countQuery, countArgs...)
	if err != nil {
		return nil, 0, err
	}
	return items, total, rows.Err()
}

func (a *App) queryLogs(ctx context.Context, level, scope, actor, search string, afterID int64, limit, offset int) ([]LogRecord, int, error) {
	var where []string
	var args []any
	if level != "" && level != "all" {
		where = append(where, "level = ?")
		args = append(args, level)
	}
	if scope != "" && scope != "all" {
		where = append(where, "scope = ?")
		args = append(args, scope)
	}
	if actor != "" && actor != "all" {
		where = append(where, "actor = ?")
		args = append(args, actor)
	}
	if afterID > 0 {
		where = append(where, "id > ?")
		args = append(args, afterID)
	}
	if search != "" {
		pattern := "%" + search + "%"
		where = append(where, "(message LIKE ? OR details_json LIKE ?)")
		args = append(args, pattern, pattern)
	}

	query := `
		SELECT id, created_at, level, scope, actor, message, details_json
		FROM system_logs
	`
	countQuery := `SELECT COUNT(*) FROM system_logs`
	if len(where) > 0 {
		clause := " WHERE " + strings.Join(where, " AND ")
		query += clause
		countQuery += clause
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"

	countArgs := append([]any(nil), args...)
	args = append(args, limit, offset)
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []LogRecord
	for rows.Next() {
		record, err := scanLog(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, record)
	}
	total, err := a.countDynamicQuery(ctx, countQuery, countArgs...)
	if err != nil {
		return nil, 0, err
	}
	return items, total, rows.Err()
}

func scanLog(scanner rowScanner) (LogRecord, error) {
	var record LogRecord
	var actor sql.NullString
	var details string
	if err := scanner.Scan(&record.ID, &record.CreatedAt, &record.Level, &record.Scope, &actor, &record.Message, &details); err != nil {
		return LogRecord{}, err
	}
	record.Actor = toPointer(actor)
	record.Details = json.RawMessage(details)
	return record, nil
}

func (a *App) logSystem(ctx context.Context, level, scope, actor, message string, details any) {
	payload := "{}"
	if details != nil {
		if encoded, err := json.Marshal(details); err == nil {
			payload = string(encoded)
		}
	}
	_, _ = a.db.ExecContext(ctx, `
		INSERT INTO system_logs (created_at, level, scope, actor, message, details_json)
		VALUES (?, ?, ?, ?, ?, ?)
	`, nowRFC3339(), level, scope, nullString(actor), message, payload)
}

func (a *App) countQuery(ctx context.Context, query string) (int, error) {
	return a.countDynamicQuery(ctx, query)
}

func (a *App) countDynamicQuery(ctx context.Context, query string, args ...any) (int, error) {
	var value int
	if err := a.db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
}

func (a *App) getLastJob(ctx context.Context, jobType string) (map[string]any, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT id, job_type, trigger_source, started_at, finished_at, status, details_json
		FROM job_runs
		WHERE job_type = ?
		ORDER BY started_at DESC, id DESC
		LIMIT 1
	`, jobType)

	var run JobRun
	var finishedAt sql.NullString
	var details string
	if err := row.Scan(&run.ID, &run.JobType, &run.TriggerSource, &run.StartedAt, &finishedAt, &run.Status, &details); err != nil {
		if err == sql.ErrNoRows {
			return map[string]any{}, nil
		}
		return nil, err
	}
	run.FinishedAt = toPointer(finishedAt)
	run.Details = json.RawMessage(details)

	return map[string]any{
		"id":             run.ID,
		"job_type":       run.JobType,
		"trigger_source": run.TriggerSource,
		"started_at":     run.StartedAt,
		"finished_at":    run.FinishedAt,
		"status":         run.Status,
		"details":        run.Details,
	}, nil
}

func (a *App) createJobRun(ctx context.Context, jobType, triggerSource, startedAt string) (int64, error) {
	res, err := a.db.ExecContext(ctx, `
		INSERT INTO job_runs (job_type, trigger_source, started_at, status, details_json)
		VALUES (?, ?, ?, ?, ?)
	`, jobType, triggerSource, startedAt, "running", "{}")
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (a *App) finishJobRun(ctx context.Context, jobID int64, status, finishedAt string, details []byte) error {
	_, err := a.db.ExecContext(ctx, `
		UPDATE job_runs
		SET status = ?, finished_at = ?, details_json = ?
		WHERE id = ?
	`, status, finishedAt, string(details), jobID)
	return err
}

func (a *App) queryJobHistory(ctx context.Context, jobType string, limit int) ([]JobRun, error) {
	items, _, err := a.queryJobs(ctx, jobType, "all", limit, 0)
	return items, err
}

func (a *App) hasJobRunToday(ctx context.Context, jobType, triggerSource string) (bool, error) {
	startOfDay := time.Now().UTC().Format("2006-01-02") + "T00:00:00Z"
	count, err := a.countDynamicQuery(ctx, `
		SELECT COUNT(*)
		FROM job_runs
		WHERE job_type = ?
		  AND trigger_source = ?
		  AND started_at >= ?
		  AND status IN ('success', 'partial', 'dry-run')
	`, jobType, triggerSource, startOfDay)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (a *App) pruneMetadata(ctx context.Context) error {
	thresholdLogs := time.Now().UTC().AddDate(0, 0, -a.cfg.LogRetentionDays).Format(time.RFC3339)
	thresholdEvents := time.Now().UTC().AddDate(0, 0, -a.cfg.EventRetentionDays).Format(time.RFC3339)
	thresholdJobs := time.Now().UTC().AddDate(0, 0, -a.cfg.JobRetentionDays).Format(time.RFC3339)

	if _, err := a.db.ExecContext(ctx, "DELETE FROM system_logs WHERE created_at < ?", thresholdLogs); err != nil {
		return err
	}
	if _, err := a.db.ExecContext(ctx, "DELETE FROM events WHERE received_at < ?", thresholdEvents); err != nil {
		return err
	}
	if _, err := a.db.ExecContext(ctx, "DELETE FROM job_runs WHERE started_at < ?", thresholdJobs); err != nil {
		return err
	}
	if _, err := a.db.ExecContext(ctx, "DELETE FROM artifact_tags WHERE repo || '|' || digest NOT IN (SELECT repo || '|' || digest FROM artifacts)"); err != nil {
		return err
	}
	return nil
}

func (a *App) isProtected(repo, tag string) bool {
	if repo != "" && a.protectedRepos != nil && a.protectedRepos.MatchString(repo) {
		return true
	}
	if tag != "" && a.protectedTags != nil && a.protectedTags.MatchString(tag) {
		return true
	}
	return false
}
