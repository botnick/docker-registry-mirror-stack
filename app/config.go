package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	second = time.Second
	minute = time.Minute
	hour   = time.Hour
)

type Config struct {
	AppMode                      string
	Upstreams                    []UpstreamTarget
	RegistryURL                  string
	RegistryDataPath             string
	RegistryConfigPath           string
	RegistryBinaryPath           string
	SQLitePath                   string
	GCRequestFlag                string
	GCActiveFlag                 string
	ListenHost                   string
	ListenPort                   int
	PublicBaseURL                string
	CookieSecure                 bool
	AllowInsecureControl         bool
	TrustProxyHeaders            bool
	SessionSecret                string
	SessionTTL                   time.Duration
	SessionRefreshInterval       time.Duration
	BootstrapUsername            string
	BootstrapPassword            string
	BootstrapForcePasswordChange bool
	DryRun                       bool
	JanitorInterval              time.Duration
	MaxDeleteBatch               int
	UnusedDays                   int
	MinCacheAgeDays              int
	LowWatermarkPct              int
	TargetFreePct                int
	EmergencyFreePct             int
	GCHourUTC                    int
	ProtectedReposPattern        string
	ProtectedTagsPattern         string
	UpstreamURL                  string
	HealthCheckInterval          time.Duration
	UpstreamTimeout              time.Duration
	LogRetentionDays             int
	EventRetentionDays           int
	JobRetentionDays             int
	LoginMaxAttempts             int
	LoginLockDuration            time.Duration
	NotificationsUsername        string
	NotificationsPassword        string
	GCWorkerPollInterval         time.Duration
}

