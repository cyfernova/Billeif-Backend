package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// A2ATaskService handles A2A v0.3 task operations
type A2ATaskService struct {
	db              *gorm.DB
	log             *logger.Logger
	pushService     *A2APushService
	subscribers     map[string][]chan<- a2a.StreamEvent
	subscriberMutex sync.RWMutex
	taskProcessors  map[string]TaskProcessor
}

// TaskProcessor defines the interface for task processing
type TaskProcessor interface {
	Process(ctx context.Context, task *a2a.Task, message a2a.Message) error
}

// NewA2ATaskService creates a new A2A task service
func NewA2ATaskService(db *gorm.DB, log *logger.Logger, pushService *A2APushService) *A2ATaskService {
	return &A2ATaskService{
		db:             db,
		log:            log,
		pushService:    pushService,
		subscribers:    make(map[string][]chan<- a2a.StreamEvent),
		taskProcessors: make(map[string]TaskProcessor),
	}
}

// RegisterProcessor registers a task processor for a specific task type
func (s *A2ATaskService) RegisterProcessor(taskType string, processor TaskProcessor) {
	s.taskProcessors[taskType] = processor
}

// SendTask creates and processes a task
func (s *A2ATaskService) SendTask(ctx context.Context, req *a2a.SendTaskRequest, sourceAgentID, targetAgentID string) (*a2a.Task, error) {
	// Generate task ID if not provided
	taskID := req.ID
	if taskID == "" {
		taskID = uuid.New().String()
	}

	// Create or retrieve session
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	// Check if this is a continuation of an existing task
	var task *a2a.Task
	if req.ID != "" {
		existingTask, err := s.GetTask(ctx, req.ID)
		if err == nil {
			task = existingTask
		}
	}

	// Create new task if not found
	if task == nil {
		task = a2a.NewTask(sessionID, targetAgentID, sourceAgentID)
		task.ID = taskID

		// Copy metadata from request
		if req.Metadata != nil {
			task.Metadata = req.Metadata
		}

		// Configure push notifications if provided
		if req.PushConfig != nil {
			task.SetMetadata("push_config", req.PushConfig)
		}
	}

	// Add the message to the task
	task.AddMessage(req.Message)

	// Process the task
	if err := s.processTask(ctx, task); err != nil {
		if stateErr := task.SetState(a2a.TaskStateFailed, err.Error()); stateErr != nil {
			s.log.Error("failed to set task state", "task_id", task.ID, "error", stateErr)
		}
	}

	// Save task to database
	if err := s.saveTask(ctx, task); err != nil {
		s.log.Error("failed to save task", "task_id", task.ID, "error", err)
		return nil, fmt.Errorf("failed to save task: %w", err)
	}

	// Send push notification for state change
	s.notifyStateChange(ctx, task)

	return task, nil
}

// StreamTask creates a task and streams responses
func (s *A2ATaskService) StreamTask(ctx context.Context, req *a2a.SendTaskRequest, sourceAgentID, targetAgentID string, events chan<- a2a.StreamEvent) error {
	// Generate task ID if not provided
	taskID := req.ID
	if taskID == "" {
		taskID = uuid.New().String()
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	// Create new task
	task := a2a.NewTask(sessionID, targetAgentID, sourceAgentID)
	task.ID = taskID

	if req.Metadata != nil {
		task.Metadata = req.Metadata
	}

	// Add the initial message
	task.AddMessage(req.Message)

	// Send initial state event
	s.sendStateEvent(events, task)

	// Process task with streaming
	err := s.processTaskWithStreaming(ctx, task, events)
	if err != nil {
		if stateErr := task.SetState(a2a.TaskStateFailed, err.Error()); stateErr != nil {
			s.log.Error("failed to set task state", "task_id", task.ID, "error", stateErr)
		}
		s.sendStateEvent(events, task)
	}

	// Save task
	if saveErr := s.saveTask(ctx, task); saveErr != nil {
		s.log.Error("failed to save streamed task", "task_id", task.ID, "error", saveErr)
	}

	// Send done event
	doneData, _ := json.Marshal(map[string]interface{}{
		"task_id": task.ID,
		"state":   task.State,
	})
	events <- a2a.StreamEvent{
		ID:        uuid.New().String(),
		Event:     a2a.StreamEventDone,
		Data:      doneData,
		Timestamp: time.Now(),
	}

	return err
}

// GetTask retrieves a task by ID
func (s *A2ATaskService) GetTask(ctx context.Context, taskID string) (*a2a.Task, error) {
	var task a2a.Task
	result := s.db.WithContext(ctx).Where("id = ?", taskID).First(&task)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, a2a.ErrTaskNotFound
		}
		return nil, result.Error
	}

	// Deserialize JSONB fields
	if err := task.AfterFind(); err != nil {
		return nil, err
	}

	return &task, nil
}

