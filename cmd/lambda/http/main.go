package main

import (
	"context"
	"fmt"
	"sync"

	"invoice-backend/internal/app"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	ginadapter "github.com/awslabs/aws-lambda-go-api-proxy/gin"
)

var (
	initOnce sync.Once
	adapter  *ginadapter.GinLambda
	initErr  error
)

func initRuntime() {
	ctx := context.Background()
	rt, err := app.Initialize(ctx, app.InitializeOptions{EnableWorker: false})
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
	return adapter.ProxyWithContext(ctx, req)
}

func main() {
	lambda.Start(handle)
}
