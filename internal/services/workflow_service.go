package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"invoice-backend/pkg/logger"

	"github.com/go-co-op/gocron"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// WorkflowStatus represents the status of a workflow
type WorkflowStatus string

const (
	WorkflowStatusActive    WorkflowStatus = "active"
	WorkflowStatusPaused    WorkflowStatus = "paused"
	WorkflowStatusCompleted WorkflowStatus = "completed"
	WorkflowStatusFailed    WorkflowStatus = "failed"
	WorkflowStatusDisabled  WorkflowStatus = "disabled"
)

// TriggerType represents the type of workflow trigger
type TriggerType string

const (
	TriggerTypeTime  TriggerType = "time"
	TriggerTypePrice TriggerType = "price"
	TriggerTypeBoth  TriggerType = "both"
)

// PriceCondition represents price comparison conditions
type PriceCondition string

const (
	PriceConditionAbove     PriceCondition = "above"
	PriceConditionBelow     PriceCondition = "below"
	PriceConditionEquals    PriceCondition = "equals"
	PriceConditionChangePct PriceCondition = "change_pct"
)

// WorkflowActionType represents the type of workflow action
type WorkflowActionType string

const (
	WorkflowActionBuy    WorkflowActionType = "buy"
	WorkflowActionSell   WorkflowActionType = "sell"
	WorkflowActionNotify WorkflowActionType = "notify"
)

