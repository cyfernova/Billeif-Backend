package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

const (
	HeaderRuntimeSessionID = "X-Amzn-Bedrock-AgentCore-Runtime-Session-Id"

	StatusHealthy     = "Healthy"
	StatusHealthyBusy = "HealthyBusy"

	MinRuntimeSessionIDLength = 33
	MaxRuntimeSessionIDLength = 256
	MaxInvocationBodySize     = 16 << 10
)

var ErrShuttingDown = errors.New("voice runtime is shutting down")

var errInvocationHandlerPanicked = errors.New("invocation handler panicked")

// Config contains non-secret runtime metadata.
type Config struct {
	RuntimeID string
	AWSRegion string
}

// InvocationRequest carries a validated JSON request and the trusted headers
// forwarded by AgentCore. Callers must never log Authorization.
type InvocationRequest struct {
	Body             json.RawMessage
	Authorization    string
	RuntimeSessionID string
	RuntimeID        string
	AWSRegion        string
}

// InvocationResponse is a bounded signaling response produced by an injected
// handler. Body must contain one valid JSON value unless StatusCode is 204.
type InvocationResponse struct {
	StatusCode int
	Body       json.RawMessage
}

// InvocationHandler handles one validated AgentCore invocation.
type InvocationHandler interface {
	Invoke(context.Context, InvocationRequest) (InvocationResponse, error)
}

// InvocationHandlerFunc adapts a function to InvocationHandler.
type InvocationHandlerFunc func(context.Context, InvocationRequest) (InvocationResponse, error)

func (fn InvocationHandlerFunc) Invoke(ctx context.Context, request InvocationRequest) (InvocationResponse, error) {
	return fn(ctx, request)
}

// Server implements the AgentCore HTTP runtime contract.
type Server struct {
	config     Config
	handler    InvocationHandler
	activities *activityTracker

	rootContext context.Context
	cancelRoot  context.CancelFunc

	shutdown shutdownState
}

// NewServer creates a provider-free AgentCore HTTP handler.
func NewServer(config Config, handler InvocationHandler, closers ...io.Closer) *Server {
	rootContext, cancelRoot := context.WithCancel(context.Background())
	return &Server{
		config:      config,
		handler:     handler,
		activities:  newActivityTracker(),
		rootContext: rootContext,
		cancelRoot:  cancelRoot,
		shutdown:    newShutdownState(closers),
	}
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	setSecurityHeaders(writer.Header())

	switch request.URL.Path {
	case "/ping":
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handlePing(writer)
	case "/invocations":
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodPost)
			writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handleInvocation(writer, request)
	default:
		writeError(writer, http.StatusNotFound, "not found")
	}
}

func (s *Server) handlePing(writer http.ResponseWriter) {
	status := StatusHealthy
	if s.activities.active() > 0 {
		status = StatusHealthyBusy
	}
	writeJSON(writer, http.StatusOK, json.RawMessage(`{"status":"`+status+`"}`))
}

func (s *Server) handleInvocation(writer http.ResponseWriter, request *http.Request) {
	if s.shutdown.started.Load() {
		writeError(writer, http.StatusServiceUnavailable, "shutting down")
		return
	}

	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(writer, http.StatusUnsupportedMediaType, "unsupported media type")
		return
	}

	authorization, ok := exactlyOneHeaderValue(request.Header, "Authorization")
	if !ok || !validBearerAuthorization(authorization) {
		writeError(writer, http.StatusUnauthorized, "unauthorized")
		return
	}

	runtimeSessionID, ok := exactlyOneHeaderValue(request.Header, HeaderRuntimeSessionID)
	if !ok || len(runtimeSessionID) < MinRuntimeSessionIDLength || len(runtimeSessionID) > MaxRuntimeSessionIDLength {
		writeError(writer, http.StatusBadRequest, "invalid request")
		return
	}

	body, decodeStatus := decodeInvocationBody(writer, request)
	if decodeStatus != 0 {
		if decodeStatus == http.StatusRequestEntityTooLarge {
			writeError(writer, decodeStatus, "request too large")
		} else {
			writeError(writer, decodeStatus, "invalid request")
		}
		return
	}

	lease, err := s.AcquireActivity()
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "shutting down")
		return
	}
	defer lease.Close()

	invocationContext, cancel := context.WithCancel(s.rootContext)
	stopClientCancellation := context.AfterFunc(request.Context(), cancel)
	defer func() {
		stopClientCancellation()
		cancel()
	}()

	if s.handler == nil {
		writeError(writer, http.StatusServiceUnavailable, "runtime unavailable")
		return
	}

	response, err := invokeHandler(invocationContext, s.handler, InvocationRequest{
		Body:             body,
		Authorization:    authorization,
		RuntimeSessionID: runtimeSessionID,
		RuntimeID:        s.config.RuntimeID,
		AWSRegion:        s.config.AWSRegion,
	})
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "internal error")
		return
	}

	statusCode := response.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	if statusCode < http.StatusOK || statusCode > 599 {
		writeError(writer, http.StatusInternalServerError, "internal error")
		return
	}
	if statusCode == http.StatusNoContent {
		writer.WriteHeader(statusCode)
		return
	}
	if len(bytes.TrimSpace(response.Body)) == 0 {
		response.Body = json.RawMessage(`{}`)
	}
	if len(response.Body) > MaxInvocationBodySize || !json.Valid(response.Body) {
		writeError(writer, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(writer, statusCode, response.Body)
}

func invokeHandler(ctx context.Context, handler InvocationHandler, request InvocationRequest) (response InvocationResponse, err error) {
	defer func() {
		if recover() != nil {
			response = InvocationResponse{}
			err = errInvocationHandlerPanicked
		}
	}()
	return handler.Invoke(ctx, request)
}

func decodeInvocationBody(writer http.ResponseWriter, request *http.Request) (json.RawMessage, int) {
	limitedBody := http.MaxBytesReader(writer, request.Body, MaxInvocationBodySize)
	decoder := json.NewDecoder(limitedBody)

	var body json.RawMessage
	if err := decoder.Decode(&body); err != nil {
		if isBodyTooLarge(err) {
			return nil, http.StatusRequestEntityTooLarge
		}
		return nil, http.StatusBadRequest
	}
	trimmedBody := bytes.TrimSpace(body)
	if len(trimmedBody) == 0 || trimmedBody[0] != '{' {
		return nil, http.StatusBadRequest
	}

	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if isBodyTooLarge(err) {
			return nil, http.StatusRequestEntityTooLarge
		}
		return nil, http.StatusBadRequest
	}
	return body, 0
}

func isBodyTooLarge(err error) bool {
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError)
}

func validBearerAuthorization(value string) bool {
	parts := strings.Fields(value)
	return len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] != ""
}

func exactlyOneHeaderValue(header http.Header, name string) (string, bool) {
	values := header.Values(name)
	if len(values) != 1 {
		return "", false
	}
	return values[0], true
}

func setSecurityHeaders(header http.Header) {
	header.Set("Content-Type", "application/json")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
}

func writeError(writer http.ResponseWriter, statusCode int, message string) {
	body, _ := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: message})
	writeJSON(writer, statusCode, body)
}

func writeJSON(writer http.ResponseWriter, statusCode int, body json.RawMessage) {
	writer.WriteHeader(statusCode)
	_, _ = writer.Write(body)
}
