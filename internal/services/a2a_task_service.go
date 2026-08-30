package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/ap2"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type A2ATaskService struct {
	db                *gorm.DB
	log               *logger.Logger
	pushService       *A2APushService
	ap2Repo           interfaces.AP2Repository
	merchantSvc       *MerchantAgentService
	sellerNegotiation *SellerNegotiationService
	signer            *ap2.SignatureService
}

func NewA2ATaskService(db *gorm.DB, log *logger.Logger, pushService *A2APushService) *A2ATaskService {
	return &A2ATaskService{
		db:          db,
		log:         log,
		pushService: pushService,
	}
}

func (s *A2ATaskService) ConfigureDomainServices(ap2Repo interfaces.AP2Repository, merchantSvc *MerchantAgentService, sellerNegotiation *SellerNegotiationService, signer *ap2.SignatureService) {
	s.ap2Repo = ap2Repo
	s.merchantSvc = merchantSvc
	s.sellerNegotiation = sellerNegotiation
	s.signer = signer
}

func (s *A2ATaskService) SendMessage(ctx context.Context, req *a2a.SendMessageRequest, userID, businessID string) (*a2a.SendMessageResponse, error) {
	if err := a2a.ValidateSendMessageRequest(req); err != nil {
		return nil, err
	}

	task, err := s.loadOrCreateTask(ctx, req, userID, businessID)
	if err != nil {
		return nil, err
	}

	if err := s.saveTask(ctx, task); err != nil {
		return nil, err
	}
	if err := s.ensurePushConfig(ctx, task, req, userID, businessID); err != nil {
		return nil, err
	}

	if err := s.emitTaskSnapshot(ctx, task, nil); err != nil {
		return nil, err
	}
	if err := s.runTask(ctx, task, req, nil); err != nil {
		return nil, err
	}
	if err := s.saveTask(ctx, task); err != nil {
		return nil, err
	}

	historyLength := historyLengthFromConfig(req.Configuration)
	return &a2a.SendMessageResponse{
		Task: task.Clone(historyLength, true),
	}, nil
}

func (s *A2ATaskService) SendStreamingMessage(ctx context.Context, req *a2a.SendMessageRequest, userID, businessID string, events chan<- a2a.StreamEvent) error {
	if err := a2a.ValidateSendMessageRequest(req); err != nil {
		return err
	}

	task, err := s.loadOrCreateTask(ctx, req, userID, businessID)
	if err != nil {
		return err
	}

	if err := s.saveTask(ctx, task); err != nil {
		return err
	}
	if err := s.ensurePushConfig(ctx, task, req, userID, businessID); err != nil {
		return err
	}

	if err := s.emitTaskSnapshot(ctx, task, events); err != nil {
		return err
	}
	if err := s.runTask(ctx, task, req, events); err != nil {
		return err
	}
	return s.saveTask(ctx, task)
}

func (s *A2ATaskService) GetTask(ctx context.Context, taskID, userID, businessID string, historyLength *int) (*a2a.Task, error) {
	task, err := s.getTaskForScope(ctx, taskID, userID, businessID)
	if err != nil {
		return nil, err
	}
	return task.Clone(historyLength, true), nil
}

func (s *A2ATaskService) ListTasks(ctx context.Context, req *a2a.ListTasksRequest, userID, businessID string) (*a2a.ListTasksResponse, error) {
	pageSize := a2a.DefaultListPageSize
	if req.PageSize != nil && *req.PageSize > 0 {
		pageSize = *req.PageSize
	}
	if pageSize > a2a.MaxListPageSize {
		pageSize = a2a.MaxListPageSize
	}

	query := s.db.WithContext(ctx).Model(&a2a.Task{}).Where("user_id = ?", userID)
	if businessID != "" {
		query = query.Where("business_id = ?", businessID)
	}
	if strings.TrimSpace(req.ContextID) != "" {
		query = query.Where("session_id = ?", req.ContextID)
	}
	if req.Status != "" && req.Status != a2a.TaskStateUnspecified {
		query = query.Where("state = ?", req.Status)
	}
	if req.StatusTimestampAfter != nil {
		query = query.Where("updated_at >= ?", req.StatusTimestampAfter.UTC())
	}

	cursorTime, cursorID, err := a2a.DecodePageToken(req.PageToken)
	if err != nil {
		return nil, a2a.ErrInvalidPageToken
	}
	if !cursorTime.IsZero() && cursorID != "" {
		query = query.Where("(created_at < ?) OR (created_at = ? AND id < ?)", cursorTime, cursorTime, cursorID)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count tasks: %w", err)
	}

	var rows []a2a.Task
	if err := query.Order("created_at DESC, id DESC").Limit(pageSize + 1).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}

	nextPageToken := ""
	if len(rows) > pageSize {
		last := rows[pageSize-1]
		nextPageToken = a2a.EncodePageToken(last.CreatedAt, last.ID)
		rows = rows[:pageSize]
	}

	includeArtifacts := req.IncludeArtifacts != nil && *req.IncludeArtifacts
	tasks := make([]a2a.Task, 0, len(rows))
	for i := range rows {
		if err := rows[i].HydrateFromStorage(); err != nil {
			return nil, fmt.Errorf("decode task %s: %w", rows[i].ID, err)
		}
		tasks = append(tasks, *rows[i].Clone(req.HistoryLength, includeArtifacts))
	}

	return &a2a.ListTasksResponse{
		Tasks:         tasks,
		NextPageToken: nextPageToken,
		PageSize:      pageSize,
		TotalSize:     int(total),
	}, nil
}

