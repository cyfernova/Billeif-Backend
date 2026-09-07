package verify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentcorecontrol"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitoTypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/scheduler"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	sesTypes "github.com/aws/aws-sdk-go-v2/service/ses/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/redis/go-redis/v9"
)

// --- fake AWS clients -------------------------------------------------------

type fakeSTS struct {
	err     error
	account string
	arn     string
}

func (f *fakeSTS) GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	account := f.account
	if account == "" {
		account = "123456789012"
	}
	arn := f.arn
	if arn == "" {
		arn = "arn:aws:iam::123456789012:user/verify"
	}
	return &sts.GetCallerIdentityOutput{Account: aws.String(account), Arn: aws.String(arn)}, nil
}

type fakeCognito struct {
	describeErr error
	listErr     error
	providers   []string
	mfa         string
}

func (f *fakeCognito) DescribeUserPool(context.Context, *cognitoidentityprovider.DescribeUserPoolInput, ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.DescribeUserPoolOutput, error) {
	if f.describeErr != nil {
		return nil, f.describeErr
	}
	return &cognitoidentityprovider.DescribeUserPoolOutput{
		UserPool: &cognitoTypes.UserPoolType{MfaConfiguration: cognitoTypes.UserPoolMfaType(f.mfa)},
	}, nil
}

func (f *fakeCognito) ListIdentityProviders(context.Context, *cognitoidentityprovider.ListIdentityProvidersInput, ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ListIdentityProvidersOutput, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := &cognitoidentityprovider.ListIdentityProvidersOutput{}
	for _, name := range f.providers {
		out.Providers = append(out.Providers, cognitoTypes.ProviderDescription{ProviderName: aws.String(name)})
	}
	return out, nil
}

type fakeS3 struct {
	errs map[string]error
}

func (f *fakeS3) HeadBucket(_ context.Context, params *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if err := f.errs[aws.ToString(params.Bucket)]; err != nil {
		return nil, err
	}
	return &s3.HeadBucketOutput{}, nil
}

type fakeSQS struct {
	errs map[string]error
}

func (f *fakeSQS) GetQueueAttributes(_ context.Context, params *sqs.GetQueueAttributesInput, _ ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error) {
	if err := f.errs[aws.ToString(params.QueueUrl)]; err != nil {
		return nil, err
	}
	return &sqs.GetQueueAttributesOutput{
		Attributes: map[string]string{
			"QueueArn":                    "arn:aws:sqs:ap-south-1:123456789012:verify",
			"ApproximateNumberOfMessages": "2",
		},
	}, nil
}

type fakeScheduler struct {
	err error
}

func (f *fakeScheduler) ListSchedules(context.Context, *scheduler.ListSchedulesInput, ...func(*scheduler.Options)) (*scheduler.ListSchedulesOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &scheduler.ListSchedulesOutput{Schedules: nil, NextToken: nil}, nil
}

type fakeSES struct {
	err      error
	verified bool
}

type fakeAgentCore struct {
	err error
	arn string
}

func (f *fakeAgentCore) GetAgentRuntime(_ context.Context, _ *bedrockagentcorecontrol.GetAgentRuntimeInput, _ ...func(*bedrockagentcorecontrol.Options)) (*bedrockagentcorecontrol.GetAgentRuntimeOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &bedrockagentcorecontrol.GetAgentRuntimeOutput{AgentRuntimeArn: aws.String(f.arn)}, nil
}

func (f *fakeSES) GetIdentityVerificationAttributes(context.Context, *ses.GetIdentityVerificationAttributesInput, ...func(*ses.Options)) (*ses.GetIdentityVerificationAttributesOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	value := sesTypes.VerificationStatusPending
	if f.verified {
		value = sesTypes.VerificationStatusSuccess
	}
	return &ses.GetIdentityVerificationAttributesOutput{
		VerificationAttributes: map[string]sesTypes.IdentityVerificationAttributes{
			"verify@example.com": {VerificationStatus: value},
		},
	}, nil
}

func (f *fakeSES) GetSendQuota(context.Context, *ses.GetSendQuotaInput, ...func(*ses.Options)) (*ses.GetSendQuotaOutput, error) {
	return &ses.GetSendQuotaOutput{Max24HourSend: 200, MaxSendRate: 1}, nil
}

type fakeRedis struct {
	err error
}

func (f *fakeRedis) Ping(context.Context) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(context.Background())
	if f.err != nil {
		cmd.SetErr(f.err)
		return cmd
	}
	cmd.SetVal("PONG")
	return cmd
}

type fakePostgres struct {
	err     error
	version string
	called  bool
}

func (f *fakePostgres) PingContext(context.Context) error { return f.err }

