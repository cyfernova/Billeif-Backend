package webrtc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesisvideo"
	kvstypes "github.com/aws/aws-sdk-go-v2/service/kinesisvideo/types"
	"github.com/aws/aws-sdk-go-v2/service/kinesisvideosignaling"
)

func TestNewKVSICECredentialSourceRejectsInvalidDependencies(t *testing.T) {
	t.Parallel()

	validControl := kvsEndpointClientFunc(validKVSEndpointCall)
	validFactory := kvsSignalingFactoryFunc(func(string) (kvsSignalingClient, error) {
		return kvsSignalingClientFunc(validKVSICECall), nil
	})
	var typedNilControl *fakeKVSEndpointClient
	var typedNilFactory *fakeKVSSignalingFactory

	tests := []struct {
		name    string
		region  string
		control kvsEndpointClient
		factory kvsSignalingClientFactory
		now     func() time.Time
	}{
		{name: "empty region", control: validControl, factory: validFactory, now: time.Now},
		{name: "foreign region", region: "us-east-1", control: validControl, factory: validFactory, now: time.Now},
		{name: "nil control", region: MumbaiRegion, factory: validFactory, now: time.Now},
		{name: "typed nil control", region: MumbaiRegion, control: typedNilControl, factory: validFactory, now: time.Now},
		{name: "nil factory", region: MumbaiRegion, control: validControl, now: time.Now},
		{name: "typed nil factory", region: MumbaiRegion, control: validControl, factory: typedNilFactory, now: time.Now},
		{name: "nil clock", region: MumbaiRegion, control: validControl, factory: validFactory},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			source, err := newKVSICECredentialSource(testCase.region, testCase.control, testCase.factory, testCase.now)
			if source != nil || !errors.Is(err, ErrInvalidKVSICEConfig) {
				t.Fatalf("newKVSICECredentialSource() = (%#v, %v), want nil, %v", source, err, ErrInvalidKVSICEConfig)
			}
		})
	}

	if source, err := NewKVSICECredentialSource(aws.Config{Region: "us-east-1"}); source != nil || !errors.Is(err, ErrInvalidKVSICEConfig) {
		t.Fatalf("NewKVSICECredentialSource(foreign region) = (%#v, %v), want nil, %v", source, err, ErrInvalidKVSICEConfig)
	}
	if source, err := NewKVSICECredentialSource(aws.Config{Region: MumbaiRegion}); source != nil || !errors.Is(err, ErrInvalidKVSICEConfig) {
		t.Fatalf("NewKVSICECredentialSource(nil credentials) = (%#v, %v), want nil, %v", source, err, ErrInvalidKVSICEConfig)
	}
}

