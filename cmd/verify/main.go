package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"invoice-backend/internal/verify"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentcorecontrol"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/scheduler"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/redis/go-redis/v9"
)

type cliOptions struct {
	environment     string
	mode            string
	allowProduction bool
	allowWrites     bool
	profile         string
	only            string
	exclude         string
	concurrency     int
	checkTimeout    time.Duration
	cleanupTimeout  time.Duration
	maxAttempts     int
	pretty          bool
}

func main() {
	code := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	options, err := parseCLI(args)
	if err != nil {
		writeCLIError(stderr, "invalid_arguments", err)
		return 2
	}
	selectedEnvironment, err := verify.SelectEnvironment(options.environment, options.allowProduction)
	if err != nil {
		writeCLIError(stderr, "environment_refused", err)
		return 2
	}
	if options.profile != "default" {
		writeCLIError(stderr, "aws_profile_refused", errors.New("only the default AWS profile is permitted"))
		return 2
	}
	for _, name := range []string{"AWS_PROFILE", "AWS_DEFAULT_PROFILE"} {
		if value := env(name); value != "" && value != "default" {
			writeCLIError(stderr, "aws_profile_refused", fmt.Errorf("%s must be default", name))
			return 2
		}
	}
	if options.mode != verify.ModeRead && options.mode != verify.ModeWrite {
		writeCLIError(stderr, "invalid_mode", fmt.Errorf("mode must be %q or %q", verify.ModeRead, verify.ModeWrite))
		return 2
	}
	if options.mode == verify.ModeWrite && !options.allowWrites {
		writeCLIError(stderr, verify.ReasonWriteRequiresExplicitAllow, errors.New("write mode also requires --allow-writes"))
		return 2
	}
	targetEnvironment, err := verify.SelectEnvironment(env("VERIFY_TARGET_ENVIRONMENT"), options.allowProduction)
	if err != nil || targetEnvironment != selectedEnvironment {
		if err == nil {
			err = fmt.Errorf("VERIFY_TARGET_ENVIRONMENT %q does not match --environment %q", targetEnvironment, selectedEnvironment)
		}
		writeCLIError(stderr, "environment_binding_refused", err)
		return 2
	}
	if err := validateEnvironmentTargetBindings(); err != nil {
		writeCLIError(stderr, "target_binding_refused", err)
		return 2
	}
	if name := firstConfiguredEnv(
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
		"AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_ROLE_ARN",
		"AWS_CONTAINER_CREDENTIALS_FULL_URI", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
	); name != "" {
		writeCLIError(stderr, "ambient_aws_credentials_refused", fmt.Errorf("%s overrides the required default shared profile", name))
		return 2
	}

	runID := newRunID()
	checks := buildChecks(ctx, runID)
	report, err := verify.RunHarness(ctx, verify.RunOptions{
		RunID:                   runID,
		Environment:             options.environment,
		AllowProductionOverride: options.allowProduction,
		Mode:                    options.mode,
		AllowWrites:             options.allowWrites,
		Only:                    csvList(options.only),
		Exclude:                 csvList(options.exclude),
		Concurrency:             options.concurrency,
		CheckTimeout:            options.checkTimeout,
		CleanupTimout:           options.cleanupTimeout,
		MaxAttempts:             options.maxAttempts,
	}, checks)
	if err != nil {
		writeCLIError(stderr, "verification_refused", err)
		return 2
	}
	encoder := json.NewEncoder(stdout)
	if options.pretty {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(report); err != nil {
		writeCLIError(stderr, "output_failed", err)
		return 2
	}
	if report.Aggregate.Status != string(verify.StatusPassed) {
		return 1
	}
	return 0
}

func parseCLI(args []string) (cliOptions, error) {
	var options cliOptions
	flags := flag.NewFlagSet("billeif-verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.environment, "environment", "", "explicit target: dev|local|staging|qa|preview")
	flags.StringVar(&options.mode, "mode", verify.ModeRead, "read or write")
	flags.BoolVar(&options.allowProduction, "allow-production", false, "deliberate production override")
	flags.BoolVar(&options.allowWrites, "allow-writes", false, "allow externally visible sandbox checks")
	flags.StringVar(&options.profile, "aws-profile", "default", "AWS shared profile; only default is allowed")
	flags.StringVar(&options.only, "only", "", "comma-separated check IDs")
	flags.StringVar(&options.exclude, "exclude", "", "comma-separated check IDs")
	flags.IntVar(&options.concurrency, "concurrency", verify.DefaultConcurrency, "maximum concurrent checks")
	flags.DurationVar(&options.checkTimeout, "check-timeout", verify.DefaultCheckTimeout, "per-attempt timeout")
	flags.DurationVar(&options.cleanupTimeout, "cleanup-timeout", verify.DefaultCleanupTime, "cleanup timeout")
	flags.IntVar(&options.maxAttempts, "max-attempts", verify.DefaultMaxAttempts, "bounded attempts per retryable check")
	flags.BoolVar(&options.pretty, "pretty", false, "pretty-print JSON")
	if err := flags.Parse(args); err != nil {
		return cliOptions{}, err
	}
	if flags.NArg() != 0 {
		return cliOptions{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	return options, nil
}

func buildChecks(ctx context.Context, runID string) []verify.Check {
	region := env("AWS_REGION")
	var (
		stsClient       verify.STSIdentityClient
		cognitoClient   verify.CognitoGoogleAuthClient
		s3Client        verify.S3HeadBucketer
		sqsClient       verify.SQSAttributesClient
		schedulerClient verify.SchedulerListClient
		sesClient       verify.SESIdentityClient
		agentCoreClient verify.AgentCoreRuntimeClient
	)
	if region != "" {
		if cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region), awsconfig.WithSharedConfigProfile("default")); err == nil {
			stsClient = sts.NewFromConfig(cfg)
			cognitoClient = cognitoidentityprovider.NewFromConfig(cfg)
			s3Client = s3.NewFromConfig(cfg)
			sqsClient = sqs.NewFromConfig(cfg)
			schedulerClient = scheduler.NewFromConfig(cfg)
			sesClient = ses.NewFromConfig(cfg)
			agentCoreClient = bedrockagentcorecontrol.NewFromConfig(cfg)
		}
	}

	httpClient := &http.Client{Timeout: 20 * time.Second}
	redisHost := env("REDIS_HOST")
	redisPort := envInt("REDIS_PORT", 6379)
	redisTLS := envBool("REDIS_TLS_ENABLED")
	checks := []verify.Check{
		&verify.AWSIdentityProbe{Region: region, ExpectedAccountID: env("VERIFY_AWS_ACCOUNT_ID"), Client: stsClient},
		&verify.CognitoUserPoolProbe{PoolID: env("COGNITO_USER_POOL_ID"), Region: region, Client: cognitoClient},
		&verify.CognitoPhonePoolProbe{PoolID: env("COGNITO_PHONE_USER_POOL_ID"), Region: region, Client: cognitoClient},
		&verify.GoogleAuthProbe{HostedUIDomain: env("COGNITO_DOMAIN"), PoolID: env("COGNITO_USER_POOL_ID"), Region: region, Client: cognitoClient, HTTPClient: httpClient},
		&verify.PostgresProbe{
			Host: env("DATABASE_HOST"), Port: envInt("DATABASE_PORT", 5432), User: env("DATABASE_USER"),
			Password: env("DATABASE_PASSWORD"), Name: env("DATABASE_NAME"), SSLMode: env("DATABASE_SSL_MODE"),
			Dial: verify.DialPostgres(env("DATABASE_HOST"), envInt("DATABASE_PORT", 5432), env("DATABASE_USER"), env("DATABASE_PASSWORD"), env("DATABASE_NAME"), env("DATABASE_SSL_MODE")),
		},
		&verify.RedisProbe{
			Host: redisHost, Port: redisPort, Password: env("REDIS_PASSWORD"), DB: envInt("REDIS_DB", 0),
			TLSEnabled: redisTLS, ClusterMode: envBool("REDIS_CLUSTER_MODE"), IAMAuthEnabled: envBool("REDIS_IAM_AUTH_ENABLED"),
			Dial: func() (verify.RedisPinger, error) {
				options := &redis.Options{Addr: net.JoinHostPort(redisHost, strconv.Itoa(redisPort)), Password: env("REDIS_PASSWORD"), DB: envInt("REDIS_DB", 0)}
				if redisTLS {
					options.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: redisHost}
				}
				return redis.NewClient(options), nil
			},
		},
		&verify.S3BucketsProbe{Region: region, Buckets: envList("S3_BUCKET_LOGOS", "S3_BUCKET_INVOICES", "S3_BUCKET_PRODUCTS", "S3_BUCKET_EMAIL_SINK", "S3_BUCKET_DRIVE"), Client: s3Client},
		&verify.SQSQueuesProbe{Region: region, QueueURLs: envList("SQS_INVOICE_QUEUE", "SQS_EMAIL_DELIVERY_QUEUE", "SQS_GST_QUEUE", "SQS_BARGAINING_QUEUE"), Client: sqsClient},
		&verify.EventBridgeSchedulerProbe{Region: region, Client: schedulerClient},
		&verify.WebSocketEndpointProbe{Endpoint: env("WEBSOCKET_API_ENDPOINT"), HTTPClient: httpClient},
		&verify.SESIdentityProbe{SenderEmail: env("SES_SENDER_EMAIL"), Region: region, Client: sesClient},
		&verify.RazorpayTestModeProbe{KeyID: env("RAZORPAY_KEY_ID"), KeySecret: env("RAZORPAY_KEY_SECRET"), Mode: env("RAZORPAY_MODE"), BaseURL: razorpayAPIBase(env("RAZORPAY_BASE_URL")), HTTPClient: httpClient},
		&verify.WhatsAppTestEnvProbe{BaseURL: env("WHATSAPP_BASE_URL"), PhoneNumberID: env("VERIFY_WHATSAPP_PHONE_NUMBER_ID"), AccessTokenEnv: env("VERIFY_WHATSAPP_ACCESS_TOKEN_ENV"), PhoneNumberEnv: env("VERIFY_WHATSAPP_PHONE_NUMBER_ENV"), HTTPClient: httpClient},
		&verify.GSTSandboxProbe{BaseURL: env("GST_BASE_URL"), ValidatePath: env("GST_VALIDATE_PATH"), Sandbox: envBool("GST_SANDBOX"), APIToken: env("GST_API_TOKEN"), ClientID: env("GST_CLIENT_ID"), ClientSecret: env("GST_CLIENT_SECRET"), Username: env("GST_USERNAME"), Password: env("GST_PASSWORD"), GSPName: env("GST_GSP_NAME"), HTTPClient: httpClient},
		&verify.LLMGatewayProbe{Family: "claude", Model: env("LLM_MODEL"), APIURL: env("LLM_API_URL"), APIKey: env("LLM_API_KEY"), HTTPClient: httpClient},
		&verify.LLMGatewayProbe{Family: "gemini", Model: env("LLM_MODEL"), APIURL: env("LLM_API_URL"), APIKey: env("LLM_API_KEY"), HTTPClient: httpClient},
		&verify.SarvamAPIProbe{APIKey: env("SARVAM_API_KEY"), BaseURL: env("VERIFY_SARVAM_PROBE_URL"), HTTPClient: httpClient},
		&verify.AgentCoreRuntimeProbe{RuntimeARN: env("AGENTCORE_RUNTIME_ARN"), Qualifier: env("AGENTCORE_RUNTIME_QUALIFIER"), Region: region, Client: agentCoreClient},
	}
	for _, journey := range []struct {
		id, description, prefix, kind string
	}{
		{"journey.invoice", "Controlled invoice create and cleanup", "INVOICE", "synthetic-invoice"},
		{"journey.report", "Controlled report export and cleanup", "REPORT", "synthetic-report"},
		{"journey.upload", "Controlled upload completion and cleanup", "UPLOAD", "synthetic-upload"},
		{"journey.recurring", "Controlled recurring run and cleanup", "RECURRING", "synthetic-recurring-run"},
		{"journey.websocket_ticket", "Controlled WebSocket ticket issue and consume", "WEBSOCKET_TICKET", "synthetic-websocket-ticket"},
		{"journey.checkout", "Controlled Razorpay test checkout", "CHECKOUT", "provider-test-order"},
	} {
		prefix := "VERIFY_JOURNEY_" + journey.prefix + "_"
		checks = append(checks, &verify.HTTPJourneyProbe{
			ID: journey.id, Description: journey.description, Endpoint: env(prefix + "URL"), Method: env(prefix + "METHOD"),
			BearerToken: env("VERIFY_JOURNEY_BEARER_TOKEN"), Body: env(prefix + "BODY"), IdempotencyKey: runID + ":" + journey.id,
			ResourceKind: journey.kind, ReferenceField: env(prefix + "REFERENCE_FIELD"), CleanupURL: env(prefix + "CLEANUP_URL"),
			CleanupMethod: env(prefix + "CLEANUP_METHOD"), HTTPClient: httpClient,
		})
	}
	return checks
}

func writeCLIError(output io.Writer, code string, err error) {
	_ = err
	_ = json.NewEncoder(output).Encode(map[string]any{
		"schema_version": verify.SchemaVersion,
		"status":         "refused",
		"error": map[string]string{
			"code":    code,
			"message": code,
		},
	})
}

func newRunID() string {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "verify-unspecified"
	}
	return "verify-" + hex.EncodeToString(raw)
}

