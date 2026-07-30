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

var (
	initOnce sync.Once
	adapter  *ginadapter.GinLambda
	initErr  error
)

func initRuntime() {
	ctx := context.Background()
	rt, err := app.Initialize(ctx, app.InitializeOptions{
		EnableWorker: false,
		Profile:      config.ProfileHTTP,
	})
	if err != nil {
		initErr = fmt.Errorf("initialize runtime: %w", err)
		return
	}
	adapter = ginadapter.New(rt.Router)
}

func handle(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	initOnce.Do(initRuntime)
	if initErr != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: initErr.Error()}, nil
	}
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

	const (
		taskPrefix      = "/api/v1/a2a/tasks/"
		subscribeSuffix = "/subscribe"
	)
	if !strings.HasPrefix(req.Path, taskPrefix) || !strings.HasSuffix(req.Path, subscribeSuffix) {
		return false
	}
	taskID := strings.TrimSuffix(strings.TrimPrefix(req.Path, taskPrefix), subscribeSuffix)
	return taskID != "" && !strings.Contains(taskID, "/")
}

func main() {
	lambda.Start(handle)
}