func LoadConfig() (Config, error) {
	cfg := Config{
		AppMode:                      getEnv("APP_MODE", "control"),
		RegistryURL:                  strings.TrimRight(getEnv("REGISTRY_URL", "http://registry-dockerhub:5000"), "/"),
		RegistryDataPath:             getEnv("REGISTRY_DATA_PATH", "/var/lib/registry"),
		RegistryConfigPath:           getEnv("REGISTRY_CONFIG_PATH", "/etc/distribution/config.yml"),
		RegistryBinaryPath:           getEnv("REGISTRY_BINARY_PATH", "/usr/local/bin/registry"),
		SQLitePath:                   getEnv("SQLITE_PATH", "/data/metadata/registry_meta.db"),
		GCRequestFlag:                getEnv("GC_REQUEST_FLAG", "/data/state/gc.requested"),
		GCActiveFlag:                 getEnv("GC_ACTIVE_FLAG", "/data/state/gc.running"),
		ListenHost:                   getEnv("LISTEN_HOST", "0.0.0.0"),
		PublicBaseURL:                strings.TrimRight(getEnv("PUBLIC_BASE_URL", ""), "/"),
		CookieSecure:                 parseBool(getEnv("COOKIE_SECURE", "true")),
		AllowInsecureControl:         parseBool(getEnv("ALLOW_INSECURE_CONTROL", "false")),
		TrustProxyHeaders:            parseBool(getEnv("TRUST_PROXY_HEADERS", "true")),
		SessionSecret:                strings.TrimSpace(os.Getenv("SESSION_SECRET")),
		BootstrapUsername:            getEnv("CONTROL_BOOTSTRAP_USERNAME", "admin"),
		BootstrapPassword:            os.Getenv("CONTROL_BOOTSTRAP_PASSWORD"),
		BootstrapForcePasswordChange: parseBool(getEnv("CONTROL_BOOTSTRAP_FORCE_PASSWORD_CHANGE", "false")),
		DryRun:                       parseBool(getEnv("DRY_RUN", "false")),
		ProtectedReposPattern:        getEnv("PROTECTED_REPOS_REGEX", ""),
		ProtectedTagsPattern:         getEnv("PROTECTED_TAGS_REGEX", ""),
		UpstreamURL:                  strings.TrimRight(getEnv("UPSTREAM_HEALTH_URL", "https://registry-1.docker.io/v2/"), "/"),
		NotificationsUsername:        getEnv("NOTIFICATIONS_USERNAME", "registry-notify"),
		NotificationsPassword:        strings.TrimSpace(os.Getenv("NOTIFICATIONS_PASSWORD")),
	}

	targets, err := parseUpstreamTargets(os.Getenv("UPSTREAMS_JSON"))
	if err != nil {
		return Config{}, err
	}
	if len(targets) == 0 {
		targets = []UpstreamTarget{{
			Host:               strings.ToLower(getEnv("DEFAULT_UPSTREAM_HOST", "docker.io")),
			DisplayName:        getEnv("DEFAULT_UPSTREAM_NAME", "Docker Hub"),
			BackendURL:         cfg.RegistryURL,
			UpstreamHealthURL:  cfg.UpstreamURL,
			RegistryDataPath:   cfg.RegistryDataPath,
			RegistryConfigPath: cfg.RegistryConfigPath,
			RegistryBinaryPath: cfg.RegistryBinaryPath,
			GCRequestFlag:      cfg.GCRequestFlag,
			GCActiveFlag:       cfg.GCActiveFlag,
			Default:            true,
		}}
	}
	cfg.Upstreams = targets

	defaultTarget := cfg.DefaultTarget()
	cfg.RegistryURL = defaultTarget.BackendURL
	cfg.RegistryDataPath = defaultTarget.RegistryDataPath
	cfg.RegistryConfigPath = defaultTarget.RegistryConfigPath
	cfg.RegistryBinaryPath = defaultTarget.RegistryBinaryPath
	cfg.GCRequestFlag = defaultTarget.GCRequestFlag
	cfg.GCActiveFlag = defaultTarget.GCActiveFlag
	cfg.UpstreamURL = defaultTarget.UpstreamHealthURL

	if cfg.ListenPort, err = getEnvInt("LISTEN_PORT", 8080); err != nil {
		return Config{}, err
	}

	sessionHours, err := getEnvInt("SESSION_TTL_HOURS", 24)
	if err != nil {
		return Config{}, err
	}
	cfg.SessionTTL = time.Duration(sessionHours) * hour

	refreshMinutes, err := getEnvInt("SESSION_REFRESH_MINUTES", 15)
	if err != nil {
		return Config{}, err
	}
	cfg.SessionRefreshInterval = time.Duration(refreshMinutes) * minute

	janitorSeconds, err := getEnvInt("JANITOR_INTERVAL_SECONDS", 3600)
	if err != nil {
		return Config{}, err
	}
	cfg.JanitorInterval = time.Duration(janitorSeconds) * second

	if cfg.MaxDeleteBatch, err = getEnvInt("MAX_DELETE_BATCH", 20); err != nil {
		return Config{}, err
	}
	if cfg.UnusedDays, err = getEnvInt("UNUSED_DAYS", 30); err != nil {
		return Config{}, err
	}
	if cfg.MinCacheAgeDays, err = getEnvInt("MIN_CACHE_AGE_DAYS", 3); err != nil {
		return Config{}, err
	}
	if cfg.LowWatermarkPct, err = getEnvInt("LOW_WATERMARK_PCT", 20); err != nil {
		return Config{}, err
	}
	if cfg.TargetFreePct, err = getEnvInt("TARGET_FREE_PCT", 35); err != nil {
		return Config{}, err
	}
	if cfg.EmergencyFreePct, err = getEnvInt("EMERGENCY_FREE_PCT", 10); err != nil {
		return Config{}, err
	}
	if cfg.GCHourUTC, err = getEnvInt("GC_HOUR_UTC", 19); err != nil {
		return Config{}, err
	}

	healthCheckSeconds, err := getEnvInt("HEALTH_CHECK_INTERVAL_SECONDS", 60)
	if err != nil {
		return Config{}, err
	}
	cfg.HealthCheckInterval = time.Duration(healthCheckSeconds) * second

	upstreamTimeoutSeconds, err := getEnvInt("UPSTREAM_TIMEOUT_SECONDS", 6)
	if err != nil {
		return Config{}, err
	}
	cfg.UpstreamTimeout = time.Duration(upstreamTimeoutSeconds) * second

	if cfg.LogRetentionDays, err = getEnvInt("LOG_RETENTION_DAYS", 30); err != nil {
		return Config{}, err
	}
	if cfg.EventRetentionDays, err = getEnvInt("EVENT_RETENTION_DAYS", 30); err != nil {
		return Config{}, err
	}
	if cfg.JobRetentionDays, err = getEnvInt("JOB_RETENTION_DAYS", 90); err != nil {
		return Config{}, err
	}
	if cfg.LoginMaxAttempts, err = getEnvInt("LOGIN_MAX_ATTEMPTS", 5); err != nil {
		return Config{}, err
	}
	lockMinutes, err := getEnvInt("LOGIN_LOCK_MINUTES", 15)
	if err != nil {
		return Config{}, err
	}
	cfg.LoginLockDuration = time.Duration(lockMinutes) * minute

	gcWorkerPollSeconds, err := getEnvInt("GC_WORKER_POLL_SECONDS", 15)
	if err != nil {
		return Config{}, err
	}
	cfg.GCWorkerPollInterval = time.Duration(gcWorkerPollSeconds) * second

	switch {
	case cfg.AppMode != "control" && cfg.AppMode != "gc-worker" && cfg.AppMode != "router":
		return Config{}, fmt.Errorf("APP_MODE must be control, gc-worker, or router")
	case cfg.ListenPort <= 0 || cfg.ListenPort > 65535:
		return Config{}, fmt.Errorf("LISTEN_PORT must be between 1 and 65535")
	case cfg.AppMode == "control" && cfg.SessionSecret == "":
		return Config{}, fmt.Errorf("SESSION_SECRET is required")
	case cfg.AppMode == "control" && !cfg.AllowInsecureControl && !cfg.CookieSecure:
		return Config{}, fmt.Errorf("COOKIE_SECURE=false requires ALLOW_INSECURE_CONTROL=true")
	case cfg.AppMode == "control" && cfg.PublicBaseURL != "" && !cfg.AllowInsecureControl && !strings.HasPrefix(strings.ToLower(cfg.PublicBaseURL), "https://"):
		return Config{}, fmt.Errorf("PUBLIC_BASE_URL must use https unless ALLOW_INSECURE_CONTROL=true")
	case cfg.LowWatermarkPct < 0 || cfg.LowWatermarkPct > 100:
		return Config{}, fmt.Errorf("LOW_WATERMARK_PCT must be between 0 and 100")
	case cfg.TargetFreePct < 0 || cfg.TargetFreePct > 100:
		return Config{}, fmt.Errorf("TARGET_FREE_PCT must be between 0 and 100")
	case cfg.EmergencyFreePct < 0 || cfg.EmergencyFreePct > 100:
		return Config{}, fmt.Errorf("EMERGENCY_FREE_PCT must be between 0 and 100")
	case cfg.EmergencyFreePct >= cfg.LowWatermarkPct:
		return Config{}, fmt.Errorf("EMERGENCY_FREE_PCT must be lower than LOW_WATERMARK_PCT")
	case cfg.GCHourUTC < 0 || cfg.GCHourUTC > 23:
		return Config{}, fmt.Errorf("GC_HOUR_UTC must be between 0 and 23")
	case cfg.UpstreamURL == "":
		return Config{}, fmt.Errorf("UPSTREAM_HEALTH_URL must not be empty")
	case len(cfg.Upstreams) == 0:
		return Config{}, fmt.Errorf("at least one upstream target is required")
	case cfg.MaxDeleteBatch <= 0:
		return Config{}, fmt.Errorf("MAX_DELETE_BATCH must be greater than 0")
	case cfg.UnusedDays <= 0:
		return Config{}, fmt.Errorf("UNUSED_DAYS must be greater than 0")
	case cfg.MinCacheAgeDays < 0:
		return Config{}, fmt.Errorf("MIN_CACHE_AGE_DAYS must be 0 or greater")
	case cfg.LoginMaxAttempts < 1:
		return Config{}, fmt.Errorf("LOGIN_MAX_ATTEMPTS must be greater than 0")
	case cfg.SessionTTL <= 0:
		return Config{}, fmt.Errorf("SESSION_TTL_HOURS must be greater than 0")
	case cfg.SessionRefreshInterval <= 0:
		return Config{}, fmt.Errorf("SESSION_REFRESH_MINUTES must be greater than 0")
	case cfg.HealthCheckInterval <= 0:
		return Config{}, fmt.Errorf("HEALTH_CHECK_INTERVAL_SECONDS must be greater than 0")
	case cfg.UpstreamTimeout <= 0:
		return Config{}, fmt.Errorf("UPSTREAM_TIMEOUT_SECONDS must be greater than 0")
	case cfg.LogRetentionDays <= 0:
		return Config{}, fmt.Errorf("LOG_RETENTION_DAYS must be greater than 0")
	case cfg.EventRetentionDays <= 0:
		return Config{}, fmt.Errorf("EVENT_RETENTION_DAYS must be greater than 0")
	case cfg.JobRetentionDays <= 0:
		return Config{}, fmt.Errorf("JOB_RETENTION_DAYS must be greater than 0")
	case cfg.NotificationsUsername == "":
		return Config{}, fmt.Errorf("NOTIFICATIONS_USERNAME must not be empty")
	case cfg.AppMode == "control" && cfg.NotificationsPassword == "":
		return Config{}, fmt.Errorf("NOTIFICATIONS_PASSWORD is required")
	case cfg.GCWorkerPollInterval <= 0:
		return Config{}, fmt.Errorf("GC_WORKER_POLL_SECONDS must be greater than 0")
	}

	if cfg.AppMode != "router" {
		for _, target := range cfg.Upstreams {
			switch {
			case target.RegistryDataPath == "":
				return Config{}, fmt.Errorf("upstream %s registry_data_path is required", target.Host)
			case target.RegistryConfigPath == "":
				return Config{}, fmt.Errorf("upstream %s registry_config_path is required", target.Host)
			case target.RegistryBinaryPath == "":
				return Config{}, fmt.Errorf("upstream %s registry_binary_path is required", target.Host)
			case target.GCRequestFlag == "":
				return Config{}, fmt.Errorf("upstream %s gc_request_flag is required", target.Host)
			case target.GCActiveFlag == "":
				return Config{}, fmt.Errorf("upstream %s gc_active_flag is required", target.Host)
			}
		}
	}

	return cfg, nil
}

func (c Config) ListenAddress() string {
	return fmt.Sprintf("%s:%d", c.ListenHost, c.ListenPort)
}

func (c Config) EnsurePaths() error {
	if c.AppMode == "router" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.SQLitePath), 0o755); err != nil {
		return err
	}
	for _, target := range c.Upstreams {
		if err := os.MkdirAll(filepath.Dir(target.GCRequestFlag), 0o755); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target.GCActiveFlag), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func getEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getEnvInt(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return parsed, nil
}