func TestKVSICECredentialSourceRejectsInvalidRequestWithoutAWSCalls(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	control := kvsEndpointClientFunc(func(
		context.Context,
		*kinesisvideo.GetSignalingChannelEndpointInput,
		...func(*kinesisvideo.Options),
	) (*kinesisvideo.GetSignalingChannelEndpointOutput, error) {
		calls.Add(1)
		return validKVSEndpointOutput(), nil
	})
	source, err := newKVSICECredentialSource(
		MumbaiRegion,
		control,
		kvsSignalingFactoryFunc(func(string) (kvsSignalingClient, error) {
			return kvsSignalingClientFunc(validKVSICECall), nil
		}),
		time.Now,
	)
	if err != nil {
		t.Fatalf("newKVSICECredentialSource() error = %v", err)
	}

	invalidARNs := []string{
		"",
		" " + testKVSChannelARN,
		strings.Replace(testKVSChannelARN, "arn:aws:", "arn:aws-cn:", 1),
		strings.Replace(testKVSChannelARN, ":kinesisvideo:", ":kinesis:", 1),
		strings.Replace(testKVSChannelARN, ":ap-south-1:", ":us-east-1:", 1),
		strings.Replace(testKVSChannelARN, ":123456789012:", ":12345678901:", 1),
		strings.Replace(testKVSChannelARN, ":123456789012:", ":12345678901x:", 1),
		strings.Replace(testKVSChannelARN, ":channel/", ":stream/", 1),
		strings.Replace(testKVSChannelARN, "voice-01", "voice/01", 1),
		strings.Replace(testKVSChannelARN, "voice-01", "voice+01", 1),
		strings.Replace(testKVSChannelARN, "1720000000000", "timestamp", 1),
		testKVSChannelARN + "/extra",
		"arn:aws:kinesisvideo:ap-south-1:123456789012:channel/" + strings.Repeat("a", 257) + "/1720000000000",
	}
	for _, invalidARN := range invalidARNs {
		_, err := source.GetTURN(context.Background(), invalidARN)
		if !errors.Is(err, ErrInvalidKVSICERequest) {
			t.Fatalf("GetTURN(%q) error = %v, want %v", invalidARN, err, ErrInvalidKVSICERequest)
		}
	}
	if _, err := source.GetTURN(nil, testKVSChannelARN); !errors.Is(err, ErrInvalidKVSICERequest) { //nolint:staticcheck // nil context is the contract under test.
		t.Fatalf("GetTURN(nil context) error = %v, want %v", err, ErrInvalidKVSICERequest)
	}
	var nilSource *KVSICECredentialSource
	if _, err := nilSource.GetTURN(context.Background(), testKVSChannelARN); !errors.Is(err, ErrInvalidKVSICERequest) {
		t.Fatalf("nil source GetTURN() error = %v, want %v", err, ErrInvalidKVSICERequest)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.GetTURN(canceled, testKVSChannelARN); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetTURN(canceled) error = %v, want context canceled", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("invalid requests made %d endpoint calls, want 0", got)
	}
}

