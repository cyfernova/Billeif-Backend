package verify

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentcorecontrol"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/scheduler"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqsTypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

// Stable probe reason codes.
const (
	ReasonTestModeRequired    = "test_mode_required"
	ReasonSandboxRequired     = "sandbox_required"
	ReasonIAMAuthUnsupported  = "iam_auth_probe_unsupported"
	ReasonMalformedARN        = "malformed_runtime_arn"
	ReasonSenderUnverified    = "sender_identity_unverified"
	ReasonFamilyNotOffered    = "model_family_not_offered"
	ReasonEndpointUnsupported = "probe_endpoint_unsupported"
	ReasonAWSAccountMismatch  = "aws_account_mismatch"
	ReasonRootPrincipal       = "root_principal_refused"
)

// --- AWS identity ------------------------------------------------------------

// STSIdentityClient is the consumer-side STS surface the harness needs.
type STSIdentityClient interface {
	GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

// AWSIdentityProbe verifies AWS credential and region resolution with a
// read-only STS GetCallerIdentity call.
type AWSIdentityProbe struct {
	Region            string
	ExpectedAccountID string
	Client            STSIdentityClient
}

func (p *AWSIdentityProbe) Metadata() Metadata {
	return Metadata{
		ID:          "aws.identity",
		Provider:    "aws",
		Description: "AWS credentials resolve via STS GetCallerIdentity (read-only)",
		Impact:      ImpactReadOnly,
	}
}

func (p *AWSIdentityProbe) Probe(ctx context.Context) Outcome {
	if strings.TrimSpace(p.Region) == "" {
		return NotConfigured("AWS_REGION")
	}
	if strings.TrimSpace(p.ExpectedAccountID) == "" {
		return NotConfigured("VERIFY_AWS_ACCOUNT_ID")
	}
	if p.Client == nil {
		return NotConfigured("aws credentials")
	}
	output, err := p.Client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return Failed(err, "sts_identity_failed", false)
	}
	if output == nil || aws.ToString(output.Account) != strings.TrimSpace(p.ExpectedAccountID) {
		return Blocked(ReasonAWSAccountMismatch)
	}
	if strings.HasSuffix(strings.ToLower(aws.ToString(output.Arn)), ":root") {
		return Blocked(ReasonRootPrincipal)
	}
	return Outcome{
		Status: StatusPassed,
		Evidence: []Evidence{
			{Key: "region", Value: strings.TrimSpace(p.Region)},
			{Key: "identity_confirmed", Value: true},
		},
	}
}

// --- Cognito -----------------------------------------------------------------

// CognitoDescribeClient is the consumer-side Cognito surface.
type CognitoDescribeClient interface {
	DescribeUserPool(ctx context.Context, params *cognitoidentityprovider.DescribeUserPoolInput, opts ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.DescribeUserPoolOutput, error)
}

type CognitoIdentityListClient interface {
	ListIdentityProviders(ctx context.Context, params *cognitoidentityprovider.ListIdentityProvidersInput, opts ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ListIdentityProvidersOutput, error)
}

type CognitoGoogleAuthClient interface {
	CognitoDescribeClient
	CognitoIdentityListClient
}

// CognitoUserPoolProbe verifies a user pool exists and is describable with the
// configured credentials (read-only DescribeUserPool).
type CognitoUserPoolProbe struct {
	PoolID string
	Region string
	Client CognitoDescribeClient
}

func (p *CognitoUserPoolProbe) Metadata() Metadata {
	return Metadata{
		ID:           "cognito.userpool",
		Provider:     "cognito",
		Description:  "Cognito user pool is describable (read-only DescribeUserPool)",
		Impact:       ImpactReadOnly,
		Dependencies: []string{"aws.identity"},
	}
}

