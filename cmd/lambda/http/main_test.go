package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	ginadapter "github.com/awslabs/aws-lambda-go-api-proxy/gin"
	"github.com/gin-gonic/gin"
)

func TestProxyWithRequestDeadlineStripsHTTPAPIStageBeforeGinRouting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "We are live!")
	})

	response, err := proxyWithRequestDeadline(
		context.Background(),
		events.APIGatewayProxyRequest{
			HTTPMethod: "GET",
			Path:       "/dev/health",
			RequestContext: events.APIGatewayProxyRequestContext{
				DomainName: "api.example.com",
				Stage:      "dev",
			},
		},
		ginadapter.New(router).ProxyWithContext,
	)
	if err != nil {
		t.Fatalf("proxy staged HTTP API request: %v", err)
	}
	if response.StatusCode != http.StatusOK || response.Body != "We are live!" {
		t.Fatalf("response = %#v, want staged request routed to /health", response)
	}
}

func TestStripHTTPAPIStagePrefixOnlyRemovesExactStageBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		path  string
		stage string
		want  string
	}{
		{name: "stage root", path: "/dev", stage: "dev", want: "/"},
		{name: "nested route", path: "/dev/health", stage: "dev", want: "/health"},
		{name: "similar prefix", path: "/development/health", stage: "dev", want: "/development/health"},
		{name: "default stage", path: "/health", stage: "$default", want: "/health"},
		{name: "blank stage", path: "/health", stage: "", want: "/health"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			request := stripHTTPAPIStagePrefix(events.APIGatewayProxyRequest{
				Path: test.path,
				RequestContext: events.APIGatewayProxyRequestContext{
					Stage: test.stage,
				},
			})
			if request.Path != test.want {
				t.Fatalf("path = %q, want %q", request.Path, test.want)
			}
		})
	}
}

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

func TestProxyWithRequestDeadlinePreservesEarlierLambdaDeadline(t *testing.T) {
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

func TestProxyWithRequestDeadlineBoundsLazyRuntimeInitialization(t *testing.T) {
	startedAt := time.Now()
	var gotInitializationDeadline time.Time
	runtime := &lazyRuntimeProxy{
		initialize: func(ctx context.Context) (requestProxy, error) {
			var ok bool
			gotInitializationDeadline, ok = ctx.Deadline()
			if !ok {
				t.Fatal("lazy runtime initialization context has no deadline")
			}
			return func(context.Context, events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
				return events.APIGatewayProxyResponse{StatusCode: 204}, nil
			}, nil
		},
	}

	response, err := proxyWithRequestDeadline(
		context.Background(),
		events.APIGatewayProxyRequest{HTTPMethod: "GET", Path: "/health"},
		runtime.ProxyWithContext,
	)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	if response.StatusCode != 204 {
		t.Fatalf("status = %d, want 204", response.StatusCode)
	}
	remaining := gotInitializationDeadline.Sub(startedAt)
	if remaining < 24*time.Second || remaining > 26*time.Second {
		t.Fatalf("initialization deadline = %s after start, want a 25-second cap", remaining)
	}
}

func TestProxyWithRequestDeadlinePropagatesLambdaCancellationToLazyInitialization(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()

	var gotInitializationError error
	runtime := &lazyRuntimeProxy{
		initialize: func(ctx context.Context) (requestProxy, error) {
			gotInitializationError = ctx.Err()
			return nil, ctx.Err()
		},
	}

	response, err := proxyWithRequestDeadline(
		parent,
		events.APIGatewayProxyRequest{HTTPMethod: "GET", Path: "/health"},
		runtime.ProxyWithContext,
	)
	if err != nil {
		t.Fatalf("proxy canceled request: %v", err)
	}
	if response.StatusCode != 500 {
		t.Fatalf("status = %d, want 500 when initialization is canceled", response.StatusCode)
	}
	if gotInitializationError != context.Canceled {
		t.Fatalf("initialization error = %v, want Lambda context cancellation", gotInitializationError)
	}
}

func TestProxyWithRequestDeadlineRejectsRESTOwnedStreamingRoutes(t *testing.T) {
	t.Parallel()

	tests := []events.APIGatewayProxyRequest{
		{HTTPMethod: "POST", Path: "/api/v1/a2a/message:stream"},
		{HTTPMethod: "GET", Path: "/api/v1/a2a/tasks/task-123/subscribe"},
		{HTTPMethod: "GET", Path: "/api/v1/a2a/tasks/task-123:subscribe"},
		{
			HTTPMethod: "POST",
			Path:       "/dev/api/v1/a2a/message:stream",
			RequestContext: events.APIGatewayProxyRequestContext{
				Stage: "dev",
			},
		},
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
