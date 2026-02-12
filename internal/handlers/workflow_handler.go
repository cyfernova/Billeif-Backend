package handlers

import (
	"net/http"
	"strconv"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// WorkflowHandler handles workflow API endpoints
type WorkflowHandler struct {
	workflowService *services.WorkflowService
	log             *logger.Logger
}

// NewWorkflowHandler creates a new workflow handler
func NewWorkflowHandler(workflowService *services.WorkflowService, log *logger.Logger) *WorkflowHandler {
	return &WorkflowHandler{
		workflowService: workflowService,
		log:             log,
	}
}

// CreateWorkflowRequest represents a request to create a workflow
type CreateWorkflowRequest struct {
	Name                 string                         `json:"name" binding:"required"`
	Description          string                         `json:"description,omitempty"`
	AgentID              *string                        `json:"agentId,omitempty"`
	Trigger              *services.WorkflowTrigger      `json:"trigger" binding:"required"`
	Action               *services.WorkflowAction       `json:"action" binding:"required"`
	NotificationSettings *services.NotificationSettings `json:"notificationSettings,omitempty"`
}

// UpdateWorkflowRequest represents a request to update a workflow
type UpdateWorkflowRequest struct {
	Name                 *string                        `json:"name,omitempty"`
	Description          *string                        `json:"description,omitempty"`
	Trigger              *services.WorkflowTrigger      `json:"trigger,omitempty"`
	Action               *services.WorkflowAction       `json:"action,omitempty"`
	NotificationSettings *services.NotificationSettings `json:"notificationSettings,omitempty"`
	IsEnabled            *bool                          `json:"isEnabled,omitempty"`
}

// CreateWorkflow handles POST /workflows
// Creates a new workflow
func (h *WorkflowHandler) CreateWorkflow(c *gin.Context) {
	var req CreateWorkflowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("failed to parse create workflow request", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request format: " + err.Error(),
		})
		return
	}

	// Get user ID from context
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "User authentication required",
		})
		return
	}

	// Create workflow model
	workflow := &services.Workflow{
		Name:        req.Name,
		Description: req.Description,
		UserID:      userID.(string),
		AgentID:     req.AgentID,
		Trigger:     req.Trigger,
		Action:      req.Action,
		Status:      services.WorkflowStatusActive,
		IsEnabled:   true,
	}

	if err := h.workflowService.CreateWorkflow(c.Request.Context(), workflow); err != nil {
		h.log.Error("failed to create workflow", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create workflow: " + err.Error(),
		})
		return
	}

	h.log.Info("workflow created", "workflow_id", workflow.ID, "user_id", userID, "name", workflow.Name)

	c.JSON(http.StatusCreated, gin.H{
		"workflow": workflow,
		"message":  "Workflow created successfully",
	})
}

// GetWorkflow handles GET /workflows/:id
// Returns a specific workflow
func (h *WorkflowHandler) GetWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Workflow ID is required",
		})
		return
	}

	workflow, err := h.workflowService.GetWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		h.log.Error("failed to get workflow", "workflow_id", workflowID, "error", err)
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Workflow not found",
		})
		return
	}

	// Verify user owns this workflow
	userID, _ := c.Get("user_id")
	if workflow.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Access denied",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"workflow": workflow,
	})
}

// ListWorkflows handles GET /workflows
// Returns all workflows for the authenticated user
func (h *WorkflowHandler) ListWorkflows(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "User authentication required",
		})
		return
	}

	// Parse pagination
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	workflows, total, err := h.workflowService.GetWorkflowsByUser(c.Request.Context(), userID.(string), page, limit)
	if err != nil {
		h.log.Error("failed to list workflows", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve workflows",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"workflows": workflows,
		"total":     total,
		"page":      page,
		"limit":     limit,
	})
}

// UpdateWorkflow handles PUT /workflows/:id
// Updates a workflow
func (h *WorkflowHandler) UpdateWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Workflow ID is required",
		})
		return
	}

	var req UpdateWorkflowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("failed to parse update workflow request", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request format: " + err.Error(),
		})
		return
	}

	// Get existing workflow
	workflow, err := h.workflowService.GetWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Workflow not found",
		})
		return
	}

	// Verify user owns this workflow
	userID, _ := c.Get("user_id")
	if workflow.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Access denied",
		})
		return
	}

	// Apply updates
	if req.Name != nil {
		workflow.Name = *req.Name
	}
	if req.Description != nil {
		workflow.Description = *req.Description
	}
	if req.Trigger != nil {
		workflow.Trigger = req.Trigger
	}
	if req.Action != nil {
		workflow.Action = req.Action
	}
	if req.IsEnabled != nil {
		workflow.IsEnabled = *req.IsEnabled
	}

	if err := h.workflowService.UpdateWorkflow(c.Request.Context(), workflow); err != nil {
		h.log.Error("failed to update workflow", "workflow_id", workflowID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to update workflow: " + err.Error(),
		})
		return
	}

	h.log.Info("workflow updated", "workflow_id", workflowID)

	c.JSON(http.StatusOK, gin.H{
		"workflow": workflow,
		"message":  "Workflow updated successfully",
	})
}