func TestKVSICECredentialSourceRejectsInvalidEndpointResponses(t *testing.T) {
	t.Parallel()

	endpointCases := []struct {
		name   string
		output func() *kinesisvideo.GetSignalingChannelEndpointOutput
	}{
		{name: "nil output", output: func() *kinesisvideo.GetSignalingChannelEndpointOutput { return nil }},
		{name: "empty list", output: func() *kinesisvideo.GetSignalingChannelEndpointOutput {
			return &kinesisvideo.GetSignalingChannelEndpointOutput{}
		}},
		{name: "multiple entries", output: func() *kinesisvideo.GetSignalingChannelEndpointOutput {
			output := validKVSEndpointOutput()
			output.ResourceEndpointList = append(output.ResourceEndpointList, output.ResourceEndpointList[0])
			return output
		}},
		{name: "wrong protocol", output: func() *kinesisvideo.GetSignalingChannelEndpointOutput {
			output := validKVSEndpointOutput()
			output.ResourceEndpointList[0].Protocol = kvstypes.ChannelProtocolWss
			return output
		}},
		{name: "nil endpoint", output: func() *kinesisvideo.GetSignalingChannelEndpointOutput {
			output := validKVSEndpointOutput()
			output.ResourceEndpointList[0].ResourceEndpoint = nil
			return output
		}},
	}
	invalidURLs := []string{
		"",
		"http://r-abc123.kinesisvideo.ap-south-1.amazonaws.com",
		"wss://r-abc123.kinesisvideo.ap-south-1.amazonaws.com",
		"HTTPS://r-abc123.kinesisvideo.ap-south-1.amazonaws.com",
		"https://R-abc123.kinesisvideo.ap-south-1.amazonaws.com",
		"https://127.0.0.1",
		"https://r-abc123.kinesisvideo.us-east-1.amazonaws.com",
		"https://r-abc123.kinesisvideo.ap-south-1.amazonaws.com.attacker.invalid",
		"https://first.second.kinesisvideo.ap-south-1.amazonaws.com",
		"https://user@r-abc123.kinesisvideo.ap-south-1.amazonaws.com",
		"https://r-abc123.kinesisvideo.ap-south-1.amazonaws.com:80",
		"https://r-abc123.kinesisvideo.ap-south-1.amazonaws.com:0443",
		"https://r-abc123.kinesisvideo.ap-south-1.amazonaws.com/",
		"https://r-abc123.kinesisvideo.ap-south-1.amazonaws.com/path",
		"https://r-abc123.kinesisvideo.ap-south-1.amazonaws.com?query=1",
		"https://r-abc123.kinesisvideo.ap-south-1.amazonaws.com?",
		"https://r-abc123.kinesisvideo.ap-south-1.amazonaws.com#fragment",
		"https://r-abc123.kinesisvideo.ap-south-1.amazonaws.com.",
		"https:r-abc123.kinesisvideo.ap-south-1.amazonaws.com",
		"https://r-%C3%A9.kinesisvideo.ap-south-1.amazonaws.com",
		"https://" + strings.Repeat("a", maxKVSDiscoveredEndpointSize) + ".kinesisvideo.ap-south-1.amazonaws.com",
	}
	for _, rawURL := range invalidURLs {
		rawURL := rawURL
		endpointCases = append(endpointCases, struct {
			name   string
			output func() *kinesisvideo.GetSignalingChannelEndpointOutput
		}{
			name: "url " + rawURL,
			output: func() *kinesisvideo.GetSignalingChannelEndpointOutput {
				return &kinesisvideo.GetSignalingChannelEndpointOutput{
					ResourceEndpointList: []kvstypes.ResourceEndpointListItem{{
						Protocol: kvstypes.ChannelProtocolHttps, ResourceEndpoint: aws.String(rawURL),
					}},
				}
			},
		})
	}

	for _, testCase := range endpointCases {
		t.Run(testCase.name, func(t *testing.T) {
			factoryCalls := 0
			control := kvsEndpointClientFunc(func(
				context.Context,
				*kinesisvideo.GetSignalingChannelEndpointInput,
				...func(*kinesisvideo.Options),
			) (*kinesisvideo.GetSignalingChannelEndpointOutput, error) {
				return testCase.output(), nil
			})
			factory := kvsSignalingFactoryFunc(func(string) (kvsSignalingClient, error) {
				factoryCalls++
				return kvsSignalingClientFunc(validKVSICECall), nil
			})
			source, err := newKVSICECredentialSource(MumbaiRegion, control, factory, time.Now)
			if err != nil {
				t.Fatalf("newKVSICECredentialSource() error = %v", err)
			}
			_, err = source.GetTURN(context.Background(), testKVSChannelARN)
			if !errors.Is(err, ErrInvalidKVSICEEndpoint) || factoryCalls != 0 {
				t.Fatalf("GetTURN() = (%v, factory calls %d), want invalid endpoint before factory", err, factoryCalls)
			}
		})
	}
}

