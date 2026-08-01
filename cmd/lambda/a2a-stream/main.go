package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"invoice-backend/internal/app"
	"invoice-backend/internal/config"

	"github.com/aws/aws-lambda-go/events"
	"github.com/gin-gonic/gin"
)

const (
	lambdaRuntimeVersion = "2018-06-01"
	streamContentType    = "application/vnd.awslambda.http-integration-response"
)

type invocation struct {
	RequestID  string
	DeadlineMS string
	Event      events.APIGatewayProxyRequest
}

type runtimeErrorResponse struct {
	ErrorMessage string `json:"errorMessage"`
	ErrorType    string `json:"errorType"`
}

type streamResponseMetadata struct {
	StatusCode int               `json:"statusCode"`
	Headers    map[string]string `json:"headers,omitempty"`
}

type runtimeClient struct {
	baseURL string
	client  *http.Client
}

func newRuntimeClient(runtimeAPI string) *runtimeClient {
	return &runtimeClient{
		baseURL: fmt.Sprintf("http://%s/%s/runtime", runtimeAPI, lambdaRuntimeVersion),
		client:  &http.Client{},
	}
}

func (c *runtimeClient) nextInvocation(ctx context.Context) (*invocation, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/invocation/next", nil)
	if err != nil {
		return nil, fmt.Errorf("build next invocation request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get next invocation: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read invocation payload: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected runtime status %d: %s", resp.StatusCode, string(body))
	}

	var event events.APIGatewayProxyRequest
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, fmt.Errorf("decode api gateway event: %w", err)
	}

	return &invocation{
		RequestID:  resp.Header.Get("Lambda-Runtime-Aws-Request-Id"),
		DeadlineMS: resp.Header.Get("Lambda-Runtime-Deadline-Ms"),
		Event:      event,
	}, nil
}

func (c *runtimeClient) postInitError(ctx context.Context, err error) {
	_ = c.postError(ctx, c.baseURL+"/init/error", err)
}

func (c *runtimeClient) postInvocationError(ctx context.Context, requestID string, err error) {
	_ = c.postError(ctx, c.baseURL+"/invocation/"+requestID+"/error", err)
}