func (s *A2ATaskService) CancelTask(ctx context.Context, taskID, userID, businessID string) (*a2a.Task, error) {
	task, err := s.getTaskForScope(ctx, taskID, userID, businessID)
	if err != nil {
		return nil, err
	}

	statusMessage := a2a.NewTextMessage(a2a.RoleAgent, "Task cancelled by client request.")
	if err := task.SetState(a2a.TaskStateCancelled, &statusMessage); err != nil {
		return nil, err
	}
	if err := s.saveTask(ctx, task); err != nil {
		return nil, err
	}
	if err := s.emitStatusUpdate(ctx, task, true, nil); err != nil {
		return nil, err
	}
	return task.Clone(nil, true), nil
}

func (s *A2ATaskService) SubscribeTaskWithReplay(ctx context.Context, taskID, userID, businessID, lastEventID string, historyLength *int, includeArtifacts bool, events chan<- a2a.StreamEvent) error {
	task, err := s.getTaskForScope(ctx, taskID, userID, businessID)
	if err != nil {
		return err
	}
	if task.State.IsTerminal() {
		return a2a.ErrUnsupportedOperation
	}

	cursor := lastEventID
	replayed, err := s.loadTaskEventsAfter(ctx, taskID, lastEventID, 500)
	if err != nil {
		return err
	}
	for _, event := range replayed {
		select {
		case events <- event:
			cursor = event.ID
			if event.Final {
				return nil
			}
		case <-ctx.Done():
			return nil
		}
	}

	if len(replayed) == 0 {
		snapshot := task.Clone(historyLength, includeArtifacts)
		data, err := a2a.MarshalStreamResponse(a2a.StreamResponse{Task: snapshot})
		if err != nil {
			return err
		}
		select {
		case events <- a2a.StreamEvent{ID: fmt.Sprintf("bootstrap-%s", task.ID), Event: a2a.StreamEventTask, Data: data, Timestamp: time.Now().UTC()}:
		case <-ctx.Done():
			return nil
		}
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			newEvents, err := s.loadTaskEventsAfter(ctx, taskID, cursor, 200)
			if err != nil {
				return err
			}
			for _, event := range newEvents {
				select {
				case events <- event:
					cursor = event.ID
					if event.Final {
						return nil
					}
				case <-ctx.Done():
					return nil
				}
			}
		}
	}
}

func (s *A2ATaskService) loadOrCreateTask(ctx context.Context, req *a2a.SendMessageRequest, userID, businessID string) (*a2a.Task, error) {
	if strings.TrimSpace(req.Message.TaskID) != "" {
		task, err := s.getTaskForScope(ctx, req.Message.TaskID, userID, businessID)
		if err != nil {
			return nil, err
		}
		if task.State.IsTerminal() {
			return nil, a2a.ErrTaskStateTerminal
		}
		task.AddHistory(req.Message.Clone())
		return task, nil
	}

	task := a2a.NewTask(req.Message.ContextID, userID, businessID)
	task.AddHistory(req.Message.Clone())
	return task, nil
}

func (s *A2ATaskService) getTaskForScope(ctx context.Context, taskID, userID, businessID string) (*a2a.Task, error) {
	var task a2a.Task
	query := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", taskID, userID)
	if businessID != "" {
		query = query.Where("business_id = ?", businessID)
	}
	if err := query.First(&task).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, a2a.ErrTaskNotFound
		}
		return nil, err
	}
	if err := task.HydrateFromStorage(); err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *A2ATaskService) saveTask(ctx context.Context, task *a2a.Task) error {
	if err := task.PrepareForSave(); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Save(task).Error
}