func TestKVSICECredentialSourceSanitizesDependencyFailures(t *testing.T) {
	t.Parallel()

	canary := "provider-turn-password-secret-canary"
	tests := []struct {
		name    string
		control kvsEndpointClient
		factory kvsSignalingClientFactory
		want    error
	}{
		{
			name: "endpoint error",
			control: kvsEndpointClientFunc(func(context.Context, *kinesisvideo.GetSignalingChannelEndpointInput, ...func(*kinesisvideo.Options)) (*kinesisvideo.GetSignalingChannelEndpointOutput, error) {
				return nil, errors.New(canary)
			}),
			factory: validKVSFactory(),
			want:    ErrKVSICEEndpointDiscovery,
		},
		{
			name: "endpoint panic",
			control: kvsEndpointClientFunc(func(context.Context, *kinesisvideo.GetSignalingChannelEndpointInput, ...func(*kinesisvideo.Options)) (*kinesisvideo.GetSignalingChannelEndpointOutput, error) {
				panic(canary)
			}),
			factory: validKVSFactory(),
			want:    ErrKVSICEEndpointDiscovery,
		},
		{
			name:    "factory error",
			control: kvsEndpointClientFunc(validKVSEndpointCall),
			factory: kvsSignalingFactoryFunc(func(string) (kvsSignalingClient, error) { return nil, errors.New(canary) }),
			want:    ErrKVSICECredentialRequest,
		},
		{
			name:    "factory panic",
			control: kvsEndpointClientFunc(validKVSEndpointCall),
			factory: kvsSignalingFactoryFunc(func(string) (kvsSignalingClient, error) { panic(canary) }),
			want:    ErrKVSICECredentialRequest,
		},
		{
			name:    "factory nil client",
			control: kvsEndpointClientFunc(validKVSEndpointCall),
			factory: kvsSignalingFactoryFunc(func(string) (kvsSignalingClient, error) { return nil, nil }),
			want:    ErrKVSICECredentialRequest,
		},
		{
			name:    "credential error",
			control: kvsEndpointClientFunc(validKVSEndpointCall),
			factory: kvsSignalingFactoryFunc(func(string) (kvsSignalingClient, error) {
				return kvsSignalingClientFunc(func(context.Context, *kinesisvideosignaling.GetIceServerConfigInput, ...func(*kinesisvideosignaling.Options)) (*kinesisvideosignaling.GetIceServerConfigOutput, error) {
					return nil, errors.New(canary)
				}), nil
			}),
			want: ErrKVSICECredentialRequest,
		},
		{
			name:    "credential panic",
			control: kvsEndpointClientFunc(validKVSEndpointCall),
			factory: kvsSignalingFactoryFunc(func(string) (kvsSignalingClient, error) {
				return kvsSignalingClientFunc(func(context.Context, *kinesisvideosignaling.GetIceServerConfigInput, ...func(*kinesisvideosignaling.Options)) (*kinesisvideosignaling.GetIceServerConfigOutput, error) {
					panic(canary)
				}), nil
			}),
			want: ErrKVSICECredentialRequest,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			source, err := newKVSICECredentialSource(MumbaiRegion, testCase.control, testCase.factory, time.Now)
			if err != nil {
				t.Fatalf("newKVSICECredentialSource() error = %v", err)
			}
			_, err = source.GetTURN(context.Background(), testKVSChannelARN)
			if !errors.Is(err, testCase.want) || strings.Contains(fmt.Sprint(err), canary) {
				t.Fatalf("GetTURN() error = %q, want sanitized %v", err, testCase.want)
			}
		})
	}
}

