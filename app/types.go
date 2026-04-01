package main

import "encoding/json"

const (
	maxBodyBytes    = 4 << 20
	maxLogTailBytes = 8192
)

type AppPageData struct {
	Title              string
	PageID             string
	Username           string
	CSRFToken          string
	MustChangePassword bool
	RestrictedMode     bool
}

type User struct {
	ID                   int64
	Username             string
	PasswordHash         string
	MustChangePassword   bool
	FailedAttempts       int
	LockedUntil          *string
	LastLoginAt          *string
	LastPasswordChangeAt *string
	CreatedAt            string
	UpdatedAt            string
}

type Session struct {
	ID         int64
	UserID     int64
	TokenHash  string
	CSRFToken  string
	CreatedAt  string
	ExpiresAt  string
	LastSeenAt string
	RemoteAddr *string
	UserAgent  *string
	RevokedAt  *string
}

type AuthSession struct {
	User    User
	Session Session
}

type Artifact struct {
	Repo              string   `json:"repo"`
	Tag               *string  `json:"tag"`
	Digest            string   `json:"digest"`
	MediaType         *string  `json:"media_type"`
	SizeBytes         int64    `json:"size_bytes"`
	FirstSeenAt       string   `json:"first_seen_at"`
	LastUsedAt        string   `json:"last_used_at"`
	UseCount          int64    `json:"use_count"`
	Pinned            bool     `json:"pinned"`
	ExplicitProtected bool     `json:"explicit_protected"`
	RegexProtected    bool     `json:"regex_protected"`
	Protected         bool     `json:"protected"`
	DeletedAt         *string  `json:"deleted_at"`
	DeleteReason      *string  `json:"delete_reason"`
	Candidate         bool     `json:"candidate"`
	BlockedReasons    []string `json:"blocked_reasons,omitempty"`
}

type ArtifactDetail struct {
	Artifact       Artifact      `json:"artifact"`
	Tags           []string      `json:"tags"`
	RecentEvents   []EventRecord `json:"recent_events"`
	RecentActivity []LogRecord   `json:"recent_activity"`
}

type EventRecord struct {
	ID         int64           `json:"id"`
	ReceivedAt string          `json:"received_at"`
	Action     string          `json:"action"`
	Repo       *string         `json:"repo"`
	Tag        *string         `json:"tag"`
	Digest     *string         `json:"digest"`
	Raw        json.RawMessage `json:"raw_json,omitempty"`
}

type JobRun struct {
	ID            int64           `json:"id"`
	JobType       string          `json:"job_type"`
	TriggerSource string          `json:"trigger_source"`
	StartedAt     string          `json:"started_at"`
	FinishedAt    *string         `json:"finished_at"`
	Status        string          `json:"status"`
	Details       json.RawMessage `json:"details"`
}

type LogRecord struct {
	ID        int64           `json:"id"`
	CreatedAt string          `json:"created_at"`
	Level     string          `json:"level"`
	Scope     string          `json:"scope"`
	Actor     *string         `json:"actor"`
	Message   string          `json:"message"`
	Details   json.RawMessage `json:"details"`
}

type NotificationEnvelope struct {
	Events []json.RawMessage `json:"events"`
}

type NotificationEvent struct {
	ID         string             `json:"id"`
	Action     string             `json:"action"`
	Repository string             `json:"repository"`
	Target     NotificationTarget `json:"target"`
	Request    NotificationSource `json:"request"`
	Actor      NotificationActor  `json:"actor"`
}

type NotificationTarget struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Digest     string `json:"digest"`
	MediaType  string `json:"mediaType"`
	Length     int64  `json:"length"`
}

type NotificationSource struct {
	Addr string `json:"addr"`
	Host string `json:"host"`
}

type NotificationActor struct {
	Name string `json:"name"`
}

type HealthProbe struct {
	Name        string `json:"name"`
	Healthy     bool   `json:"healthy"`
	StatusCode  int    `json:"status_code"`
	LatencyMS   int64  `json:"latency_ms"`
	Message     string `json:"message"`
	LastCheckAt string `json:"last_check_at"`
}

type FallbackStatus struct {
	State             string         `json:"state"`
	Summary           string         `json:"summary"`
	Details           string         `json:"details"`
	Since             string         `json:"since"`
	LastCheckAt       string         `json:"last_check_at"`
	CachedModeUsable  bool           `json:"cached_mode_usable"`
	DestructivePaused bool           `json:"destructive_paused"`
	Registry          HealthProbe    `json:"registry"`
	Upstream          HealthProbe    `json:"upstream"`
	Storage           map[string]any `json:"storage"`
	Maintenance       map[string]any `json:"maintenance"`
}

type MaintenanceState struct {
	MaintenanceMode bool    `json:"maintenance_mode"`
	JanitorPaused   bool    `json:"janitor_paused"`
	GCPaused        bool    `json:"gc_paused"`
	Note            string  `json:"note"`
	UpdatedAt       *string `json:"updated_at"`
}

type JanitorItemResult struct {
	Repo       string  `json:"repo"`
	Tag        *string `json:"tag"`
	Digest     string  `json:"digest"`
	SizeBytes  int64   `json:"size_bytes"`
	StatusCode int     `json:"status_code,omitempty"`
	Status     string  `json:"status"`
	Error      string  `json:"error,omitempty"`
}

type JanitorResult struct {
	TriggerSource           string              `json:"trigger_source"`
	StartedAt               string              `json:"started_at"`
	FinishedAt              string              `json:"finished_at"`
	DryRun                  bool                `json:"dry_run"`
	Forced                  bool                `json:"forced"`
	Skipped                 bool                `json:"skipped"`
	SkipReason              string              `json:"skip_reason,omitempty"`
	FreePctBefore           float64             `json:"free_pct_before"`
	FreePctCurrent          float64             `json:"free_pct_current"`
	LowWatermarkPct         int                 `json:"low_watermark_pct"`
	TargetFreePct           int                 `json:"target_free_pct"`
	EmergencyFreePct        int                 `json:"emergency_free_pct"`
	MustFree                bool                `json:"must_free"`
	EmergencyMode           bool                `json:"emergency_mode"`
	RequiredBytes           int64               `json:"required_bytes"`
	CandidateCount          int                 `json:"candidate_count"`
	DeletedCount            int                 `json:"deleted_count"`
	PlannedCount            int                 `json:"planned_count"`
	ErrorCount              int                 `json:"error_count"`
	EstimatedRecoveredBytes int64               `json:"estimated_recovered_bytes"`
	GCRequested             bool                `json:"gc_requested"`
	BatchLimit              int                 `json:"batch_limit"`
	Results                 []JanitorItemResult `json:"results"`
	FallbackState           string              `json:"fallback_state"`
}

type GCResult struct {
	Queued        bool   `json:"queued,omitempty"`
	RequestedAt   string `json:"requested_at,omitempty"`
	TriggerSource string `json:"trigger_source"`
	StartedAt     string `json:"started_at"`
	FinishedAt    string `json:"finished_at"`
	Forced        bool   `json:"forced"`
	Skipped       bool   `json:"skipped"`
	SkipReason    string `json:"skip_reason,omitempty"`
	RegistryImage string `json:"registry_image"`
	StatusCode    int    `json:"status_code"`
	LogsTail      string `json:"logs_tail"`
	GCPending     bool   `json:"gc_pending"`
	GCFlagCleared bool   `json:"gc_flag_cleared"`
	FallbackState string `json:"fallback_state"`
}

type PaginatedResponse[T any] struct {
	Items   []T  `json:"items"`
	Total   int  `json:"total"`
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}
