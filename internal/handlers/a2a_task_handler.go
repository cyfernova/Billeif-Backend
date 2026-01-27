package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// A2ATaskHandler handles A2A v0.3 task API endpoints
type A2ATaskHandler struct {
	taskService *services.A2ATaskService
	log         *logger.Logger
}

// NewA2ATaskHandler creates a new A2A task handler
func NewA2ATaskHandler(taskService *services.A2ATaskService, log *logger.Logger) *A2ATaskHandler {
	return &A2ATaskHandler{
		taskService: taskService,
		log:         log,
	}
}

// SendTask handles POST /a2a/v0.3/tasks:send
// Creates a new task or continues an existing session
func (h *A2ATaskHandler) SendTask(c *gin.Context) {
	var req a2a.SendTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("failed to parse send task request", "error", err)
		c.JSON(http.StatusBadRequest, a2a.SendTaskResponse{
			Error: &a2a.TaskError{
				Code:    a2a.ErrorCodeInvalidRequest,
				Message: "Invalid request format: " + err.Error(),
			},
		})
		return
	}

	// Validate the request
	if len(req.Message.Parts) == 0 {
		c.JSON(http.StatusBadRequest, a2a.SendTaskResponse{
			Error: &a2a.TaskError{
				Code:    a2a.ErrorCodeInvalidRequest,
				Message: "Message must contain at least one part",
			},
		})
		return
	}

	// Get agent context from auth middleware (if present)
	sourceAgentID, _ := c.Get("agent_id")
	targetAgentID := req.TargetAgentID
	if targetAgentID == "" {
		targetAgentID = c.GetString("target_agent_id")
	}

	// Process the task
	task, err := h.taskService.SendTask(c.Request.Context(), &req, fmt.Sprintf("%v", sourceAgentID), targetAgentID)
	if err != nil {
		h.log.Error("failed to process task", "error", err)
		c.JSON(http.StatusInternalServerError, a2a.SendTaskResponse{
			Error: &a2a.TaskError{
				Code:    a2a.ErrorCodeInternalError,
				Message: "Failed to process task: " + err.Error(),
			},
		})
		return
	}

	h.log.Info("task sent successfully", "task_id", task.ID, "state", task.State)

	c.JSON(http.StatusOK, a2a.SendTaskResponse{
		Task: task,
	})
}

// StreamTask handles POST /a2a/v0.3/tasks:stream
// Creates a task and streams responses via SSE
func (h *A2ATaskHandler) StreamTask(c *gin.Context) {
	var req a2a.SendTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("failed to parse stream task request", "error", err)
		c.JSON(http.StatusBadRequest, a2a.SendTaskResponse{
			Error: &a2a.TaskError{
				Code:    a2a.ErrorCodeInvalidRequest,
				Message: "Invalid request format: " + err.Error(),
			},
		})
		return
	}

	// Get agent context
	sourceAgentID, _ := c.Get("agent_id")
	targetAgentID := req.TargetAgentID

	// Set up SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // Disable nginx buffering

	// Create event channel
	events := make(chan a2a.StreamEvent, 100)
	done := make(chan struct{})

	// Start streaming task processing
	go func() {
		defer close(done)
		err := h.taskService.StreamTask(c.Request.Context(), &req, fmt.Sprintf("%v", sourceAgentID), targetAgentID, events)
		if err != nil {
			h.log.Error("stream task error", "error", err)
			// Send error event
			errorData, _ := json.Marshal(a2a.TaskError{
				Code:    a2a.ErrorCodeInternalError,
				Message: err.Error(),
			})
			events <- a2a.StreamEvent{
				Event:     a2a.StreamEventError,
				Data:      errorData,
				Timestamp: time.Now(),
			}
		}
		close(events)
	}()

	// Stream events to client
	c.Stream(func(w io.Writer) bool {
		select {
		case event, ok := <-events:
			if !ok {
				return false
			}

			// Format as SSE
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "id: %s\n", event.ID)
			fmt.Fprintf(w, "event: %s\n", event.Event)
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}

			// Check if this is the final event
			if event.Event == a2a.StreamEventDone || event.Event == a2a.StreamEventError {
				return false
			}
			return true

		case <-c.Request.Context().Done():
			h.log.Info("client disconnected from stream")
			return false

		case <-done:
			return false
		}
	})
}

// GetTask handles GET /a2a/v0.3/tasks/:taskId
// Returns the current state of a task
func (h *A2ATaskHandler) GetTask(c *gin.Context) {
	taskID := c.Param("taskId")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, a2a.SendTaskResponse{
			Error: &a2a.TaskError{
				Code:    a2a.ErrorCodeInvalidRequest,
				Message: "Task ID is required",
			},
		})
		return
	}

	task, err := h.taskService.GetTask(c.Request.Context(), taskID)
	if err != nil {
		if err == a2a.ErrTaskNotFound {
			c.JSON(http.StatusNotFound, a2a.SendTaskResponse{
				Error: &a2a.TaskError{
					Code:    a2a.ErrorCodeTaskNotFound,
					Message: "Task not found",
				},
			})
			return
		}
		h.log.Error("failed to get task", "task_id", taskID, "error", err)
		c.JSON(http.StatusInternalServerError, a2a.SendTaskResponse{
			Error: &a2a.TaskError{
				Code:    a2a.ErrorCodeInternalError,
				Message: "Failed to retrieve task",
			},
		})
		return
	}

	c.JSON(http.StatusOK, a2a.SendTaskResponse{
		Task: task,
	})
}

