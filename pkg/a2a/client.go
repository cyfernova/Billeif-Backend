package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"invoice-backend/pkg/ap2"
	"invoice-backend/pkg/logger"
)

type A2AClient struct {
	httpClient *http.Client
	log        *logger.Logger
}

func NewA2AClient(_ *ap2.SignatureService, log *logger.Logger) *A2AClient {
	return &A2AClient{
		httpClient: newSafeA2AHTTPClient(30 * time.Second),
		log:        log,
	}
}

func (c *A2AClient) SendMessage(ctx context.Context, receiverEndpoint string, req *SendMessageRequest) (*SendMessageResponse, error) {
	if err := ValidateSendMessageRequest(req); err != nil {
		return nil, err
	}

	endpoint, err := safeA2AMessageURL(receiverEndpoint)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal send message request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build send message request: %w", err)
	}
	applyStandardHeaders(httpReq, ContentTypeA2AJSON)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send A2A message: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read A2A response: %w", err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, parseProblemResponse(resp.StatusCode, respBody)
	}

	var result SendMessageResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode A2A response: %w", err)
	}

	return &result, nil
}

func (c *A2AClient) SendMessageWithRetry(ctx context.Context, receiverEndpoint string, req *SendMessageRequest, maxRetries int) (*SendMessageResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err := c.SendMessage(ctx, receiverEndpoint, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt == maxRetries {
			break
		}
		backoff := time.Duration(1<<attempt) * time.Second
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

func applyStandardHeaders(req *http.Request, contentType string) {
	req.Header.Set("Accept", ContentTypeA2AJSON)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set(HeaderVersion, SupportedVersion)
}

func normalizeA2ABaseURL(endpoint string) string {
	base := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	switch {
	case strings.HasSuffix(base, "/message:send"):
		return strings.TrimSuffix(base, "/message:send")
	case strings.HasSuffix(base, "/message:stream"):
		return strings.TrimSuffix(base, "/message:stream")
	case strings.Contains(base, "/api/v1/a2a"):
		return base
	default:
		return base + "/api/v1/a2a"
	}
}

func parseProblemResponse(statusCode int, body []byte) error {
	var problem Problem
	if err := json.Unmarshal(body, &problem); err == nil && problem.Type != "" {
		return fmt.Errorf("%s: %s", problem.Title, problem.Detail)
	}
	return fmt.Errorf("A2A request failed with status %d: %s", statusCode, strings.TrimSpace(string(body)))
}