func (p *CognitoUserPoolProbe) Probe(ctx context.Context) Outcome {
	poolID := strings.TrimSpace(p.PoolID)
	if poolID == "" || p.Client == nil {
		return NotConfigured("COGNITO_USER_POOL_ID", "aws credentials")
	}
	output, err := p.Client.DescribeUserPool(ctx, &cognitoidentityprovider.DescribeUserPoolInput{
		UserPoolId: aws.String(poolID),
	})
	if err != nil {
		return Failed(err, "cognito_describe_failed", false)
	}
	if output == nil || output.UserPool == nil {
		return Failed(errors.New("Cognito user pool response was empty"), "cognito_response_invalid", false)
	}
	evidence := []Evidence{
		{Key: "mfa_configuration", Value: string(output.UserPool.MfaConfiguration)},
	}
	if output.UserPool.EmailConfiguration != nil {
		evidence = append(evidence, Evidence{Key: "email_sending_account", Value: string(output.UserPool.EmailConfiguration.EmailSendingAccount)})
	}
	return Outcome{Status: StatusPassed, Evidence: evidence}
}

// CognitoPhonePoolProbe verifies the dedicated phone user pool.
type CognitoPhonePoolProbe struct {
	PoolID string
	Region string
	Client CognitoDescribeClient
}

func (p *CognitoPhonePoolProbe) Metadata() Metadata {
	return Metadata{
		ID:           "cognito.phone_pool",
		Provider:     "cognito",
		Description:  "Cognito phone user pool is describable (read-only DescribeUserPool)",
		Impact:       ImpactReadOnly,
		Dependencies: []string{"aws.identity"},
	}
}

func (p *CognitoPhonePoolProbe) Probe(ctx context.Context) Outcome {
	poolID := strings.TrimSpace(p.PoolID)
	if poolID == "" || p.Client == nil {
		return NotConfigured("COGNITO_PHONE_USER_POOL_ID", "aws credentials")
	}
	if _, err := p.Client.DescribeUserPool(ctx, &cognitoidentityprovider.DescribeUserPoolInput{
		UserPoolId: aws.String(poolID),
	}); err != nil {
		return Failed(err, "cognito_phone_describe_failed", false)
	}
	return Outcome{
		Status:   StatusPassed,
		Evidence: []Evidence{{Key: "phone_pool_describable", Value: true}},
	}
}

// GoogleAuthProbe verifies the Cognito hosted UI OIDC discovery document and
// whether a Google identity provider is registered on the user pool.
type GoogleAuthProbe struct {
	HostedUIDomain string
	PoolID         string
	Region         string
	Client         CognitoGoogleAuthClient
	HTTPClient     *http.Client
}

func (p *GoogleAuthProbe) Metadata() Metadata {
	return Metadata{
		ID:           "cognito.google_auth",
		Provider:     "google",
		Description:  "Google authentication via Cognito hosted UI OIDC discovery and identity provider listing (read-only)",
		Impact:       ImpactReadOnly,
		Dependencies: []string{"aws.identity"},
	}
}

func (p *GoogleAuthProbe) Probe(ctx context.Context) Outcome {
	domain := strings.TrimSpace(p.HostedUIDomain)
	if domain == "" || strings.TrimSpace(p.PoolID) == "" || p.Client == nil {
		return NotConfigured("COGNITO_DOMAIN", "COGNITO_USER_POOL_ID", "aws credentials")
	}
	if !strings.HasPrefix(domain, "http") {
		domain = "https://" + domain
	}
	if err := validateProbeURL(domain); err != nil {
		return Blocked("unsafe_provider_endpoint")
	}
	httpClient := p.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(domain, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return Failed(err, "google_oidc_request_failed", false)
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return Failed(err, "google_oidc_unreachable", true)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Failed(fmt.Errorf("hosted UI OIDC discovery returned status %d", response.StatusCode), "google_oidc_unavailable", false)
	}

	evidence := []Evidence{{Key: "oidc_discovery_ok", Value: true}}
	listing, err := p.Client.ListIdentityProviders(ctx, &cognitoidentityprovider.ListIdentityProvidersInput{
		UserPoolId: aws.String(strings.TrimSpace(p.PoolID)),
		MaxResults: aws.Int32(60),
	})
	if err != nil {
		return Failed(err, "cognito_list_providers_failed", false, evidence...)
	}
	googlePresent := false
	for _, provider := range listing.Providers {
		if strings.EqualFold(aws.ToString(provider.ProviderName), "Google") {
			googlePresent = true
			break
		}
	}
	evidence = append(evidence, Evidence{Key: "google_provider_present", Value: googlePresent})
	if !googlePresent {
		return NotConfigured("cognito google identity provider")
	}
	return Outcome{Status: StatusPassed, Evidence: evidence}
}

