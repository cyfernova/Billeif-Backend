package main

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
)

func TestProxyWithRequestDeadlineCapsOrdinaryRequestsAtTwentyFiveSeconds(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		HTTPMethod: "GET",
		Path:       "/health",
	}
	startedAt := time.Now()
	var gotRequest events.APIGatewayProxyRequest
	var gotDeadline time.Time

	response, err := proxyWithRequestDeadline(
		context.Background(),
		request,
		func(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
			gotRequest = request
			var ok bool
			gotDeadline, ok = ctx.Deadline()
			if !ok {
				t.Fatal("ordinary request context has no deadline")
			}
			return events.APIGatewayProxyResponse{StatusCode: 204}, nil
		},
	)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	if response.StatusCode != 204 {
		t.Fatalf("status = %d, want 204", response.StatusCode)
	}
	if gotRequest.HTTPMethod != request.HTTPMethod || gotRequest.Path != request.Path {
		t.Fatalf("request = %#v, want %#v", gotRequest, request)
	}

	remaining := gotDeadline.Sub(startedAt)
	if remaining < 24*time.Second || remaining > 26*time.Second {
		t.Fatalf("request deadline = %s after start, want a 25-second cap", remaining)
	}
}

func TestProxyWithRequestDeadlinePreservesEarlierLambdaCancellation(t *testing.T) {
	parentDeadline := time.Now().Add(2 * time.Second)
	parent, cancel := context.WithDeadline(context.Background(), parentDeadline)
	defer cancel()

	var gotDeadline time.Time
	_, err := proxyWithRequestDeadline(
		parent,
		events.APIGatewayProxyRequest{},
		func(ctx context.Context, _ events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
			var ok bool
			gotDeadline, ok = ctx.Deadline()
			if !ok {
				t.Fatal("proxied request context has no deadline")
			}
			return events.APIGatewayProxyResponse{}, nil
		},
	)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	if !gotDeadline.Equal(parentDeadline) {
		t.Fatalf("request deadline = %s, want earlier parent deadline %s", gotDeadline, parentDeadline)
	}
}

func TestProxyWithRequestDeadlineRejectsRESTOwnedStreamingRoutes(t *testing.T) {
	t.Parallel()

	tests := []events.APIGatewayProxyRequest{
		{HTTPMethod: "POST", Path: "/api/v1/a2a/message:stream"},
		{HTTPMethod: "GET", Path: "/api/v1/a2a/tasks/task-123/subscribe"},
	}
	for _, request := range tests {
		request := request
		t.Run(request.HTTPMethod+" "+request.Path, func(t *testing.T) {
			proxyCalled := false
			response, err := proxyWithRequestDeadline(
				context.Background(),
				request,
				func(context.Context, events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
					proxyCalled = true
					return events.APIGatewayProxyResponse{StatusCode: 200}, nil
				},
			)
			if err != nil {
				t.Fatalf("reject streaming request: %v", err)
			}
			if proxyCalled {
				t.Fatal("ordinary HTTP proxy handled a REST-owned streaming request")
			}
			if response.StatusCode != 404 {
				t.Fatalf("status = %d, want 404", response.StatusCode)
			}
		})
	}
}

func TestProxyWithRequestDeadlineDoesNotOvermatchOrdinaryRoutes(t *testing.T) {
	t.Parallel()

	tests := []events.APIGatewayProxyRequest{
		{HTTPMethod: "GET", Path: "/api/v1/a2a/message:stream"},
		{HTTPMethod: "POST", Path: "/api/v1/a2a/tasks/task-123/subscribe"},
		{HTTPMethod: "GET", Path: "/api/v1/a2a/tasks/task-123/subscribe/extra"},
	}
	for _, request := range tests {
		request := request
		t.Run(request.HTTPMethod+" "+request.Path, func(t *testing.T) {
			response, err := proxyWithRequestDeadline(
				context.Background(),
				request,
				func(context.Context, events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
					return events.APIGatewayProxyResponse{StatusCode: 204}, nil
				},
			)
			if err != nil {
				t.Fatalf("proxy ordinary request: %v", err)
			}
			if response.StatusCode != 204 {
				t.Fatalf("status = %d, want ordinary proxy status 204", response.StatusCode)
			}
		})
	}
}
