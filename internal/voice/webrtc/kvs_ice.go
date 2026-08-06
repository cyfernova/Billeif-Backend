package webrtc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesisvideo"
	kvstypes "github.com/aws/aws-sdk-go-v2/service/kinesisvideo/types"
	"github.com/aws/aws-sdk-go-v2/service/kinesisvideosignaling"
	signalingtypes "github.com/aws/aws-sdk-go-v2/service/kinesisvideosignaling/types"
)

const (
	maxKVSDiscoveredEndpointSize = 2 << 10
	kvsCredentialTTLSeconds      = int32(300)
	maxKVSTURNURIs               = 16
)

var (
	ErrInvalidKVSICEConfig     = errors.New("invalid KVS ICE credential source configuration")
	ErrInvalidKVSICERequest    = errors.New("invalid KVS ICE credential request")
	ErrKVSICEEndpointDiscovery = errors.New("KVS ICE endpoint discovery failed")
	ErrInvalidKVSICEEndpoint   = errors.New("invalid KVS ICE endpoint response")
	ErrKVSICECredentialRequest = errors.New("KVS ICE credential request failed")
	ErrInvalidKVSICEResponse   = errors.New("invalid KVS ICE credential response")
)

type kvsEndpointClient interface {
	GetSignalingChannelEndpoint(
		context.Context,
		*kinesisvideo.GetSignalingChannelEndpointInput,
		...func(*kinesisvideo.Options),
	) (*kinesisvideo.GetSignalingChannelEndpointOutput, error)
}

type kvsSignalingClient interface {
	GetIceServerConfig(
		context.Context,
		*kinesisvideosignaling.GetIceServerConfigInput,
		...func(*kinesisvideosignaling.Options),
	) (*kinesisvideosignaling.GetIceServerConfigOutput, error)
}

type kvsSignalingClientFactory interface {
	New(string) (kvsSignalingClient, error)
}

// KVSICECredentialSource obtains short-lived managed TURN credentials through
// the Kinesis Video Streams endpoint-discovery flow. It is safe for concurrent
// use and intentionally performs discovery on every request.
type KVSICECredentialSource struct {
	region           string
	endpointClient   kvsEndpointClient
	signalingFactory kvsSignalingClientFactory
	now              func() time.Time
}

var _ ICECredentialSource = (*KVSICECredentialSource)(nil)

// NewKVSICECredentialSource constructs the production AWS SDK adapter. Endpoint
// overrides and SDK request/response logging are deliberately not inherited:
// the control plane is pinned to the regional AWS host and the signaling client
// is pinned to the exact validated endpoint returned by endpoint discovery.
func NewKVSICECredentialSource(config aws.Config) (*KVSICECredentialSource, error) {
	if config.Region != MumbaiRegion || isNilInterface(config.Credentials) {
		return nil, ErrInvalidKVSICEConfig
	}

	config = restrictedKVSConfig(config)
	controlEndpoint := "https://" + kvsControlPlaneHost(config.Region)
	control := kinesisvideo.NewFromConfig(config, func(options *kinesisvideo.Options) {
		options.BaseEndpoint = aws.String(controlEndpoint)
		options.EndpointResolverV2 = kinesisvideo.NewDefaultEndpointResolverV2()
		options.ClientLogMode = 0
	})
	factory := &awsKVSSignalingClientFactory{config: config}
	return newKVSICECredentialSource(config.Region, control, factory, time.Now)
}

func newKVSICECredentialSource(
	region string,
	endpointClient kvsEndpointClient,
	signalingFactory kvsSignalingClientFactory,
	now func() time.Time,
) (*KVSICECredentialSource, error) {
	if region != MumbaiRegion || isNilInterface(endpointClient) || isNilInterface(signalingFactory) || now == nil {
		return nil, ErrInvalidKVSICEConfig
	}
	return &KVSICECredentialSource{
		region:           region,
		endpointClient:   endpointClient,
		signalingFactory: signalingFactory,
		now:              now,
	}, nil
}

// restrictedKVSConfig retains only transport, signing, retry, and regional
// settings. In particular, it drops endpoint resolvers, middleware, service
// options, and wire logging that could redirect or expose the ICE response.
func restrictedKVSConfig(config aws.Config) aws.Config {
	return aws.Config{
		Region:             config.Region,
		Credentials:        config.Credentials,
		HTTPClient:         config.HTTPClient,
		RetryMaxAttempts:   config.RetryMaxAttempts,
		RetryMode:          config.RetryMode,
		Retryer:            config.Retryer,
		DefaultsMode:       config.DefaultsMode,
		RuntimeEnvironment: config.RuntimeEnvironment,
	}
}

type awsKVSSignalingClientFactory struct {
	config aws.Config
}

