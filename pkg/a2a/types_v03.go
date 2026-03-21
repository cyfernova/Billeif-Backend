package a2a

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	SupportedVersion         = "1.0"
	HeaderVersion            = "A2A-Version"
	HeaderExtensions         = "A2A-Extensions"
	HeaderNotificationToken  = "X-A2A-Notification-Token"
	ContentTypeJSON          = "application/json"
	ContentTypeA2AJSON       = "application/a2a+json"
	ContentTypeProblemJSON   = "application/problem+json"
	ContentTypeEventStream   = "text/event-stream"
	ProtocolBindingHTTPJSON  = "HTTP+JSON"
	ProtocolBindingGRPC      = "GRPC"
	ProtocolBindingJSONRPC   = "JSONRPC"
	DefaultListPageSize      = 50
	MaxListPageSize          = 100
	DefaultPushListPageSize  = 50
	MaxPushListPageSize      = 100
	statusMessageMetadataKey = "status_message"
)

type Role string

const (
	RoleUnspecified Role = "ROLE_UNSPECIFIED"
	RoleUser        Role = "ROLE_USER"
	RoleAgent       Role = "ROLE_AGENT"
)

type TaskState string

const (
	TaskStateUnspecified   TaskState = "TASK_STATE_UNSPECIFIED"
	TaskStateSubmitted     TaskState = "TASK_STATE_SUBMITTED"
	TaskStateWorking       TaskState = "TASK_STATE_WORKING"
	TaskStateCompleted     TaskState = "TASK_STATE_COMPLETED"
	TaskStateFailed        TaskState = "TASK_STATE_FAILED"
	TaskStateCancelled     TaskState = "TASK_STATE_CANCELLED"
	TaskStateInputRequired TaskState = "TASK_STATE_INPUT_REQUIRED"
	TaskStateRejected      TaskState = "TASK_STATE_REJECTED"
	TaskStateAuthRequired  TaskState = "TASK_STATE_AUTH_REQUIRED"
)

func (s TaskState) IsValid() bool {
	switch s {
	case TaskStateUnspecified,
		TaskStateSubmitted,
		TaskStateWorking,
		TaskStateCompleted,
		TaskStateFailed,
		TaskStateCancelled,
		TaskStateInputRequired,
		TaskStateRejected,
		TaskStateAuthRequired:
		return true
	default:
		return false
	}
}

func (s TaskState) IsTerminal() bool {
	switch s {
	case TaskStateCompleted, TaskStateFailed, TaskStateCancelled, TaskStateRejected:
		return true
	default:
		return false
	}
}