func TestKVSICECredentialSourceRejectsMalformedCredentialResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output func() *kinesisvideosignaling.GetIceServerConfigOutput
	}{
		{name: "nil output", output: func() *kinesisvideosignaling.GetIceServerConfigOutput { return nil }},
		{name: "empty list", output: func() *kinesisvideosignaling.GetIceServerConfigOutput {
			return &kinesisvideosignaling.GetIceServerConfigOutput{}
		}},
		{name: "multiple servers", output: mutateKVSICEOutput(func(output *kinesisvideosignaling.GetIceServerConfigOutput) {
			output.IceServerList = append(output.IceServerList, output.IceServerList[0])
		})},
		{name: "nil username", output: mutateKVSICEOutput(func(output *kinesisvideosignaling.GetIceServerConfigOutput) { output.IceServerList[0].Username = nil })},
		{name: "nil password", output: mutateKVSICEOutput(func(output *kinesisvideosignaling.GetIceServerConfigOutput) { output.IceServerList[0].Password = nil })},
		{name: "nil ttl", output: mutateKVSICEOutput(func(output *kinesisvideosignaling.GetIceServerConfigOutput) { output.IceServerList[0].Ttl = nil })},
		{name: "ttl 299", output: mutateKVSICEOutput(func(output *kinesisvideosignaling.GetIceServerConfigOutput) {
			output.IceServerList[0].Ttl = aws.Int32(299)
		})},
		{name: "ttl 301", output: mutateKVSICEOutput(func(output *kinesisvideosignaling.GetIceServerConfigOutput) {
			output.IceServerList[0].Ttl = aws.Int32(301)
		})},
		{name: "empty username", output: mutateKVSICEOutput(func(output *kinesisvideosignaling.GetIceServerConfigOutput) {
			output.IceServerList[0].Username = aws.String("")
		})},
		{name: "unsafe username", output: mutateKVSICEOutput(func(output *kinesisvideosignaling.GetIceServerConfigOutput) {
			output.IceServerList[0].Username = aws.String("user+secret")
		})},
		{name: "unsafe password", output: mutateKVSICEOutput(func(output *kinesisvideosignaling.GetIceServerConfigOutput) {
			output.IceServerList[0].Password = aws.String("password/secret")
		})},
		{name: "oversized username", output: mutateKVSICEOutput(func(output *kinesisvideosignaling.GetIceServerConfigOutput) {
			output.IceServerList[0].Username = aws.String(strings.Repeat("u", 257))
		})},
		{name: "empty uris", output: mutateKVSICEOutput(func(output *kinesisvideosignaling.GetIceServerConfigOutput) { output.IceServerList[0].Uris = nil })},
		{name: "too many uris", output: mutateKVSICEOutput(func(output *kinesisvideosignaling.GetIceServerConfigOutput) {
			uris := make([]string, maxKVSTURNURIs+1)
			for index := range uris {
				uris[index] = fmt.Sprintf("turn:34-219-91-62.t-%d.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp", index)
			}
			output.IceServerList[0].Uris = uris
		})},
		{name: "duplicate uri", output: mutateKVSICEOutput(func(output *kinesisvideosignaling.GetIceServerConfigOutput) {
			output.IceServerList[0].Uris = append(output.IceServerList[0].Uris, output.IceServerList[0].Uris[0])
		})},
		{name: "tcp only", output: iceOutputWithURI("turn:34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443?transport=tcp")},
		{name: "turns udp", output: iceOutputWithURI("turns:34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp")},
		{name: "stun", output: iceOutputWithURI("stun:stun.kinesisvideo.ap-south-1.amazonaws.com:443")},
		{name: "one host label", output: iceOutputWithURI("turn:r-abc123.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp")},
		{name: "three host labels", output: iceOutputWithURI("turn:first.second.third.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp")},
		{name: "foreign region", output: iceOutputWithURI("turn:34-219-91-62.t-1cd92f6b.kinesisvideo.us-east-1.amazonaws.com:443?transport=udp")},
		{name: "suffix injection", output: iceOutputWithURI("turn:34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com.attacker.invalid:443?transport=udp")},
		{name: "ip host", output: iceOutputWithURI("turn:127.0.0.1:443?transport=udp")},
		{name: "wrong port", output: iceOutputWithURI("turn:34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:3478?transport=udp")},
		{name: "userinfo", output: iceOutputWithURI("turn:user@34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp")},
		{name: "path", output: iceOutputWithURI("turn:34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443/path?transport=udp")},
		{name: "escaped host", output: iceOutputWithURI("turn:34-219-91-62.t-%31cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp")},
		{name: "missing query", output: iceOutputWithURI("turn:34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443")},
		{name: "extra query", output: iceOutputWithURI("turn:34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp&x=1")},
		{name: "uppercase transport", output: iceOutputWithURI("turn:34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443?transport=UDP")},
		{name: "turn URL syntax", output: iceOutputWithURI("turn://34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp")},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			ice := kvsSignalingClientFunc(func(
				context.Context,
				*kinesisvideosignaling.GetIceServerConfigInput,
				...func(*kinesisvideosignaling.Options),
			) (*kinesisvideosignaling.GetIceServerConfigOutput, error) {
				return testCase.output(), nil
			})
			source, err := newKVSICECredentialSource(
				MumbaiRegion,
				kvsEndpointClientFunc(validKVSEndpointCall),
				kvsSignalingFactoryFunc(func(string) (kvsSignalingClient, error) { return ice, nil }),
				time.Now,
			)
			if err != nil {
				t.Fatalf("newKVSICECredentialSource() error = %v", err)
			}
			credentials, err := source.GetTURN(context.Background(), testKVSChannelARN)
			if !errors.Is(err, ErrInvalidKVSICEResponse) || len(credentials.uris) != 0 || credentials.username != "" || credentials.password != "" {
				t.Fatalf("GetTURN() = (%#v, %v), want empty credentials and %v", credentials, err, ErrInvalidKVSICEResponse)
			}
		})
	}
}

