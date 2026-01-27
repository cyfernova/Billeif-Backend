package a2a

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// A2A Protocol v0.3 Types
// Based on Google's Agent2Agent Protocol specification

// TaskState represents the possible states of an A2A task per v0.3 spec
type TaskState string

const (
	TaskStateWorking       TaskState = "working"
	TaskStateCompleted     TaskState = "completed"
	TaskStateFailed        TaskState = "failed"
	TaskStateCancelled     TaskState = "cancelled"
	TaskStateInputRequired TaskState = "input-required"
	TaskStateRejected      TaskState = "rejected"
)

// IsTerminal returns true if the task state is terminal (no further transitions)
func (s TaskState) IsTerminal() bool {
	return s == TaskStateCompleted || s == TaskStateFailed ||
		s == TaskStateCancelled || s == TaskStateRejected
}

// IsValid returns true if the task state is a valid A2A v0.3 state
func (s TaskState) IsValid() bool {
	switch s {
	case TaskStateWorking, TaskStateCompleted, TaskStateFailed,
		TaskStateCancelled, TaskStateInputRequired, TaskStateRejected:
		return true
	}
	return false
}

// MessageRole represents the role of a message sender in A2A v0.3
type MessageRole string

const (
	MessageRoleUser  MessageRole = "user"
	MessageRoleAgent MessageRole = "agent"
)

// PartType represents the type of content part in a message
type PartType string

const (
	PartTypeText PartType = "text"
	PartTypeFile PartType = "file"
	PartTypeData PartType = "data"
)

// Part is the interface for message content parts
type Part interface {
	GetType() PartType
}

// TextPart represents a text content part
type TextPart struct {
	Type    PartType `json:"type"`
	Text    string   `json:"text"`
	MimeType string  `json:"mimeType,omitempty"`
}

func (p TextPart) GetType() PartType { return PartTypeText }

// FilePart represents a file content part
type FilePart struct {
	Type      PartType `json:"type"`
	FileID    string   `json:"fileId,omitempty"`
	FileName  string   `json:"fileName"`
	MimeType  string   `json:"mimeType"`
	Size      int64    `json:"size,omitempty"`
	URL       string   `json:"url,omitempty"`
	Data      string   `json:"data,omitempty"` // Base64 encoded for inline files
}

func (p FilePart) GetType() PartType { return PartTypeFile }

// DataPart represents a structured data content part
type DataPart struct {
	Type     PartType               `json:"type"`
	MimeType string                 `json:"mimeType"`
	Data     map[string]interface{} `json:"data"`
}

func (p DataPart) GetType() PartType { return PartTypeData }

// PartWrapper wraps a Part for JSON marshaling/unmarshaling
type PartWrapper struct {
	Part Part
}

// UnmarshalJSON implements custom JSON unmarshaling for parts
func (pw *PartWrapper) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type PartType `json:"type"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	switch raw.Type {
	case PartTypeText:
		var p TextPart
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		pw.Part = p
	case PartTypeFile:
		var p FilePart
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		pw.Part = p
	case PartTypeData:
		var p DataPart
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		pw.Part = p
	default:
		// Default to text part
		var p TextPart
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		pw.Part = p
	}
	return nil
}

// MarshalJSON implements custom JSON marshaling for parts
func (pw PartWrapper) MarshalJSON() ([]byte, error) {
	return json.Marshal(pw.Part)
}

// Message represents a message in A2A v0.3 task communication
type Message struct {
	Role      MessageRole   `json:"role"`
	Parts     []PartWrapper `json:"parts"`
	Timestamp time.Time     `json:"timestamp,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// NewTextMessage creates a new text message
func NewTextMessage(role MessageRole, text string) Message {
	return Message{
		Role: role,
		Parts: []PartWrapper{
			{Part: TextPart{Type: PartTypeText, Text: text}},
		},
		Timestamp: time.Now(),
	}
}

// AddPart adds a content part to the message
func (m *Message) AddPart(part Part) {
	m.Parts = append(m.Parts, PartWrapper{Part: part})
}

// GetText returns the concatenated text from all text parts
func (m *Message) GetText() string {
	var result string
	for _, pw := range m.Parts {
		if tp, ok := pw.Part.(TextPart); ok {
			if result != "" {
				result += "\n"
			}
			result += tp.Text
		}
	}
	return result
}

