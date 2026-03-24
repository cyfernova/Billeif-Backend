package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type A2ATaskHandler struct {
	taskService *services.A2ATaskService
	pushService *services.A2APushService
	log         *logger.Logger
}

func NewA2ATaskHandler(taskService *services.A2ATaskService, pushService *services.A2APushService, log *logger.Logger) *A2ATaskHandler {
	return &A2ATaskHandler{
		taskService: taskService,
		pushService: pushService,
		log:         log,
	}
}

func (h *A2ATaskHandler) Handle(c *gin.Context) {
	if !h.ensureVersion(c) {
		return
	}

	path := strings.TrimPrefix(c.Param("a2aPath"), "/")
	switch {
	case c.Request.Method == http.MethodPost && path == "message:send":
		h.handleSendMessage(c)
	case c.Request.Method == http.MethodPost && path == "message:stream":
		h.handleMessageStream(c)
	case c.Request.Method == http.MethodGet && path == "tasks":
		h.handleListTasks(c)
	case c.Request.Method == http.MethodGet && isSimpleTaskPath(path):
		h.handleGetTask(c, strings.TrimPrefix(path, "tasks/"))
	case c.Request.Method == http.MethodPost && strings.HasPrefix(path, "tasks/") && strings.HasSuffix(path, ":cancel"):
		taskID := strings.TrimSuffix(strings.TrimPrefix(path, "tasks/"), ":cancel")
		h.handleCancelTask(c, taskID)
	case c.Request.Method == http.MethodGet:
		taskID, ok := parseSubscribeTaskPath(path)
		if !ok {
			break
		}
		h.handleSubscribeTask(c, taskID)
	case strings.HasPrefix(path, "tasks/") && strings.Contains(path, "/pushNotificationConfigs"):
		h.handlePushConfigRoutes(c, path)
	case c.Request.Method == http.MethodGet && path == "extendedAgentCard":
		h.problem(c, a2a.NewProblem(http.StatusNotImplemented, a2a.ProblemTypeUnsupportedOperation, "Extended Agent Card Unsupported", "This agent does not expose an authenticated extended agent card."))
	default:
		h.problem(c, a2a.NewProblem(http.StatusNotFound, a2a.ProblemTypeInvalidRequest, "Unknown A2A Operation", "No A2A operation matches this path and method."))
	}
}

func (h *A2ATaskHandler) ensureVersion(c *gin.Context) bool {
	version := strings.TrimSpace(c.GetHeader(a2a.HeaderVersion))
	if version == "" {
		h.problem(c, a2a.NewProblem(http.StatusBadRequest, a2a.ProblemTypeVersionNotSupported, "Missing Protocol Version", "A2A-Version header is required."))
		return false
	}
	if version != a2a.SupportedVersion {
		h.problem(c, a2a.VersionNotSupportedProblem(version))
		return false
	}
	return true
}

func (h *A2ATaskHandler) handleSendMessage(c *gin.Context) {
	var req a2a.SendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.problem(c, a2a.NewProblem(http.StatusBadRequest, a2a.ProblemTypeInvalidRequest, "Invalid Request", "Malformed send message payload: "+err.Error()))
		return
	}

	resp, err := h.taskService.SendMessage(c.Request.Context(), &req, c.GetString("user_id"), c.GetString("business_id"))
	if err != nil {
		h.handleTaskError(c, err)
		return
	}

	h.writeJSON(c, http.StatusOK, resp)
}

func (h *A2ATaskHandler) handleMessageStream(c *gin.Context) {
	var req a2a.SendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.problem(c, a2a.NewProblem(http.StatusBadRequest, a2a.ProblemTypeInvalidRequest, "Invalid Request", "Malformed send message payload: "+err.Error()))
		return
	}

	c.Header("Content-Type", a2a.ContentTypeEventStream)
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	events := make(chan a2a.StreamEvent, 16)
	go func() {
		defer close(events)
		if err := h.taskService.SendStreamingMessage(c.Request.Context(), &req, c.GetString("user_id"), c.GetString("business_id"), events); err != nil {
			payload, _ := json.Marshal(a2a.NewProblem(http.StatusInternalServerError, a2a.ProblemTypeInternal, "Streaming Failed", err.Error()))
			events <- a2a.StreamEvent{
				ID:        fmt.Sprintf("error-%d", time.Now().UnixNano()),
				Event:     a2a.StreamEventError,
				Data:      payload,
				Timestamp: time.Now().UTC(),
				Final:     true,
			}
		}
	}()

	c.Stream(func(w io.Writer) bool {
		select {
		case event, ok := <-events:
			if !ok {
				return false
			}
			fmt.Fprintf(w, "id: %s\n", event.ID)
			fmt.Fprintf(w, "event: %s\n", event.Event)
			fmt.Fprintf(w, "data: %s\n\n", string(event.Data))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			return !event.Final && event.Event != a2a.StreamEventError
		case <-c.Request.Context().Done():
			return false
		}
	})
}