// --- PostgreSQL --------------------------------------------------------------

// RowScanner is the minimal row contract used by the harness.
type RowScanner interface {
	Scan(dest ...any) error
}

// PostgresClient is the consumer-side PostgreSQL surface.
type PostgresClient interface {
	PingContext(ctx context.Context) error
	QueryRowContext(ctx context.Context, query string, args ...any) RowScanner
}

// PostgresDialFunc opens a PostgreSQL client. The production adapter wraps
// database/sql with the pq driver; tests inject fakes.
type PostgresDialFunc func(ctx context.Context) (PostgresClient, error)

// PostgresProbe verifies PostgreSQL connectivity with SELECT version().
type PostgresProbe struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
	Dial     PostgresDialFunc
}

func (p *PostgresProbe) Metadata() Metadata {
	return Metadata{
		ID:          "postgres.connect",
		Provider:    "postgresql",
		Description: "PostgreSQL connectivity and SELECT version() (read-only)",
		Impact:      ImpactReadOnly,
	}
}

func (p *PostgresProbe) Probe(ctx context.Context) Outcome {
	if strings.TrimSpace(p.Host) == "" || strings.TrimSpace(p.User) == "" ||
		strings.TrimSpace(p.Password) == "" || strings.TrimSpace(p.Name) == "" {
		return NotConfigured("DATABASE_HOST", "DATABASE_USER", "DATABASE_PASSWORD", "DATABASE_NAME")
	}
	if p.Dial == nil {
		return NotConfigured("postgres dialer")
	}
	client, err := p.Dial(ctx)
	if err != nil {
		return Failed(err, "postgres_connect_failed", true)
	}
	defer closePostgres(client)
	if err := client.PingContext(ctx); err != nil {
		return Failed(err, "postgres_ping_failed", true)
	}
	var version string
	if err := client.QueryRowContext(ctx, "SELECT version()").Scan(&version); err != nil {
		return Failed(err, "postgres_version_failed", true)
	}
	sslMode := strings.TrimSpace(p.SSLMode)
	if sslMode == "" {
		sslMode = "require"
	}
	return Outcome{
		Status: StatusPassed,
		Evidence: []Evidence{
			{Key: "connected", Value: true},
			{Key: "server_version", Value: postgresVersion(version)},
			{Key: "ssl_mode", Value: sslMode},
		},
	}
}

func postgresVersion(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return ""
	}
	if len(fields) > 1 && strings.EqualFold(fields[0], "PostgreSQL") {
		return fields[1]
	}
	return fields[0]
}

// closePostgreSQL best-effort closes *sql.DB-backed clients.
func closePostgres(client PostgresClient) {
	if closer, ok := client.(interface{ Close() error }); closer != nil && ok {
		_ = closer.Close()
	}
}

// DialPostgres builds the production PostgreSQL dialer. The DSN is assembled
// at call time and never logged, stored, or returned.
func DialPostgres(host string, port int, user, password, name, sslMode string) PostgresDialFunc {
	return func(ctx context.Context) (PostgresClient, error) {
		dsn := buildPostgresDSN(host, port, user, password, name, sslMode)
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return nil, err
		}
		return &postgresClient{db: db}, nil
	}
}