func (s *A2ATaskService) ensurePushConfig(ctx context.Context, task *a2a.Task, req *a2a.SendMessageRequest, userID, businessID string) error {
	if req.Configuration == nil || req.Configuration.TaskPushNotificationConfig == nil {
		return nil
	}
	config := req.Configuration.TaskPushNotificationConfig
	if strings.TrimSpace(config.ID) == "" {
		config.ID = uuidString()
	}
	_, err := s.pushService.CreateTaskPushConfig(ctx, task.ID, userID, businessID, config.ID, config)
	if err != nil {
		return err
	}
	return nil
}

func (s *A2ATaskService) runTask(ctx context.Context, task *a2a.Task, req *a2a.SendMessageRequest, events chan<- a2a.StreamEvent) error {
	workingMessage := a2a.NewTextMessage(a2a.RoleAgent, "Task is being processed.")
	if err := task.SetState(a2a.TaskStateWorking, &workingMessage); err != nil {
		return err
	}
	if err := s.saveTask(ctx, task); err != nil {
		return err
	}
	if err := s.emitStatusUpdate(ctx, task, false, events); err != nil {
		return err
	}

	responseMessage, responseArtifact, err := s.executeTask(ctx, task, req)
	if err != nil {
		return err
	}
	task.AddHistory(responseMessage)
	if err := s.saveTask(ctx, task); err != nil {
		return err
	}
	if err := s.emitMessage(ctx, task, &responseMessage, events); err != nil {
		return err
	}

	if responseArtifact != nil {
		task.AddArtifact(*responseArtifact)
		if err := s.saveTask(ctx, task); err != nil {
			return err
		}
		if err := s.emitArtifact(ctx, task, responseArtifact, events); err != nil {
			return err
		}
	}

	if err := task.SetState(a2a.TaskStateCompleted, &responseMessage); err != nil {
		return err
	}
	if err := s.saveTask(ctx, task); err != nil {
		return err
	}
	return s.emitStatusUpdate(ctx, task, true, events)
}

func (s *A2ATaskService) executeTask(ctx context.Context, task *a2a.Task, req *a2a.SendMessageRequest) (a2a.Message, *a2a.Artifact, error) {
	taskType := metadataString(req.Metadata, "taskType")
	if taskType == "" {
		taskType = metadataString(req.Message.Metadata, "taskType")
	}

	if taskType == "merchant.process_cart" && s.merchantSvc != nil && s.signer != nil {
		cartID := metadataString(req.Metadata, "cartMandateId")
		if cartID == "" {
			cartID = metadataString(req.Message.Metadata, "cartMandateId")
		}
		merchantAgentID := metadataString(req.Metadata, "merchantAgentId")
		if merchantAgentID == "" {
			merchantAgentID = metadataString(req.Message.Metadata, "merchantAgentId")
		}

		if cartID != "" && merchantAgentID != "" {
			ownsMerchant, err := s.ap2Repo.HasAgentOwnership(ctx, task.UserID, merchantAgentID)
			if err != nil {
				return a2a.Message{}, nil, fmt.Errorf("authorize merchant cart task: %w", err)
			}
			if !ownsMerchant {
				return a2a.Message{}, nil, ErrUnauthorized
			}
			if err := s.merchantSvc.RespondToCart(ctx, cartID, merchantAgentID, "signed"); err != nil {
				return a2a.Message{}, nil, fmt.Errorf("process merchant cart task: %w", err)
			}

			message := a2a.NewTextMessage(a2a.RoleAgent, "Merchant cart processing completed and cart signed.")
			message.ContextID = task.ContextID
			message.TaskID = task.ID
			artifact := a2a.NewDataArtifact("result", map[string]interface{}{
				"taskId":          task.ID,
				"contextId":       task.ContextID,
				"status":          "completed",
				"cartMandateId":   cartID,
				"merchantAgentId": merchantAgentID,
				"cartStatus":      "signed",
			})
			return message, &artifact, nil
		}
	}

	if s.sellerNegotiation != nil {
		switch taskType {
		case taskTypeProcurementQuoteRequest,
			taskTypeProcurementNegotiationCounter,
			taskTypeProcurementNegotiationAccept,
			taskTypeProcurementNegotiationReject:
			return s.sellerNegotiation.HandleTask(ctx, taskType, req.Metadata)
		}
	}

	message, artifact := s.buildTaskResult(task, req)
	return message, artifact, nil
}

