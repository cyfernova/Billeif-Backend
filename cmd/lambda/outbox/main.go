package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"invoice-backend/internal/app"
	"invoice-backend/internal/config"
	"invoice-backend/internal/outbox"
	postgresrepo "invoice-backend/internal/repositories/postgres"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-lambda-go/lambdacontext"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

type dispatcherRunner interface {
	Dispatch(ctx context.Context, owner string) (outbox.DispatchResult, error)
}

type lambdaHandler struct {
	dispatcher   dispatcherRunner
	metricWriter io.Writer
	environment  string
	now          func() time.Time
}

func (h lambdaHandler) Handle(ctx context.Context) (outbox.DispatchResult, error) {
	if h.dispatcher == nil {
		return outbox.DispatchResult{}, errors.New("outbox dispatcher is not configured")
	}
	lambdaContext, ok := lambdacontext.FromContext(ctx)
	if !ok || strings.TrimSpace(lambdaContext.AwsRequestID) == "" {
		return outbox.DispatchResult{}, errors.New("Lambda request ID is required")
	}
	result, dispatchErr := h.dispatcher.Dispatch(ctx, lambdaContext.AwsRequestID)
	if h.metricWriter == nil || (dispatchErr != nil && result.Claimed == 0) {
		return result, dispatchErr
	}
	now := h.now
	if now == nil {
		now = time.Now
	}
	metricErr := emitOldestPendingAgeMetric(
		h.metricWriter,
		h.environment,
		result.OldestPendingAgeSeconds,
		now().UTC(),
	)
	return result, errors.Join(dispatchErr, metricErr)
}

func emitOldestPendingAgeMetric(
	writer io.Writer,
	environment string,
	ageSeconds int64,
	now time.Time,
) error {
	metric := map[string]any{
		"_aws": map[string]any{
			"Timestamp": now.UnixMilli(),
			"CloudWatchMetrics": []any{
				map[string]any{
					"Namespace":  "Billeif/Outbox",
					"Dimensions": [][]string{{"Environment"}},
					"Metrics": []any{
						map[string]any{
							"Name": "OldestPendingAgeSeconds",
							"Unit": "Seconds",
						},
					},
				},
			},
		},
		"Environment":             strings.TrimSpace(environment),
		"OldestPendingAgeSeconds": ageSeconds,
	}
	if err := json.NewEncoder(writer).Encode(metric); err != nil {
		return fmt.Errorf("emit outbox age metric: %w", err)
	}
	return nil
}

type databasePool interface {
	SetMaxOpenConns(int)
	SetMaxIdleConns(int)
	SetConnMaxIdleTime(time.Duration)
	SetConnMaxLifetime(time.Duration)
}

func configureDatabasePool(pool databasePool) {
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(0)
	pool.SetConnMaxIdleTime(2 * time.Minute)
	pool.SetConnMaxLifetime(10 * time.Minute)
}

func newOutboxHandler(ctx context.Context) (*lambdaHandler, error) {
	cfg, err := config.LoadForProfile(config.ProfileOutbox)
	if err != nil {
		return nil, fmt.Errorf("load outbox configuration: %w", err)
	}
	log := logger.NewWithConfig(logger.Config{
		Environment:        cfg.Environment,
		Level:              cfg.Logging.Level,
		Format:             cfg.Logging.Format,
		SamplingInitial:    cfg.Logging.SamplingInitial,
		SamplingThereafter: cfg.Logging.SamplingThereafter,
		StacktraceLevel:    cfg.Logging.StacktraceLevel,
	}).Named("outbox_lambda")
	logger.SetGlobal(log)

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.AWS.Region))
	if err != nil {
		return nil, &config.ResolutionError{Resource: "AWS SDK configuration"}
	}
	secretsClient := secretsmanager.NewFromConfig(awsCfg)
	var ssmClient config.SSMAPI
	parameterNames := make([]string, 0, 1)
	if parameterName := strings.TrimSpace(cfg.SSM.DatabaseHostParam); parameterName != "" {
		ssmClient = ssm.NewFromConfig(awsCfg)
		parameterNames = append(parameterNames, parameterName)
	}
	resolver, err := config.NewRuntimeResolver(config.RuntimeResolverOptions{
		Clients: config.RuntimeResolvers{
			Secrets: secretsClient,
			SSM:     ssmClient,
		},
		SecretIdentifiers: []string{cfg.Secrets.Database},
		ParameterNames:    parameterNames,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize database resolver: %w", err)
	}
	db, err := app.OpenDatabase(cfg, resolver, log)
	if err != nil {
		return nil, fmt.Errorf("open outbox database: %w", err)
	}
	sqlDatabase, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access outbox database pool: %w", err)
	}
	configureDatabasePool(sqlDatabase)

	repository := postgresrepo.NewOutboxRepository(db)
	publisher := outbox.NewSQSOutboxPublisher(
		cfg.SQS.InvoiceQueue,
		cfg.SQS.EmailDeliveryQueue,
		sqs.NewFromConfig(awsCfg),
	)
	dispatcher, err := outbox.NewDispatcher(repository, publisher, outbox.DispatcherOptions{
		EventTypes: outbox.InvoiceDispatchEventTypes(),
	})
	if err != nil {
		return nil, fmt.Errorf("initialize outbox dispatcher: %w", err)
	}
	return &lambdaHandler{
		dispatcher:   dispatcher,
		metricWriter: os.Stdout,
		environment:  cfg.Environment,
		now:          time.Now,
	}, nil
}

var (
	outboxInitOnce sync.Once
	outboxHandler  *lambdaHandler
	outboxInitErr  error
)

func initializeOutboxRuntime() {
	outboxHandler, outboxInitErr = newOutboxHandler(context.Background())
}

func handleOutboxSchedule(ctx context.Context) (outbox.DispatchResult, error) {
	outboxInitOnce.Do(initializeOutboxRuntime)
	if outboxInitErr != nil {
		return outbox.DispatchResult{}, outboxInitErr
	}
	return outboxHandler.Handle(ctx)
}

func main() {
	lambda.Start(handleOutboxSchedule)
}
