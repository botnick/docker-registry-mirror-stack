package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"regexp"
	"runtime"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed web/*
var webFS embed.FS

type App struct {
	cfg        Config
	logger     *slog.Logger
	db         *sql.DB
	httpClient *http.Client
	templates  *template.Template

	protectedRepos *regexp.Regexp
	protectedTags  *regexp.Regexp

	opMu           sync.Mutex
	janitorRunning bool
	gcRunning      bool

	fallbackMu    sync.RWMutex
	fallbackState FallbackStatus

	startedAt time.Time
}

func NewApp(cfg Config, logger *slog.Logger) (*App, error) {
	if err := cfg.EnsurePaths(); err != nil {
		return nil, fmt.Errorf("ensure paths: %w", err)
	}

	var repoRegex *regexp.Regexp
	var tagRegex *regexp.Regexp
	var err error
	if cfg.ProtectedReposPattern != "" {
		repoRegex, err = regexp.Compile(cfg.ProtectedReposPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid PROTECTED_REPOS_REGEX: %w", err)
		}
	}
	if cfg.ProtectedTagsPattern != "" {
		tagRegex, err = regexp.Compile(cfg.ProtectedTagsPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid PROTECTED_TAGS_REGEX: %w", err)
		}
	}

	db, err := sql.Open("sqlite", cfg.SQLitePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	templateFS, err := fs.Sub(webFS, "web/templates")
	if err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}
	templates, err := template.ParseFS(templateFS, "*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	app := &App{
		cfg:            cfg,
		logger:         logger,
		db:             db,
		httpClient:     &http.Client{Timeout: cfg.UpstreamTimeout},
		templates:      templates,
		protectedRepos: repoRegex,
		protectedTags:  tagRegex,
		startedAt:      time.Now().UTC(),
	}

	if err := app.initDB(context.Background()); err != nil {
		return nil, err
	}
	if err := app.ensureBootstrapAdmin(context.Background()); err != nil {
		return nil, err
	}
	if err := app.initDefaultSettings(context.Background()); err != nil {
		return nil, err
	}
	if err := app.pruneExpiredSessions(context.Background()); err != nil {
		return nil, err
	}
	if err := app.pruneMetadata(context.Background()); err != nil {
		return nil, err
	}
	_, _ = app.refreshFallbackStatus(context.Background())

	app.logSystem(context.Background(), "info", "startup", "", "control service started", map[string]any{
		"mode":         cfg.AppMode,
		"listen":       cfg.ListenAddress(),
		"registry_url": cfg.RegistryURL,
		"go_version":   runtime.Version(),
	})

	return app, nil
}

func (a *App) initDB(ctx context.Context) error {
	if _, err := a.db.ExecContext(ctx, "PRAGMA busy_timeout = 5000;"); err != nil {
		return fmt.Errorf("sqlite busy_timeout: %w", err)
	}
	if _, err := a.db.ExecContext(ctx, "PRAGMA journal_mode = WAL;"); err != nil {
		return fmt.Errorf("sqlite journal_mode: %w", err)
	}
	if _, err := a.db.ExecContext(ctx, "PRAGMA foreign_keys = ON;"); err != nil {
		return fmt.Errorf("sqlite foreign_keys: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		must_change_password INTEGER NOT NULL DEFAULT 0,
		failed_attempts INTEGER NOT NULL DEFAULT 0,
		locked_until TEXT,
		last_login_at TEXT,
		last_password_change_at TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		token_hash TEXT NOT NULL UNIQUE,
		csrf_token TEXT NOT NULL,
		created_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		last_seen_at TEXT NOT NULL,
		remote_addr TEXT,
		user_agent TEXT,
		revoked_at TEXT,
		FOREIGN KEY (user_id) REFERENCES users(id)
	);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
	CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS artifacts (
		repo TEXT NOT NULL,
		tag TEXT,
		digest TEXT NOT NULL,
		media_type TEXT,
		size_bytes INTEGER,
		first_seen_at TEXT NOT NULL,
		last_used_at TEXT NOT NULL,
		use_count INTEGER NOT NULL DEFAULT 0,
		pinned INTEGER NOT NULL DEFAULT 0,
		explicit_protected INTEGER NOT NULL DEFAULT 0,
		deleted_at TEXT,
		delete_reason TEXT,
		PRIMARY KEY (repo, digest)
	);
	CREATE INDEX IF NOT EXISTS idx_artifacts_repo_tag ON artifacts(repo, tag);
	CREATE INDEX IF NOT EXISTS idx_artifacts_last_used ON artifacts(last_used_at);
	CREATE INDEX IF NOT EXISTS idx_artifacts_deleted ON artifacts(deleted_at, pinned, explicit_protected);
	CREATE TABLE IF NOT EXISTS artifact_tags (
		repo TEXT NOT NULL,
		digest TEXT NOT NULL,
		tag TEXT NOT NULL,
		first_seen_at TEXT NOT NULL,
		last_seen_at TEXT NOT NULL,
		PRIMARY KEY (repo, digest, tag)
	);
	CREATE INDEX IF NOT EXISTS idx_artifact_tags_lookup ON artifact_tags(repo, digest, last_seen_at DESC);
	CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		received_at TEXT NOT NULL,
		action TEXT,
		repo TEXT,
		tag TEXT,
		digest TEXT,
		raw_json TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_events_received_at ON events(received_at DESC);
	CREATE INDEX IF NOT EXISTS idx_events_artifact ON events(repo, digest, received_at DESC);
	CREATE TABLE IF NOT EXISTS job_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		job_type TEXT NOT NULL,
		trigger_source TEXT NOT NULL,
		started_at TEXT NOT NULL,
		finished_at TEXT,
		status TEXT NOT NULL,
		details_json TEXT NOT NULL DEFAULT '{}'
	);
	CREATE INDEX IF NOT EXISTS idx_job_runs_type_started ON job_runs(job_type, started_at DESC);
	CREATE TABLE IF NOT EXISTS system_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at TEXT NOT NULL,
		level TEXT NOT NULL,
		scope TEXT NOT NULL,
		actor TEXT,
		message TEXT NOT NULL,
		details_json TEXT NOT NULL DEFAULT '{}'
	);
	CREATE INDEX IF NOT EXISTS idx_system_logs_created_at ON system_logs(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_system_logs_scope ON system_logs(scope, created_at DESC);
	`
	if _, err := a.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}

	if err := a.ensureColumn(ctx, "artifacts", "use_count", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := a.ensureColumn(ctx, "artifacts", "explicit_protected", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := a.ensureColumn(ctx, "artifacts", "delete_reason", "TEXT"); err != nil {
		return err
	}
	if err := a.ensureColumn(ctx, "system_logs", "actor", "TEXT"); err != nil {
		return err
	}
	return nil
}

func (a *App) ensureColumn(ctx context.Context, tableName, columnName, definition string) error {
	rows, err := a.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return fmt.Errorf("table_info %s: %w", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var dataType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == columnName {
			return nil
		}
	}
	if _, err := a.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, columnName, definition)); err != nil {
		return fmt.Errorf("alter table %s add column %s: %w", tableName, columnName, err)
	}
	return nil
}

func (a *App) initDefaultSettings(ctx context.Context) error {
	defaults := map[string]string{
		"maintenance_mode": "false",
		"janitor_paused":   "false",
		"gc_paused":        "false",
		"maintenance_note": "",
	}
	for key, value := range defaults {
		if err := a.setSettingIfMissing(ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	assets, _ := fs.Sub(webFS, "web/assets")
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))

	mux.HandleFunc("/healthz", a.handleHealthz)
	if a.cfg.AppMode == "control" {
		mux.HandleFunc("/notifications", a.handleNotifications)
		mux.HandleFunc("/login", a.handleLoginPage)
		mux.HandleFunc("/auth/login", a.handleLogin)
	}

	if a.cfg.AppMode == "control" {
		protected := a.authMiddleware(http.HandlerFunc(a.handleProtectedRoutes))
		mux.Handle("/", protected)
	}

	return a.loggingMiddleware(mux)
}

func (a *App) handleProtectedRoutes(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/" || r.URL.Path == "/dashboard":
		a.renderAppPage(w, r, "ภาพรวมระบบ", "dashboard", false)
	case r.URL.Path == "/force-password":
		a.renderAppPage(w, r, "เปลี่ยนรหัสผ่านครั้งแรก", "force-password", true)
	case r.URL.Path == "/password":
		a.renderAppPage(w, r, "เปลี่ยนรหัสผ่าน", "password", false)
	case r.URL.Path == "/cache":
		a.renderAppPage(w, r, "ภาพรวมแคช", "cache", false)
	case r.URL.Path == "/artifacts":
		a.renderAppPage(w, r, "คลัง Artifacts", "artifacts", false)
	case r.URL.Path == "/artifact":
		a.renderAppPage(w, r, "รายละเอียด Artifact", "artifact", false)
	case r.URL.Path == "/events":
		a.renderAppPage(w, r, "เหตุการณ์จาก Registry", "events", false)
	case r.URL.Path == "/jobs":
		a.renderAppPage(w, r, "สถานะงานเบื้องหลัง", "jobs", false)
	case r.URL.Path == "/protections":
		a.renderAppPage(w, r, "Pinned และ Protected", "protections", false)
	case r.URL.Path == "/cleanup":
		a.renderAppPage(w, r, "งาน Cleanup", "cleanup", false)
	case r.URL.Path == "/gc":
		a.renderAppPage(w, r, "งาน Garbage Collection", "gc", false)
	case r.URL.Path == "/health":
		a.renderAppPage(w, r, "สถานะ Fallback และ Upstream", "health", false)
	case r.URL.Path == "/maintenance":
		a.renderAppPage(w, r, "Maintenance และการควบคุม", "maintenance", false)
	case r.URL.Path == "/logs":
		a.renderAppPage(w, r, "Logs และกิจกรรมระบบ", "logs", false)
	case r.URL.Path == "/settings":
		a.renderAppPage(w, r, "Settings และ Runtime Config", "settings", false)
	case stringsHasPrefix(r.URL.Path, "/api/"):
		a.handleProtectedAPI(w, r)
	case r.URL.Path == "/auth/logout":
		a.handleLogout(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (a *App) handleProtectedAPI(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/auth/me":
		a.handleAuthMe(w, r)
	case "/api/auth/change-password":
		a.handleChangePassword(w, r)
	case "/api/system/overview":
		a.handleOverview(w, r)
	case "/api/system/config":
		a.handleConfig(w, r)
	case "/api/system/fallback":
		a.handleFallback(w, r)
	case "/api/system/logs":
		a.handleLogs(w, r)
	case "/api/events":
		a.handleEvents(w, r)
	case "/api/cache/overview":
		a.handleCacheOverview(w, r)
	case "/api/cleanup/candidates":
		a.handleCandidates(w, r)
	case "/api/cleanup/run":
		a.handleRunJanitor(w, r)
	case "/api/cleanup/history":
		a.handleCleanupHistory(w, r)
	case "/api/gc/status":
		a.handleGCStatus(w, r)
	case "/api/gc/run":
		a.handleRunGC(w, r)
	case "/api/gc/history":
		a.handleGCHistory(w, r)
	case "/api/gc/clear-request":
		a.handleClearGCRequest(w, r)
	case "/api/artifacts":
		a.handleArtifacts(w, r)
	case "/api/artifacts/detail":
		a.handleArtifactDetail(w, r)
	case "/api/artifacts/history":
		a.handleArtifactHistory(w, r)
	case "/api/artifacts/pin":
		a.handlePin(w, r)
	case "/api/artifacts/protect":
		a.handleProtect(w, r)
	case "/api/jobs":
		a.handleJobs(w, r)
	case "/api/maintenance/state":
		a.handleMaintenanceState(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (a *App) renderAppPage(w http.ResponseWriter, r *http.Request, title, pageID string, restricted bool) {
	auth, ok := a.authSessionFromRequest(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.templates.ExecuteTemplate(w, "app.html", AppPageData{
		Title:              title,
		PageID:             pageID,
		Username:           auth.User.Username,
		CSRFToken:          auth.Session.CSRFToken,
		MustChangePassword: auth.User.MustChangePassword,
		RestrictedMode:     restricted,
	})
}

func (a *App) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		a.logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

func (a *App) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

func (a *App) writeError(w http.ResponseWriter, status int, message string) {
	a.writeJSON(w, status, map[string]any{
		"error":  message,
		"status": status,
		"time":   nowRFC3339(),
	})
}

func (a *App) writeMethodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	a.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}
