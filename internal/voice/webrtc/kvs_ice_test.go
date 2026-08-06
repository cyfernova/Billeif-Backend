package webrtc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesisvideo"
	kvstypes "github.com/aws/aws-sdk-go-v2/service/kinesisvideo/types"
	"github.com/aws/aws-sdk-go-v2/service/kinesisvideosignaling"
	signalingtypes "github.com/aws/aws-sdk-go-v2/service/kinesisvideosignaling/types"
)

const testKVSChannelARN = "arn:aws:kinesisvideo:ap-south-1:123456789012:channel/voice-01/1720000000000"

func TestKVSICECredentialSourceDiscoversHTTPSMasterThenFetchesAndFiltersTURN(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 7, 3, 0, 0, 0, time.UTC)
	endpoint := "https://r-abc123.kinesisvideo.ap-south-1.amazonaws.com:443"
	order := make([]string, 0, 3)
	endpointOutput := &kinesisvideo.GetSignalingChannelEndpointOutput{
		ResourceEndpointList: []kvstypes.ResourceEndpointListItem{{
			Protocol: kvstypes.ChannelProtocolHttps, ResourceEndpoint: aws.String(endpoint),
		}},
	}
	control := &fakeKVSEndpointClient{output: endpointOutput, order: &order}
	iceOutput := &kinesisvideosignaling.GetIceServerConfigOutput{
		IceServerList: []signalingtypes.IceServer{{
			Username: aws.String("turn-user-01"),
			Password: aws.String("turn-password-01"),
			Ttl:      aws.Int32(300),
			Uris: []string{
				"turn:34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443?transport=tcp",
				"turn:34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp",
				"turns:34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443?transport=tcp",
			},
		}},
	}
	iceClient := &fakeKVSSignalingClient{output: iceOutput, order: &order}
	factory := &fakeKVSSignalingFactory{client: iceClient, order: &order}

	source, err := newKVSICECredentialSource(MumbaiRegion, control, factory, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newKVSICECredentialSource() error = %v", err)
	}
	credentials, err := source.GetTURN(context.Background(), testKVSChannelARN)
	if err != nil {
		t.Fatalf("GetTURN() error = %v", err)
	}

	if len(credentials.uris) != 1 || credentials.uris[0] != "turn:34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp" {
		t.Fatalf("GetTURN().uris = %#v, want only same-region UDP TURN :443", credentials.uris)
	}
	if credentials.username != "turn-user-01" || credentials.password != "turn-password-01" {
		t.Fatalf("GetTURN() returned unexpected credential values")
	}
	if want := now.Add(5 * time.Minute); !credentials.ExpiresAt.Equal(want) {
		t.Fatalf("GetTURN().ExpiresAt = %v, want %v", credentials.ExpiresAt, want)
	}
	if got, want := order, []string{"endpoint", "factory", "ice"}; !equalStrings(got, want) {
		t.Fatalf("AWS operation order = %#v, want %#v", got, want)
	}
	assertExactKVSRequests(t, control.input, iceClient.input)
	if factory.endpoint != endpoint {
		t.Fatalf("signaling factory endpoint = %q, want exact discovered endpoint %q", factory.endpoint, endpoint)
	}

	// Returned credentials must not alias SDK response memory.
	endpointOutput.ResourceEndpointList[0].ResourceEndpoint = aws.String("https://attacker.invalid")
	iceOutput.IceServerList[0].Uris[1] = "turn:attacker.invalid:443?transport=udp"
	iceOutput.IceServerList[0].Username = aws.String("mutated-user")
	iceOutput.IceServerList[0].Password = aws.String("mutated-password")
	if credentials.uris[0] != "turn:34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp" ||
		credentials.username != "turn-user-01" || credentials.password != "turn-password-01" {
		t.Fatal("GetTURN() result aliases mutable SDK response memory")
	}
}

func TestKVSICECredentialSourceContainsPanickingClock(t *testing.T) {
	t.Parallel()

	source, _, _, _ := newValidKVSICECredentialSourceForTest(t)
	source.now = func() time.Time { panic("clock-secret-canary") }
	_, err := source.GetTURN(context.Background(), testKVSChannelARN)
	if !errors.Is(err, ErrInvalidKVSICEResponse) {
		t.Fatalf("GetTURN() error = %v, want %v", err, ErrInvalidKVSICEResponse)
	}
}