func buildPostgresDSN(host string, port int, user, password, name, sslMode string) string {
	if port == 0 {
		port = 5432
	}
	if sslMode == "" {
		sslMode = "require"
	}
	dsn := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/" + name,
	}
	query := dsn.Query()
	query.Set("sslmode", sslMode)
	query.Set("connect_timeout", "10")
	dsn.RawQuery = query.Encode()
	return dsn.String()
}

type postgresClient struct {
	db *sql.DB
}

func (c *postgresClient) PingContext(ctx context.Context) error { return c.db.PingContext(ctx) }

func (c *postgresClient) QueryRowContext(ctx context.Context, query string, args ...any) RowScanner {
	return c.db.QueryRowContext(ctx, query, args...)
}

func (c *postgresClient) Close() error { return c.db.Close() }

// --- Redis -------------------------------------------------------------------

// RedisPinger is the consumer-side Redis surface.
type RedisPinger interface {
	Ping(ctx context.Context) *redis.StatusCmd
}

// RedisDialFunc builds a Redis client for the probe.
type RedisDialFunc func() (RedisPinger, error)

// RedisProbe verifies Redis/Valkey reachability with PING.
type RedisProbe struct {
	Host           string
	Port           int
	Password       string
	DB             int
	TLSEnabled     bool
	ClusterMode    bool
	IAMAuthEnabled bool
	Dial           RedisDialFunc
}

func (p *RedisProbe) Metadata() Metadata {
	return Metadata{
		ID:          "redis.ping",
		Provider:    "redis",
		Description: "Redis or Valkey reachability via PING (read-only)",
		Impact:      ImpactReadOnly,
	}
}

func (p *RedisProbe) Probe(ctx context.Context) Outcome {
	if strings.TrimSpace(p.Host) == "" {
		return NotConfigured("REDIS_HOST")
	}
	if p.IAMAuthEnabled {
		return Blocked(ReasonIAMAuthUnsupported, Evidence{Key: "tls", Value: p.TLSEnabled})
	}
	if p.Dial == nil {
		return NotConfigured("redis dialer")
	}
	client, err := p.Dial()
	if err != nil {
		return Failed(err, "redis_dial_failed", true)
	}
	if closer, ok := client.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	reply, err := client.Ping(ctx).Result()
	if err != nil {
		return Failed(err, "redis_ping_failed", true)
	}
	mode := "standalone"
	if p.ClusterMode {
		mode = "cluster"
	}
	return Outcome{
		Status: StatusPassed,
		Evidence: []Evidence{
			{Key: "reply", Value: reply},
			{Key: "mode", Value: mode},
			{Key: "tls", Value: p.TLSEnabled},
		},
	}
}

// --- S3 ----------------------------------------------------------------------

// S3HeadBucketer is the consumer-side S3 surface.
type S3HeadBucketer interface {
	HeadBucket(ctx context.Context, params *s3.HeadBucketInput, opts ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
}

// S3BucketsProbe verifies every configured bucket is accessible (HeadBucket,
// read-only).
type S3BucketsProbe struct {
	Region  string
	Buckets []string
	Client  S3HeadBucketer
}

func (p *S3BucketsProbe) Metadata() Metadata {
	return Metadata{
		ID:           "s3.buckets",
		Provider:     "s3",
		Description:  "Configured S3 buckets are accessible (read-only HeadBucket)",
		Impact:       ImpactReadOnly,
		Dependencies: []string{"aws.identity"},
	}
}

func (p *S3BucketsProbe) Probe(ctx context.Context) Outcome {
	buckets := nonEmptyStrings(p.Buckets)
	if len(buckets) == 0 || p.Client == nil {
		return NotConfigured("S3_BUCKET_*", "aws credentials")
	}
	evidence := make([]Evidence, 0, len(buckets))
	allAccessible := true
	for index, bucket := range buckets {
		alias := fmt.Sprintf("bucket_%d", index+1)
		_, err := p.Client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)})
		if err != nil {
			allAccessible = false
			evidence = append(evidence, Evidence{Key: alias, Value: "error:" + httpStatusOrError(err)})
			continue
		}
		evidence = append(evidence, Evidence{Key: alias, Value: "accessible"})
	}
	if !allAccessible {
		return Failed(errors.New("one or more S3 buckets are not accessible"), "s3_bucket_inaccessible", false, evidence...)
	}
	return Outcome{Status: StatusPassed, Evidence: evidence}
}

