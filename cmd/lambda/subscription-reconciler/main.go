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
	"invoice-backend/pkg/operationsmetrics"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-lambda-go/lambdacontext"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

const (
	subscriptionMaintenanceLimit      = 50
	subscriptionMaintenanceRunTimeout = 50 * time.Second
)

type subscriptionMaintenanceRunner interface {
	RunMaintenance(context.Context, int) (services.SubscriptionMaintenanceResult, error)
}

// reconciliationBacklogCounter reports the aggregate number of operations that
// currently require reconciliation. It is telemetry-only infrastructure and
// carries no tenant, provider, or payload detail.
type reconciliationBacklogCounter interface {
	CountReconciliationBacklog(context.Context) (int64, error)
}

type lambdaHandler struct {
	runner       subscriptionMaintenanceRunner
	backlog      reconciliationBacklogCounter
	limit        int
	runTimeout   time.Duration
	metricWriter io.Writer
	environment  string
	now          func() time.Time
}

func (h lambdaHandler) Handle(ctx context.Context) (services.SubscriptionMaintenanceResult, error) {
	if h.runner == nil {
		return services.SubscriptionMaintenanceResult{}, errors.New("subscription maintenance runner is not configured")
	}
	lambdaContext, ok := lambdacontext.FromContext(ctx)
	if !ok || strings.TrimSpace(lambdaContext.AwsRequestID) == "" {
		return services.SubscriptionMaintenanceResult{}, errors.New("Lambda request ID is required")
	}
	ctx = services.ContextWithActor(ctx, services.ActorContext{RequestID: lambdaContext.AwsRequestID})
	runTimeout := h.runTimeout
	if runTimeout <= 0 {
		runTimeout = subscriptionMaintenanceRunTimeout
	}
	runCtx, cancelRun := context.WithTimeout(ctx, runTimeout)
	defer cancelRun()
	result, runErr := h.runner.RunMaintenance(runCtx, h.limit)
	if h.metricWriter == nil {
		return result, runErr
	}
	now := h.now
	if now == nil {
		now = time.Now
	}
	metricErr := json.NewEncoder(h.metricWriter).Encode(map[string]any{
		"_aws": map[string]any{"Timestamp": now().UTC().UnixMilli(), "CloudWatchMetrics": []any{map[string]any{
			"Namespace": "Billeif/SubscriptionLifecycle", "Dimensions": [][]string{{"Environment"}},
			"Metrics": []any{map[string]any{"Name": "Reconciled", "Unit": "Count"}, map[string]any{"Name": "Suspended", "Unit": "Count"}, map[string]any{"Name": "Failed", "Unit": "Count"}},
		}}},
		"Environment": strings.TrimSpace(h.environment), "Reconciled": result.Reconciled, "Suspended": result.Suspended, "Failed": result.Failed,
	})
	return result, errors.Join(runErr, metricErr, h.emitReconciliationBacklog(now().UTC()))
}

// emitReconciliationBacklog emits the periodic aggregate backlog gauge. A nil
// counter or an empty environment disables emission without failing the run.
func (h lambdaHandler) emitReconciliationBacklog(now time.Time) error {
	if h.backlog == nil || operationsmetrics.NewRuntimeEmitter(h.environment) == nil {
		return nil
	}
	count, err := h.backlog.CountReconciliationBacklog(context.Background())
	if err != nil {
		return fmt.Errorf("count reconciliation backlog: %w", err)
	}
	if count < 0 {
		return nil
	}
	metric := map[string]any{
		"_aws": map[string]any{
			"Timestamp": now.UnixMilli(),
			"CloudWatchMetrics": []any{map[string]any{
				"Namespace": operationsmetrics.Namespace, "Dimensions": [][]string{{"Environment", "Category"}},
				"Metrics": []any{map[string]any{"Name": "ReconciliationBacklog", "Unit": "Count"}},
			}},
		},
		"Environment":           strings.TrimSpace(h.environment),
		"Category":              "reconciliation",
		"ReconciliationBacklog": count,
	}
	return json.NewEncoder(h.metricWriter).Encode(metric)
}

func newSubscriptionMaintenanceHandler(ctx context.Context) (*lambdaHandler, error) {
	cfg, err := config.LoadForProfile(config.ProfileSubscriptionReconciler)
	if err != nil {
		return nil, fmt.Errorf("load subscription reconciliation configuration: %w", err)
	}
	log := logger.NewWithConfig(logger.Config{Environment: cfg.Environment, Level: cfg.Logging.Level, Format: cfg.Logging.Format}).Named("subscription_reconciler_lambda")
	logger.SetGlobal(log)
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.AWS.Region))
	if err != nil {
		return nil, &config.ResolutionError{Resource: "AWS SDK configuration"}
	}
	var ssmClient config.SSMAPI
	parameterNames := []string{}
	if name := strings.TrimSpace(cfg.SSM.DatabaseHostParam); name != "" {
		ssmClient = ssm.NewFromConfig(awsCfg)
		parameterNames = append(parameterNames, name)
	}
	resolver, err := config.NewRuntimeResolver(config.RuntimeResolverOptions{
		Clients:           config.RuntimeResolvers{Secrets: secretsmanager.NewFromConfig(awsCfg), SSM: ssmClient},
		SecretIdentifiers: []string{cfg.Secrets.Database, cfg.Secrets.Razorpay}, ParameterNames: parameterNames,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize subscription resolver: %w", err)
	}
	db, err := app.OpenDatabase(cfg, resolver, log)
	if err != nil {
		return nil, fmt.Errorf("open subscription database: %w", err)
	}
	provider := services.NewRazorpayPaymentService(cfg, db, log, resolver)
	runner := services.NewSubscriptionLifecycleService(postgresrepo.NewSubscriptionLifecycleRepository(db), provider, services.SubscriptionLifecycleConfig{Resolve: provider.SubscriptionProviderSettings}, log)
	return &lambdaHandler{
		runner: runner, backlog: postgresrepo.NewOperationRepository(db), limit: subscriptionMaintenanceLimit,
		runTimeout: subscriptionMaintenanceRunTimeout, metricWriter: os.Stdout, environment: cfg.Environment, now: time.Now,
	}, nil
}

var initOnce sync.Once
var initialized *lambdaHandler
var initErr error

func handleSubscriptionMaintenance(ctx context.Context) (services.SubscriptionMaintenanceResult, error) {
	initOnce.Do(func() { initialized, initErr = newSubscriptionMaintenanceHandler(context.Background()) })
	if initErr != nil {
		return services.SubscriptionMaintenanceResult{}, initErr
	}
	return initialized.Handle(ctx)
}

func main() { lambda.Start(handleSubscriptionMaintenance) }