func (c *runtimeClient) postError(ctx context.Context, endpoint string, err error) error {
	payload, marshalErr := json.Marshal(runtimeErrorResponse{
		ErrorMessage: err.Error(),
		ErrorType:    "RuntimeError",
	})
	if marshalErr != nil {
		return fmt.Errorf("marshal runtime error: %w", marshalErr)
	}

	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if reqErr != nil {
		return fmt.Errorf("build runtime error request: %w", reqErr)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, callErr := c.client.Do(req)
	if callErr != nil {
		return fmt.Errorf("post runtime error: %w", callErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("runtime rejected error response %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (c *runtimeClient) postStreamingResponse(ctx context.Context, requestID string, metadata streamResponseMetadata, body io.Reader) error {
	responseURL := c.baseURL + "/invocation/" + requestID + "/response"
	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()

		metaJSON, err := json.Marshal(metadata)
		if err != nil {
			_ = pw.CloseWithError(fmt.Errorf("marshal stream metadata: %w", err))
			return
		}
		if _, err := pw.Write(metaJSON); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("write stream metadata: %w", err))
			return
		}

		if _, err := pw.Write(make([]byte, 8)); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("write stream metadata delimiter: %w", err))
			return
		}

		if _, err := io.Copy(pw, body); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("write stream body: %w", err))
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, responseURL, pr)
	if err != nil {
		return fmt.Errorf("build runtime response request: %w", err)
	}
	req.Header.Set("Content-Type", streamContentType)
	req.Header.Set("Lambda-Runtime-Function-Response-Mode", "streaming")
	req.Header.Set("Transfer-Encoding", "chunked")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("post streaming runtime response: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("runtime rejected streaming response %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

type streamingResponseWriter struct {
	headers     http.Header
	statusCode  int
	wroteHeader bool
	headerReady chan struct{}
	headerOnce  sync.Once
	closeNotify chan bool
	closeOnce   sync.Once
	body        *io.PipeWriter
	mu          sync.Mutex
}

func newStreamingResponseWriter(body *io.PipeWriter) *streamingResponseWriter {
	return &streamingResponseWriter{
		headers:     make(http.Header),
		headerReady: make(chan struct{}),
		closeNotify: make(chan bool, 1),
		body:        body,
	}
}

func (w *streamingResponseWriter) Header() http.Header {
	return w.headers
}

func (w *streamingResponseWriter) WriteHeader(code int) {
	w.mu.Lock()
	if w.wroteHeader {
		w.mu.Unlock()
		return
	}
	w.statusCode = code
	w.wroteHeader = true
	w.mu.Unlock()
	w.signalHeaderReady()
}

func (w *streamingResponseWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	if !w.wroteHeader {
		w.statusCode = http.StatusOK
		w.wroteHeader = true
		w.mu.Unlock()
		w.signalHeaderReady()
	} else {
		w.mu.Unlock()
	}
	return w.body.Write(p)
}

func (w *streamingResponseWriter) Flush() {}

func (w *streamingResponseWriter) CloseNotify() <-chan bool {
	return w.closeNotify
}

func (w *streamingResponseWriter) metadata() streamResponseMetadata {
	w.mu.Lock()
	statusCode := w.statusCode
	w.mu.Unlock()

	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	headers := make(map[string]string, len(w.headers))
	for key, values := range w.headers {
		if len(values) == 0 {
			continue
		}
		headers[key] = strings.Join(values, ",")
	}

	return streamResponseMetadata{
		StatusCode: statusCode,
		Headers:    headers,
	}
}

func (w *streamingResponseWriter) close(err error) {
	w.signalHeaderReady()
	w.signalClose()
	if err != nil {
		_ = w.body.CloseWithError(err)
		return
	}
	_ = w.body.Close()
}

func (w *streamingResponseWriter) waitForHeaders(timeout time.Duration) {
	select {
	case <-w.headerReady:
	case <-time.After(timeout):
	}
}

func (w *streamingResponseWriter) signalHeaderReady() {
	w.headerOnce.Do(func() {
		close(w.headerReady)
	})
}

func (w *streamingResponseWriter) signalClose() {
	w.closeOnce.Do(func() {
		select {
		case w.closeNotify <- true:
		default:
		}
		close(w.closeNotify)
	})
}

type appRuntime struct {
	once sync.Once
	rt   *app.Runtime
	err  error
}

var streamRuntime appRuntime

func (r *appRuntime) initialize() {
	r.rt, r.err = app.Initialize(context.Background(), app.InitializeOptions{
		EnableWorker: false,
		Profile:      config.ProfileA2A,
	})
}

func (r *appRuntime) get() (*app.Runtime, error) {
	r.once.Do(r.initialize)
	if r.err != nil {
		return nil, r.err
	}
	return r.rt, nil
}

func (r *appRuntime) close() {
	if r.rt != nil {
		r.rt.Close()
	}
}

func main() {
	runtimeAPI := os.Getenv("AWS_LAMBDA_RUNTIME_API")
	if runtimeAPI == "" {
		if err := runLocalBridge(context.Background()); err != nil {
			panic(fmt.Sprintf("stream bridge server error: %v", err))
		}
		return
	}

	if err := runLambdaRuntime(context.Background(), runtimeAPI); err != nil {
		panic(fmt.Sprintf("lambda runtime loop exited: %v", err))
	}
}

func runLocalBridge(ctx context.Context) error {
	rt, err := streamRuntime.get()
	if err != nil {
		return fmt.Errorf("failed to initialize stream runtime: %w", err)
	}
	defer streamRuntime.close()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           rt.Router,
		ReadHeaderTimeout: 15 * time.Second,
	}

	rt.Log.Info("starting A2A stream bridge", "port", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	_ = ctx
	return nil
}

func runLambdaRuntime(ctx context.Context, runtimeAPI string) error {
	client := newRuntimeClient(runtimeAPI)
	rt, err := streamRuntime.get()
	if err != nil {
		client.postInitError(ctx, fmt.Errorf("failed to initialize stream runtime: %w", err))
		return err
	}
	defer streamRuntime.close()

	for {
		inv, err := client.nextInvocation(ctx)
		if err != nil {
			return err
		}

		invocationCtx, cancel := invocationContext(inv.DeadlineMS)
		err = handleInvocation(invocationCtx, rt.Router, client, inv)
		cancel()

		if err != nil {
			rt.Log.Error("failed to process streaming invocation", "request_id", inv.RequestID, "error", err)
			client.postInvocationError(context.Background(), inv.RequestID, err)
		}
	}
}

func invocationContext(deadlineMS string) (context.Context, context.CancelFunc) {
	if deadlineMS == "" {
		return context.WithCancel(context.Background())
	}

	ms, err := strconv.ParseInt(deadlineMS, 10, 64)
	if err != nil {
		return context.WithCancel(context.Background())
	}

	deadline := time.UnixMilli(ms).Add(-100 * time.Millisecond)
	return context.WithDeadline(context.Background(), deadline)
}

func handleInvocation(ctx context.Context, router *gin.Engine, client *runtimeClient, inv *invocation) error {
	req, err := buildHTTPRequest(ctx, inv.Event)
	if err != nil {
		return client.postStreamingResponse(ctx, inv.RequestID, streamResponseMetadata{
			StatusCode: http.StatusBadRequest,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
		}, strings.NewReader(fmt.Sprintf("{\"error\":\"invalid request: %s\"}", escapeJSON(err.Error()))))
	}

	bodyReader, bodyWriter := io.Pipe()
	respWriter := newStreamingResponseWriter(bodyWriter)

	done := make(chan struct{})
	go func() {
		defer close(done)
		router.ServeHTTP(respWriter, req)
		respWriter.close(nil)
	}()

	respWriter.waitForHeaders(150 * time.Millisecond)
	metadata := respWriter.metadata()

	if err := client.postStreamingResponse(ctx, inv.RequestID, metadata, bodyReader); err != nil {
		respWriter.close(err)
		<-done
		return err
	}

	<-done
	return nil
}

func buildHTTPRequest(ctx context.Context, event events.APIGatewayProxyRequest) (*http.Request, error) {
	method := event.HTTPMethod
	if method == "" {
		method = http.MethodGet
	}

	path := event.Path
	if path == "" {
		path = "/"
	}

	rawQuery := encodeRawQuery(event)
	u := &url.URL{
		Scheme:   "https",
		Host:     "lambda.internal",
		Path:     path,
		RawQuery: rawQuery,
	}

	bodyBytes := []byte(event.Body)
	if event.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(event.Body)
		if err != nil {
			return nil, fmt.Errorf("decode base64 body: %w", err)
		}
		bodyBytes = decoded
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}

	for key, values := range event.MultiValueHeaders {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	for key, value := range event.Headers {
		if req.Header.Get(key) == "" {
			req.Header.Set(key, value)
		}
	}

	if sourceIP := event.RequestContext.Identity.SourceIP; sourceIP != "" {
		req.RemoteAddr = sourceIP
	}

	return req, nil
}

func encodeRawQuery(event events.APIGatewayProxyRequest) string {
	values := url.Values{}
	for key, vals := range event.MultiValueQueryStringParameters {
		for _, value := range vals {
			values.Add(key, value)
		}
	}
	for key, value := range event.QueryStringParameters {
		if _, exists := values[key]; exists {
			continue
		}
		values.Set(key, value)
	}
	return values.Encode()
}

func escapeJSON(value string) string {
	b, err := json.Marshal(value)
	if err != nil {
		return "unknown"
	}
	// Trim quotes because marshaled string is returned with surrounding quotes.
	if len(b) >= 2 {
		return string(b[1 : len(b)-1])
	}
	return string(b)
}