func (factory *awsKVSSignalingClientFactory) New(endpoint string) (kvsSignalingClient, error) {
	if factory == nil || factory.config.Region != MumbaiRegion || !validKVSHTTPSEndpoint(factory.config.Region, endpoint) {
		return nil, ErrInvalidKVSICEEndpoint
	}
	client := kinesisvideosignaling.NewFromConfig(factory.config, func(options *kinesisvideosignaling.Options) {
		options.BaseEndpoint = aws.String(strings.Clone(endpoint))
		options.EndpointResolverV2 = kinesisvideosignaling.NewDefaultEndpointResolverV2()
		options.ClientLogMode = 0
	})
	return client, nil
}

func (source *KVSICECredentialSource) GetTURN(ctx context.Context, channelARN string) (TURNCredentials, error) {
	if ctx == nil || source == nil || source.region != MumbaiRegion || isNilInterface(source.endpointClient) ||
		isNilInterface(source.signalingFactory) || source.now == nil || !validKVSChannelARN(source.region, channelARN) {
		return TURNCredentials{}, ErrInvalidKVSICERequest
	}
	if err := ctx.Err(); err != nil {
		return TURNCredentials{}, err
	}

	channelARN = strings.Clone(channelARN)
	endpointOutput, err := safelyDiscoverKVSEndpoint(ctx, source.endpointClient, &kinesisvideo.GetSignalingChannelEndpointInput{
		ChannelARN: aws.String(channelARN),
		SingleMasterChannelEndpointConfiguration: &kvstypes.SingleMasterChannelEndpointConfiguration{
			Protocols: []kvstypes.ChannelProtocol{kvstypes.ChannelProtocolHttps},
			Role:      kvstypes.ChannelRoleMaster,
		},
	})
	if err != nil {
		return TURNCredentials{}, contextualKVSICEError(ctx, ErrKVSICEEndpointDiscovery)
	}
	if err := ctx.Err(); err != nil {
		return TURNCredentials{}, err
	}
	endpoint, ok := validatedKVSHTTPSEndpoint(source.region, endpointOutput)
	if !ok {
		return TURNCredentials{}, ErrInvalidKVSICEEndpoint
	}

	signalingClient, err := safelyCreateKVSSignalingClient(source.signalingFactory, endpoint)
	if err != nil || isNilInterface(signalingClient) {
		return TURNCredentials{}, contextualKVSICEError(ctx, ErrKVSICECredentialRequest)
	}
	if err := ctx.Err(); err != nil {
		return TURNCredentials{}, err
	}
	iceOutput, err := safelyGetKVSTURN(ctx, signalingClient, &kinesisvideosignaling.GetIceServerConfigInput{
		ChannelARN: aws.String(channelARN),
		Service:    signalingtypes.ServiceTurn,
	})
	if err != nil {
		return TURNCredentials{}, contextualKVSICEError(ctx, ErrKVSICECredentialRequest)
	}
	if err := ctx.Err(); err != nil {
		return TURNCredentials{}, err
	}
	now, ok := safelyKVSNow(source.now)
	if !ok {
		return TURNCredentials{}, ErrInvalidKVSICEResponse
	}
	credentials, ok := validatedKVSTURNCredentials(source.region, now, iceOutput)
	if !ok {
		return TURNCredentials{}, ErrInvalidKVSICEResponse
	}
	return credentials, nil
}

func safelyKVSNow(now func() time.Time) (current time.Time, ok bool) {
	defer func() {
		if recover() != nil {
			current = time.Time{}
			ok = false
		}
	}()
	return now().UTC(), true
}

func safelyDiscoverKVSEndpoint(
	ctx context.Context,
	client kvsEndpointClient,
	input *kinesisvideo.GetSignalingChannelEndpointInput,
) (output *kinesisvideo.GetSignalingChannelEndpointOutput, err error) {
	defer func() {
		if recover() != nil {
			output = nil
			err = ErrKVSICEEndpointDiscovery
		}
	}()
	return client.GetSignalingChannelEndpoint(ctx, input)
}

func safelyCreateKVSSignalingClient(factory kvsSignalingClientFactory, endpoint string) (client kvsSignalingClient, err error) {
	defer func() {
		if recover() != nil {
			client = nil
			err = ErrKVSICECredentialRequest
		}
	}()
	return factory.New(endpoint)
}

func safelyGetKVSTURN(
	ctx context.Context,
	client kvsSignalingClient,
	input *kinesisvideosignaling.GetIceServerConfigInput,
) (output *kinesisvideosignaling.GetIceServerConfigOutput, err error) {
	defer func() {
		if recover() != nil {
			output = nil
			err = ErrKVSICECredentialRequest
		}
	}()
	return client.GetIceServerConfig(ctx, input)
}