func TestKVSICECredentialSourceStopsAfterDependencyCancellation(t *testing.T) {
	t.Parallel()

	t.Run("endpoint discovery", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		factoryCalls := 0
		control := kvsEndpointClientFunc(func(
			context.Context,
			*kinesisvideo.GetSignalingChannelEndpointInput,
			...func(*kinesisvideo.Options),
		) (*kinesisvideo.GetSignalingChannelEndpointOutput, error) {
			cancel()
			return validKVSEndpointOutput(), nil
		})
		factory := kvsSignalingFactoryFunc(func(string) (kvsSignalingClient, error) {
			factoryCalls++
			return kvsSignalingClientFunc(validKVSICECall), nil
		})
		source, err := newKVSICECredentialSource(MumbaiRegion, control, factory, time.Now)
		if err != nil {
			t.Fatalf("newKVSICECredentialSource() error = %v", err)
		}
		_, err = source.GetTURN(ctx, testKVSChannelARN)
		if !errors.Is(err, context.Canceled) || factoryCalls != 0 {
			t.Fatalf("GetTURN() = (%v, factory calls %d), want context canceled before factory", err, factoryCalls)
		}
	})

	t.Run("credential request", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		ice := kvsSignalingClientFunc(func(
			context.Context,
			*kinesisvideosignaling.GetIceServerConfigInput,
			...func(*kinesisvideosignaling.Options),
		) (*kinesisvideosignaling.GetIceServerConfigOutput, error) {
			cancel()
			return validKVSICEOutput(), nil
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
		_, err = source.GetTURN(ctx, testKVSChannelARN)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("GetTURN() error = %v, want context canceled", err)
		}
	})
}

func validKVSEndpointOutput() *kinesisvideo.GetSignalingChannelEndpointOutput {
	return &kinesisvideo.GetSignalingChannelEndpointOutput{
		ResourceEndpointList: []kvstypes.ResourceEndpointListItem{{
			Protocol:         kvstypes.ChannelProtocolHttps,
			ResourceEndpoint: aws.String("https://r-abc123.kinesisvideo.ap-south-1.amazonaws.com"),
		}},
	}
}

func validKVSICEOutput() *kinesisvideosignaling.GetIceServerConfigOutput {
	return &kinesisvideosignaling.GetIceServerConfigOutput{
		IceServerList: []signalingtypes.IceServer{{
			Username: aws.String("turn-user-01"),
			Password: aws.String("turn-password-01"),
			Ttl:      aws.Int32(300),
			Uris: []string{
				"turn:34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp",
			},
		}},
	}
}

func validKVSEndpointCall(
	context.Context,
	*kinesisvideo.GetSignalingChannelEndpointInput,
	...func(*kinesisvideo.Options),
) (*kinesisvideo.GetSignalingChannelEndpointOutput, error) {
	return validKVSEndpointOutput(), nil
}

func validKVSICECall(
	context.Context,
	*kinesisvideosignaling.GetIceServerConfigInput,
	...func(*kinesisvideosignaling.Options),
) (*kinesisvideosignaling.GetIceServerConfigOutput, error) {
	return validKVSICEOutput(), nil
}

func newValidKVSICECredentialSourceForTest(t *testing.T) (
	*KVSICECredentialSource,
	*fakeKVSEndpointClient,
	*fakeKVSSignalingClient,
	*fakeKVSSignalingFactory,
) {
	t.Helper()
	order := make([]string, 0, 3)
	control := &fakeKVSEndpointClient{
		output: &kinesisvideo.GetSignalingChannelEndpointOutput{
			ResourceEndpointList: []kvstypes.ResourceEndpointListItem{{
				Protocol:         kvstypes.ChannelProtocolHttps,
				ResourceEndpoint: aws.String("https://r-abc123.kinesisvideo.ap-south-1.amazonaws.com"),
			}},
		},
		order: &order,
	}
	ice := &fakeKVSSignalingClient{
		output: &kinesisvideosignaling.GetIceServerConfigOutput{
			IceServerList: []signalingtypes.IceServer{{
				Username: aws.String("turn-user-01"),
				Password: aws.String("turn-password-01"),
				Ttl:      aws.Int32(300),
				Uris: []string{
					"turn:34-219-91-62.t-1cd92f6b.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp",
				},
			}},
		},
		order: &order,
	}
	factory := &fakeKVSSignalingFactory{client: ice, order: &order}
	source, err := newKVSICECredentialSource(
		MumbaiRegion,
		control,
		factory,
		func() time.Time { return time.Date(2026, time.August, 7, 3, 0, 0, 0, time.UTC) },
	)
	if err != nil {
		t.Fatalf("newKVSICECredentialSource() error = %v", err)
	}
	return source, control, ice, factory
}