func TestKVSICECredentialSourceIsRedacted(t *testing.T) {
	t.Parallel()

	source, _, ice, factory := newValidKVSICECredentialSourceForTest(t)
	ice.output.IceServerList[0].Username = aws.String("username-secret-canary")
	ice.output.IceServerList[0].Password = aws.String("password-secret-canary")
	factory.endpoint = "https://endpoint-secret-canary.invalid"
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("json.Marshal(source) error = %v", err)
	}
	for _, rendered := range []string{fmt.Sprint(source), fmt.Sprintf("%#v", source), string(encoded)} {
		for _, canary := range []string{"username-secret-canary", "password-secret-canary", "endpoint-secret-canary", testKVSChannelARN} {
			if strings.Contains(rendered, canary) {
				t.Fatalf("source rendering %q exposed %q", rendered, canary)
			}
		}
	}
	if got := string(encoded); got != "{}" {
		t.Fatalf("json.Marshal(source) = %s, want {}", got)
	}
}

func TestKVSICECredentialSourceConcurrentCallsRediscoverCredentials(t *testing.T) {
	t.Parallel()

	const callCount = 32
	var endpointCalls atomic.Int64
	var factoryCalls atomic.Int64
	var iceCalls atomic.Int64
	control := kvsEndpointClientFunc(func(
		context.Context,
		*kinesisvideo.GetSignalingChannelEndpointInput,
		...func(*kinesisvideo.Options),
	) (*kinesisvideo.GetSignalingChannelEndpointOutput, error) {
		endpointCalls.Add(1)
		return validKVSEndpointOutput(), nil
	})
	ice := kvsSignalingClientFunc(func(
		context.Context,
		*kinesisvideosignaling.GetIceServerConfigInput,
		...func(*kinesisvideosignaling.Options),
	) (*kinesisvideosignaling.GetIceServerConfigOutput, error) {
		iceCalls.Add(1)
		return validKVSICEOutput(), nil
	})
	factory := kvsSignalingFactoryFunc(func(string) (kvsSignalingClient, error) {
		factoryCalls.Add(1)
		return ice, nil
	})
	source, err := newKVSICECredentialSource(MumbaiRegion, control, factory, func() time.Time {
		return time.Date(2026, time.August, 7, 3, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("newKVSICECredentialSource() error = %v", err)
	}

	errorsSeen := make(chan error, callCount)
	var wait sync.WaitGroup
	for index := 0; index < callCount; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			credentials, callErr := source.GetTURN(context.Background(), testKVSChannelARN)
			if callErr != nil || len(credentials.uris) != 1 {
				errorsSeen <- fmt.Errorf("GetTURN() = (%#v, %w)", credentials, callErr)
			}
		}()
	}
	wait.Wait()
	close(errorsSeen)
	for callErr := range errorsSeen {
		t.Error(callErr)
	}
	if endpointCalls.Load() != callCount || factoryCalls.Load() != callCount || iceCalls.Load() != callCount {
		t.Fatalf("calls = endpoint:%d factory:%d ice:%d, want %d each", endpointCalls.Load(), factoryCalls.Load(), iceCalls.Load(), callCount)
	}
}

func validKVSFactory() kvsSignalingClientFactory {
	return kvsSignalingFactoryFunc(func(string) (kvsSignalingClient, error) {
		return kvsSignalingClientFunc(validKVSICECall), nil
	})
}

func mutateKVSICEOutput(mutate func(*kinesisvideosignaling.GetIceServerConfigOutput)) func() *kinesisvideosignaling.GetIceServerConfigOutput {
	return func() *kinesisvideosignaling.GetIceServerConfigOutput {
		output := validKVSICEOutput()
		mutate(output)
		return output
	}
}

func iceOutputWithURI(uri string) func() *kinesisvideosignaling.GetIceServerConfigOutput {
	return mutateKVSICEOutput(func(output *kinesisvideosignaling.GetIceServerConfigOutput) {
		output.IceServerList[0].Uris = []string{uri}
	})
}