func contextualKVSICEError(ctx context.Context, fallback error) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return fallback
}

func validatedKVSHTTPSEndpoint(region string, output *kinesisvideo.GetSignalingChannelEndpointOutput) (string, bool) {
	if output == nil || len(output.ResourceEndpointList) != 1 {
		return "", false
	}
	resource := output.ResourceEndpointList[0]
	if resource.Protocol != kvstypes.ChannelProtocolHttps || resource.ResourceEndpoint == nil {
		return "", false
	}
	endpoint := strings.Clone(aws.ToString(resource.ResourceEndpoint))
	if !validKVSHTTPSEndpoint(region, endpoint) {
		return "", false
	}
	return endpoint, true
}

func validKVSHTTPSEndpoint(region, endpoint string) bool {
	if region != MumbaiRegion || endpoint == "" || len(endpoint) > maxKVSDiscoveredEndpointSize || !isASCII(endpoint) ||
		!strings.HasPrefix(endpoint, "https://") {
		return false
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" ||
		parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return false
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port != "" && port != "443" {
		return false
	}
	wantAuthority := host
	if port == "443" {
		wantAuthority += ":443"
	}
	return parsed.Host == wantAuthority && net.ParseIP(host) == nil && isStrictDNSName(host) && isRegionBoundKVSHost(region, host)
}

func validatedKVSTURNCredentials(
	region string,
	now time.Time,
	output *kinesisvideosignaling.GetIceServerConfigOutput,
) (TURNCredentials, bool) {
	if region != MumbaiRegion || now.IsZero() || output == nil || len(output.IceServerList) != 1 {
		return TURNCredentials{}, false
	}
	server := output.IceServerList[0]
	if server.Username == nil || server.Password == nil || server.Ttl == nil || *server.Ttl != kvsCredentialTTLSeconds ||
		len(server.Uris) == 0 || len(server.Uris) > maxKVSTURNURIs {
		return TURNCredentials{}, false
	}
	username := strings.Clone(aws.ToString(server.Username))
	password := strings.Clone(aws.ToString(server.Password))
	if len(username) > maxSignalingICEFieldBytes || len(password) > maxSignalingICEFieldBytes ||
		!validICECredentialToken(username) || !validICECredentialToken(password) {
		return TURNCredentials{}, false
	}

	selected := make([]string, 0, len(server.Uris))
	seen := make(map[string]struct{}, len(server.Uris))
	for _, rawURI := range server.Uris {
		if len(rawURI) == 0 || len(rawURI) > maxSignalingICEFieldBytes {
			return TURNCredentials{}, false
		}
		if _, duplicate := seen[rawURI]; duplicate {
			return TURNCredentials{}, false
		}
		seen[rawURI] = struct{}{}
		transport, valid := validKVSTURNURI(region, rawURI)
		if !valid {
			return TURNCredentials{}, false
		}
		if transport == "udp" && strings.HasPrefix(rawURI, "turn:") {
			selected = append(selected, strings.Clone(rawURI))
		}
	}
	if len(selected) == 0 {
		return TURNCredentials{}, false
	}

	return TURNCredentials{
		uris:      selected,
		username:  username,
		password:  password,
		ExpiresAt: now.Add(time.Duration(*server.Ttl) * time.Second),
	}, true
}

func validKVSTURNURI(region, rawURI string) (string, bool) {
	if region != MumbaiRegion || rawURI == "" || !isASCII(rawURI) || strings.Contains(rawURI, "://") {
		return "", false
	}
	scheme := ""
	body := ""
	switch {
	case strings.HasPrefix(rawURI, "turn:"):
		scheme, body = "turn", strings.TrimPrefix(rawURI, "turn:")
	case strings.HasPrefix(rawURI, "turns:"):
		scheme, body = "turns", strings.TrimPrefix(rawURI, "turns:")
	default:
		return "", false
	}
	if strings.Count(body, "?") != 1 {
		return "", false
	}
	address, query, _ := strings.Cut(body, "?")
	if strings.ContainsAny(address, "/@#%") {
		return "", false
	}
	transport := ""
	switch query {
	case "transport=udp":
		transport = "udp"
	case "transport=tcp":
		transport = "tcp"
	default:
		return "", false
	}
	if scheme == "turns" && transport != "tcp" {
		return "", false
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "443" || net.ParseIP(host) != nil || !isStrictDNSName(host) || !isRegionBoundKVSTURNHost(region, host) {
		return "", false
	}
	return transport, true
}

func (source *KVSICECredentialSource) String() string {
	if source == nil {
		return "kvs_ice_credential_source{nil}"
	}
	return "kvs_ice_credential_source{redacted}"
}

func (source *KVSICECredentialSource) GoString() string {
	return source.String()
}

func (*KVSICECredentialSource) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct{}{})
}