func (h *A2ATaskHandler) handleGetTask(c *gin.Context, taskID string) {
	historyLength, err := optionalIntQuery(c, "historyLength")
	if err != nil {
		h.problem(c, a2a.NewProblem(http.StatusBadRequest, a2a.ProblemTypeInvalidRequest, "Invalid Request", err.Error()))
		return
	}

	task, err := h.taskService.GetTask(c.Request.Context(), taskID, c.GetString("user_id"), c.GetString("business_id"), historyLength)
	if err != nil {
		h.handleTaskError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, task)
}

func (h *A2ATaskHandler) handleListTasks(c *gin.Context) {
	pageSize, err := optionalIntQuery(c, "pageSize")
	if err != nil {
		h.problem(c, a2a.NewProblem(http.StatusBadRequest, a2a.ProblemTypeInvalidRequest, "Invalid Request", err.Error()))
		return
	}
	historyLength, err := optionalIntQuery(c, "historyLength")
	if err != nil {
		h.problem(c, a2a.NewProblem(http.StatusBadRequest, a2a.ProblemTypeInvalidRequest, "Invalid Request", err.Error()))
		return
	}
	includeArtifacts, err := optionalBoolQuery(c, "includeArtifacts")
	if err != nil {
		h.problem(c, a2a.NewProblem(http.StatusBadRequest, a2a.ProblemTypeInvalidRequest, "Invalid Request", err.Error()))
		return
	}

	var statusTimestampAfter *time.Time
	if raw := strings.TrimSpace(c.Query("statusTimestampAfter")); raw != "" {
		parsed, parseErr := time.Parse(time.RFC3339, raw)
		if parseErr != nil {
			h.problem(c, a2a.NewProblem(http.StatusBadRequest, a2a.ProblemTypeInvalidRequest, "Invalid Request", "statusTimestampAfter must be RFC3339"))
			return
		}
		statusTimestampAfter = &parsed
	}

	req := &a2a.ListTasksRequest{
		ContextID:            strings.TrimSpace(c.Query("contextId")),
		PageSize:             pageSize,
		PageToken:            strings.TrimSpace(c.Query("pageToken")),
		HistoryLength:        historyLength,
		StatusTimestampAfter: statusTimestampAfter,
		IncludeArtifacts:     includeArtifacts,
	}
	if rawStatus := strings.TrimSpace(c.Query("status")); rawStatus != "" {
		req.Status = a2a.TaskState(rawStatus)
		if !req.Status.IsValid() {
			h.problem(c, a2a.NewProblem(http.StatusBadRequest, a2a.ProblemTypeInvalidRequest, "Invalid Request", "status must be a valid A2A task state"))
			return
		}
	}

	resp, err := h.taskService.ListTasks(c.Request.Context(), req, c.GetString("user_id"), c.GetString("business_id"))
	if err != nil {
		h.handleTaskError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, resp)
}

func (h *A2ATaskHandler) handleCancelTask(c *gin.Context, taskID string) {
	task, err := h.taskService.CancelTask(c.Request.Context(), taskID, c.GetString("user_id"), c.GetString("business_id"))
	if err != nil {
		h.handleTaskError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, task)
}

