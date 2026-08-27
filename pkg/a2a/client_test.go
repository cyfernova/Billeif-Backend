package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSendMessageRejectsLocalhostEndpoint(t *testing.T) {
	client := NewA2AClient(nil, nil)

	_, err := client.SendMessage(context.Background(), "http://127.0.0.1:8080", validSendMessageRequest())
	if err == nil {
		t.Fatal("expected localhost A2A endpoint to be rejected")
	}
}

func TestSendMessageRejectsPrivateDNSResolution(t *testing.T) {
	client := &A2AClient{
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
			Transport: newSafeA2ATransport(
				func(ctx context.Context, host string) ([]net.IPAddr, error) {
					return []net.IPAddr{{IP: net.ParseIP("10.0.0.5")}}, nil
				},
				func(ctx context.Context, network, address string) (net.Conn, error) {
					t.Fatalf("dial should not be called for private DNS result %q", address)
					return nil, nil
				},
			),
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}

	_, err := client.SendMessage(context.Background(), "https://seller.example", validSendMessageRequest())
	if err == nil {
		t.Fatal("expected private DNS result to be rejected")
	}
	if !strings.Contains(err.Error(), "private or local IP addresses are not allowed") {
		t.Fatalf("expected private IP error, got %v", err)
	}
}

func TestSendMessageDoesNotFollowRedirects(t *testing.T) {
	transport := &redirectOnceTransport{}
	client := &A2AClient{
		httpClient: &http.Client{
			Timeout:   2 * time.Second,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}

	_, err := client.SendMessage(context.Background(), "https://seller.example", validSendMessageRequest())
	if err == nil {
		t.Fatal("expected redirect response to be returned as an A2A error")
	}
	if transport.count != 1 {
		t.Fatalf("expected redirect not to be followed, got %d requests", transport.count)
	}
}

func TestSendMessageRejectsOversizedSuccessResponse(t *testing.T) {
	client := a2aClientWithResponse(t, http.StatusOK, oversizedA2AResponseBody(), -1)

	_, err := client.SendMessage(context.Background(), "https://seller.example", validSendMessageRequest())
	if err == nil || !strings.Contains(err.Error(), "A2A response body exceeds") {
		t.Fatalf("expected oversized response error, got %v", err)
	}
}

func TestSendMessageRejectsOversizedNonSuccessResponse(t *testing.T) {
	client := a2aClientWithResponse(t, http.StatusBadGateway, oversizedA2AResponseBody(), -1)

	_, err := client.SendMessage(context.Background(), "https://seller.example", validSendMessageRequest())
	if err == nil || !strings.Contains(err.Error(), "A2A response body exceeds") {
		t.Fatalf("expected oversized response error, got %v", err)
	}
}

func TestSendMessageRejectsResponseThatExceedsMisreportedContentLength(t *testing.T) {
	client := a2aClientWithResponse(t, http.StatusOK, oversizedA2AResponseBody(), 1)

	_, err := client.SendMessage(context.Background(), "https://seller.example", validSendMessageRequest())
	if err == nil || !strings.Contains(err.Error(), "A2A response body exceeds") {
		t.Fatalf("expected oversized response error, got %v", err)
	}
}

func TestSendMessageAcceptsNormalResponse(t *testing.T) {
	responseBody, err := json.Marshal(&SendMessageResponse{Message: &Message{MessageID: "response-1"}})
	if err != nil {
		t.Fatal(err)
	}
	client := a2aClientWithResponse(t, http.StatusOK, responseBody, int64(len(responseBody)))

	response, err := client.SendMessage(context.Background(), "https://seller.example", validSendMessageRequest())
	if err != nil {
		t.Fatalf("expected normal response to succeed: %v", err)
	}
	if response.Message == nil || response.Message.MessageID != "response-1" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestReadResponseBodyRejectsDeclaredOversizeWithoutReading(t *testing.T) {
	body := &countingReader{reader: strings.NewReader("not read")}
	_, err := ReadResponseBody(&http.Response{
		ContentLength: MaxA2AResponseBodyBytes + 1,
		Body:          io.NopCloser(body),
	})
	if !errors.Is(err, ErrA2AResponseBodyTooLarge) {
		t.Fatalf("expected oversized response error, got %v", err)
	}
	if body.reads != 0 {
		t.Fatalf("expected declared oversized body not to be read, got %d reads", body.reads)
	}
}

func TestReadResponseBodyReadsOnlyLimitPlusOneForUnknownLength(t *testing.T) {
	body := &countingReader{reader: strings.NewReader(strings.Repeat("x", int(MaxA2AResponseBodyBytes)+100))}
	_, err := ReadResponseBody(&http.Response{
		ContentLength: -1,
		Body:          io.NopCloser(body),
	})
	if !errors.Is(err, ErrA2AResponseBodyTooLarge) {
		t.Fatalf("expected oversized response error, got %v", err)
	}
	if body.bytesRead != int(MaxA2AResponseBodyBytes)+1 {
		t.Fatalf("expected exactly max+1 bytes to be read, got %d", body.bytesRead)
	}
}

func a2aClientWithResponse(t *testing.T, status int, body []byte, contentLength int64) *A2AClient {
	t.Helper()
	return &A2AClient{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    status,
			ContentLength: contentLength,
			Header:        make(http.Header),
			Body:          io.NopCloser(strings.NewReader(string(body))),
			Request:       req,
		}, nil
	})}}
}

func oversizedA2AResponseBody() []byte {
	return []byte(`{"message":{"messageId":"response-1"}}` + strings.Repeat(" ", 1<<20))
}

func TestApplyStandardHeadersDoesNotForwardCallerAuthorization(t *testing.T) {
	req, err := http.NewRequestWithContext(
		WithAuthorizationHeader(context.Background(), "Bearer caller-token"),
		http.MethodPost,
		"https://example.com/api/v1/a2a/message:send",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	applyStandardHeaders(req, ContentTypeA2AJSON)

	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("expected outbound A2A request to omit caller Authorization header, got %q", got)
	}
}

func validSendMessageRequest() *SendMessageRequest {
	return &SendMessageRequest{
		Message: NewTextMessage(RoleUser, "hello"),
	}
}

type redirectOnceTransport struct {
	count int
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type countingReader struct {
	reader    io.Reader
	reads     int
	bytesRead int
}

func (r *countingReader) Read(p []byte) (int, error) {
	r.reads++
	n, err := r.reader.Read(p)
	r.bytesRead += n
	return n, err
}

func (t *redirectOnceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.count++
	return &http.Response{
		StatusCode: http.StatusFound,
		Header: http.Header{
			"Location": []string{"http://127.0.0.1:8080/private"},
		},
		Body:    io.NopCloser(strings.NewReader("redirect")),
		Request: req,
	}, nil
}