// Workflow represents an automated workflow
type Workflow struct {
	ID                   string           `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	Name                 string           `gorm:"not null" json:"name"`
	Description          string           `json:"description,omitempty"`
	UserID               string           `gorm:"not null;index" json:"user_id"`
	AgentID              *string          `gorm:"type:uuid" json:"agent_id,omitempty"`
	TriggerJSON          json.RawMessage  `gorm:"column:trigger;type:jsonb;not null" json:"-"`
	ActionJSON           json.RawMessage  `gorm:"column:action;type:jsonb;not null" json:"-"`
	Trigger              *WorkflowTrigger `gorm:"-" json:"trigger"`
	Action               *WorkflowAction  `gorm:"-" json:"action"`
	Status               WorkflowStatus   `gorm:"type:varchar(20);default:'active'" json:"status"`
	IsEnabled            bool             `gorm:"default:true" json:"is_enabled"`
	LastRun              *time.Time       `json:"last_run,omitempty"`
	NextRun              *time.Time       `json:"next_run,omitempty"`
	RunCount             int              `gorm:"default:0" json:"run_count"`
	SuccessCount         int              `gorm:"default:0" json:"success_count"`
	FailureCount         int              `gorm:"default:0" json:"failure_count"`
	LastError            *string          `json:"last_error,omitempty"`
	NotificationSettings json.RawMessage  `gorm:"type:jsonb;default:'{}'" json:"notification_settings,omitempty"`
	CreatedAt            time.Time        `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt            time.Time        `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt            gorm.DeletedAt   `gorm:"index" json:"-"`
}

// TableName returns the table name for GORM
func (Workflow) TableName() string {
	return "workflows"
}

// BeforeSave serializes trigger and action to JSON
func (w *Workflow) BeforeSave(tx *gorm.DB) error {
	if w.Trigger != nil {
		data, err := json.Marshal(w.Trigger)
		if err != nil {
			return err
		}
		w.TriggerJSON = data
	}
	if w.Action != nil {
		data, err := json.Marshal(w.Action)
		if err != nil {
			return err
		}
		w.ActionJSON = data
	}
	return nil
}

// AfterFind deserializes trigger and action from JSON
func (w *Workflow) AfterFind(tx *gorm.DB) error {
	if len(w.TriggerJSON) > 0 {
		w.Trigger = &WorkflowTrigger{}
		if err := json.Unmarshal(w.TriggerJSON, w.Trigger); err != nil {
			return err
		}
	}
	if len(w.ActionJSON) > 0 {
		w.Action = &WorkflowAction{}
		if err := json.Unmarshal(w.ActionJSON, w.Action); err != nil {
			return err
		}
	}
	return nil
}

// WorkflowTrigger defines when a workflow should execute
type WorkflowTrigger struct {
	Type      TriggerType `json:"type"`
	Schedule  *string     `json:"schedule,omitempty"`  // Cron expression for time triggers
	Timezone  *string     `json:"timezone,omitempty"`  // Timezone for schedule
	PriceRule *PriceRule  `json:"priceRule,omitempty"` // Price condition for price triggers
	OneTime   bool        `json:"oneTime,omitempty"`   // Execute only once
}

// PriceRule defines a price-based trigger condition
type PriceRule struct {
	ProductID    string         `json:"productId"`
	Condition    PriceCondition `json:"condition"`
	TargetValue  float64        `json:"targetValue"`
	CurrentValue float64        `json:"currentValue,omitempty"`
}

// WorkflowAction defines what the workflow should do when triggered
type WorkflowAction struct {
	Type       WorkflowActionType     `json:"type"`
	ProductID  string                 `json:"productId,omitempty"`
	Quantity   int                    `json:"quantity,omitempty"`
	MaxPrice   *float64               `json:"maxPrice,omitempty"`   // For buy actions
	MinPrice   *float64               `json:"minPrice,omitempty"`   // For sell actions
	Recipients []string               `json:"recipients,omitempty"` // For notify actions
	Message    string                 `json:"message,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// WorkflowRun represents a single execution of a workflow
type WorkflowRun struct {
	ID           string          `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	WorkflowID   string          `gorm:"not null;index" json:"workflow_id"`
	TriggerType  string          `gorm:"not null" json:"trigger_type"`
	TriggerData  json.RawMessage `gorm:"type:jsonb;default:'{}'" json:"trigger_data,omitempty"`
	Status       string          `gorm:"not null;default:'pending'" json:"status"`
	Result       json.RawMessage `gorm:"type:jsonb;default:'{}'" json:"result,omitempty"`
	ErrorMessage *string         `json:"error_message,omitempty"`
	StartedAt    time.Time       `gorm:"autoCreateTime" json:"started_at"`
	CompletedAt  *time.Time      `json:"completed_at,omitempty"`
	DurationMs   *int            `json:"duration_ms,omitempty"`
}

// TableName returns the table name for GORM
func (WorkflowRun) TableName() string {
	return "workflow_runs"
}

// PriceAlert represents a price monitoring configuration
type PriceAlert struct {
	ID           string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	WorkflowID   string         `gorm:"not null;index" json:"workflow_id"`
	ProductID    string         `gorm:"not null;index" json:"product_id"`
	Condition    PriceCondition `gorm:"not null" json:"condition"`
	TargetPrice  float64        `gorm:"not null" json:"target_price"`
	CurrentPrice *float64       `json:"current_price,omitempty"`
	IsTriggered  bool           `gorm:"default:false" json:"is_triggered"`
	TriggeredAt  *time.Time     `json:"triggered_at,omitempty"`
	IsActive     bool           `gorm:"default:true" json:"is_active"`
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName returns the table name for GORM
func (PriceAlert) TableName() string {
	return "price_alerts"
}

// NotificationSettings defines how workflow notifications are sent
type NotificationSettings struct {
	InApp  bool     `json:"inApp"`
	Email  bool     `json:"email"`
	Emails []string `json:"emails,omitempty"`
}

// WorkflowService handles workflow operations
type WorkflowService struct {
	db            *gorm.DB
	log           *logger.Logger
	scheduler     *gocron.Scheduler
	emailService  EmailSender
	pushService   *A2APushService
	scheduledJobs map[string]*gocron.Job
	jobMutex      sync.RWMutex
	priceMonitor  *PriceMonitor
}

// EmailSender interface for sending emails
type EmailSender interface {
	SendEmail(ctx context.Context, to, subject, body string) error
}

// PriceMonitor monitors prices for price-based triggers
type PriceMonitor struct {
	service   *WorkflowService
	log       *logger.Logger
	stopChan  chan struct{}
	isRunning bool
	mutex     sync.Mutex
}

// NewWorkflowService creates a new workflow service
func NewWorkflowService(db *gorm.DB, log *logger.Logger, emailService EmailSender, pushService *A2APushService) *WorkflowService {
	scheduler := gocron.NewScheduler(time.UTC)
	scheduler.StartAsync()

	service := &WorkflowService{
		db:            db,
		log:           log,
		scheduler:     scheduler,
		emailService:  emailService,
		pushService:   pushService,
		scheduledJobs: make(map[string]*gocron.Job),
	}

	// Initialize price monitor
	service.priceMonitor = &PriceMonitor{
		service:  service,
		log:      log,
		stopChan: make(chan struct{}),
	}

	return service
}

// Start starts the workflow service and loads existing workflows
func (s *WorkflowService) Start(ctx context.Context) error {
	// Load active workflows
	var workflows []Workflow
	if err := s.db.WithContext(ctx).Where("is_enabled = ? AND status = ?", true, WorkflowStatusActive).Find(&workflows).Error; err != nil {
		return fmt.Errorf("failed to load workflows: %w", err)
	}

	// Schedule each workflow
	for _, w := range workflows {
		if err := s.scheduleWorkflow(ctx, &w); err != nil {
			s.log.Error("failed to schedule workflow", "workflow_id", w.ID, "error", err)
		}
	}

	// Start price monitor
	s.priceMonitor.Start(ctx)

	s.log.Info("workflow service started", "active_workflows", len(workflows))
	return nil
}

// Stop stops the workflow service
func (s *WorkflowService) Stop() {
	s.scheduler.Stop()
	s.priceMonitor.Stop()
	s.log.Info("workflow service stopped")
}

// CreateWorkflow creates a new workflow
func (s *WorkflowService) CreateWorkflow(ctx context.Context, workflow *Workflow) error {
	if workflow.ID == "" {
		workflow.ID = uuid.New().String()
	}

	if err := s.db.WithContext(ctx).Create(workflow).Error; err != nil {
		return fmt.Errorf("failed to create workflow: %w", err)
	}

	// Schedule the workflow if active
	if workflow.IsEnabled && workflow.Status == WorkflowStatusActive {
		if err := s.scheduleWorkflow(ctx, workflow); err != nil {
			s.log.Error("failed to schedule new workflow", "workflow_id", workflow.ID, "error", err)
		}
	}

	s.log.Info("workflow created", "workflow_id", workflow.ID, "name", workflow.Name)
	return nil
}

// GetWorkflow retrieves a workflow by ID
func (s *WorkflowService) GetWorkflow(ctx context.Context, workflowID string) (*Workflow, error) {
	var workflow Workflow
	if err := s.db.WithContext(ctx).Where("id = ?", workflowID).First(&workflow).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("workflow not found")
		}
		return nil, err
	}
	return &workflow, nil
}

// GetWorkflowsByUser retrieves all workflows for a user
func (s *WorkflowService) GetWorkflowsByUser(ctx context.Context, userID string, page, limit int) ([]Workflow, int64, error) {
	var workflows []Workflow
	var total int64

	query := s.db.WithContext(ctx).Model(&Workflow{}).Where("user_id = ?", userID)
	query.Count(&total)

	offset := (page - 1) * limit
	if err := query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&workflows).Error; err != nil {
		return nil, 0, err
	}

	return workflows, total, nil
}

// UpdateWorkflow updates a workflow
func (s *WorkflowService) UpdateWorkflow(ctx context.Context, workflow *Workflow) error {
	if err := s.db.WithContext(ctx).Save(workflow).Error; err != nil {
		return fmt.Errorf("failed to update workflow: %w", err)
	}

	// Reschedule the workflow
	s.unscheduleWorkflow(workflow.ID)
	if workflow.IsEnabled && workflow.Status == WorkflowStatusActive {
		if err := s.scheduleWorkflow(ctx, workflow); err != nil {
			s.log.Error("failed to reschedule workflow", "workflow_id", workflow.ID, "error", err)
		}
	}

	s.log.Info("workflow updated", "workflow_id", workflow.ID)
	return nil
}

// DeleteWorkflow deletes a workflow (soft delete)
func (s *WorkflowService) DeleteWorkflow(ctx context.Context, workflowID string) error {
	s.unscheduleWorkflow(workflowID)

	if err := s.db.WithContext(ctx).Delete(&Workflow{}, "id = ?", workflowID).Error; err != nil {
		return fmt.Errorf("failed to delete workflow: %w", err)
	}

	s.log.Info("workflow deleted", "workflow_id", workflowID)
	return nil
}

// PauseWorkflow pauses a workflow
func (s *WorkflowService) PauseWorkflow(ctx context.Context, workflowID string) error {
	workflow, err := s.GetWorkflow(ctx, workflowID)
	if err != nil {
		return err
	}

	workflow.Status = WorkflowStatusPaused
	s.unscheduleWorkflow(workflowID)

	if err := s.db.WithContext(ctx).Save(workflow).Error; err != nil {
		return fmt.Errorf("failed to pause workflow: %w", err)
	}

	s.log.Info("workflow paused", "workflow_id", workflowID)
	return nil
}

// ResumeWorkflow resumes a paused workflow
func (s *WorkflowService) ResumeWorkflow(ctx context.Context, workflowID string) error {
	workflow, err := s.GetWorkflow(ctx, workflowID)
	if err != nil {
		return err
	}

	workflow.Status = WorkflowStatusActive
	if err := s.scheduleWorkflow(ctx, workflow); err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).Save(workflow).Error; err != nil {
		return fmt.Errorf("failed to resume workflow: %w", err)
	}

	s.log.Info("workflow resumed", "workflow_id", workflowID)
	return nil
}

// RunWorkflow executes a workflow immediately
func (s *WorkflowService) RunWorkflow(ctx context.Context, workflowID string) (*WorkflowRun, error) {
	workflow, err := s.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, err
	}

	return s.executeWorkflow(ctx, workflow, "manual")
}

// scheduleWorkflow schedules a workflow based on its trigger
func (s *WorkflowService) scheduleWorkflow(ctx context.Context, workflow *Workflow) error {
	if workflow.Trigger == nil {
		return fmt.Errorf("workflow has no trigger")
	}

	switch workflow.Trigger.Type {
	case TriggerTypeTime, TriggerTypeBoth:
		return s.scheduleTimeBasedWorkflow(ctx, workflow)
	case TriggerTypePrice:
		return s.schedulePriceBasedWorkflow(ctx, workflow)
	default:
		return fmt.Errorf("unknown trigger type: %s", workflow.Trigger.Type)
	}
}

// scheduleTimeBasedWorkflow schedules a time-based workflow
func (s *WorkflowService) scheduleTimeBasedWorkflow(ctx context.Context, workflow *Workflow) error {
	if workflow.Trigger.Schedule == nil {
		return fmt.Errorf("time-based workflow requires a schedule")
	}

	// Parse timezone
	loc := time.UTC
	if workflow.Trigger.Timezone != nil {
		var err error
		loc, err = time.LoadLocation(*workflow.Trigger.Timezone)
		if err != nil {
			s.log.Warn("invalid timezone, using UTC", "timezone", *workflow.Trigger.Timezone)
		}
	}

	// Create a scheduler with the correct timezone and schedule the job
	s.scheduler.ChangeLocation(loc)
	job, err := s.scheduler.Cron(*workflow.Trigger.Schedule).
		Do(func() {
			s.log.Info("executing scheduled workflow", "workflow_id", workflow.ID)
			_, err := s.executeWorkflow(context.Background(), workflow, "scheduled")
			if err != nil {
				s.log.Error("scheduled workflow execution failed", "workflow_id", workflow.ID, "error", err)
			}
		})

	if err != nil {
		return fmt.Errorf("failed to schedule workflow: %w", err)
	}

	// Store job reference
	s.jobMutex.Lock()
	s.scheduledJobs[workflow.ID] = job
	s.jobMutex.Unlock()

	// Update next run time
	nextRun := job.NextRun()
	workflow.NextRun = &nextRun
	s.db.WithContext(ctx).Model(workflow).Update("next_run", nextRun)

	s.log.Info("workflow scheduled", "workflow_id", workflow.ID, "next_run", nextRun)
	return nil
}

// schedulePriceBasedWorkflow sets up price monitoring for a workflow
func (s *WorkflowService) schedulePriceBasedWorkflow(ctx context.Context, workflow *Workflow) error {
	if workflow.Trigger.PriceRule == nil {
		return fmt.Errorf("price-based workflow requires a price rule")
	}

	// Create price alert
	alert := &PriceAlert{
		ID:          uuid.New().String(),
		WorkflowID:  workflow.ID,
		ProductID:   workflow.Trigger.PriceRule.ProductID,
		Condition:   workflow.Trigger.PriceRule.Condition,
		TargetPrice: workflow.Trigger.PriceRule.TargetValue,
		IsActive:    true,
	}

	if err := s.db.WithContext(ctx).Create(alert).Error; err != nil {
		return fmt.Errorf("failed to create price alert: %w", err)
	}

	s.log.Info("price alert created for workflow", "workflow_id", workflow.ID, "alert_id", alert.ID)
	return nil
}

// unscheduleWorkflow removes a workflow from the scheduler
func (s *WorkflowService) unscheduleWorkflow(workflowID string) {
	s.jobMutex.Lock()
	defer s.jobMutex.Unlock()

	if job, exists := s.scheduledJobs[workflowID]; exists {
		s.scheduler.RemoveByReference(job)
		delete(s.scheduledJobs, workflowID)
		s.log.Info("workflow unscheduled", "workflow_id", workflowID)
	}
}

// executeWorkflow executes a workflow
func (s *WorkflowService) executeWorkflow(ctx context.Context, workflow *Workflow, triggerType string) (*WorkflowRun, error) {
	startTime := time.Now()

	// Create workflow run
	run := &WorkflowRun{
		ID:          uuid.New().String(),
		WorkflowID:  workflow.ID,
		TriggerType: triggerType,
		Status:      "running",
		StartedAt:   startTime,
	}

	if err := s.db.WithContext(ctx).Create(run).Error; err != nil {
		return nil, fmt.Errorf("failed to create workflow run: %w", err)
	}

	// Execute the action
	var actionErr error
	var result map[string]interface{}

	switch workflow.Action.Type {
	case WorkflowActionBuy:
		result, actionErr = s.executeBuyAction(ctx, workflow)
	case WorkflowActionSell:
		result, actionErr = s.executeSellAction(ctx, workflow)
	case WorkflowActionNotify:
		result, actionErr = s.executeNotifyAction(ctx, workflow)
	default:
		actionErr = fmt.Errorf("unknown action type: %s", workflow.Action.Type)
	}

	// Update run status
	completedAt := time.Now()
	durationMs := int(completedAt.Sub(startTime).Milliseconds())
	run.CompletedAt = &completedAt
	run.DurationMs = &durationMs

	if actionErr != nil {
		run.Status = "failed"
		errMsg := actionErr.Error()
		run.ErrorMessage = &errMsg
		workflow.FailureCount++
		workflow.LastError = &errMsg
	} else {
		run.Status = "completed"
		resultJSON, _ := json.Marshal(result)
		run.Result = resultJSON
		workflow.SuccessCount++
	}

	// Update workflow
	workflow.RunCount++
	workflow.LastRun = &startTime

	// Handle one-time workflows
	if workflow.Trigger.OneTime && run.Status == "completed" {
		workflow.Status = WorkflowStatusCompleted
		s.unscheduleWorkflow(workflow.ID)
	}

	// Save updates
	s.db.WithContext(ctx).Save(run)
	s.db.WithContext(ctx).Save(workflow)

	// Send notification
	s.sendWorkflowNotification(ctx, workflow, run)

	s.log.Info("workflow executed", "workflow_id", workflow.ID, "run_id", run.ID, "status", run.Status, "duration_ms", durationMs)

	return run, actionErr
}

// executeBuyAction executes a buy action
func (s *WorkflowService) executeBuyAction(ctx context.Context, workflow *Workflow) (map[string]interface{}, error) {
	// This would integrate with the shopping agent service
	// For now, return a placeholder result
	result := map[string]interface{}{
		"action":     "buy",
		"product_id": workflow.Action.ProductID,
		"quantity":   workflow.Action.Quantity,
		"status":     "order_placed",
	}

	s.log.Info("buy action executed", "workflow_id", workflow.ID, "product_id", workflow.Action.ProductID)
	return result, nil
}

// executeSellAction executes a sell action
func (s *WorkflowService) executeSellAction(ctx context.Context, workflow *Workflow) (map[string]interface{}, error) {
	// This would integrate with merchant services
	result := map[string]interface{}{
		"action":     "sell",
		"product_id": workflow.Action.ProductID,
		"quantity":   workflow.Action.Quantity,
		"status":     "listing_created",
	}

	s.log.Info("sell action executed", "workflow_id", workflow.ID, "product_id", workflow.Action.ProductID)
	return result, nil
}

// executeNotifyAction executes a notify action
func (s *WorkflowService) executeNotifyAction(ctx context.Context, workflow *Workflow) (map[string]interface{}, error) {
	// Parse notification settings
	var settings NotificationSettings
	json.Unmarshal(workflow.NotificationSettings, &settings)

	result := map[string]interface{}{
		"action":     "notify",
		"recipients": workflow.Action.Recipients,
		"sent":       0,
	}

	// Send in-app notification
	if settings.InApp {
		// Would integrate with push service
		s.log.Info("in-app notification sent", "workflow_id", workflow.ID)
	}

	// Send email notifications
	if settings.Email && s.emailService != nil {
		for _, email := range workflow.Action.Recipients {
			if err := s.emailService.SendEmail(ctx, email, "Workflow Notification", workflow.Action.Message); err != nil {
				s.log.Error("failed to send email", "email", email, "error", err)
			} else {
				result["sent"] = result["sent"].(int) + 1
			}
		}
	}

	return result, nil
}

// sendWorkflowNotification sends a notification about workflow execution
func (s *WorkflowService) sendWorkflowNotification(ctx context.Context, workflow *Workflow, run *WorkflowRun) {
	var settings NotificationSettings
	json.Unmarshal(workflow.NotificationSettings, &settings)

	if !settings.InApp && !settings.Email {
		return
	}

	subject := fmt.Sprintf("Workflow '%s' Completed", workflow.Name)
	if run.Status == "failed" {
		subject = fmt.Sprintf("Workflow '%s' Failed", workflow.Name)
	}

	body := fmt.Sprintf("Workflow: %s\nStatus: %s\nRun ID: %s\nDuration: %dms",
		workflow.Name, run.Status, run.ID, *run.DurationMs)

	if run.ErrorMessage != nil {
		body += fmt.Sprintf("\nError: %s", *run.ErrorMessage)
	}

	if settings.Email && s.emailService != nil {
		for _, email := range settings.Emails {
			s.emailService.SendEmail(ctx, email, subject, body)
		}
	}
}

// GetWorkflowRuns retrieves runs for a workflow
func (s *WorkflowService) GetWorkflowRuns(ctx context.Context, workflowID string, page, limit int) ([]WorkflowRun, int64, error) {
	var runs []WorkflowRun
	var total int64

	query := s.db.WithContext(ctx).Model(&WorkflowRun{}).Where("workflow_id = ?", workflowID)
	query.Count(&total)

	offset := (page - 1) * limit
	if err := query.Offset(offset).Limit(limit).Order("started_at DESC").Find(&runs).Error; err != nil {
		return nil, 0, err
	}

	return runs, total, nil
}

// PriceMonitor methods

// Start starts the price monitor
func (pm *PriceMonitor) Start(ctx context.Context) {
	pm.mutex.Lock()
	if pm.isRunning {
		pm.mutex.Unlock()
		return
	}
	pm.isRunning = true
	pm.mutex.Unlock()

	go pm.monitorPrices(ctx)
	pm.log.Info("price monitor started")
}

// Stop stops the price monitor
func (pm *PriceMonitor) Stop() {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if pm.isRunning {
		close(pm.stopChan)
		pm.isRunning = false
		pm.log.Info("price monitor stopped")
	}
}

// monitorPrices periodically checks prices and triggers workflows
func (pm *PriceMonitor) monitorPrices(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pm.checkPrices(ctx)
		case <-pm.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

// checkPrices checks all active price alerts
func (pm *PriceMonitor) checkPrices(ctx context.Context) {
	var alerts []PriceAlert
	if err := pm.service.db.WithContext(ctx).Where("is_active = ? AND is_triggered = ?", true, false).Find(&alerts).Error; err != nil {
		pm.log.Error("failed to fetch price alerts", "error", err)
		return
	}

	for _, alert := range alerts {
		// Get current price (would integrate with product service)
		currentPrice := pm.getCurrentPrice(ctx, alert.ProductID)
		if currentPrice == nil {
			continue
		}

		// Check if condition is met
		triggered := false
		switch alert.Condition {
		case PriceConditionAbove:
			triggered = *currentPrice > alert.TargetPrice
		case PriceConditionBelow:
			triggered = *currentPrice < alert.TargetPrice
		case PriceConditionEquals:
			triggered = *currentPrice == alert.TargetPrice
		}

		if triggered {
			pm.triggerAlert(ctx, &alert, *currentPrice)
		} else {
			// Update current price
			pm.service.db.WithContext(ctx).Model(&alert).Update("current_price", currentPrice)
		}
	}
}

// getCurrentPrice gets the current price for a product
func (pm *PriceMonitor) getCurrentPrice(ctx context.Context, productID string) *float64 {
	// This would integrate with the product/marketplace service
	// For now, return nil (no price available)
	return nil
}

// triggerAlert triggers a price alert and executes the associated workflow
func (pm *PriceMonitor) triggerAlert(ctx context.Context, alert *PriceAlert, currentPrice float64) {
	now := time.Now()
	alert.IsTriggered = true
	alert.TriggeredAt = &now
	alert.CurrentPrice = &currentPrice

	if err := pm.service.db.WithContext(ctx).Save(alert).Error; err != nil {
		pm.log.Error("failed to update alert", "alert_id", alert.ID, "error", err)
		return
	}

	// Get and execute the workflow
	workflow, err := pm.service.GetWorkflow(ctx, alert.WorkflowID)
	if err != nil {
		pm.log.Error("failed to get workflow for alert", "alert_id", alert.ID, "error", err)
		return
	}

	_, err = pm.service.executeWorkflow(ctx, workflow, "price_trigger")
	if err != nil {
		pm.log.Error("failed to execute workflow for price alert", "workflow_id", workflow.ID, "error", err)
	}

	pm.log.Info("price alert triggered", "alert_id", alert.ID, "workflow_id", alert.WorkflowID, "price", currentPrice)
}