func (f *fakePostgres) QueryRowContext(_ context.Context, _ string, _ ...any) RowScanner {
	f.called = true
	return fakeRow{err: f.err, version: f.version}
}

type fakeRow struct {
	err     error
	version string
}

func (f fakeRow) Scan(dest ...any) error {
	if f.err != nil {
		return f.err
	}
	if len(dest) == 1 {
		if target, ok := dest[0].(*string); ok {
			*target = f.version
		}
	}
	return nil
}

func evidenceMap(evidence []Evidence) map[string]any {
	out := map[string]any{}
	for _, item := range evidence {
		out[item.Key] = item.Value
	}
	return out
}

func TestAWSIdentityProbe(t *testing.T) {
	probe := &AWSIdentityProbe{Region: "ap-south-1", ExpectedAccountID: "123456789012", Client: &fakeSTS{}}
	outcome := probe.Probe(context.Background())
	if outcome.Status != StatusPassed {
		t.Fatalf("status = %q (%v)", outcome.Status, outcome.Err)
	}
	if probe.Region == "" {
		t.Fatal("fixture broken")
	}

	unconfigured := &AWSIdentityProbe{Region: "", ExpectedAccountID: "123456789012", Client: &fakeSTS{}}
	if got := unexecutedProbe(unexecutedProbeArgs{probe: unconfigured}).Status; got != StatusNotConfigured {
		t.Fatalf("missing region status = %q, want not_configured", got)
	}

	failing := &AWSIdentityProbe{Region: "ap-south-1", ExpectedAccountID: "123456789012", Client: &fakeSTS{err: errors.New("access denied: token=abc1234567890123")}}
	outcome = failing.Probe(context.Background())
	if outcome.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", outcome.Status)
	}
	for _, item := range SanitizeEvidence(outcome.Evidence) {
		if raw, ok := item.Value.(string); ok && strings.Contains(raw, "abc1234567890123") {
			t.Fatalf("error evidence leaked credential: %#v", item)
		}
	}
}

func TestAWSIdentityProbeBlocksMismatchAndRoot(t *testing.T) {
	mismatch := (&AWSIdentityProbe{Region: "ap-south-1", ExpectedAccountID: "123456789012", Client: &fakeSTS{account: "999999999999"}}).Probe(context.Background())
	if mismatch.Status != StatusBlocked || evidenceMap(mismatch.Evidence)["reason"] != ReasonAWSAccountMismatch {
		t.Fatalf("mismatch = %+v", mismatch)
	}
	root := (&AWSIdentityProbe{Region: "ap-south-1", ExpectedAccountID: "123456789012", Client: &fakeSTS{arn: "arn:aws:iam::123456789012:root"}}).Probe(context.Background())
	if root.Status != StatusBlocked || evidenceMap(root.Evidence)["reason"] != ReasonRootPrincipal {
		t.Fatalf("root = %+v", root)
	}
}

// unexecutedProbe ensures a nil client is reported not_configured without any
// client call.
func unexecutedProbe(args unexecutedProbeArgs) Outcome {
	if args.probe == nil {
		return Outcome{Status: StatusFailed}
	}
	return args.probe.Probe(context.Background())
}

type unexecutedProbeArgs struct {
	probe Prober
}

type Prober interface {
	Probe(ctx context.Context) Outcome
}

func TestCognitoUserPoolProbeClassifications(t *testing.T) {
	if got := (&CognitoUserPoolProbe{}).Probe(context.Background()).Status; got != StatusNotConfigured {
		t.Fatalf("missing pool id status = %q, want not_configured", got)
	}
	failed := (&CognitoUserPoolProbe{PoolID: "ap-south-1_ABC", Region: "ap-south-1", Client: &fakeCognito{describeErr: errors.New("not found")}}).Probe(context.Background())
	if failed.Status != StatusFailed {
		t.Fatalf("api error status = %q, want failed", failed.Status)
	}
	passed := (&CognitoUserPoolProbe{PoolID: "ap-south-1_ABC", Region: "ap-south-1", Client: &fakeCognito{mfa: "ON"}}).Probe(context.Background())
	if passed.Status != StatusPassed {
		t.Fatalf("status = %q (%v)", passed.Status, passed.Err)
	}
	if value, _ := evidenceMap(passed.Evidence)["mfa_configuration"].(string); value != "ON" {
		t.Fatalf("evidence = %#v", passed.Evidence)
	}
}