// ListTasks returns a paginated list of tasks
func (s *A2ATaskService) ListTasks(ctx context.Context, req *a2a.ListTasksRequest, userID, agentID string) (*a2a.ListTasksResponse, error) {
	query := s.db.WithContext(ctx).Model(&a2a.Task{})

	// Apply filters
	if req.SessionID != "" {
		query = query.Where("session_id = ?", req.SessionID)
	}
	if req.State != nil {
		query = query.Where("state = ?", *req.State)
	}
	if agentID != "" {
		query = query.Where("source_agent_id = ? OR target_agent_id = ?", agentID, agentID)
	}

	// Get total count
	var totalCount int64
	query.Count(&totalCount)

	// Apply pagination
	offset := (req.Page - 1) * req.PageSize
	query = query.Offset(offset).Limit(req.PageSize).Order("created_at DESC")

	// Execute query
	var tasks []a2a.Task
	if err := query.Find(&tasks).Error; err != nil {
		return nil, err
	}

	// Deserialize JSONB fields for each task
	for i := range tasks {
		if err := tasks[i].AfterFind(); err != nil {
			s.log.Warn("failed to deserialize task fields", "task_id", tasks[i].ID, "error", err)
		}
	}

	return &a2a.ListTasksResponse{
		Tasks:      tasks,
		TotalCount: int(totalCount),
		Page:       req.Page,
		PageSize:   req.PageSize,
	}, nil
}

// CancelTask cancels a running task
func (s *A2ATaskService) CancelTask(ctx context.Context, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	task, err := s.GetTask(ctx, req.TaskID)
	if err != nil {
		return nil, err
	}

	// Set state to cancelled
	reason := req.Reason
	if reason == "" {
		reason = "Cancelled by user request"
	}
	if err := task.SetState(a2a.TaskStateCancelled, reason); err != nil {
		return nil, err
	}

	// Save task
	if err := s.saveTask(ctx, task); err != nil {
		return nil, err
	}

	// Notify subscribers
	s.notifyStateChange(ctx, task)

	return task, nil
}