// --- SQS ---------------------------------------------------------------------

// SQSAttributesClient is the consumer-side SQS surface.
type SQSAttributesClient interface {
	GetQueueAttributes(ctx context.Context, params *sqs.GetQueueAttributesInput, opts ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
}

// SQSQueuesProbe verifies every configured queue is readable (read-only
// GetQueueAttributes).
type SQSQueuesProbe struct {
	Region    string
	QueueURLs []string
	Client    SQSAttributesClient
}

func (p *SQSQueuesProbe) Metadata() Metadata {
	return Metadata{
		ID:           "sqs.queues",
		Provider:     "sqs",
		Description:  "Configured SQS queues are readable (read-only GetQueueAttributes)",
		Impact:       ImpactReadOnly,
		Dependencies: []string{"aws.identity"},
	}
}

func (p *SQSQueuesProbe) Probe(ctx context.Context) Outcome {
	urls := nonEmptyStrings(p.QueueURLs)
	if len(urls) == 0 || p.Client == nil {
		return NotConfigured("SQS_*_QUEUE", "aws credentials")
	}
	evidence := make([]Evidence, 0, len(urls))
	allReadable := true
	for index, queueURL := range urls {
		alias := fmt.Sprintf("queue_%d", index+1)
		output, err := p.Client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
			QueueUrl: aws.String(queueURL),
			AttributeNames: []sqsTypes.QueueAttributeName{
				sqsTypes.QueueAttributeNameQueueArn,
				sqsTypes.QueueAttributeNameApproximateNumberOfMessages,
			},
		})
		if err != nil {
			allReadable = false
			evidence = append(evidence, Evidence{Key: alias, Value: "error:" + httpStatusOrError(err)})
			continue
		}
		evidence = append(evidence, Evidence{
			Key:   alias,
			Value: "visible;messages=" + output.Attributes["ApproximateNumberOfMessages"],
		})
	}
	if !allReadable {
		return Failed(errors.New("one or more SQS queues are not readable"), "sqs_queue_unreadable", false, evidence...)
	}
	return Outcome{Status: StatusPassed, Evidence: evidence}
}

// --- EventBridge Scheduler ---------------------------------------------------

// SchedulerListClient is the consumer-side EventBridge Scheduler surface.
type SchedulerListClient interface {
	ListSchedules(ctx context.Context, params *scheduler.ListSchedulesInput, opts ...func(*scheduler.Options)) (*scheduler.ListSchedulesOutput, error)
}

// EventBridgeSchedulerProbe verifies EventBridge Scheduler control-plane
// reachability with a bounded ListSchedules read.
type EventBridgeSchedulerProbe struct {
	Region string
	Client SchedulerListClient
}

func (p *EventBridgeSchedulerProbe) Metadata() Metadata {
	return Metadata{
		ID:           "eventbridge.scheduler",
		Provider:     "eventbridge",
		Description:  "EventBridge Scheduler control plane is reachable (read-only ListSchedules, bounded)",
		Impact:       ImpactReadOnly,
		Dependencies: []string{"aws.identity"},
	}
}