func assertExactKVSRequests(t *testing.T, endpoint *kinesisvideo.GetSignalingChannelEndpointInput, ice *kinesisvideosignaling.GetIceServerConfigInput) {
	t.Helper()
	if endpoint == nil || aws.ToString(endpoint.ChannelARN) != testKVSChannelARN || endpoint.SingleMasterChannelEndpointConfiguration == nil ||
		len(endpoint.SingleMasterChannelEndpointConfiguration.Protocols) != 1 ||
		endpoint.SingleMasterChannelEndpointConfiguration.Protocols[0] != kvstypes.ChannelProtocolHttps ||
		endpoint.SingleMasterChannelEndpointConfiguration.Role != kvstypes.ChannelRoleMaster {
		t.Fatalf("GetSignalingChannelEndpoint input = %#v, want exact HTTPS MASTER request", endpoint)
	}
	if ice == nil || aws.ToString(ice.ChannelARN) != testKVSChannelARN || ice.Service != signalingtypes.ServiceTurn ||
		ice.ClientId != nil || ice.Username != nil {
		t.Fatalf("GetIceServerConfig input = %#v, want exact channel TURN request", ice)
	}
}

type fakeKVSEndpointClient struct {
	output *kinesisvideo.GetSignalingChannelEndpointOutput
	err    error
	input  *kinesisvideo.GetSignalingChannelEndpointInput
	order  *[]string
}

func (client *fakeKVSEndpointClient) GetSignalingChannelEndpoint(
	_ context.Context,
	input *kinesisvideo.GetSignalingChannelEndpointInput,
	_ ...func(*kinesisvideo.Options),
) (*kinesisvideo.GetSignalingChannelEndpointOutput, error) {
	*client.order = append(*client.order, "endpoint")
	client.input = input
	return client.output, client.err
}

type fakeKVSSignalingClient struct {
	output *kinesisvideosignaling.GetIceServerConfigOutput
	err    error
	input  *kinesisvideosignaling.GetIceServerConfigInput
	order  *[]string
}

func (client *fakeKVSSignalingClient) GetIceServerConfig(
	_ context.Context,
	input *kinesisvideosignaling.GetIceServerConfigInput,
	_ ...func(*kinesisvideosignaling.Options),
) (*kinesisvideosignaling.GetIceServerConfigOutput, error) {
	*client.order = append(*client.order, "ice")
	client.input = input
	return client.output, client.err
}

type fakeKVSSignalingFactory struct {
	client   kvsSignalingClient
	err      error
	endpoint string
	order    *[]string
}

type kvsEndpointClientFunc func(
	context.Context,
	*kinesisvideo.GetSignalingChannelEndpointInput,
	...func(*kinesisvideo.Options),
) (*kinesisvideo.GetSignalingChannelEndpointOutput, error)

func (function kvsEndpointClientFunc) GetSignalingChannelEndpoint(
	ctx context.Context,
	input *kinesisvideo.GetSignalingChannelEndpointInput,
	options ...func(*kinesisvideo.Options),
) (*kinesisvideo.GetSignalingChannelEndpointOutput, error) {
	return function(ctx, input, options...)
}

type kvsSignalingClientFunc func(
	context.Context,
	*kinesisvideosignaling.GetIceServerConfigInput,
	...func(*kinesisvideosignaling.Options),
) (*kinesisvideosignaling.GetIceServerConfigOutput, error)

func (function kvsSignalingClientFunc) GetIceServerConfig(
	ctx context.Context,
	input *kinesisvideosignaling.GetIceServerConfigInput,
	options ...func(*kinesisvideosignaling.Options),
) (*kinesisvideosignaling.GetIceServerConfigOutput, error) {
	return function(ctx, input, options...)
}

type kvsSignalingFactoryFunc func(string) (kvsSignalingClient, error)

func (function kvsSignalingFactoryFunc) New(endpoint string) (kvsSignalingClient, error) {
	return function(endpoint)
}

func (factory *fakeKVSSignalingFactory) New(endpoint string) (kvsSignalingClient, error) {
	*factory.order = append(*factory.order, "factory")
	factory.endpoint = endpoint
	return factory.client, factory.err
}

func equalStrings(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}
