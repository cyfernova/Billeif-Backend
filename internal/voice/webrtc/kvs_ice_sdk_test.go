package webrtc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/kinesisvideo"
	"github.com/aws/aws-sdk-go-v2/service/kinesisvideosignaling"
	"github.com/aws/smithy-go/logging"
	"github.com/aws/smithy-go/middleware"
)

func TestKVSICECredentialSourceProductionSDKUsesOnlyPinnedEndpoints(t *testing.T) {
	t.Parallel()

	const discoveredEndpoint = "https://r-sdk123.kinesisvideo.ap-south-1.amazonaws.com:443"
	transport := &scriptedKVSHTTPClient{responses: []string{
		`{"ResourceEndpointList":[{"Protocol":"HTTPS","ResourceEndpoint":"` + discoveredEndpoint + `"}]}`,
		`{"IceServerList":[{"Uris":["turn:34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp"],"Username":"sdk-user-01","Password":"sdk-password-01","Ttl":300}]}`,
	}}
	var endpointResolverCalls atomic.Int64
	var apiOptionCalls atomic.Int64
	var serviceOptionCalls atomic.Int64
	var logCalls atomic.Int64
	config := aws.Config{
		Region:       MumbaiRegion,
		Credentials:  aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("fake-access", "fake-secret", "")),
		HTTPClient:   transport,
		BaseEndpoint: aws.String("https://endpoint-secret-canary.invalid"),
		//nolint:staticcheck // Deliberate deprecated-resolver canary proves production drops this redirect path.
		EndpointResolverWithOptions: aws.EndpointResolverWithOptionsFunc(func(string, string, ...interface{}) (aws.Endpoint, error) {
			endpointResolverCalls.Add(1)
			return aws.Endpoint{URL: "https://resolver-secret-canary.invalid"}, nil //nolint:staticcheck // Same redirect canary.
		}),
		APIOptions: []func(*middleware.Stack) error{func(*middleware.Stack) error {
			apiOptionCalls.Add(1)
			return nil
		}},
		ServiceOptions: []func(string, any){func(_ string, options any) {
			serviceOptionCalls.Add(1)
			switch typed := options.(type) {
			case *kinesisvideo.Options:
				typed.BaseEndpoint = aws.String("https://service-option-secret-canary.invalid")
			case *kinesisvideosignaling.Options:
				typed.BaseEndpoint = aws.String("https://service-option-secret-canary.invalid")
			}
		}},
		Logger: logging.LoggerFunc(func(logging.Classification, string, ...interface{}) {
			logCalls.Add(1)
		}),
		ClientLogMode: aws.LogRequestWithBody | aws.LogResponseWithBody | aws.LogSigning,
	}

	source, err := NewKVSICECredentialSource(config)
	if err != nil {
		t.Fatalf("NewKVSICECredentialSource() error = %v", err)
	}
	now := time.Date(2026, time.August, 7, 3, 0, 0, 0, time.UTC)
	source.now = func() time.Time { return now }
	turn, err := source.GetTURN(context.Background(), testKVSChannelARN)
	if err != nil {
		t.Fatalf("GetTURN() error = %v", err)
	}
	if turn.username != "sdk-user-01" || turn.password != "sdk-password-01" || len(turn.uris) != 1 ||
		!turn.ExpiresAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("GetTURN() returned unexpected SDK credential response: %#v", turn)
	}

	requests := transport.snapshot()
	if len(requests) != 2 {
		t.Fatalf("SDK made %d HTTP requests, want exactly 2", len(requests))
	}
	assertPinnedKVSHTTPRequest(
		t,
		requests[0],
		"https://kinesisvideo.ap-south-1.amazonaws.com/getSignalingChannelEndpoint",
		[]string{`"ChannelARN":"` + testKVSChannelARN + `"`, `"Protocols":["HTTPS"]`, `"Role":"MASTER"`},
	)
	assertPinnedKVSHTTPRequest(
		t,
		requests[1],
		discoveredEndpoint+"/v1/get-ice-server-config",
		[]string{`"ChannelARN":"` + testKVSChannelARN + `"`, `"Service":"TURN"`},
	)
	if strings.Contains(requests[1].body, "ClientId") || strings.Contains(requests[1].body, "Username") {
		t.Fatalf("GetIceServerConfig request included optional identity fields: %s", requests[1].body)
	}
	if endpointResolverCalls.Load() != 0 || apiOptionCalls.Load() != 0 || serviceOptionCalls.Load() != 0 || logCalls.Load() != 0 {
		t.Fatalf(
			"untrusted SDK hooks invoked: resolver=%d api=%d service=%d logs=%d",
			endpointResolverCalls.Load(), apiOptionCalls.Load(), serviceOptionCalls.Load(), logCalls.Load(),
		)
	}
}

type recordedKVSHTTPRequest struct {
	method        string
	url           string
	body          string
	authorization string
}

type scriptedKVSHTTPClient struct {
	mu        sync.Mutex
	responses []string
	requests  []recordedKVSHTTPRequest
}

func (client *scriptedKVSHTTPClient) Do(request *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, fmt.Errorf("read local SDK request: %w", err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	index := len(client.requests)
	client.requests = append(client.requests, recordedKVSHTTPRequest{
		method:        request.Method,
		url:           request.URL.String(),
		body:          string(body),
		authorization: request.Header.Get("Authorization"),
	})
	if index >= len(client.responses) {
		return nil, fmt.Errorf("unexpected local SDK request %d", index+1)
	}
	responseBody := client.responses[index]
	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(strings.NewReader(responseBody)),
		ContentLength: int64(len(responseBody)),
		Request:       request,
	}, nil
}

func (client *scriptedKVSHTTPClient) snapshot() []recordedKVSHTTPRequest {
	client.mu.Lock()
	defer client.mu.Unlock()
	return append([]recordedKVSHTTPRequest(nil), client.requests...)
}

func assertPinnedKVSHTTPRequest(t *testing.T, request recordedKVSHTTPRequest, wantURL string, bodyFragments []string) {
	t.Helper()
	if request.method != http.MethodPost || request.url != wantURL {
		t.Fatalf("SDK request = %s %s, want POST %s", request.method, request.url, wantURL)
	}
	if !json.Valid([]byte(request.body)) {
		t.Fatalf("SDK request body is not JSON: %q", request.body)
	}
	for _, fragment := range bodyFragments {
		if !strings.Contains(request.body, fragment) {
			t.Fatalf("SDK request body %s does not contain %s", request.body, fragment)
		}
	}
	if !strings.Contains(request.authorization, "Credential=fake-access/") {
		t.Fatalf("SDK request was not signed with the injected local test credential")
	}
}