func (p *EventBridgeSchedulerProbe) Probe(ctx context.Context) Outcome {
	if strings.TrimSpace(p.Region) == "" {
		return NotConfigured("AWS_REGION")
	}
	if p.Client == nil {
		return NotConfigured("aws credentials")
	}
	output, err := p.Client.ListSchedules(ctx, &scheduler.ListSchedulesInput{MaxResults: aws.Int32(10)})
	if err != nil {
		return Failed(err, "eventbridge_scheduler_unreachable", true)
	}
	count := 0
	if output != nil {
		count = len(output.Schedules)
	}
	return Outcome{
		Status: StatusPassed,
		Evidence: []Evidence{
			{Key: "schedules_visible", Value: count},
			{Key: "bounded", Value: 10},
		},
	}
}

// --- WebSocket endpoint ------------------------------------------------------

// WebSocketEndpointProbe verifies the WebSocket API endpoint responds over TLS
// with a plain HTTP GET (API Gateway rejects non-WebSocket GETs with 4xx).
type WebSocketEndpointProbe struct {
	Endpoint   string
	HTTPClient *http.Client
}

func (p *WebSocketEndpointProbe) Metadata() Metadata {
	return Metadata{
		ID:          "websocket.endpoint",
		Provider:    "websocket",
		Description: "WebSocket API endpoint responds over TLS (read-only GET)",
		Impact:      ImpactReadOnly,
	}
}

func (p *WebSocketEndpointProbe) Probe(ctx context.Context) Outcome {
	endpoint := strings.TrimSpace(p.Endpoint)
	if endpoint == "" {
		return NotConfigured("WEBSOCKET_API_ENDPOINT")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return Failed(err, "websocket_endpoint_invalid", false)
	}
	switch parsed.Scheme {
	case "wss":
		parsed.Scheme = "https"
	case "ws":
		parsed.Scheme = "http"
	}
	endpoint = parsed.String()
	if err := validateProbeURL(endpoint); err != nil {
		return Blocked("unsafe_provider_endpoint")
	}
	httpClient := p.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Failed(err, "websocket_endpoint_invalid", false)
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return Failed(err, "websocket_endpoint_unreachable", true)
	}
	defer response.Body.Close()
	evidence := []Evidence{
		{Key: "status_code", Value: response.StatusCode},
		{Key: "endpoint_reachable", Value: true},
	}
	if response.StatusCode >= 500 {
		return Failed(fmt.Errorf("websocket endpoint returned status %d", response.StatusCode), "websocket_endpoint_unhealthy", true, evidence...)
	}
	return Outcome{Status: StatusPassed, Evidence: evidence}
}

// --- SES ---------------------------------------------------------------------

// SESIdentityClient is the consumer-side SES surface.
type SESIdentityClient interface {
	GetIdentityVerificationAttributes(ctx context.Context, params *ses.GetIdentityVerificationAttributesInput, opts ...func(*ses.Options)) (*ses.GetIdentityVerificationAttributesOutput, error)
}

// SESIdentityProbe verifies the configured sender identity exists and is
// verified (read-only).
type SESIdentityProbe struct {
	SenderEmail string
	Region      string
	Client      SESIdentityClient
}

func (p *SESIdentityProbe) Metadata() Metadata {
	return Metadata{
		ID:           "ses.identity",
		Provider:     "ses",
		Description:  "SES sender identity verification status (read-only)",
		Impact:       ImpactReadOnly,
		Dependencies: []string{"aws.identity"},
	}
}

func (p *SESIdentityProbe) Probe(ctx context.Context) Outcome {
	sender := strings.TrimSpace(p.SenderEmail)
	if sender == "" || p.Client == nil {
		return NotConfigured("SES_SENDER_EMAIL", "aws credentials")
	}
	verification, err := p.Client.GetIdentityVerificationAttributes(ctx, &ses.GetIdentityVerificationAttributesInput{
		Identities: []string{sender},
	})
	if err != nil {
		return Failed(err, "ses_verification_lookup_failed", true)
	}
	if verification == nil {
		return Failed(errors.New("SES verification response was empty"), "ses_response_invalid", false)
	}
	attributes, ok := verification.VerificationAttributes[sender]
	if !ok || string(attributes.VerificationStatus) != "Success" {
		return Failed(errors.New("sender identity is not verified"), ReasonSenderUnverified, false,
			Evidence{Key: "verification_status", Value: string(attributes.VerificationStatus)})
	}
	return Outcome{
		Status: StatusPassed,
		Evidence: []Evidence{
			{Key: "sender_verified", Value: true},
			{Key: "region", Value: strings.TrimSpace(p.Region)},
		},
	}
}