// SubscribeTask subscribes to real-time updates for a task
func (s *A2ATaskService) SubscribeTask(ctx context.Context, taskID string, events chan<- a2a.StreamEvent) error {
	// Add subscriber
	s.subscriberMutex.Lock()
	s.subscribers[taskID] = append(s.subscribers[taskID], events)
	s.subscriberMutex.Unlock()

	// Remove subscriber on context done
	go func() {
		<-ctx.Done()
		s.subscriberMutex.Lock()
		subs := s.subscribers[taskID]
		for i, sub := range subs {
			if sub == events {
				s.subscribers[taskID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		s.subscriberMutex.Unlock()
	}()

	// Send current state
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	s.sendStateEvent(events, task)

	// Keep connection open until context is done or task completes
	<-ctx.Done()
	return nil
}

// processTask processes a task based on its content
func (s *A2ATaskService) processTask(ctx context.Context, task *a2a.Task) error {
	// Extract task type from metadata or first message
	taskType := ""
	if t, ok := task.Metadata["task_type"].(string); ok {
		taskType = t
	}

	// Use registered processor if available
	if processor, ok := s.taskProcessors[taskType]; ok {
		if len(task.Messages) > 0 {
			return processor.Process(ctx, task, task.Messages[len(task.Messages)-1])
		}
	}

	// Default processing: simple echo response
	if len(task.Messages) > 0 {
		lastMsg := task.Messages[len(task.Messages)-1]
		if lastMsg.Role == a2a.MessageRoleUser {
			// Create agent response
			responseText := fmt.Sprintf("Received your message. Task %s is being processed.", task.ID)
			task.AddMessage(a2a.NewTextMessage(a2a.MessageRoleAgent, responseText))
			if stateErr := task.SetState(a2a.TaskStateCompleted, "Task completed successfully"); stateErr != nil {
				s.log.Error("failed to set task state", "task_id", task.ID, "error", stateErr)
			}
		}
	}

	return nil
}

// processTaskWithStreaming processes a task and emits streaming events
func (s *A2ATaskService) processTaskWithStreaming(ctx context.Context, task *a2a.Task, events chan<- a2a.StreamEvent) error {
	// Emit message event for the initial message
	if len(task.Messages) > 0 {
		msgData, _ := json.Marshal(task.Messages[len(task.Messages)-1])
		events <- a2a.StreamEvent{
			ID:        uuid.New().String(),
			Event:     a2a.StreamEventMessage,
			Data:      msgData,
			Timestamp: time.Now(),
		}
	}

	// Simulate progressive processing with state updates
	// In production, this would be actual task processing

	// Update to working state
	if stateErr := task.SetState(a2a.TaskStateWorking, "Processing task"); stateErr != nil {
		s.log.Error("failed to set task state", "task_id", task.ID, "error", stateErr)
	}
	s.sendStateEvent(events, task)

	// Simulate processing with response messages
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(100 * time.Millisecond):
		// Continue processing
	}

	// Create agent response
	responseText := fmt.Sprintf("Task %s processed successfully.", task.ID)
	response := a2a.NewTextMessage(a2a.MessageRoleAgent, responseText)
	task.AddMessage(response)

	// Emit message event
	msgData, _ := json.Marshal(response)
	events <- a2a.StreamEvent{
		ID:        uuid.New().String(),
		Event:     a2a.StreamEventMessage,
		Data:      msgData,
		Timestamp: time.Now(),
	}

	// Create artifact if applicable
	artifact := a2a.NewArtifact("result", "application/json")
	artifact.Parts = []a2a.PartWrapper{
		{Part: a2a.DataPart{
			Type:     a2a.PartTypeData,
			MimeType: "application/json",
			Data: map[string]interface{}{
				"status":  "success",
				"task_id": task.ID,
			},
		}},
	}
	task.AddArtifact(*artifact)

	// Emit artifact event
	artifactData, _ := json.Marshal(artifact)
	events <- a2a.StreamEvent{
		ID:        uuid.New().String(),
		Event:     a2a.StreamEventArtifact,
		Data:      artifactData,
		Timestamp: time.Now(),
	}

	// Complete task
	if stateErr := task.SetState(a2a.TaskStateCompleted, "Task completed successfully"); stateErr != nil {
		s.log.Error("failed to set task state", "task_id", task.ID, "error", stateErr)
	}
	s.sendStateEvent(events, task)

	return nil
}

// saveTask saves a task to the database
func (s *A2ATaskService) saveTask(ctx context.Context, task *a2a.Task) error {
	// Serialize JSONB fields
	if err := task.BeforeSave(); err != nil {
		return err
	}

	// Upsert task
	result := s.db.WithContext(ctx).Save(task)
	return result.Error
}

// sendStateEvent sends a state change event
func (s *A2ATaskService) sendStateEvent(events chan<- a2a.StreamEvent, task *a2a.Task) {
	stateData, _ := json.Marshal(map[string]interface{}{
		"task_id": task.ID,
		"state":   task.State,
		"history": task.History,
	})

	events <- a2a.StreamEvent{
		ID:        uuid.New().String(),
		Event:     a2a.StreamEventState,
		Data:      stateData,
		Timestamp: time.Now(),
	}
}

// notifyStateChange sends notifications for task state changes
func (s *A2ATaskService) notifyStateChange(ctx context.Context, task *a2a.Task) {
	// Notify SSE subscribers
	s.subscriberMutex.RLock()
	subs := s.subscribers[task.ID]
	s.subscriberMutex.RUnlock()

	stateData, _ := json.Marshal(map[string]interface{}{
		"task_id": task.ID,
		"state":   task.State,
	})

	event := a2a.StreamEvent{
		ID:        uuid.New().String(),
		Event:     a2a.StreamEventState,
		Data:      stateData,
		Timestamp: time.Now(),
	}

	for _, sub := range subs {
		select {
		case sub <- event:
		default:
			// Channel full, skip
		}
	}

	// Send push notification if configured
	if s.pushService != nil {
		if pushConfig, ok := task.Metadata["push_config"].(*a2a.PushNotificationConfig); ok {
			go func() {
				if err := s.pushService.SendNotification(ctx, pushConfig, "state_change", event); err != nil {
					s.log.Error("failed to send push notification", "task_id", task.ID, "error", err)
				}
			}()
		}
	}
}
