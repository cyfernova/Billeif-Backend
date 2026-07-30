package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"invoice-backend/internal/app"
	"invoice-backend/internal/config"
	"invoice-backend/internal/emaildelivery"
	postgresrepo "invoice-backend/internal/repositories/postgres"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-lambda-go/lambdacontext"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

const perMessageTimeout = 50 * time.Second

type deliveryProcessor interface {
	Process(ctx context.Context, message emaildelivery.DeliveryMessage, owner string) error
}

type emailDeliveryHandler struct {
	worker deliveryProcessor
}

func (h emailDeliveryHandler) Handle(
	ctx context.Context,
	event events.SQSEvent,
) (events.SQSEventResponse, error) {
	if h.worker == nil {
		return events.SQSEventResponse{}, errors.New("email delivery worker is not configured")
	}
	lambdaContext, ok := lambdacontext.FromContext(ctx)
	if !ok || strings.TrimSpace(lambdaContext.AwsRequestID) == "" {
		return events.SQSEventResponse{}, errors.New("Lambda request ID is required")
	}
	response := events.SQSEventResponse{
		BatchItemFailures: make([]events.SQSBatchItemFailure, 0),
	}
	for _, record := range event.Records {
		if strings.TrimSpace(record.MessageId) == "" {
			response.BatchItemFailures = append(response.BatchItemFailures, events.SQSBatchItemFailure{})
			continue
		}
		message, err := decodeDeliveryMessage(record.Body)
		if err == nil {
			messageContext, cancel := boundedMessageContext(ctx)
			err = h.worker.Process(
				messageContext,
				message,
				lambdaContext.AwsRequestID+":"+record.MessageId,
			)
			cancel()
		}
		if err != nil {
			response.BatchItemFailures = append(
				response.BatchItemFailures,
				events.SQSBatchItemFailure{ItemIdentifier: record.MessageId},
			)
		}
	}
	return response, nil
}

func boundedMessageContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := perMessageTimeout
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

func decodeDeliveryMessage(body string) (emaildelivery.DeliveryMessage, error) {
	var message emaildelivery.DeliveryMessage
	decoder := json.NewDecoder(bytes.NewBufferString(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&message); err != nil {
		return emaildelivery.DeliveryMessage{}, emaildelivery.ErrDeliveryMalformed
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return emaildelivery.DeliveryMessage{}, emaildelivery.ErrDeliveryMalformed
	}
	return message, nil
}

type databasePool interface {
	SetMaxOpenConns(int)
	SetMaxIdleConns(int)
	SetConnMaxIdleTime(time.Duration)
	SetConnMaxLifetime(time.Duration)
}

func configureEmailDeliveryDatabasePool(pool databasePool) {
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(0)
	pool.SetConnMaxIdleTime(2 * time.Minute)
	pool.SetConnMaxLifetime(10 * time.Minute)
}

func newEmailDeliveryHandler(ctx context.Context) (*emailDeliveryHandler, error) {
	cfg, err := config.LoadForProfile(config.ProfileEmailDelivery)
	if err != nil {
		return nil, fmt.Errorf("load email delivery configuration: %w", err)
	}
	log := logger.NewWithConfig(logger.Config{
		Environment:        cfg.Environment,
		Level:              cfg.Logging.Level,
		Format:             cfg.Logging.Format,
		SamplingInitial:    cfg.Logging.SamplingInitial,
		SamplingThereafter: cfg.Logging.SamplingThereafter,
		StacktraceLevel:    cfg.Logging.StacktraceLevel,
	}).Named("email_delivery_lambda")
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
		return nil, fmt.Errorf("initialize email delivery database resolver: %w", err)
	}
	db, err := app.OpenDatabase(cfg, resolver, log)
	if err != nil {
		return nil, fmt.Errorf("open email delivery database: %w", err)
	}
	sqlDatabase, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access email delivery database pool: %w", err)
	}
	configureEmailDeliveryDatabasePool(sqlDatabase)

	repository := postgresrepo.NewEmailDeliveryWorkerRepository(db)
	worker, err := emaildelivery.NewWorker(emaildelivery.WorkerOptions{
		Repository:       repository,
		Objects:          s3.NewFromConfig(awsCfg),
		Email:            ses.NewFromConfig(awsCfg),
		Bucket:           cfg.S3.BucketInvoices,
		SenderEmail:      cfg.SES.SenderEmail,
		ConfigurationSet: cfg.SES.ConfigurationSet,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize email delivery worker: %w", err)
	}
	return &emailDeliveryHandler{worker: worker}, nil
}

var (
	emailDeliveryInitOnce sync.Once
	emailDeliveryRuntime  *emailDeliveryHandler
	emailDeliveryInitErr  error
)

func initializeEmailDeliveryRuntime() {
	emailDeliveryRuntime, emailDeliveryInitErr = newEmailDeliveryHandler(context.Background())
}

func handleEmailDelivery(
	ctx context.Context,
	event events.SQSEvent,
) (events.SQSEventResponse, error) {
	emailDeliveryInitOnce.Do(initializeEmailDeliveryRuntime)
	if emailDeliveryInitErr != nil {
		return events.SQSEventResponse{}, emailDeliveryInitErr
	}
	return emailDeliveryRuntime.Handle(ctx, event)
}

func main() {
	lambda.Start(handleEmailDelivery)
}
