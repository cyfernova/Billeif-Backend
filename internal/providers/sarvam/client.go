package sarvam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxResponseDrainBytes int64 = 64 << 10

var (
	ErrAPIKeyRequired   = errors.New("sarvam API key is required")
	ErrInvalidBaseURL   = errors.New("invalid Sarvam base URL")
	ErrInvalidRequest   = errors.New("invalid Sarvam request")
	ErrContextRequired  = errors.New("caller context is required")
	ErrRequestTooLarge  = errors.New("Sarvam request exceeds size limit")
	ErrResponseTooLarge = errors.New("Sarvam response exceeds size limit")
	ErrRequestFailed    = errors.New("Sarvam request failed")
	ErrResponseRead     = errors.New("Sarvam response could not be read")
	ErrInvalidResponse  = errors.New("invalid Sarvam response")
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	apiKey  string
	baseURL url.URL
	doer    HTTPDoer
}

type JSONRequest struct {
	Path          string
	Payload       any
	Accept        string
	ResponseLimit int64
}

type Response struct {
	Body        []byte
	StatusCode  int
	ContentType string
	RequestID   string
}

type ProviderError struct {
	StatusCode int
	RequestID  string
}

func (e *ProviderError) Error() string {
	if e == nil {
		return "Sarvam provider request failed"
	}
	if e.RequestID == "" {
		return fmt.Sprintf("Sarvam provider returned status %d", e.StatusCode)
	}
	return fmt.Sprintf("Sarvam provider returned status %d (request_id=%s)", e.StatusCode, e.RequestID)
}

func NewClient(cfg Config, doer HTTPDoer) (*Client, error) {
	cfg = cfg.withDefaults()
	if cfg.APIKey == "" {
		return nil, ErrAPIKeyRequired
	}
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrInvalidBaseURL
	}
	if doer == nil {
		doer = http.DefaultClient
	}
	return &Client{apiKey: cfg.APIKey, baseURL: *parsed, doer: doer}, nil
}

func (c *Client) PostJSON(ctx context.Context, input JSONRequest) (*Response, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	if c == nil || c.doer == nil || c.apiKey == "" {
		return nil, ErrInvalidRequest
	}
	endpoint, err := c.endpoint(input.Path)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(input.Payload)
	if err != nil {
		return nil, fmt.Errorf("%w: JSON encoding failed", ErrInvalidRequest)
	}
	if int64(len(body)) > MaxRequestBytes {
		return nil, ErrRequestTooLarge
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, ErrInvalidRequest
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(APIKeyHeader, c.apiKey)
	if accept := strings.TrimSpace(input.Accept); accept != "" {
		request.Header.Set("Accept", accept)
	}

	response, err := c.doer.Do(request)
	if err != nil {
		if response != nil {
			drainAndClose(response.Body)
		}
		return nil, safeRequestError(ctx, err)
	}
	if response == nil || response.Body == nil {
		return nil, ErrInvalidResponse
	}
	defer drainAndClose(response.Body)

	requestID := safeRequestID(response.Header.Get("x-request-id"), c.apiKey)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, &ProviderError{StatusCode: response.StatusCode, RequestID: requestID}
	}
	responseLimit := input.ResponseLimit
	if responseLimit <= 0 || responseLimit > MaxResponseBytes {
		responseLimit = MaxResponseBytes
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil {
		return nil, ErrResponseRead
	}
	if int64(len(responseBody)) > responseLimit {
		return nil, ErrResponseTooLarge
	}
	return &Response{
		Body:        responseBody,
		StatusCode:  response.StatusCode,
		ContentType: strings.TrimSpace(response.Header.Get("Content-Type")),
		RequestID:   requestID,
	}, nil
}

func (c *Client) endpoint(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return "", ErrInvalidRequest
	}
	reference, err := url.Parse(path)
	if err != nil || reference.IsAbs() || reference.Host != "" || reference.RawQuery != "" || reference.Fragment != "" {
		return "", ErrInvalidRequest
	}
	endpoint := c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + reference.Path
	endpoint.RawPath = ""
	return endpoint.String(), nil
}

func safeRequestError(ctx context.Context, transportErr error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%w: %w", ErrRequestFailed, ctxErr)
	}
	if errors.Is(transportErr, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", ErrRequestFailed, context.DeadlineExceeded)
	}
	if errors.Is(transportErr, context.Canceled) {
		return fmt.Errorf("%w: %w", ErrRequestFailed, context.Canceled)
	}
	return ErrRequestFailed
}

func safeRequestID(value, apiKey string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || (apiKey != "" && strings.Contains(value, apiKey)) {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || strings.ContainsRune("-_.:", r) {
			continue
		}
		return ""
	}
	return value
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, body, maxResponseDrainBytes)
	_ = body.Close()
}