func TestGoogleAuthProbeClassifications(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"issuer":"https://example.com","authorization_endpoint":"https://example.com/auth"}`))
	}))
	defer server.Close()

	if got := (&GoogleAuthProbe{HTTPClient: server.Client()}).Probe(context.Background()).Status; got != StatusNotConfigured {
		t.Fatalf("missing domain status = %q, want not_configured", got)
	}

	passed := (&GoogleAuthProbe{
		HostedUIDomain: server.URL,
		PoolID:         "ap-south-1_ABC",
		Region:         "ap-south-1",
		Client:         &fakeCognito{providers: []string{"Google"}},
		HTTPClient:     server.Client(),
	}).Probe(context.Background())
	if passed.Status != StatusPassed {
		t.Fatalf("status = %q (%v)", passed.Status, passed.Err)
	}
	if value, _ := evidenceMap(passed.Evidence)["google_provider_present"].(bool); !value {
		t.Fatalf("evidence = %#v", passed.Evidence)
	}

	missingProvider := (&GoogleAuthProbe{
		HostedUIDomain: server.URL,
		PoolID:         "ap-south-1_ABC",
		Region:         "ap-south-1",
		Client:         &fakeCognito{},
		HTTPClient:     server.Client(),
	}).Probe(context.Background())
	if missingProvider.Status != StatusNotConfigured {
		t.Fatalf("status = %q, want not_configured when no Google provider", missingProvider.Status)
	}
}

func TestPostgresProbeClassifications(t *testing.T) {
	if got := (&PostgresProbe{}).Probe(context.Background()).Status; got != StatusNotConfigured {
		t.Fatalf("missing dsn status = %q, want not_configured", got)
	}
	passed := (&PostgresProbe{Host: "db", User: "u", Password: "p", Name: "billeif", SSLMode: "require", Dial: func(context.Context) (PostgresClient, error) {
		return &fakePostgres{version: "PostgreSQL 16.2"}, nil
	}}).Probe(context.Background())
	if passed.Status != StatusPassed {
		t.Fatalf("status = %q (%v)", passed.Status, passed.Err)
	}
	if value, _ := evidenceMap(passed.Evidence)["server_version"].(string); value != "16.2" {
		t.Fatalf("server_version = %q, want 16.2", value)
	}
	raw := fmt.Sprintf("%#v", passed.Evidence)
	if strings.Contains(raw, "p;") || strings.Contains(raw, "Password") {
		t.Fatalf("credentials leaked into evidence: %s", raw)
	}
	failed := (&PostgresProbe{Host: "db", User: "u", Password: "p", Name: "billeif", Dial: func(context.Context) (PostgresClient, error) {
		return nil, errors.New("connection refused")
	}}).Probe(context.Background())
	if failed.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", failed.Status)
	}
}

func TestPostgresDSNEscapesCredentialsAndDatabaseName(t *testing.T) {
	dsn := buildPostgresDSN("db.example", 5432, "user name", "p@ss word", "invoice db", "require")
	if strings.Contains(dsn, "p@ss word") || !strings.Contains(dsn, "p%40ss%20word") || !strings.Contains(dsn, "invoice%20db") {
		t.Fatalf("DSN is not safely escaped: %q", dsn)
	}
}

func TestRedisProbeClassifications(t *testing.T) {
	if got := (&RedisProbe{}).Probe(context.Background()).Status; got != StatusNotConfigured {
		t.Fatalf("missing host status = %q, want not_configured", got)
	}
	blocked := (&RedisProbe{Host: "cache", IAMAuthEnabled: true}).Probe(context.Background())
	if blocked.Status != StatusBlocked {
		t.Fatalf("iam auth status = %q, want blocked", blocked.Status)
	}
	passed := (&RedisProbe{Host: "cache", Dial: func() (RedisPinger, error) { return &fakeRedis{}, nil }}).Probe(context.Background())
	if passed.Status != StatusPassed {
		t.Fatalf("status = %q (%v)", passed.Status, passed.Err)
	}
	failed := (&RedisProbe{Host: "cache", Dial: func() (RedisPinger, error) { return &fakeRedis{err: errors.New("unavailable")}, nil }}).Probe(context.Background())
	if failed.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", failed.Status)
	}
}

func TestS3BucketsProbeClassifications(t *testing.T) {
	if got := (&S3BucketsProbe{}).Probe(context.Background()).Status; got != StatusNotConfigured {
		t.Fatalf("no buckets status = %q, want not_configured", got)
	}
	passed := (&S3BucketsProbe{
		Region:  "ap-south-1",
		Buckets: []string{"logos", "invoices"},
		Client:  &fakeS3{},
	}).Probe(context.Background())
	if passed.Status != StatusPassed {
		t.Fatalf("status = %q (%v)", passed.Status, passed.Err)
	}
	partial := (&S3BucketsProbe{
		Region:  "ap-south-1",
		Buckets: []string{"logos", "invoices"},
		Client:  &fakeS3{errs: map[string]error{"invoices": errors.New("forbidden")}},
	}).Probe(context.Background())
	if partial.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", partial.Status)
	}
	if _, ok := evidenceMap(partial.Evidence)["bucket_1"]; !ok {
		t.Fatalf("per-bucket evidence missing: %#v", partial.Evidence)
	}
}

func TestSQSQueuesProbeClassifications(t *testing.T) {
	if got := (&SQSQueuesProbe{}).Probe(context.Background()).Status; got != StatusNotConfigured {
		t.Fatalf("no queues status = %q, want not_configured", got)
	}
	passed := (&SQSQueuesProbe{
		Region:    "ap-south-1",
		QueueURLs: []string{"https://sqs.ap-south-1.amazonaws.com/123/invoice"},
		Client:    &fakeSQS{},
	}).Probe(context.Background())
	if passed.Status != StatusPassed {
		t.Fatalf("status = %q (%v)", passed.Status, passed.Err)
	}
	failed := (&SQSQueuesProbe{
		Region:    "ap-south-1",
		QueueURLs: []string{"https://sqs.ap-south-1.amazonaws.com/123/invoice"},
		Client:    &fakeSQS{errs: map[string]error{"https://sqs.ap-south-1.amazonaws.com/123/invoice": errors.New("denied")}},
	}).Probe(context.Background())
	if failed.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", failed.Status)
	}
}

func TestEventBridgeSchedulerProbeClassifications(t *testing.T) {
	if got := (&EventBridgeSchedulerProbe{}).Probe(context.Background()).Status; got != StatusNotConfigured {
		t.Fatalf("missing region status = %q, want not_configured", got)
	}
	passed := (&EventBridgeSchedulerProbe{Region: "ap-south-1", Client: &fakeScheduler{}}).Probe(context.Background())
	if passed.Status != StatusPassed {
		t.Fatalf("status = %q (%v)", passed.Status, passed.Err)
	}
	failed := (&EventBridgeSchedulerProbe{Region: "ap-south-1", Client: &fakeScheduler{err: errors.New("denied")}}).Probe(context.Background())
	if failed.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", failed.Status)
	}
}

func TestWebSocketEndpointProbeClassifications(t *testing.T) {
	if got := (&WebSocketEndpointProbe{}).Probe(context.Background()).Status; got != StatusNotConfigured {
		t.Fatalf("missing endpoint status = %q, want not_configured", got)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	passed := (&WebSocketEndpointProbe{Endpoint: server.URL, HTTPClient: server.Client()}).Probe(context.Background())
	if passed.Status != StatusPassed {
		t.Fatalf("403 status = %q (%v), want passed (API Gateway rejects plain GET)", passed.Status, passed.Err)
	}
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer dead.Close()
	failed := (&WebSocketEndpointProbe{Endpoint: dead.URL, HTTPClient: dead.Client()}).Probe(context.Background())
	if failed.Status != StatusFailed {
		t.Fatalf("500 status = %q, want failed", failed.Status)
	}
}

func TestSESIdentityProbeClassifications(t *testing.T) {
	if got := (&SESIdentityProbe{}).Probe(context.Background()).Status; got != StatusNotConfigured {
		t.Fatalf("missing sender status = %q, want not_configured", got)
	}
	passed := (&SESIdentityProbe{SenderEmail: "verify@example.com", Region: "ap-south-1", Client: &fakeSES{verified: true}}).Probe(context.Background())
	if passed.Status != StatusPassed {
		t.Fatalf("status = %q (%v)", passed.Status, passed.Err)
	}
	failed := (&SESIdentityProbe{SenderEmail: "verify@example.com", Region: "ap-south-1", Client: &fakeSES{verified: false}}).Probe(context.Background())
	if failed.Status != StatusFailed {
		t.Fatalf("unverified status = %q, want failed", failed.Status)
	}
}

func TestAgentCoreProbeClassifications(t *testing.T) {
	if got := (&AgentCoreRuntimeProbe{}).Probe(context.Background()).Status; got != StatusNotConfigured {
		t.Fatalf("missing arn status = %q, want not_configured", got)
	}
	passed := (&AgentCoreRuntimeProbe{
		RuntimeARN: "arn:aws:bedrock-agentcore:ap-south-1:123456789012:runtime/abc",
		Qualifier:  "PROD",
		Region:     "ap-south-1",
		Client:     &fakeAgentCore{arn: "arn:aws:bedrock-agentcore:ap-south-1:123456789012:runtime/abc"},
	}).Probe(context.Background())
	if passed.Status != StatusPassed {
		t.Fatalf("status = %q (%v)", passed.Status, passed.Err)
	}
	malformed := (&AgentCoreRuntimeProbe{RuntimeARN: "not-an-arn", Qualifier: "PROD"}).Probe(context.Background())
	if malformed.Status != StatusFailed {
		t.Fatalf("malformed arn status = %q, want failed", malformed.Status)
	}
}