func (s *A2ATaskService) buildTaskResult(task *a2a.Task, req *a2a.SendMessageRequest) (a2a.Message, *a2a.Artifact) {
	taskType := metadataString(req.Metadata, "taskType")
	if taskType == "" {
		taskType = metadataString(req.Message.Metadata, "taskType")
	}

	lastText := extractMessageText(req.Message)
	result := map[string]interface{}{
		"taskId":    task.ID,
		"contextId": task.ContextID,
		"status":    "completed",
	}

	var text string
	switch taskType {
	case "merchant.process_cart":
		text = "Merchant cart processing completed."
		if cartID := metadataString(req.Metadata, "cartMandateId"); cartID != "" {
			result["cartMandateId"] = cartID
		}
	case "payment.process":
		text = "Payment processing completed."
		if paymentID := metadataString(req.Metadata, "paymentMandateId"); paymentID != "" {
			result["paymentMandateId"] = paymentID
		}
	case "bargaining.notification":
		text = "Bargaining notification received."
	default:
		if lastText == "" {
			text = "Request processed successfully."
		} else {
			text = "Processed request: " + lastText
		}
	}

	message := a2a.NewTextMessage(a2a.RoleAgent, text)
	message.ContextID = task.ContextID
	message.TaskID = task.ID

	artifact := a2a.NewDataArtifact("result", result)
	return message, &artifact
}

func (s *A2ATaskService) emitTaskSnapshot(ctx context.Context, task *a2a.Task, events chan<- a2a.StreamEvent) error {
	payload, err := a2a.MarshalStreamResponse(a2a.StreamResponse{Task: task.Clone(nil, true)})
	if err != nil {
		return err
	}
	return s.recordAndDeliverEvent(ctx, task, a2a.StreamEvent{
		ID:        uuidString(),
		Event:     a2a.StreamEventTask,
		Data:      payload,
		Timestamp: time.Now().UTC(),
	}, events)
}

func (s *A2ATaskService) emitStatusUpdate(ctx context.Context, task *a2a.Task, final bool, events chan<- a2a.StreamEvent) error {
	update := &a2a.TaskStatusUpdateEvent{
		TaskID:    task.ID,
		ContextID: task.ContextID,
		Status:    task.Status,
		Final:     final,
	}
	payload, err := a2a.MarshalStreamResponse(a2a.StreamResponse{StatusUpdate: update})
	if err != nil {
		return err
	}
	return s.recordAndDeliverEvent(ctx, task, a2a.StreamEvent{
		ID:        uuidString(),
		Event:     a2a.StreamEventStatusUpdate,
		Data:      payload,
		Timestamp: time.Now().UTC(),
		Final:     final,
	}, events)
}

func (s *A2ATaskService) emitMessage(ctx context.Context, task *a2a.Task, message *a2a.Message, events chan<- a2a.StreamEvent) error {
	payload, err := a2a.MarshalStreamResponse(a2a.StreamResponse{Message: message})
	if err != nil {
		return err
	}
	return s.recordAndDeliverEvent(ctx, task, a2a.StreamEvent{
		ID:        uuidString(),
		Event:     a2a.StreamEventMessage,
		Data:      payload,
		Timestamp: time.Now().UTC(),
	}, events)
}

func (s *A2ATaskService) emitArtifact(ctx context.Context, task *a2a.Task, artifact *a2a.Artifact, events chan<- a2a.StreamEvent) error {
	update := &a2a.TaskArtifactUpdateEvent{
		TaskID:    task.ID,
		ContextID: task.ContextID,
		Artifact:  *artifact,
		LastChunk: true,
	}
	payload, err := a2a.MarshalStreamResponse(a2a.StreamResponse{ArtifactUpdate: update})
	if err != nil {
		return err
	}
	return s.recordAndDeliverEvent(ctx, task, a2a.StreamEvent{
		ID:        uuidString(),
		Event:     a2a.StreamEventArtifact,
		Data:      payload,
		Timestamp: time.Now().UTC(),
	}, events)
}

func (s *A2ATaskService) recordAndDeliverEvent(ctx context.Context, task *a2a.Task, event a2a.StreamEvent, events chan<- a2a.StreamEvent) error {
	persisted, err := s.recordStreamEvent(ctx, task.ID, event)
	if err != nil {
		return err
	}
	if s.pushService != nil {
		s.pushService.NotifyTaskEvent(ctx, task, persisted)
	}
	if events != nil {
		select {
		case events <- persisted:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func extractMessageText(message a2a.Message) string {
	parts := make([]string, 0, len(message.Parts))
	for _, part := range message.Parts {
		if strings.TrimSpace(part.Text) != "" {
			parts = append(parts, strings.TrimSpace(part.Text))
		}
	}
	return strings.Join(parts, "\n")
}

func metadataString(metadata map[string]interface{}, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	str, ok := value.(string)
	if !ok {
		return ""
	}
	return str
}

func historyLengthFromConfig(config *a2a.SendMessageConfiguration) *int {
	if config == nil {
		return nil
	}
	return config.HistoryLength
}

func uuidString() string {
	return uuid.NewString()
}