type Part struct {
	Text      string                 `json:"text,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Raw       string                 `json:"raw,omitempty"`
	URL       string                 `json:"url,omitempty"`
	Filename  string                 `json:"filename,omitempty"`
	MediaType string                 `json:"mediaType,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

func (p Part) IsZero() bool {
	return p.Text == "" && len(p.Data) == 0 && p.Raw == "" && p.URL == "" && p.Filename == "" && p.MediaType == "" && len(p.Metadata) == 0
}

type Message struct {
	MessageID        string                 `json:"messageId"`
	ContextID        string                 `json:"contextId,omitempty"`
	TaskID           string                 `json:"taskId,omitempty"`
	Role             Role                   `json:"role"`
	Parts            []Part                 `json:"parts"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	Extensions       []string               `json:"extensions,omitempty"`
	ReferenceTaskIDs []string               `json:"referenceTaskIds,omitempty"`
}

func NewTextMessage(role Role, text string) Message {
	return Message{
		MessageID: uuid.NewString(),
		Role:      role,
		Parts: []Part{
			{Text: text},
		},
	}
}

func (m *Message) EnsureID() {
	if strings.TrimSpace(m.MessageID) == "" {
		m.MessageID = uuid.NewString()
	}
}

func (m Message) Clone() Message {
	clone := m
	if len(m.Parts) > 0 {
		clone.Parts = append([]Part(nil), m.Parts...)
	}
	if len(m.Extensions) > 0 {
		clone.Extensions = append([]string(nil), m.Extensions...)
	}
	if len(m.ReferenceTaskIDs) > 0 {
		clone.ReferenceTaskIDs = append([]string(nil), m.ReferenceTaskIDs...)
	}
	if len(m.Metadata) > 0 {
		clone.Metadata = cloneMap(m.Metadata)
	}
	return clone
}

type Artifact struct {
	ArtifactID  string                 `json:"artifactId"`
	Name        string                 `json:"name,omitempty"`
	Description string                 `json:"description,omitempty"`
	Parts       []Part                 `json:"parts"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Extensions  []string               `json:"extensions,omitempty"`
}

func NewDataArtifact(name string, data map[string]interface{}) Artifact {
	return Artifact{
		ArtifactID: uuid.NewString(),
		Name:       name,
		Parts: []Part{
			{
				Data:      cloneMap(data),
				MediaType: "application/json",
			},
		},
	}
}

type TaskStatus struct {
	State     TaskState `json:"state"`
	Message   *Message  `json:"message,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
}

type Task struct {
	ID            string                 `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	ContextID     string                 `json:"contextId" gorm:"column:session_id;type:uuid;index"`
	Status        TaskStatus             `json:"status" gorm:"-"`
	State         TaskState              `json:"-" gorm:"column:state;type:varchar(32);not null;default:'TASK_STATE_SUBMITTED'"`
	Artifacts     []Artifact             `json:"artifacts,omitempty" gorm:"-"`
	ArtifactsJSON json.RawMessage        `json:"-" gorm:"column:artifacts;type:jsonb;default:'[]'"`
	History       []Message              `json:"history,omitempty" gorm:"-"`
	HistoryJSON   json.RawMessage        `json:"-" gorm:"column:messages;type:jsonb;not null;default:'[]'"`
	Metadata      map[string]interface{} `json:"metadata,omitempty" gorm:"-"`
	MetadataJSON  json.RawMessage        `json:"-" gorm:"column:metadata;type:jsonb;default:'{}'"`
	UserID        string                 `json:"-" gorm:"column:user_id;type:varchar(255);index"`
	BusinessID    string                 `json:"-" gorm:"column:business_id;type:varchar(255);index"`
	CreatedAt     time.Time              `json:"-" gorm:"autoCreateTime"`
	UpdatedAt     time.Time              `json:"-" gorm:"autoUpdateTime"`
}

func (Task) TableName() string {
	return "a2a_tasks"
}

func NewTask(contextID, userID, businessID string) *Task {
	now := time.Now().UTC()
	if strings.TrimSpace(contextID) == "" {
		contextID = uuid.NewString()
	}
	task := &Task{
		ID:         uuid.NewString(),
		ContextID:  contextID,
		State:      TaskStateSubmitted,
		Artifacts:  []Artifact{},
		History:    []Message{},
		Metadata:   map[string]interface{}{},
		UserID:     userID,
		BusinessID: businessID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	task.refreshStatus()
	return task
}

func (t *Task) PrepareForSave() error {
	if err := t.refreshStatusMessageMetadata(); err != nil {
		return err
	}

	var err error
	t.HistoryJSON, err = json.Marshal(t.History)
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
	return nil
}

func (t *Task) HydrateFromStorage() error {
	if len(t.HistoryJSON) > 0 {
		if err := json.Unmarshal(t.HistoryJSON, &t.History); err != nil {
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
	t.refreshStatus()
	return nil
}

func (t *Task) AddHistory(message Message) {
	message.EnsureID()
	if message.ContextID == "" {
		message.ContextID = t.ContextID
	}
	if message.TaskID == "" {
		message.TaskID = t.ID
	}
	t.History = append(t.History, message)
	t.UpdatedAt = time.Now().UTC()
}

func (t *Task) AddArtifact(artifact Artifact) {
	if strings.TrimSpace(artifact.ArtifactID) == "" {
		artifact.ArtifactID = uuid.NewString()
	}
	t.Artifacts = append(t.Artifacts, artifact)
	t.UpdatedAt = time.Now().UTC()
}

func (t *Task) SetState(state TaskState, statusMessage *Message) error {
	if !state.IsValid() {
		return ErrInvalidTaskState
	}
	if t.State.IsTerminal() {
		return ErrTaskStateTerminal
	}
	t.State = state
	t.Status = TaskStatus{
		State:     state,
		Timestamp: time.Now().UTC(),
	}
	if statusMessage != nil {
		statusMessage.EnsureID()
		msg := statusMessage.Clone()
		if msg.ContextID == "" {
			msg.ContextID = t.ContextID
		}
		if msg.TaskID == "" {
			msg.TaskID = t.ID
		}
		t.Status.Message = &msg
	}
	t.UpdatedAt = t.Status.Timestamp
	return nil
}

func (t *Task) refreshStatus() {
	t.Status = TaskStatus{
		State:     t.State,
		Timestamp: t.UpdatedAt.UTC(),
	}
	if t.Status.Timestamp.IsZero() {
		t.Status.Timestamp = t.CreatedAt.UTC()
	}
	if len(t.Metadata) == 0 {
		return
	}
	raw, ok := t.Metadata[statusMessageMetadataKey]
	if !ok {
		return
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return
	}
	var statusMessage Message
	if err := json.Unmarshal(payload, &statusMessage); err != nil {
		return
	}
	t.Status.Message = &statusMessage
}

func (t *Task) refreshStatusMessageMetadata() error {
	if t.Metadata == nil {
		t.Metadata = map[string]interface{}{}
	}
	if t.Status.Message == nil {
		delete(t.Metadata, statusMessageMetadataKey)
		t.refreshStatus()
		return nil
	}
	payload, err := json.Marshal(t.Status.Message)
	if err != nil {
		return err
	}
	var value map[string]interface{}
	if err := json.Unmarshal(payload, &value); err != nil {
		return err
	}
	t.Metadata[statusMessageMetadataKey] = value
	t.refreshStatus()
	return nil
}

func (t *Task) Clone(historyLength *int, includeArtifacts bool) *Task {
	clone := *t
	if len(t.Metadata) > 0 {
		clone.Metadata = cloneMap(t.Metadata)
	}
	if includeArtifacts && len(t.Artifacts) > 0 {
		clone.Artifacts = append([]Artifact(nil), t.Artifacts...)
	} else {
		clone.Artifacts = nil
	}

	switch {
	case historyLength == nil:
		clone.History = append([]Message(nil), t.History...)
	case *historyLength <= 0:
		clone.History = nil
	case len(t.History) <= *historyLength:
		clone.History = append([]Message(nil), t.History...)
	default:
		start := len(t.History) - *historyLength
		clone.History = append([]Message(nil), t.History[start:]...)
	}

	clone.refreshStatus()
	return &clone
}

type SendMessageConfiguration struct {
	AcceptedOutputModes        []string                `json:"acceptedOutputModes,omitempty"`
	TaskPushNotificationConfig *PushNotificationConfig `json:"pushNotificationConfig,omitempty"`
	HistoryLength              *int                    `json:"historyLength,omitempty"`
	Blocking                   bool                    `json:"blocking,omitempty"`
}

type SendMessageRequest struct {
	Message       Message                   `json:"message"`
	Configuration *SendMessageConfiguration `json:"configuration,omitempty"`
	Metadata      map[string]interface{}    `json:"metadata,omitempty"`
}

type SendMessageResponse struct {
	Task    *Task    `json:"task,omitempty"`
	Message *Message `json:"message,omitempty"`
}

type StreamResponse struct {
	Task           *Task                    `json:"task,omitempty"`
	Message        *Message                 `json:"message,omitempty"`
	StatusUpdate   *TaskStatusUpdateEvent   `json:"statusUpdate,omitempty"`
	ArtifactUpdate *TaskArtifactUpdateEvent `json:"artifactUpdate,omitempty"`
}

type TaskStatusUpdateEvent struct {
	TaskID    string                 `json:"taskId"`
	ContextID string                 `json:"contextId"`
	Status    TaskStatus             `json:"status"`
	Final     bool                   `json:"final"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type TaskArtifactUpdateEvent struct {
	TaskID    string                 `json:"taskId"`
	ContextID string                 `json:"contextId"`
	Artifact  Artifact               `json:"artifact"`
	Append    bool                   `json:"append,omitempty"`
	LastChunk bool                   `json:"lastChunk,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type GetTaskRequest struct {
	Name          string `json:"name"`
	HistoryLength *int   `json:"historyLength,omitempty"`
}

type ListTasksRequest struct {
	ContextID            string     `json:"contextId,omitempty"`
	Status               TaskState  `json:"status,omitempty"`
	PageSize             *int       `json:"pageSize,omitempty"`
	PageToken            string     `json:"pageToken,omitempty"`
	HistoryLength        *int       `json:"historyLength,omitempty"`
	StatusTimestampAfter *time.Time `json:"statusTimestampAfter,omitempty"`
	IncludeArtifacts     *bool      `json:"includeArtifacts,omitempty"`
}

type ListTasksResponse struct {
	Tasks         []Task `json:"tasks"`
	NextPageToken string `json:"nextPageToken"`
	PageSize      int    `json:"pageSize"`
	TotalSize     int    `json:"totalSize"`
}

type CancelTaskRequest struct {
	Name string `json:"name"`
}

type PushNotificationConfig struct {
	ID             string              `json:"id,omitempty"`
	URL            string              `json:"url"`
	Token          string              `json:"token,omitempty"`
	Authentication *AuthenticationInfo `json:"authentication,omitempty"`
}

type AuthenticationInfo struct {
	Schemes     []string `json:"schemes"`
	Credentials string   `json:"credentials,omitempty"`
}

type TaskPushNotificationConfig struct {
	Name                   string                 `json:"name"`
	PushNotificationConfig PushNotificationConfig `json:"pushNotificationConfig"`
}

type CreateTaskPushNotificationConfigRequest struct {
	Parent   string                     `json:"parent"`
	ConfigID string                     `json:"configId"`
	Config   TaskPushNotificationConfig `json:"config"`
}

type GetTaskPushNotificationConfigRequest struct {
	Name string `json:"name"`
}

type DeleteTaskPushNotificationConfigRequest struct {
	Name string `json:"name"`
}

type ListTaskPushNotificationConfigRequest struct {
	Parent    string `json:"parent"`
	PageSize  int    `json:"pageSize,omitempty"`
	PageToken string `json:"pageToken,omitempty"`
}

type ListTaskPushNotificationConfigResponse struct {
	Configs       []TaskPushNotificationConfig `json:"configs"`
	NextPageToken string                       `json:"nextPageToken,omitempty"`
}

type StreamEvent struct {
	ID        string          `json:"-"`
	Event     string          `json:"-"`
	Data      json.RawMessage `json:"-"`
	Timestamp time.Time       `json:"-"`
	Final     bool            `json:"-"`
}

const (
	StreamEventTask         = "task"
	StreamEventMessage      = "message"
	StreamEventStatusUpdate = "status-update"
	StreamEventArtifact     = "artifact-update"
	StreamEventError        = "error"
)

func MarshalStreamResponse(response StreamResponse) (json.RawMessage, error) {
	data, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func TaskName(taskID string) string {
	return "tasks/" + taskID
}

func TaskPushConfigName(taskID, configID string) string {
	return fmt.Sprintf("tasks/%s/pushNotificationConfigs/%s", taskID, configID)
}

func ParseTaskName(name string) (string, error) {
	parts := strings.Split(strings.Trim(name, "/"), "/")
	if len(parts) != 2 || parts[0] != "tasks" || strings.TrimSpace(parts[1]) == "" {
		return "", fmt.Errorf("invalid task resource name %q", name)
	}
	return parts[1], nil
}

func ParseTaskPushConfigName(name string) (string, string, error) {
	parts := strings.Split(strings.Trim(name, "/"), "/")
	if len(parts) != 4 || parts[0] != "tasks" || parts[2] != "pushNotificationConfigs" || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[3]) == "" {
		return "", "", fmt.Errorf("invalid push config resource name %q", name)
	}
	return parts[1], parts[3], nil
}

func EncodePageToken(createdAt time.Time, id string) string {
	raw := fmt.Sprintf("%d|%s", createdAt.UTC().UnixNano(), id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func DecodePageToken(token string) (time.Time, string, error) {
	if strings.TrimSpace(token) == "" {
		return time.Time{}, "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("decode page token: %w", err)
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("invalid page token")
	}
	nanos, err := time.ParseDuration(parts[0] + "ns")
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid page token timestamp: %w", err)
	}
	return time.Unix(0, nanos.Nanoseconds()).UTC(), parts[1], nil
}

func cloneMap(src map[string]interface{}) map[string]interface{} {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func ValidateSendMessageRequest(req *SendMessageRequest) error {
	if req == nil {
		return ErrInvalidMessage
	}
	return ValidateMessage(req.Message)
}

func ValidateMessage(message Message) error {
	if strings.TrimSpace(message.MessageID) == "" {
		return fmt.Errorf("%w: messageId is required", ErrInvalidMessage)
	}
	if message.Role != RoleUser && message.Role != RoleAgent {
		return fmt.Errorf("%w: role must be ROLE_USER or ROLE_AGENT", ErrInvalidMessage)
	}
	if len(message.Parts) == 0 {
		return fmt.Errorf("%w: at least one message part is required", ErrInvalidMessage)
	}
	for _, part := range message.Parts {
		if part.IsZero() {
			return fmt.Errorf("%w: empty message part", ErrInvalidMessage)
		}
	}
	return nil
}