// ListTasks handles GET /a2a/v0.3/tasks
// Returns a paginated list of tasks
func (h *A2ATaskHandler) ListTasks(c *gin.Context) {
	// Parse query parameters
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	sessionID := c.Query("sessionId")
	stateStr := c.Query("state")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	req := a2a.ListTasksRequest{
		SessionID: sessionID,
		Page:      page,
		PageSize:  pageSize,
	}

	if stateStr != "" {
		state := a2a.TaskState(stateStr)
		if state.IsValid() {
			req.State = &state
		}
	}

	// Get user/agent context for filtering
	userID, _ := c.Get("user_id")
	agentID, _ := c.Get("agent_id")

	resp, err := h.taskService.ListTasks(c.Request.Context(), &req, fmt.Sprintf("%v", userID), fmt.Sprintf("%v", agentID))
	if err != nil {
		h.log.Error("failed to list tasks", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": a2a.TaskError{
				Code:    a2a.ErrorCodeInternalError,
				Message: "Failed to list tasks",
			},
		})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// CancelTask handles POST /a2a/v0.3/tasks/:taskId:cancel
// Cancels a running task
func (h *A2ATaskHandler) CancelTask(c *gin.Context) {
	taskID := c.Param("taskId")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, a2a.SendTaskResponse{
			Error: &a2a.TaskError{
				Code:    a2a.ErrorCodeInvalidRequest,
				Message: "Task ID is required",
			},
		})
		return
	}

	var req a2a.CancelTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Allow empty body
		req = a2a.CancelTaskRequest{TaskID: taskID}
	}
	req.TaskID = taskID

	task, err := h.taskService.CancelTask(c.Request.Context(), &req)
	if err != nil {
		if err == a2a.ErrTaskNotFound {
			c.JSON(http.StatusNotFound, a2a.SendTaskResponse{
				Error: &a2a.TaskError{
					Code:    a2a.ErrorCodeTaskNotFound,
					Message: "Task not found",
				},
			})
			return
		}
		if err == a2a.ErrTaskStateTerminal {
			c.JSON(http.StatusBadRequest, a2a.SendTaskResponse{
				Error: &a2a.TaskError{
					Code:    a2a.ErrorCodeInvalidRequest,
					Message: "Task is already in a terminal state",
				},
			})
			return
		}
		h.log.Error("failed to cancel task", "task_id", taskID, "error", err)
		c.JSON(http.StatusInternalServerError, a2a.SendTaskResponse{
			Error: &a2a.TaskError{
				Code:    a2a.ErrorCodeInternalError,
				Message: "Failed to cancel task",
			},
		})
		return
	}

	h.log.Info("task cancelled", "task_id", taskID)

	c.JSON(http.StatusOK, a2a.SendTaskResponse{
		Task: task,
	})
}

// SubscribeTask handles GET /a2a/v0.3/tasks/:taskId:subscribe
// Subscribes to real-time updates for a task via SSE
func (h *A2ATaskHandler) SubscribeTask(c *gin.Context) {
	taskID := c.Param("taskId")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, a2a.SendTaskResponse{
			Error: &a2a.TaskError{
				Code:    a2a.ErrorCodeInvalidRequest,
				Message: "Task ID is required",
			},
		})
		return
	}

	// Verify task exists
	task, err := h.taskService.GetTask(c.Request.Context(), taskID)
	if err != nil {
		if err == a2a.ErrTaskNotFound {
			c.JSON(http.StatusNotFound, a2a.SendTaskResponse{
				Error: &a2a.TaskError{
					Code:    a2a.ErrorCodeTaskNotFound,
					Message: "Task not found",
				},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, a2a.SendTaskResponse{
			Error: &a2a.TaskError{
				Code:    a2a.ErrorCodeInternalError,
				Message: "Failed to retrieve task",
			},
		})
		return
	}

	// If task is already terminal, return current state
	if task.State.IsTerminal() {
		c.JSON(http.StatusOK, a2a.SendTaskResponse{
			Task: task,
		})
		return
	}

	// Set up SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Create event channel
	events := make(chan a2a.StreamEvent, 100)

	// Subscribe to task updates
	go func() {
		err := h.taskService.SubscribeTask(c.Request.Context(), taskID, events)
		if err != nil {
			h.log.Error("subscribe task error", "task_id", taskID, "error", err)
		}
		close(events)
	}()

	// Stream events to client
	c.Stream(func(w io.Writer) bool {
		select {
		case event, ok := <-events:
			if !ok {
				return false
			}

			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "id: %s\n", event.ID)
			fmt.Fprintf(w, "event: %s\n", event.Event)
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}

			if event.Event == a2a.StreamEventDone || event.Event == a2a.StreamEventError {
				return false
			}
			return true

		case <-c.Request.Context().Done():
			h.log.Info("client disconnected from task subscription", "task_id", taskID)
			return false
		}
	})
}
