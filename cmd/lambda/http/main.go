package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"invoice-backend/internal/app"
	"invoice-backend/internal/config"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	ginadapter "github.com/awslabs/aws-lambda-go-api-proxy/gin"
)

const ordinaryRequestTimeout = 25 * time.Second

type requestProxy func(context.Context, events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error)

type runtimeInitializer func(context.Context) (requestProxy, error)

type lazyRuntimeProxy struct {
	once       sync.Once
	initialize runtimeInitializer
	proxy      requestProxy
	initErr    error
}

var adapter = &lazyRuntimeProxy{initialize: initRuntime}

func initRuntime(ctx context.Context) (requestProxy, error) {
	rt, err := app.Initialize(ctx, app.InitializeOptions{
		EnableWorker: false,
		Profile:      config.ProfileHTTP,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize runtime: %w", err)
	}
	return ginadapter.New(rt.Router).ProxyWithContext, nil
}

func (r *lazyRuntimeProxy) ProxyWithContext(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	r.once.Do(func() {
		r.proxy, r.initErr = r.initialize(ctx)
	})
	if r.initErr != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: r.initErr.Error()}, nil
	}
	return r.proxy(ctx, req)
}

func handle(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	return proxyWithRequestDeadline(ctx, req, adapter.ProxyWithContext)
}

func proxyWithRequestDeadline(
	ctx context.Context,
	req events.APIGatewayProxyRequest,
	proxy requestProxy,
) (events.APIGatewayProxyResponse, error) {
	if isRESTOwnedStreamingRequest(req) {
		return events.APIGatewayProxyResponse{StatusCode: 404}, nil
	}

	requestCtx, cancel := context.WithTimeout(ctx, ordinaryRequestTimeout)
	defer cancel()
	return proxy(requestCtx, req)
}

func isRESTOwnedStreamingRequest(req events.APIGatewayProxyRequest) bool {
	if req.HTTPMethod == "POST" && req.Path == "/api/v1/a2a/message:stream" {
		return true
	}
	if req.HTTPMethod != "GET" {
		return false
	}

	const taskPrefix = "/api/v1/a2a/tasks/"
	if !strings.HasPrefix(req.Path, taskPrefix) {
		return false
	}
	for _, subscribeSuffix := range []string{"/subscribe", ":subscribe"} {
		if strings.HasSuffix(req.Path, subscribeSuffix) {
			taskID := strings.TrimSuffix(strings.TrimPrefix(req.Path, taskPrefix), subscribeSuffix)
			return taskID != "" && !strings.Contains(taskID, "/")
		}
	}
	return false
}

func main() {
	lambda.Start(handle)
}
