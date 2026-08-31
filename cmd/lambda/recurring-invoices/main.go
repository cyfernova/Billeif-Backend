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
	postgresrepo "invoice-backend/internal/repositories/postgres"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-lambda-go/lambdacontext"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

const recurringInvoiceDispatchLimit = 50

type recurringInvoiceRunner interface {
	DispatchDueInvoiceSubscriptions(context.Context, int) (services.InvoiceSubscriptionDispatchResult, error)
}

type lambdaHandler struct {
	runner       recurringInvoiceRunner
	limit        int
	metricWriter io.Writer
	environment  string
	now          func() time.Time
}

func (h lambdaHandler) Handle(ctx context.Context) (services.InvoiceSubscriptionDispatchResult, error) {
	if h.runner == nil {
		return services.InvoiceSubscriptionDispatchResult{}, errors.New("recurring invoice runner is not configured")
	}
	lambdaContext, ok := lambdacontext.FromContext(ctx)
	if !ok || strings.TrimSpace(lambdaContext.AwsRequestID) == "" {
		return services.InvoiceSubscriptionDispatchResult{}, errors.New("Lambda request ID is required")
	}
	ctx = services.ContextWithActor(ctx, services.ActorContext{RequestID: lambdaContext.AwsRequestID})
	result, dispatchErr := h.runner.DispatchDueInvoiceSubscriptions(ctx, h.limit)
	if h.metricWriter == nil || (dispatchErr != nil && result.Due == 0) {
		return result, dispatchErr
	}
	now := h.now
	if now == nil {
		now = time.Now
	}
	metricErr := emitRecurringInvoiceMetrics(h.metricWriter, h.environment, result, now().UTC())
	return result, errors.Join(dispatchErr, metricErr)
}

func emitRecurringInvoiceMetrics(
	writer io.Writer,
	environment string,
	result services.InvoiceSubscriptionDispatchResult,
	now time.Time,
) error {
	metric := map[string]any{
		"_aws": map[string]any{
			"Timestamp": now.UnixMilli(),
			"CloudWatchMetrics": []any{
				map[string]any{
					"Namespace":  "Billeif/RecurringInvoices",
					"Dimensions": [][]string{{"Environment"}},
					"Metrics": []any{
						map[string]any{"Name": "Due", "Unit": "Count"},
						map[string]any{"Name": "Completed", "Unit": "Count"},
						map[string]any{"Name": "Failed", "Unit": "Count"},
					},
				},
			},
		},
		"Environment": strings.TrimSpace(environment),
		"Due":         result.Due,
		"Completed":   result.Completed,
		"Failed":      result.Failed,
	}
	if err := json.NewEncoder(writer).Encode(metric); err != nil {
		return fmt.Errorf("emit recurring invoice metrics: %w", err)
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

func newRecurringInvoiceHandler(ctx context.Context) (*lambdaHandler, error) {
	cfg, err := config.LoadForProfile(config.ProfileRecurringInvoices)
	if err != nil {
		return nil, fmt.Errorf("load recurring invoice configuration: %w", err)
	}
	log := logger.NewWithConfig(logger.Config{
		Environment:        cfg.Environment,
		Level:              cfg.Logging.Level,
		Format:             cfg.Logging.Format,
		SamplingInitial:    cfg.Logging.SamplingInitial,
		SamplingThereafter: cfg.Logging.SamplingThereafter,
		StacktraceLevel:    cfg.Logging.StacktraceLevel,
	}).Named("recurring_invoice_lambda")
	logger.SetGlobal(log)

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.AWS.Region))
	if err != nil {
		return nil, &config.ResolutionError{Resource: "AWS SDK configuration"}
	}
	var ssmClient config.SSMAPI
	parameterNames := make([]string, 0, 1)
	if parameterName := strings.TrimSpace(cfg.SSM.DatabaseHostParam); parameterName != "" {
		ssmClient = ssm.NewFromConfig(awsCfg)
		parameterNames = append(parameterNames, parameterName)
	}
	resolver, err := config.NewRuntimeResolver(config.RuntimeResolverOptions{
		Clients: config.RuntimeResolvers{
			Secrets: secretsmanager.NewFromConfig(awsCfg),
			SSM:     ssmClient,
		},
		SecretIdentifiers: []string{cfg.Secrets.Database},
		ParameterNames:    parameterNames,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize recurring invoice database resolver: %w", err)
	}
	db, err := app.OpenDatabase(cfg, resolver, log)
	if err != nil {
		return nil, fmt.Errorf("open recurring invoice database: %w", err)
	}
	sqlDatabase, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access recurring invoice database pool: %w", err)
	}
	configureDatabasePool(sqlDatabase)

	businessRepository := postgresrepo.NewBusinessRepository(db)
	customerRepository := postgresrepo.NewCustomerRepository(db)
	invoiceRepository := postgresrepo.NewInvoiceRepository(db)
	invoiceService := services.NewInvoiceService(
		db,
		cfg,
		invoiceRepository,
		businessRepository,
		nil,
		customerRepository,
		nil,
		nil,
		nil,
		nil,
		log,
	)
	runner := services.NewBillingOpsService(
		cfg,
		db,
		customerRepository,
		nil,
		nil,
		invoiceService,
		nil,
		nil,
		nil,
		log,
	)
	return &lambdaHandler{
		runner: runner, limit: recurringInvoiceDispatchLimit,
		metricWriter: os.Stdout, environment: cfg.Environment, now: time.Now,
	}, nil
}

var (
	recurringInvoiceInitOnce sync.Once
	recurringInvoiceHandler  *lambdaHandler
	recurringInvoiceInitErr  error
)

func initializeRecurringInvoiceRuntime() {
	recurringInvoiceHandler, recurringInvoiceInitErr = newRecurringInvoiceHandler(context.Background())
}

func handleRecurringInvoiceSchedule(ctx context.Context) (services.InvoiceSubscriptionDispatchResult, error) {
	recurringInvoiceInitOnce.Do(initializeRecurringInvoiceRuntime)
	if recurringInvoiceInitErr != nil {
		return services.InvoiceSubscriptionDispatchResult{}, recurringInvoiceInitErr
	}
	return recurringInvoiceHandler.Handle(ctx)
}

func main() {
	lambda.Start(handleRecurringInvoiceSchedule)
}
