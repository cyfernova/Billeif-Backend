package main

import (
	"context"
	"errors"
	"log"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"invoice-backend/internal/voice/reconciler"
	"invoice-backend/internal/voice/session"
	voicetelemetry "invoice-backend/internal/voice/telemetry"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentcore"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

var (
	errInvalidRuntimeSettings = errors.New("invalid voice reconciler runtime settings")
	dynamoNamePattern         = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,255}$`)
	qualifierPattern          = regexp.MustCompile(`^[A-Za-z0-9_-]{1,48}$`)
	environmentPattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
)

type runtimeSettings struct {
	environment           string
	tableName             string
	leaseIndexName        string
	agentRuntimeARN       string
	agentRuntimeQualifier string
	batchSize             int32
}

func loadRuntimeSettings(getenv func(string) string) (runtimeSettings, error) {
	if getenv == nil {
		return runtimeSettings{}, errInvalidRuntimeSettings
	}
	settings := runtimeSettings{
		environment:           strings.TrimSpace(getenv("ENVIRONMENT")),
		tableName:             strings.TrimSpace(getenv("VOICE_SESSIONS_TABLE_NAME")),
		leaseIndexName:        strings.TrimSpace(getenv("VOICE_SESSION_LEASE_INDEX_NAME")),
		agentRuntimeARN:       strings.TrimSpace(getenv("AGENTCORE_RUNTIME_ARN")),
		agentRuntimeQualifier: strings.TrimSpace(getenv("AGENTCORE_RUNTIME_QUALIFIER")),
		batchSize:             10,
	}
	if settings.leaseIndexName == "" {
		settings.leaseIndexName = "gsi2"
	}
	if rawBatch := strings.TrimSpace(getenv("VOICE_RECONCILER_BATCH_SIZE")); rawBatch != "" {
		parsed, err := strconv.ParseInt(rawBatch, 10, 32)
		if err != nil {
			return runtimeSettings{}, errInvalidRuntimeSettings
		}
		settings.batchSize = int32(parsed)
	}
	runtimeARN, runtimeErr := arn.Parse(settings.agentRuntimeARN)
	if !environmentPattern.MatchString(settings.environment) || !dynamoNamePattern.MatchString(settings.tableName) || !dynamoNamePattern.MatchString(settings.leaseIndexName) ||
		runtimeErr != nil || runtimeARN.Service != "bedrock-agentcore" || !strings.HasPrefix(runtimeARN.Resource, "runtime/") ||
		!qualifierPattern.MatchString(settings.agentRuntimeQualifier) || settings.batchSize < 1 || settings.batchSize > 100 {
		return runtimeSettings{}, errInvalidRuntimeSettings
	}
	return settings, nil
}

func newVoiceReconcilerHandler(ctx context.Context) (*reconciler.Handler, error) {
	settings, err := loadRuntimeSettings(os.Getenv)
	if err != nil {
		return nil, err
	}
	awsConfig, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, errInvalidRuntimeSettings
	}
	store := session.NewDynamoDBStore(dynamodb.NewFromConfig(awsConfig), session.DynamoDBStoreConfig{
		TableName: settings.tableName, LeaseIndexName: settings.leaseIndexName,
	})
	emitter, err := voicetelemetry.NewServiceEmitter(os.Stdout, settings.environment, voicetelemetry.ReconcilerService)
	if err != nil {
		return nil, errInvalidRuntimeSettings
	}
	metrics, err := newReconcilerMetrics(emitter)
	if err != nil {
		return nil, errInvalidRuntimeSettings
	}
	return reconciler.New(reconciler.Options{
		Store: store, Stopper: session.NewAgentCoreRuntimeStopper(bedrockagentcore.NewFromConfig(awsConfig)),
		Metrics:         metrics,
		AgentRuntimeARN: settings.agentRuntimeARN, AgentRuntimeQualifier: settings.agentRuntimeQualifier,
		BatchSize: settings.batchSize,
	})
}

type reconcilerMetrics struct {
	recorder voicetelemetry.SignalRecorder
}

func newReconcilerMetrics(recorder voicetelemetry.SignalRecorder) (*reconcilerMetrics, error) {
	if nilSignalRecorder(recorder) {
		return nil, errInvalidRuntimeSettings
	}
	return &reconcilerMetrics{recorder: recorder}, nil
}

func (metrics *reconcilerMetrics) SessionLeaksRecovered(count int) {
	if metrics == nil || count <= 0 || nilSignalRecorder(metrics.recorder) {
		return
	}
	defer func() { _ = recover() }()
	_ = metrics.recorder.RecordSignal(voicetelemetry.SignalSessionLeaks, int64(count))
}

func nilSignalRecorder(recorder voicetelemetry.SignalRecorder) bool {
	if recorder == nil {
		return true
	}
	value := reflect.ValueOf(recorder)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func main() {
	handler, err := newVoiceReconcilerHandler(context.Background())
	if err != nil {
		log.Fatal("voice reconciler runtime initialization failed")
	}
	lambda.Start(handler.Handle)
}