func (h *A2ATaskHandler) handleSubscribeTask(c *gin.Context, taskID string) {
	historyLength, err := optionalIntQuery(c, "historyLength")
	if err != nil {
		h.problem(c, a2a.NewProblem(http.StatusBadRequest, a2a.ProblemTypeInvalidRequest, "Invalid Request", err.Error()))
		return
	}
	includeArtifacts, err := optionalBoolQuery(c, "includeArtifacts")
	if err != nil {
		h.problem(c, a2a.NewProblem(http.StatusBadRequest, a2a.ProblemTypeInvalidRequest, "Invalid Request", err.Error()))
		return
	}

	c.Header("Content-Type", a2a.ContentTypeEventStream)
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Header("Vary", "Last-Event-ID")

	lastEventID := strings.TrimSpace(c.GetHeader("Last-Event-ID"))
	events := make(chan a2a.StreamEvent, 16)
	go func() {
		defer close(events)
		if err := h.taskService.SubscribeTaskWithReplay(
			c.Request.Context(),
			taskID,
			c.GetString("user_id"),
			c.GetString("business_id"),
			lastEventID,
			historyLength,
			includeArtifacts == nil || *includeArtifacts,
			events,
		); err != nil {
			payload, _ := json.Marshal(a2a.NewProblem(http.StatusBadRequest, a2a.ProblemTypeUnsupportedOperation, "Subscription Failed", err.Error()))
			events <- a2a.StreamEvent{
				ID:        fmt.Sprintf("error-%d", time.Now().UnixNano()),
				Event:     a2a.StreamEventError,
				Data:      payload,
				Timestamp: time.Now().UTC(),
				Final:     true,
			}
		}
	}()

	c.Stream(func(w io.Writer) bool {
		select {
		case event, ok := <-events:
			if !ok {
				return false
			}
			fmt.Fprintf(w, "id: %s\n", event.ID)
			fmt.Fprintf(w, "event: %s\n", event.Event)
			fmt.Fprintf(w, "data: %s\n\n", string(event.Data))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			return !event.Final && event.Event != a2a.StreamEventError
		case <-c.Request.Context().Done():
			return false
		}
	})
}