// DeleteWorkflow handles DELETE /workflows/:id
// Deletes a workflow
func (h *WorkflowHandler) DeleteWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Workflow ID is required",
		})
		return
	}

	// Verify workflow exists and user owns it
	workflow, err := h.workflowService.GetWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Workflow not found",
		})
		return
	}

	userID, _ := c.Get("user_id")
	if workflow.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Access denied",
		})
		return
	}

	if err := h.workflowService.DeleteWorkflow(c.Request.Context(), workflowID); err != nil {
		h.log.Error("failed to delete workflow", "workflow_id", workflowID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to delete workflow",
		})
		return
	}

	h.log.Info("workflow deleted", "workflow_id", workflowID)

	c.JSON(http.StatusOK, gin.H{
		"message": "Workflow deleted successfully",
	})
}

// PauseWorkflow handles POST /workflows/:id/pause
// Pauses a workflow
func (h *WorkflowHandler) PauseWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Workflow ID is required",
		})
		return
	}

	// Verify workflow exists and user owns it
	workflow, err := h.workflowService.GetWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Workflow not found",
		})
		return
	}

	userID, _ := c.Get("user_id")
	if workflow.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Access denied",
		})
		return
	}

	if err := h.workflowService.PauseWorkflow(c.Request.Context(), workflowID); err != nil {
		h.log.Error("failed to pause workflow", "workflow_id", workflowID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to pause workflow: " + err.Error(),
		})
		return
	}

	h.log.Info("workflow paused", "workflow_id", workflowID)

	c.JSON(http.StatusOK, gin.H{
		"message": "Workflow paused successfully",
	})
}

// ResumeWorkflow handles POST /workflows/:id/resume
// Resumes a paused workflow
func (h *WorkflowHandler) ResumeWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Workflow ID is required",
		})
		return
	}

	// Verify workflow exists and user owns it
	workflow, err := h.workflowService.GetWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Workflow not found",
		})
		return
	}

	userID, _ := c.Get("user_id")
	if workflow.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Access denied",
		})
		return
	}

	if err := h.workflowService.ResumeWorkflow(c.Request.Context(), workflowID); err != nil {
		h.log.Error("failed to resume workflow", "workflow_id", workflowID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to resume workflow: " + err.Error(),
		})
		return
	}

	h.log.Info("workflow resumed", "workflow_id", workflowID)

	c.JSON(http.StatusOK, gin.H{
		"message": "Workflow resumed successfully",
	})
}

// RunWorkflow handles POST /workflows/:id/run
// Runs a workflow immediately
func (h *WorkflowHandler) RunWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Workflow ID is required",
		})
		return
	}

	// Verify workflow exists and user owns it
	workflow, err := h.workflowService.GetWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Workflow not found",
		})
		return
	}

	userID, _ := c.Get("user_id")
	if workflow.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Access denied",
		})
		return
	}

	run, err := h.workflowService.RunWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		h.log.Error("failed to run workflow", "workflow_id", workflowID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to run workflow: " + err.Error(),
		})
		return
	}

	h.log.Info("workflow run completed", "workflow_id", workflowID, "run_id", run.ID, "status", run.Status)

	c.JSON(http.StatusOK, gin.H{
		"run":     run,
		"message": "Workflow executed",
	})
}

// GetWorkflowRuns handles GET /workflows/:id/runs
// Returns run history for a workflow
func (h *WorkflowHandler) GetWorkflowRuns(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Workflow ID is required",
		})
		return
	}

	// Verify workflow exists and user owns it
	workflow, err := h.workflowService.GetWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Workflow not found",
		})
		return
	}

	userID, _ := c.Get("user_id")
	if workflow.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Access denied",
		})
		return
	}

	// Parse pagination
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	runs, total, err := h.workflowService.GetWorkflowRuns(c.Request.Context(), workflowID, page, limit)
	if err != nil {
		h.log.Error("failed to get workflow runs", "workflow_id", workflowID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve workflow runs",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"runs":  runs,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}