// Artifact represents a task output artifact in A2A v0.3
type Artifact struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	MimeType    string                 `json:"mimeType"`
	Parts       []PartWrapper          `json:"parts,omitempty"`
	Index       int                    `json:"index,omitempty"` // For ordered artifacts
	Append      bool                   `json:"append,omitempty"` // Whether to append to existing
	LastChunk   bool                   `json:"lastChunk,omitempty"` // Last chunk of streaming artifact
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt   time.Time              `json:"createdAt"`
}

// NewArtifact creates a new artifact with generated ID
func NewArtifact(name, mimeType string) *Artifact {
	return &Artifact{
		ID:        uuid.New().String(),
		Name:      name,
		MimeType:  mimeType,
		Parts:     []PartWrapper{},
		CreatedAt: time.Now(),
	}
}

// Task represents an A2A v0.3 task
type Task struct {
	ID            string                 `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	SessionID     string                 `json:"sessionId,omitempty" gorm:"type:uuid;index"`
	State         TaskState              `json:"state" gorm:"type:varchar(20);not null;default:'working'"`
	Messages      []Message              `json:"messages" gorm:"-"` // Stored as JSONB
	MessagesJSON  json.RawMessage        `json:"-" gorm:"column:messages;type:jsonb;not null;default:'[]'"`
	Artifacts     []Artifact             `json:"artifacts,omitempty" gorm:"-"` // Stored as JSONB
	ArtifactsJSON json.RawMessage        `json:"-" gorm:"column:artifacts;type:jsonb;default:'[]'"`
	Metadata      map[string]interface{} `json:"metadata,omitempty" gorm:"-"`
	MetadataJSON  json.RawMessage        `json:"-" gorm:"column:metadata;type:jsonb;default:'{}'"`

	// Agent references
	TargetAgentID string `json:"targetAgentId,omitempty" gorm:"type:uuid;index"`
	SourceAgentID string `json:"sourceAgentId,omitempty" gorm:"type:uuid;index"`

	// History tracking
	History      []TaskHistoryEntry `json:"history,omitempty" gorm:"-"`
	HistoryJSON  json.RawMessage    `json:"-" gorm:"column:history;type:jsonb;default:'[]'"`

	// Timestamps
	CreatedAt time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

// TableName returns the table name for GORM
func (t *Task) TableName() string {
	return "a2a_tasks"
}

// TaskHistoryEntry represents a state transition in task history
type TaskHistoryEntry struct {
	State     TaskState `json:"state"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message,omitempty"`
}