func (h *A2ATaskHandler) handlePushConfigRoutes(c *gin.Context, path string) {
	taskID, configID, ok := parsePushConfigPath(path)
	if !ok {
		h.problem(c, a2a.NewProblem(http.StatusNotFound, a2a.ProblemTypeInvalidRequest, "Unknown A2A Operation", "Invalid push notification config path."))
		return
	}

	switch {
	case c.Request.Method == http.MethodPost && configID == "":
		var body a2a.TaskPushNotificationConfig
		if err := c.ShouldBindJSON(&body); err != nil {
			h.problem(c, a2a.NewProblem(http.StatusBadRequest, a2a.ProblemTypeInvalidRequest, "Invalid Request", "Malformed push config payload: "+err.Error()))
			return
		}
		id := body.PushNotificationConfig.ID
		if id == "" && body.Name != "" {
			_, parsedID, err := a2a.ParseTaskPushConfigName(body.Name)
			if err == nil {
				id = parsedID
			}
		}
		if id == "" {
			id = fmt.Sprintf("cfg-%d", time.Now().UnixNano())
		}
		config, err := h.pushService.CreateTaskPushConfig(c.Request.Context(), taskID, c.GetString("user_id"), c.GetString("business_id"), id, &body.PushNotificationConfig)
		if err != nil {
			h.handleTaskError(c, err)
			return
		}
		h.writeJSON(c, http.StatusOK, config)
	case c.Request.Method == http.MethodGet && configID == "":
		pageSize := 0
		if raw := strings.TrimSpace(c.Query("pageSize")); raw != "" {
			pageSize, _ = strconv.Atoi(raw)
		}
		resp, err := h.pushService.ListTaskPushConfigs(c.Request.Context(), taskID, c.GetString("user_id"), c.GetString("business_id"), pageSize, strings.TrimSpace(c.Query("pageToken")))
		if err != nil {
			h.handleTaskError(c, err)
			return
		}
		h.writeJSON(c, http.StatusOK, resp)
	case c.Request.Method == http.MethodGet && configID != "":
		config, err := h.pushService.GetTaskPushConfig(c.Request.Context(), taskID, configID, c.GetString("user_id"), c.GetString("business_id"))
		if err != nil {
			h.handleTaskError(c, err)
			return
		}
		h.writeJSON(c, http.StatusOK, config)
	case c.Request.Method == http.MethodDelete && configID != "":
		if err := h.pushService.DeleteTaskPushConfig(c.Request.Context(), taskID, configID, c.GetString("user_id"), c.GetString("business_id")); err != nil {
			h.handleTaskError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	default:
		h.problem(c, a2a.NewProblem(http.StatusNotFound, a2a.ProblemTypeInvalidRequest, "Unknown A2A Operation", "Unsupported push notification config operation."))
	}
}

func (h *A2ATaskHandler) handleTaskError(c *gin.Context, err error) {
	switch err {
	case nil:
		return
	case a2a.ErrTaskNotFound, services.ErrTaskPushConfigNotFound:
		h.problem(c, a2a.NewProblem(http.StatusNotFound, a2a.ProblemTypeTaskNotFound, "Task Resource Not Found", err.Error()))
	case a2a.ErrUnauthorized, a2a.ErrMissingAuthorization:
		h.problem(c, a2a.NewProblem(http.StatusUnauthorized, a2a.ProblemTypeUnauthorized, "Unauthorized", err.Error()))
	case a2a.ErrTaskStateTerminal, a2a.ErrUnsupportedOperation:
		h.problem(c, a2a.NewProblem(http.StatusBadRequest, a2a.ProblemTypeUnsupportedOperation, "Unsupported Operation", err.Error()))
	case a2a.ErrInvalidMessage, a2a.ErrInvalidTaskState, a2a.ErrPushConfigInvalid, a2a.ErrInvalidPageToken:
		h.problem(c, a2a.NewProblem(http.StatusBadRequest, a2a.ProblemTypeInvalidRequest, "Invalid Request", err.Error()))
	default:
		h.log.Error("A2A request failed", "error", err)
		h.problem(c, a2a.NewProblem(http.StatusInternalServerError, a2a.ProblemTypeInternal, "Internal Error", err.Error()))
	}
}

func (h *A2ATaskHandler) writeJSON(c *gin.Context, status int, payload interface{}) {
	c.Header("Content-Type", a2a.ContentTypeA2AJSON)
	c.JSON(status, payload)
}

func (h *A2ATaskHandler) problem(c *gin.Context, problem a2a.Problem) {
	c.Header("Content-Type", a2a.ContentTypeProblemJSON)
	c.AbortWithStatusJSON(problem.Status, problem)
}

func optionalIntQuery(c *gin.Context, key string) (*int, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be an integer", key)
	}
	return &value, nil
}

func optionalBoolQuery(c *gin.Context, key string) (*bool, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be a boolean", key)
	}
	return &value, nil
}

func isSimpleTaskPath(path string) bool {
	if !strings.HasPrefix(path, "tasks/") {
		return false
	}
	remainder := strings.TrimPrefix(path, "tasks/")
	return remainder != "" && !strings.Contains(remainder, "/") && !strings.Contains(remainder, ":")
}

func parseSubscribeTaskPath(path string) (string, bool) {
	if !strings.HasPrefix(path, "tasks/") {
		return "", false
	}

	switch {
	case strings.HasSuffix(path, ":subscribe"):
		taskID := strings.TrimSuffix(strings.TrimPrefix(path, "tasks/"), ":subscribe")
		return taskID, taskID != "" && !strings.Contains(taskID, "/")
	case strings.HasSuffix(path, "/subscribe"):
		taskID := strings.TrimSuffix(strings.TrimPrefix(path, "tasks/"), "/subscribe")
		return taskID, taskID != "" && !strings.Contains(taskID, "/")
	default:
		return "", false
	}
}

func parsePushConfigPath(path string) (taskID, configID string, ok bool) {
	prefix, suffix, found := strings.Cut(path, "/pushNotificationConfigs")
	if !found || !strings.HasPrefix(prefix, "tasks/") {
		return "", "", false
	}
	taskID = strings.TrimPrefix(prefix, "tasks/")
	if taskID == "" {
		return "", "", false
	}
	if suffix == "" {
		return taskID, "", true
	}
	if !strings.HasPrefix(suffix, "/") {
		return "", "", false
	}
	configID = strings.TrimPrefix(suffix, "/")
	if configID == "" || strings.Contains(configID, "/") {
		return "", "", false
	}
	return taskID, configID, true
}
