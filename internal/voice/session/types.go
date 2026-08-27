package session

import "time"

type Status string

const (
	StatusActive  Status = "ACTIVE"
	StatusClosing Status = "CLOSING"
	StatusClosed  Status = "CLOSED"
	StatusFailed  Status = "FAILED"
	StatusExpired Status = "EXPIRED"
)

type RuntimeState string

const (
	RuntimeStateRunning RuntimeState = "RUNNING"
	RuntimeStateStopped RuntimeState = "STOPPED"
)

var SpokenLanguages = []string{
	"bn-IN", "en-IN", "gu-IN", "hi-IN", "kn-IN", "ml-IN",
	"mr-IN", "od-IN", "pa-IN", "ta-IN", "te-IN",
}

type Config struct {
	TableName                    string
	AgentRuntimeARN              string
	AgentRuntimeQualifier        string
	AdmissionEnabled             bool
	RolloutStage                 string
	RolloutInternalSubjectHashes []string
	ProtocolVersion              int
	KVSChannelCount              int
	MaxDuration                  time.Duration
	RotateAfter                  time.Duration
	LeaseDuration                time.Duration
	IdempotencyTTL               time.Duration
	LeaseIndexName               string
	GlobalCapacityLimit          int64
	PerUserCapacityLimit         int64
}

func (c Config) Enabled() bool {
	return c.TableName != "" && c.AgentRuntimeARN != ""
}

type Scope struct {
	UserID           string
	BusinessID       string
	AllBranches      bool
	AllowBranchless  bool
	AllowedBranchIDs []string
}

type ConsentInput struct {
	TranscriptStorage *bool  `json:"transcript_storage" binding:"required"`
	AudioRecording    *bool  `json:"audio_recording" binding:"required"`
	PolicyVersion     string `json:"policy_version" binding:"required"`
}

type ClientInput struct {
	Platform        string `json:"platform" binding:"required"`
	AppVersion      string `json:"app_version" binding:"required"`
	ProtocolVersion int    `json:"protocol_version" binding:"required"`
}

type CreateInput struct {
	BranchID          string       `json:"branch_id,omitempty"`
	IdempotencyKey    string       `json:"idempotency_key" binding:"required"`
	PreferredLanguage string       `json:"preferred_language" binding:"required"`
	FallbackLanguage  string       `json:"fallback_language" binding:"required"`
	Consent           ConsentInput `json:"consent" binding:"required"`
	Client            ClientInput  `json:"client" binding:"required"`
}

type Session struct {
	ID                       string       `json:"session_id"`
	RuntimeSessionID         string       `json:"runtime_session_id"`
	UserID                   string       `json:"-"`
	BusinessID               string       `json:"-"`
	BranchID                 string       `json:"branch_id,omitempty"`
	Status                   Status       `json:"status"`
	RuntimeState             RuntimeState `json:"-"`
	ProtocolVersion          int          `json:"protocol_version"`
	KVSChannelIndex          int          `json:"kvs_channel_index"`
	PreferredLanguage        string       `json:"preferred_language"`
	FallbackLanguage         string       `json:"fallback_language"`
	CurrentLanguage          string       `json:"current_language"`
	TurnSequence             int64        `json:"turn_sequence"`
	GenerationID             int64        `json:"generation_id"`
	ConsentTranscriptStorage bool         `json:"-"`
	ConsentAudioRecording    bool         `json:"-"`
	ConsentPolicyVersion     string       `json:"-"`
	ClientPlatform           string       `json:"-"`
	ClientAppVersion         string       `json:"-"`
	CreatedAt                time.Time    `json:"created_at"`
	UpdatedAt                time.Time    `json:"updated_at"`
	LeaseExpiresAt           time.Time    `json:"lease_expires_at"`
	ExpiresAt                time.Time    `json:"expires_at"`
	RotateAt                 time.Time    `json:"rotate_at"`
	ClosedAt                 *time.Time   `json:"closed_at,omitempty"`
	CapacityReleased         bool         `json:"-"`
	Resumable                bool         `json:"resumable"`
}

type CreateResponse struct {
	SessionID             string    `json:"session_id"`
	RuntimeSessionID      string    `json:"runtime_session_id"`
	AgentRuntimeARN       string    `json:"agent_runtime_arn"`
	AgentRuntimeQualifier string    `json:"agent_runtime_qualifier"`
	KVSChannelIndex       int       `json:"kvs_channel_index"`
	ProtocolVersion       int       `json:"protocol_version"`
	ExpiresAt             time.Time `json:"expires_at"`
	RotateAt              time.Time `json:"rotate_at"`
	SpokenLanguages       []string  `json:"spoken_languages"`
}

type SessionResponse struct {
	SessionID         string     `json:"session_id"`
	RuntimeSessionID  string     `json:"runtime_session_id"`
	BranchID          string     `json:"branch_id,omitempty"`
	Status            Status     `json:"status"`
	ProtocolVersion   int        `json:"protocol_version"`
	KVSChannelIndex   int        `json:"kvs_channel_index"`
	PreferredLanguage string     `json:"preferred_language"`
	FallbackLanguage  string     `json:"fallback_language"`
	CurrentLanguage   string     `json:"current_language"`
	TurnSequence      int64      `json:"turn_sequence"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	LeaseExpiresAt    time.Time  `json:"lease_expires_at"`
	ExpiresAt         time.Time  `json:"expires_at"`
	RotateAt          time.Time  `json:"rotate_at"`
	ClosedAt          *time.Time `json:"closed_at,omitempty"`
	Resumable         bool       `json:"resumable"`
}

type RuntimeTarget struct {
	AgentRuntimeARN  string
	RuntimeSessionID string
	Qualifier        string
}
