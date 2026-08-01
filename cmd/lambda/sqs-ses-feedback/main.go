package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"invoice-backend/internal/app"
	"invoice-backend/internal/config"
	postgresrepo "invoice-backend/internal/repositories/postgres"
	"invoice-backend/internal/sesfeedback"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

const feedbackMessageTimeout = 25 * time.Second

type feedbackProcessor interface {
	Process(context.Context, string) error
}

type feedbackHandler struct {
	processor feedbackProcessor
}

func (h feedbackHandler) Handle(
	ctx context.Context,
	event events.SQSEvent,
) (events.SQSEventResponse, error) {
	if h.processor == nil {
		return events.SQSEventResponse{}, errors.New("SES feedback processor is not configured")
	}
	for _, record := range event.Records {
		if strings.TrimSpace(record.MessageId) == "" {
			return events.SQSEventResponse{}, errors.New("SQS message ID is required")
		}
	}

	response := events.SQSEventResponse{
		BatchItemFailures: make([]events.SQSBatchItemFailure, 0),
	}
	for _, record := range event.Records {
		messageContext, cancel := boundedFeedbackContext(ctx)
		err := h.processor.Process(messageContext, record.Body)
		cancel()
		if err != nil {
			response.BatchItemFailures = append(
				response.BatchItemFailures,
				events.SQSBatchItemFailure{ItemIdentifier: record.MessageId},
			)
		}
	}
	return response, nil
}

func boundedFeedbackContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := feedbackMessageTimeout
	if deadline, ok := parent.Deadline(); ok {
		remaining := time.Until(deadline) - 2*time.Second
		if remaining < timeout {
			timeout = remaining
		}
	}
	if timeout <= 0 {
		timeout = time.Millisecond
	}
	return context.WithTimeout(parent, timeout)
}

func newFeedbackHandler(ctx context.Context) (*feedbackHandler, error) {
	cfg, err := config.LoadForProfile(config.ProfileSESFeedback)
	if err != nil {
		return nil, fmt.Errorf("load SES feedback configuration: %w", err)
	}
	log := logger.NewWithConfig(logger.Config{
		Environment:        cfg.Environment,
		Level:              cfg.Logging.Level,
		Format:             cfg.Logging.Format,
		SamplingInitial:    cfg.Logging.SamplingInitial,
		SamplingThereafter: cfg.Logging.SamplingThereafter,
		StacktraceLevel:    cfg.Logging.StacktraceLevel,
	}).Named("ses_feedback_lambda")
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
		Clients:           config.RuntimeResolvers{Secrets: secretsClient, SSM: ssmClient},
		SecretIdentifiers: []string{cfg.Secrets.Database},
		ParameterNames:    parameterNames,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize SES feedback database resolver: %w", err)
	}
	db, err := app.OpenDatabase(cfg, resolver, log)
	if err != nil {
		return nil, fmt.Errorf("open SES feedback database: %w", err)
	}
	sqlDatabase, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access SES feedback database pool: %w", err)
	}
	sqlDatabase.SetMaxOpenConns(1)
	sqlDatabase.SetMaxIdleConns(0)
	sqlDatabase.SetConnMaxIdleTime(2 * time.Minute)
	sqlDatabase.SetConnMaxLifetime(10 * time.Minute)

	processor, err := sesfeedback.NewProcessor(sesfeedback.ProcessorOptions{
		Repository: postgresrepo.NewSESFeedbackRepository(db),
		Validation: sesfeedback.ValidationOptions{
			SendingAccountID: cfg.SES.SendingAccountID,
			ConfigurationSet: cfg.SES.ConfigurationSet,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize SES feedback processor: %w", err)
	}
	return &feedbackHandler{processor: processor}, nil
}

var (
	feedbackInitOnce sync.Once
	feedbackRuntime  *feedbackHandler
	feedbackInitErr  error
)

func initializeFeedbackRuntime() {
	feedbackRuntime, feedbackInitErr = newFeedbackHandler(context.Background())
}

func handleFeedback(
	ctx context.Context,
	event events.SQSEvent,
) (events.SQSEventResponse, error) {
	feedbackInitOnce.Do(initializeFeedbackRuntime)
	if feedbackInitErr != nil {
		return events.SQSEventResponse{}, feedbackInitErr
	}
	return feedbackRuntime.Handle(ctx, event)
}

func main() {
	lambda.Start(handleFeedback)
}