func csvList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, value := range parts {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func env(name string) string { return strings.TrimSpace(os.Getenv(name)) }

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(env(name))
	if err != nil {
		return fallback
	}
	return value
}

func envBool(name string) bool {
	value, _ := strconv.ParseBool(env(name))
	return value
}

func envList(names ...string) []string {
	values := make([]string, 0, len(names))
	for _, name := range names {
		if value := env(name); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func firstConfiguredEnv(names ...string) string {
	for _, name := range names {
		if env(name) != "" {
			return name
		}
	}
	return ""
}

func razorpayAPIBase(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return ""
	}
	if strings.HasSuffix(base, "/v1") {
		return base
	}
	return base + "/v1"
}

func validateEnvironmentTargetBindings() error {
	allowed := make(map[string]struct{})
	for _, raw := range csvList(env("VERIFY_ALLOWED_HOSTS")) {
		host := normalizeHost(raw)
		if host == "" {
			return errors.New("VERIFY_ALLOWED_HOSTS contains an invalid host")
		}
		allowed[host] = struct{}{}
	}
	for _, name := range []string{"DATABASE_HOST", "REDIS_HOST"} {
		if host := normalizeHost(env(name)); host != "" && !isLoopbackHost(host) {
			if _, ok := allowed[host]; !ok {
				return fmt.Errorf("%s is not in the environment target manifest", name)
			}
		}
	}
	urlNames := []string{"WEBSOCKET_API_ENDPOINT", "GST_BASE_URL", "LLM_API_URL"}
	for _, prefix := range []string{"INVOICE", "REPORT", "UPLOAD", "RECURRING", "WEBSOCKET_TICKET", "CHECKOUT"} {
		urlNames = append(urlNames, "VERIFY_JOURNEY_"+prefix+"_URL", "VERIFY_JOURNEY_"+prefix+"_CLEANUP_URL")
	}
	for _, name := range urlNames {
		raw := env(name)
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Hostname() == "" {
			return fmt.Errorf("%s is invalid", name)
		}
		host := normalizeHost(parsed.Hostname())
		if isLoopbackHost(host) {
			continue
		}
		if _, ok := allowed[host]; !ok {
			return fmt.Errorf("%s is not in the environment target manifest", name)
		}
	}
	return nil
}

func normalizeHost(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if parsed, err := url.Parse("//" + raw); err == nil && parsed.Hostname() != "" {
		return strings.TrimSuffix(parsed.Hostname(), ".")
	}
	return strings.TrimSuffix(raw, ".")
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