// --- AgentCore ---------------------------------------------------------------

// AgentCoreRuntimeProbe verifies AgentCore runtime configuration. It never
// invokes the runtime (that is a billable, externally visible action); it
// validates the ARN shape and depends on aws.identity for credential proof.
type AgentCoreRuntimeProbe struct {
	RuntimeARN string
	Qualifier  string
	Region     string
	Client     AgentCoreRuntimeClient
}

type AgentCoreRuntimeClient interface {
	GetAgentRuntime(context.Context, *bedrockagentcorecontrol.GetAgentRuntimeInput, ...func(*bedrockagentcorecontrol.Options)) (*bedrockagentcorecontrol.GetAgentRuntimeOutput, error)
}

func (p *AgentCoreRuntimeProbe) Metadata() Metadata {
	return Metadata{
		ID:           "agentcore.runtime",
		Provider:     "agentcore",
		Description:  "AgentCore runtime ARN and qualifier are configured and well-formed (no runtime invocation)",
		Impact:       ImpactReadOnly,
		Dependencies: []string{"aws.identity"},
	}
}

func (p *AgentCoreRuntimeProbe) Probe(ctx context.Context) Outcome {
	runtimeARN := strings.TrimSpace(p.RuntimeARN)
	if runtimeARN == "" {
		return NotConfigured("AGENTCORE_RUNTIME_ARN")
	}
	parsed, err := arn.Parse(runtimeARN)
	if err != nil || parsed.Service != "bedrock-agentcore" || parsed.Region == "" || parsed.AccountID == "" || !strings.HasPrefix(parsed.Resource, "runtime/") {
		return Failed(errors.New("AgentCore runtime ARN is malformed"), ReasonMalformedARN, false)
	}
	runtimeID := strings.TrimPrefix(parsed.Resource, "runtime/")
	if runtimeID == "" || strings.Contains(runtimeID, "/") {
		return Failed(errors.New("AgentCore runtime ARN is malformed"), ReasonMalformedARN, false)
	}
	if strings.TrimSpace(p.Region) != "" && parsed.Region != strings.TrimSpace(p.Region) {
		return Blocked("agentcore_region_mismatch")
	}
	qualifier := strings.TrimSpace(p.Qualifier)
	if qualifier == "" {
		return Failed(errors.New("AgentCore runtime qualifier is required"), ReasonMalformedARN, false)
	}
	if p.Client == nil {
		return NotConfigured("AgentCore control client")
	}
	output, err := p.Client.GetAgentRuntime(ctx, &bedrockagentcorecontrol.GetAgentRuntimeInput{AgentRuntimeId: aws.String(runtimeID)})
	if err != nil {
		return Failed(err, "agentcore_runtime_lookup_failed", true)
	}
	if output == nil || aws.ToString(output.AgentRuntimeArn) != runtimeARN {
		return Failed(errors.New("AgentCore runtime lookup did not match configured ARN"), "agentcore_runtime_mismatch", false)
	}
	return Outcome{
		Status: StatusPassed,
		Evidence: []Evidence{
			{Key: "runtime_configured", Value: true},
			{Key: "runtime_reachable", Value: true},
			{Key: "invocation_performed", Value: false},
		},
	}
}

// --- shared helpers ----------------------------------------------------------

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func httpStatusOrError(err error) string {
	var httpErr interface{ HTTPStatusCode() int }
	if errors.As(err, &httpErr) {
		return fmt.Sprintf("status_%d", httpErr.HTTPStatusCode())
	}
	return "provider_error"
}