// NewTask creates a new task with generated ID
func NewTask(sessionID, targetAgentID, sourceAgentID string) *Task {
	return &Task{
		ID:            uuid.New().String(),
		SessionID:     sessionID,
		State:         TaskStateWorking,
		TargetAgentID: targetAgentID,
		SourceAgentID: sourceAgentID,
		Messages:      []Message{},
		Artifacts:     []Artifact{},
		Metadata:      make(map[string]interface{}),
		History: []TaskHistoryEntry{
			{State: TaskStateWorking, Timestamp: time.Now(), Message: "Task created"},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// BeforeSave serializes nested structs to JSON before saving
func (t *Task) BeforeSave() error {
	var err error

	t.MessagesJSON, err = json.Marshal(t.Messages)
	if err != nil {
		return err
	}

	t.ArtifactsJSON, err = json.Marshal(t.Artifacts)
	if err != nil {
		return err
	}

	t.MetadataJSON, err = json.Marshal(t.Metadata)
	if err != nil {
		return err
	}

	t.HistoryJSON, err = json.Marshal(t.History)
	if err != nil {
		return err
	}

	return nil
}

// AfterFind deserializes JSON fields after loading
func (t *Task) AfterFind() error {
	if len(t.MessagesJSON) > 0 {
		if err := json.Unmarshal(t.MessagesJSON, &t.Messages); err != nil {
			return err
		}
	}

	if len(t.ArtifactsJSON) > 0 {
		if err := json.Unmarshal(t.ArtifactsJSON, &t.Artifacts); err != nil {
			return err
		}
	}

	if len(t.MetadataJSON) > 0 {
		if err := json.Unmarshal(t.MetadataJSON, &t.Metadata); err != nil {
			return err
		}
	}

	if len(t.HistoryJSON) > 0 {
		if err := json.Unmarshal(t.HistoryJSON, &t.History); err != nil {
			return err
		}
	}

	return nil
}

// AddMessage adds a message to the task
func (t *Task) AddMessage(msg Message) {
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	t.Messages = append(t.Messages, msg)
	t.UpdatedAt = time.Now()
}

// AddArtifact adds an artifact to the task
func (t *Task) AddArtifact(artifact Artifact) {
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = time.Now()
	}
	t.Artifacts = append(t.Artifacts, artifact)
	t.UpdatedAt = time.Now()
}

// SetState transitions the task to a new state
func (t *Task) SetState(state TaskState, message string) error {
	if !state.IsValid() {
		return ErrInvalidTaskState
	}

	// Don't allow transitions from terminal states
	if t.State.IsTerminal() {
		return ErrTaskStateTerminal
	}

	t.State = state
	t.History = append(t.History, TaskHistoryEntry{
		State:     state,
		Timestamp: time.Now(),
		Message:   message,
	})
	t.UpdatedAt = time.Now()
	return nil
}

// SetMetadata sets a metadata value
func (t *Task) SetMetadata(key string, value interface{}) {
	if t.Metadata == nil {
		t.Metadata = make(map[string]interface{})
	}
	t.Metadata[key] = value
	t.UpdatedAt = time.Now()
}

// SendTaskRequest represents a request to send a task
type SendTaskRequest struct {
	ID            string                 `json:"id,omitempty"` // Optional, generated if not provided
	SessionID     string                 `json:"sessionId,omitempty"`
	Message       Message                `json:"message"`
	TargetAgentID string                 `json:"targetAgentId,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	PushConfig    *PushNotificationConfig `json:"pushNotification,omitempty"`
}

// SendTaskResponse represents the response to a send task request
type SendTaskResponse struct {
	Task    *Task  `json:"task,omitempty"`
	Error   *TaskError `json:"error,omitempty"`
}

// TaskError represents an error in task processing
type TaskError struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// Common error codes
const (
	ErrorCodeTaskNotFound      = "TASK_NOT_FOUND"
	ErrorCodeInvalidRequest    = "INVALID_REQUEST"
	ErrorCodeUnauthorized      = "UNAUTHORIZED"
	ErrorCodeAgentUnavailable  = "AGENT_UNAVAILABLE"
	ErrorCodeTaskCancelled     = "TASK_CANCELLED"
	ErrorCodeInternalError     = "INTERNAL_ERROR"
	ErrorCodeRateLimited       = "RATE_LIMITED"
	ErrorCodeInputRequired     = "INPUT_REQUIRED"
)

// ListTasksRequest represents a request to list tasks
type ListTasksRequest struct {
	SessionID string     `json:"sessionId,omitempty"`
	State     *TaskState `json:"state,omitempty"`
	Page      int        `json:"page,omitempty"`
	PageSize  int        `json:"pageSize,omitempty"`
}

// ListTasksResponse represents a response with a list of tasks
type ListTasksResponse struct {
	Tasks      []Task `json:"tasks"`
	TotalCount int    `json:"totalCount"`
	Page       int    `json:"page"`
	PageSize   int    `json:"pageSize"`
}

// CancelTaskRequest represents a request to cancel a task
type CancelTaskRequest struct {
	TaskID  string `json:"taskId"`
	Reason  string `json:"reason,omitempty"`
}

// PushNotificationConfig represents push notification configuration for a task
type PushNotificationConfig struct {
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers,omitempty"`
	Events        []string          `json:"events,omitempty"` // e.g., "state_change", "message", "artifact"
	Authentication *AuthConfig      `json:"authentication,omitempty"`
}

// AuthConfig represents authentication for push notifications
type AuthConfig struct {
	Type   string            `json:"type"` // "bearer", "api_key", "basic"
	Token  string            `json:"token,omitempty"`
	Header string            `json:"header,omitempty"` // For api_key type
	Credentials map[string]string `json:"credentials,omitempty"` // For basic auth
}

// StreamEvent represents a server-sent event for task streaming
type StreamEvent struct {
	ID        string          `json:"id"`
	Event     string          `json:"event"` // "state", "message", "artifact", "error", "done"
	Data      json.RawMessage `json:"data"`
	Timestamp time.Time       `json:"timestamp"`
}

// StreamEventType constants
const (
	StreamEventState    = "state"
	StreamEventMessage  = "message"
	StreamEventArtifact = "artifact"
	StreamEventError    = "error"
	StreamEventDone     = "done"
)
