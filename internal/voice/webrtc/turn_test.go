package webrtc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var fixedProbeTime = time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)

func validProbeConfig() Config {
	return Config{
		RuntimeAcknowledgement:   RequiredLiveProbeAcknowledgement,
		EvidenceMode:             EvidenceSynthetic,
		CampaignID:               "voice-turn-campaign-01",
		SourceRevision:           strings.Repeat("a", 40),
		ImageDigest:              "sha256:" + strings.Repeat("b", 64),
		ExpectedObserverIdentity: "observer-primary",
		ExpectedObserverKeyID:    "observer-key-01",
		ChangeWindowStart:        fixedProbeTime.Add(-time.Minute),
		ChangeWindowEnd:          fixedProbeTime.Add(45 * time.Minute),
		Region:                   MumbaiRegion,
		ChannelARN:               "arn:aws:kinesisvideo:ap-south-1:123456789012:channel/voice-01/1234567890",
		Topology: TopologyExpectation{
			Paths: []PrivatePath{
				{SubnetID: "subnet-private-a", RouteTableID: "rtb-private-a", AvailabilityZoneID: "aps1-az1"},
				{SubnetID: "subnet-private-b", RouteTableID: "rtb-private-b", AvailabilityZoneID: "aps1-az2"},
			},
			NATENIID:        "eni-nat-voice",
			RuntimeSubnetID: "subnet-private-a",
		},
		ExactTLSHosts:          []string{"kinesisvideo.ap-south-1.amazonaws.com"},
		BilleifBackendHost:     "backend.billeif.example",
		OverallTimeout:         40 * time.Minute,
		TopologyTimeout:        100 * time.Millisecond,
		EndpointTimeout:        100 * time.Millisecond,
		CredentialTimeout:      100 * time.Millisecond,
		RelayTimeout:           100 * time.Millisecond,
		MetricsTimeout:         100 * time.Millisecond,
		NetworkTimeout:         100 * time.Millisecond,
		EvidenceTimeout:        36 * time.Minute,
		EvidenceMaxAge:         2 * time.Hour,
		CredentialExpiryMargin: 5 * time.Minute,
	}
}

func validTopologyObservation() TopologyObservation {
	return TopologyObservation{
		Paths: []ObservedPrivatePath{
			{PrivatePath: PrivatePath{SubnetID: "subnet-private-a", RouteTableID: "rtb-private-a", AvailabilityZoneID: "aps1-az1"}, DefaultRouteNATENIID: "eni-nat-voice"},
			{PrivatePath: PrivatePath{SubnetID: "subnet-private-b", RouteTableID: "rtb-private-b", AvailabilityZoneID: "aps1-az2"}, DefaultRouteNATENIID: "eni-nat-voice"},
		},
		NAT:             NATObservation{ENIID: "eni-nat-voice", SourceDestCheck: false, SourceDestCheckObserved: true},
		RuntimeSubnetID: "subnet-private-a",
		ArtifactSHA256:  hashString("topology-artifact-private-a"),
	}
}

func validEndpoints() []TLSEndpoint {
	return []TLSEndpoint{
		{Protocol: EndpointHTTPS, URL: "https://kinesisvideo.ap-south-1.amazonaws.com:443"},
		{Protocol: EndpointWSS, URL: "wss://v-abc123.kinesisvideo.ap-south-1.amazonaws.com:443"},
	}
}

func validCredentials() TURNCredentials {
	return TURNCredentials{
		URIs:      []string{"turn:v-abc123.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp"},
		Username:  "turn-user-sensitive",
		Password:  "turn-password-sensitive",
		ExpiresAt: fixedProbeTime.Add(30 * time.Minute),
	}
}

func TestProbeFailsClosedBeforeDependencies(t *testing.T) {
	t.Parallel()

	deps := poisonDependencies()
	probe := NewProbe(deps)
	config := validProbeConfig()

	if liveProbeBuildEnabled {
		config.RuntimeAcknowledgement = "almost-acknowledged"
	} else {
		config.RuntimeAcknowledgement = RequiredLiveProbeAcknowledgement
	}

	_, err := probe.Run(context.Background(), config)
	if liveProbeBuildEnabled {
		if !errors.Is(err, ErrLiveProbeAcknowledgementRequired) {
			t.Fatalf("Run() error = %v, want acknowledgement error", err)
		}
	} else if !errors.Is(err, ErrLiveProbeBuildDisabled) {
		t.Fatalf("Run() error = %v, want build-disabled error", err)
	}
}

func TestValidateTopologyRequiresTwoUniqueMumbaiPathsAndExpectedNAT(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		expectation TopologyExpectation
		observation TopologyObservation
		wantErr     bool
	}{
		{name: "valid", expectation: validProbeConfig().Topology, observation: validTopologyObservation()},
		{
			name: "only one path",
			expectation: TopologyExpectation{
				Paths:    []PrivatePath{{SubnetID: "subnet-private-a", RouteTableID: "rtb-private-a", AvailabilityZoneID: "aps1-az1"}},
				NATENIID: "eni-nat-voice",
			},
			observation: validTopologyObservation(),
			wantErr:     true,
		},
		{
			name: "duplicate subnet",
			expectation: TopologyExpectation{
				Paths: []PrivatePath{
					{SubnetID: "subnet-private-a", RouteTableID: "rtb-private-a", AvailabilityZoneID: "aps1-az1"},
					{SubnetID: "subnet-private-a", RouteTableID: "rtb-private-b", AvailabilityZoneID: "aps1-az2"},
				},
				NATENIID: "eni-nat-voice",
			},
			observation: validTopologyObservation(),
			wantErr:     true,
		},
		{
			name: "duplicate route table",
			expectation: TopologyExpectation{
				Paths: []PrivatePath{
					{SubnetID: "subnet-private-a", RouteTableID: "rtb-private-a", AvailabilityZoneID: "aps1-az1"},
					{SubnetID: "subnet-private-b", RouteTableID: "rtb-private-a", AvailabilityZoneID: "aps1-az2"},
				},
				NATENIID: "eni-nat-voice",
			},
			observation: validTopologyObservation(),
			wantErr:     true,
		},
		{
			name: "duplicate availability zone",
			expectation: TopologyExpectation{
				Paths: []PrivatePath{
					{SubnetID: "subnet-private-a", RouteTableID: "rtb-private-a", AvailabilityZoneID: "aps1-az1"},
					{SubnetID: "subnet-private-b", RouteTableID: "rtb-private-b", AvailabilityZoneID: "aps1-az1"},
				},
				NATENIID: "eni-nat-voice",
			},
			observation: validTopologyObservation(),
			wantErr:     true,
		},
		{
			name: "unsupported availability zone",
			expectation: TopologyExpectation{
				Paths: []PrivatePath{
					{SubnetID: "subnet-private-a", RouteTableID: "rtb-private-a", AvailabilityZoneID: "aps1-az1"},
					{SubnetID: "subnet-private-b", RouteTableID: "rtb-private-b", AvailabilityZoneID: "aps1-az4"},
				},
				NATENIID: "eni-nat-voice",
			},
			observation: validTopologyObservation(),
			wantErr:     true,
		},
		{
			name:        "observed route does not use expected nat eni",
			expectation: validProbeConfig().Topology,
			observation: func() TopologyObservation {
				value := validTopologyObservation()
				value.Paths[1].DefaultRouteNATENIID = "eni-unexpected"
				return value
			}(),
			wantErr: true,
		},
		{
			name:        "nat eni mismatch",
			expectation: validProbeConfig().Topology,
			observation: func() TopologyObservation {
				value := validTopologyObservation()
				value.NAT.ENIID = "eni-unexpected"
				return value
			}(),
			wantErr: true,
		},
		{
			name:        "source destination check enabled",
			expectation: validProbeConfig().Topology,
			observation: func() TopologyObservation {
				value := validTopologyObservation()
				value.NAT.SourceDestCheck = true
				return value
			}(),
			wantErr: true,
		},
		{
			name:        "source destination check was not observed",
			expectation: validProbeConfig().Topology,
			observation: func() TopologyObservation {
				value := validTopologyObservation()
				value.NAT.SourceDestCheckObserved = false
				return value
			}(),
			wantErr: true,
		},
		{
			name:        "runtime is not pinned to expected subnet",
			expectation: validProbeConfig().Topology,
			observation: func() TopologyObservation {
				value := validTopologyObservation()
				value.RuntimeSubnetID = "subnet-private-b"
				return value
			}(),
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateTopology(test.expectation, test.observation)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateTopology() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestValidateTLSEndpointsRejectsHostnameAndURLTricks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   []TLSEndpoint
		wantErr bool
	}{
		{name: "valid exact and region bound host", value: validEndpoints()},
		{name: "ip literal", value: replaceWSSEndpoint("wss://127.0.0.1:443"), wantErr: true},
		{name: "userinfo", value: replaceWSSEndpoint("wss://user@v-abc123.kinesisvideo.ap-south-1.amazonaws.com:443"), wantErr: true},
		{name: "unicode", value: replaceWSSEndpoint("wss://v-é.kinesisvideo.ap-south-1.amazonaws.com:443"), wantErr: true},
		{name: "path", value: replaceWSSEndpoint("wss://v-abc123.kinesisvideo.ap-south-1.amazonaws.com:443/ws"), wantErr: true},
		{name: "query", value: replaceWSSEndpoint("wss://v-abc123.kinesisvideo.ap-south-1.amazonaws.com:443?token=x"), wantErr: true},
		{name: "wrong port", value: replaceWSSEndpoint("wss://v-abc123.kinesisvideo.ap-south-1.amazonaws.com:8443"), wantErr: true},
		{name: "suffix trick", value: replaceWSSEndpoint("wss://v-abc123.kinesisvideo.ap-south-1.amazonaws.com.attacker.invalid:443"), wantErr: true},
		{name: "extra label before suffix", value: replaceWSSEndpoint("wss://extra.v-abc123.kinesisvideo.ap-south-1.amazonaws.com:443"), wantErr: true},
		{name: "wrong region", value: replaceWSSEndpoint("wss://v-abc123.kinesisvideo.us-east-1.amazonaws.com:443"), wantErr: true},
		{name: "protocol scheme mismatch", value: []TLSEndpoint{{Protocol: EndpointHTTPS, URL: "wss://v-abc123.kinesisvideo.ap-south-1.amazonaws.com:443"}, {Protocol: EndpointWSS, URL: "wss://v-def456.kinesisvideo.ap-south-1.amazonaws.com:443"}}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateTLSEndpoints(MumbaiRegion, []string{"kinesisvideo.ap-south-1.amazonaws.com"}, test.value)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateTLSEndpoints() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestValidateTLSEndpointsRejectsNonKVSExactAllowlist(t *testing.T) {
	t.Parallel()

	endpoints := []TLSEndpoint{
		{Protocol: EndpointHTTPS, URL: "https://attacker.invalid:443"},
		{Protocol: EndpointWSS, URL: "wss://v-abc123.kinesisvideo.ap-south-1.amazonaws.com:443"},
	}
	if err := ValidateTLSEndpoints(MumbaiRegion, []string{"attacker.invalid"}, endpoints); !errors.Is(err, ErrInvalidTLSEndpoint) {
		t.Fatalf("ValidateTLSEndpoints() error = %v, want strict KVS allowlist rejection", err)
	}
}

func TestConfigRejectsBilleifBackendHostAliases(t *testing.T) {
	t.Parallel()

	aliases := []string{
		"kinesisvideo.ap-south-1.amazonaws.com",
		"secretsmanager.ap-south-1.amazonaws.com",
		"logs.ap-south-1.amazonaws.com",
		"api.sarvam.ai",
		"v-abc123.kinesisvideo.ap-south-1.amazonaws.com",
	}
	for _, alias := range aliases {
		t.Run(alias, func(t *testing.T) {
			t.Parallel()
			config := validProbeConfig()
			config.BilleifBackendHost = alias
			if err := validateConfig(config); !errors.Is(err, ErrInvalidProbeConfig) {
				t.Fatalf("validateConfig() error = %v, want backend alias rejection", err)
			}
		})
	}
}

func TestValidateTURNCredentialsRequiresUDP443AndExpiryMargin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   TURNCredentials
		wantErr bool
	}{
		{name: "valid", value: validCredentials()},
		{name: "missing username", value: mutateCredentials(func(value *TURNCredentials) { value.Username = "" }), wantErr: true},
		{name: "missing password", value: mutateCredentials(func(value *TURNCredentials) { value.Password = "" }), wantErr: true},
		{name: "credential expires inside safety margin", value: mutateCredentials(func(value *TURNCredentials) { value.ExpiresAt = fixedProbeTime.Add(5 * time.Minute) }), wantErr: true},
		{name: "tcp transport", value: mutateCredentials(func(value *TURNCredentials) {
			value.URIs[0] = "turn:v-abc123.kinesisvideo.ap-south-1.amazonaws.com:443?transport=tcp"
		}), wantErr: true},
		{name: "wrong port", value: mutateCredentials(func(value *TURNCredentials) {
			value.URIs[0] = "turn:v-abc123.kinesisvideo.ap-south-1.amazonaws.com:3478?transport=udp"
		}), wantErr: true},
		{name: "tls turn scheme", value: mutateCredentials(func(value *TURNCredentials) {
			value.URIs[0] = "turns:v-abc123.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp"
		}), wantErr: true},
		{name: "userinfo", value: mutateCredentials(func(value *TURNCredentials) {
			value.URIs[0] = "turn:user@v-abc123.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp"
		}), wantErr: true},
		{name: "ip literal", value: mutateCredentials(func(value *TURNCredentials) { value.URIs[0] = "turn:127.0.0.1:443?transport=udp" }), wantErr: true},
		{name: "suffix trick", value: mutateCredentials(func(value *TURNCredentials) {
			value.URIs[0] = "turn:v-abc123.kinesisvideo.ap-south-1.amazonaws.com.attacker.invalid:443?transport=udp"
		}), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateTURNCredentials(MumbaiRegion, fixedProbeTime, 5*time.Minute, test.value)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateTURNCredentials() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestProbeValidatesCredentialExpiryAtAcquisitionTime(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("full fake probe requires the explicit test build tag")
	}

	var clockCalls atomic.Int64
	deps := successfulFakeDependencies(t)
	deps.Clock = func() time.Time {
		switch clockCalls.Add(1) {
		case 1:
			return fixedProbeTime
		default:
			return fixedProbeTime.Add(2 * time.Minute)
		}
	}
	deps.ICE = iceFunc(func(context.Context, string) (TURNCredentials, error) {
		credentials := validCredentials()
		credentials.ExpiresAt = fixedProbeTime.Add(6 * time.Minute)
		return credentials, nil
	})

	if _, err := NewProbe(deps).Run(context.Background(), validProbeConfig()); !errors.Is(err, ErrInvalidTURNCredentials) {
		t.Fatalf("Run() error = %v, want credential freshness rejection at acquisition time", err)
	}
}

func TestProbeRevalidatesCredentialMarginAfterRelaySetup(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("full fake probe requires the explicit test build tag")
	}

	var clockCalls atomic.Int64
	deps := successfulFakeDependencies(t)
	deps.Clock = func() time.Time {
		switch clockCalls.Add(1) {
		case 1:
			return fixedProbeTime
		case 2, 3:
			return fixedProbeTime.Add(time.Minute)
		default:
			return fixedProbeTime.Add(4 * time.Minute)
		}
	}
	deps.ICE = iceFunc(func(context.Context, string) (TURNCredentials, error) {
		credentials := validCredentials()
		credentials.ExpiresAt = fixedProbeTime.Add(8 * time.Minute)
		return credentials, nil
	})

	if _, err := NewProbe(deps).Run(context.Background(), validProbeConfig()); !errors.Is(err, ErrInvalidTURNCredentials) {
		t.Fatalf("Run() error = %v, want credential margin rejection after relay setup", err)
	}
}

func TestValidateNetworkObservationsRequiresEveryStrictDNSAndTCP443Target(t *testing.T) {
	t.Parallel()

	targets := expectedNetworkTargets()
	valid := successfulNetworkObservations(targets)
	tests := []struct {
		name        string
		observation []ConnectivityObservation
		wantErr     bool
	}{
		{name: "valid", observation: valid},
		{name: "missing target", observation: valid[:len(valid)-1], wantErr: true},
		{name: "dns failed", observation: mutateNetworkObservation(valid, 0, func(value *ConnectivityObservation) { value.DNSResolved = false }), wantErr: true},
		{name: "tcp 443 failed", observation: mutateNetworkObservation(valid, 1, func(value *ConnectivityObservation) { value.TCP443Reachable = false }), wantErr: true},
		{name: "unexpected host", observation: mutateNetworkObservation(valid, 2, func(value *ConnectivityObservation) { value.Host = "attacker.invalid" }), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateNetworkObservations(fixedProbeTime, fixedProbeTime, 2*time.Hour, targets, test.observation)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateNetworkObservations() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestValidateNetworkObservationsRejectsUnboundedOrUnauthenticatedEvidence(t *testing.T) {
	t.Parallel()

	targets := expectedNetworkTargets()
	valid := successfulNetworkObservations(targets)
	tests := []struct {
		name   string
		mutate func([]ConnectivityObservation) []ConnectivityObservation
	}{
		{name: "stale timestamp", mutate: func(values []ConnectivityObservation) []ConnectivityObservation {
			return mutateNetworkObservation(values, 0, func(value *ConnectivityObservation) { value.ObservedAt = fixedProbeTime.Add(-3 * time.Hour) })
		}},
		{name: "future timestamp", mutate: func(values []ConnectivityObservation) []ConnectivityObservation {
			return mutateNetworkObservation(values, 0, func(value *ConnectivityObservation) { value.ObservedAt = fixedProbeTime.Add(time.Second) })
		}},
		{name: "non utc timestamp", mutate: func(values []ConnectivityObservation) []ConnectivityObservation {
			return mutateNetworkObservation(values, 0, func(value *ConnectivityObservation) { value.ObservedAt = fixedProbeTime.In(time.FixedZone("local", 0)) })
		}},
		{name: "bad artifact digest", mutate: func(values []ConnectivityObservation) []ConnectivityObservation {
			return mutateNetworkObservation(values, 0, func(value *ConnectivityObservation) { value.ArtifactSHA256 = "not-a-sha256" })
		}},
		{name: "duplicate artifact digest", mutate: func(values []ConnectivityObservation) []ConnectivityObservation {
			return mutateNetworkObservation(values, 1, func(value *ConnectivityObservation) { value.ArtifactSHA256 = values[0].ArtifactSHA256 })
		}},
		{name: "duplicate observation", mutate: func(values []ConnectivityObservation) []ConnectivityObservation {
			cloned := append([]ConnectivityObservation(nil), values...)
			cloned[len(cloned)-1] = cloned[0]
			return cloned
		}},
		{name: "extra observation", mutate: func(values []ConnectivityObservation) []ConnectivityObservation {
			return append(append([]ConnectivityObservation(nil), values...), values[0])
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateNetworkObservations(fixedProbeTime, fixedProbeTime, 2*time.Hour, targets, test.mutate(valid)); !errors.Is(err, ErrInvalidNetworkObservation) {
				t.Fatalf("ValidateNetworkObservations() error = %v, want invalid observation", err)
			}
		})
	}
}

func TestProbeAcceptsConnectivityObservedInsideTheRunWindow(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("full fake probe requires the explicit test build tag")
	}

	deps := successfulFakeDependencies(t)
	deps.Network = networkFunc(func(ctx context.Context, targets []NetworkTarget) ([]ConnectivityObservation, error) {
		observations := successfulNetworkObservations(targets)
		for index := range observations {
			observations[index].ObservedAt = fixedProbeTime.Add(time.Second)
		}
		return observations, ctx.Err()
	})

	if _, err := NewProbe(deps).Run(context.Background(), validProbeConfig()); err != nil {
		t.Fatalf("Run() rejected connectivity observed after run start: %v", err)
	}
}

func TestProbeRejectsConnectivityObservedBeforeTheRunWindow(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("full fake probe requires the explicit test build tag")
	}

	deps := successfulFakeDependencies(t)
	deps.Network = networkFunc(func(ctx context.Context, targets []NetworkTarget) ([]ConnectivityObservation, error) {
		observations := successfulNetworkObservations(targets)
		for index := range observations {
			observations[index].ObservedAt = fixedProbeTime.Add(-time.Second)
		}
		return observations, ctx.Err()
	})

	if _, err := NewProbe(deps).Run(context.Background(), validProbeConfig()); !errors.Is(err, ErrInvalidNetworkObservation) {
		t.Fatalf("Run() error = %v, want pre-run connectivity rejection", err)
	}
}

func TestNetworkObservationRepresentationsHideHostAndArtifact(t *testing.T) {
	t.Parallel()

	observation := successfulNetworkObservations(expectedNetworkTargets())[0]
	encoded, err := json.Marshal(observation)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	views := []string{observation.String(), fmt.Sprintf("%+v", observation), fmt.Sprintf("%#v", observation), string(encoded)}
	for _, view := range views {
		if strings.Contains(view, observation.Host) || strings.Contains(view, observation.ArtifactSHA256) {
			t.Fatalf("network representation leaked private data: %s", view)
		}
		if !strings.Contains(view, hashString(observation.Host)) {
			t.Fatalf("network representation omitted safe host fingerprint: %s", view)
		}
	}
}

func TestSensitiveTURNValuesHaveOnlyRedactedRepresentations(t *testing.T) {
	t.Parallel()

	credentials := validCredentials()
	request := RelayRequest{
		URI:       credentials.URIs[0],
		Username:  credentials.Username,
		Password:  credentials.Password,
		Payload:   []byte("payload-sensitive"),
		RelayOnly: true,
	}
	redacted, err := RedactICE(credentials)
	if err != nil {
		t.Fatalf("RedactICE() error = %v", err)
	}
	if redacted.Transport != "udp" || redacted.Port != 443 || len(redacted.HostSHA256) != 1 || len(redacted.HostSHA256[0]) != 64 || redacted.ExpiresAt != credentials.ExpiresAt {
		t.Fatalf("RedactICE() = %+v, want one hashed UDP/443 endpoint and expiry", redacted)
	}

	values := []any{credentials, request}
	for _, value := range values {
		encoded, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			t.Fatalf("json.Marshal(%T) error = %v", value, marshalErr)
		}
		views := []string{fmt.Sprint(value), fmt.Sprintf("%+v", value), fmt.Sprintf("%#v", value), string(encoded)}
		for _, view := range views {
			for _, sensitive := range []string{"turn-user-sensitive", "turn-password-sensitive", "v-abc123.kinesisvideo", "payload-sensitive"} {
				if strings.Contains(view, sensitive) {
					t.Fatalf("%T representation leaked %q: %s", value, sensitive, view)
				}
			}
		}
	}
}

func TestRedactedICESortsHostFingerprintsDeterministically(t *testing.T) {
	t.Parallel()

	first := validCredentials()
	first.URIs = []string{
		"turn:v-zulu.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp",
		"turn:v-alpha.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp",
	}
	second := first
	second.URIs = []string{first.URIs[1], first.URIs[0]}
	redactedFirst, err := RedactICE(first)
	if err != nil {
		t.Fatalf("RedactICE(first) error = %v", err)
	}
	redactedSecond, err := RedactICE(second)
	if err != nil {
		t.Fatalf("RedactICE(second) error = %v", err)
	}
	if !slices.Equal(redactedFirst.HostSHA256, redactedSecond.HostSHA256) || !slices.IsSorted(redactedFirst.HostSHA256) {
		t.Fatalf("RedactICE() hashes are not deterministic: %v versus %v", redactedFirst.HostSHA256, redactedSecond.HostSHA256)
	}
}

func TestValidateLiveEvidenceRequiresCompletePassingObservations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*LiveEvidenceObservation)
	}{
		{name: "nat cpu missing", mutate: func(value *LiveEvidenceObservation) { value.NAT.CPUObserved = false }},
		{name: "nat cpu p95 threshold is strict", mutate: func(value *LiveEvidenceObservation) {
			value.NAT.CPUUtilizationP95Percent = NATCPUUtilizationP95Limit
		}},
		{name: "nat cpu credits missing", mutate: func(value *LiveEvidenceObservation) { value.NAT.CPUCreditsObserved = false }},
		{name: "nat cpu credits throttled", mutate: func(value *LiveEvidenceObservation) { value.NAT.CPUCreditThrottled = true }},
		{name: "nat cpu surplus charged", mutate: func(value *LiveEvidenceObservation) { value.NAT.CPUSurplusCreditsCharged = true }},
		{name: "nat network missing", mutate: func(value *LiveEvidenceObservation) { value.NAT.NetworkObserved = false }},
		{name: "nat network empty", mutate: func(value *LiveEvidenceObservation) { value.NAT.NetworkBytesProcessed = 0 }},
		{name: "nat network errors", mutate: func(value *LiveEvidenceObservation) { value.NAT.NetworkErrorCount = 1 }},
		{name: "nat conntrack missing", mutate: func(value *LiveEvidenceObservation) { value.NAT.ConntrackObserved = false }},
		{name: "nat conntrack empty", mutate: func(value *LiveEvidenceObservation) { value.NAT.PeakConntrackEntries = 0 }},
		{name: "nat conntrack threshold is strict", mutate: func(value *LiveEvidenceObservation) {
			value.NAT.MaxConntrackUtilizationPercent = NATMaxConntrackUtilizationPercent
		}},
		{name: "nat drops missing", mutate: func(value *LiveEvidenceObservation) { value.NAT.DropsObserved = false }},
		{name: "nat packet drops", mutate: func(value *LiveEvidenceObservation) { value.NAT.PacketDropCount = 1 }},
		{name: "nat status missing", mutate: func(value *LiveEvidenceObservation) { value.NAT.StatusObserved = false }},
		{name: "nat instance status", mutate: func(value *LiveEvidenceObservation) { value.NAT.InstanceStatusOK = false }},
		{name: "nat system status", mutate: func(value *LiveEvidenceObservation) { value.NAT.SystemStatusOK = false }},
		{name: "ena missing", mutate: func(value *LiveEvidenceObservation) { value.NAT.ENAObserved = false }},
		{name: "ena disabled", mutate: func(value *LiveEvidenceObservation) { value.NAT.ENAEnabled = false }},
		{name: "flow logs missing", mutate: func(value *LiveEvidenceObservation) { value.NAT.FlowLogsObserved = false }},
		{name: "flow logs disabled", mutate: func(value *LiveEvidenceObservation) { value.NAT.FlowLogsEnabled = false }},
		{name: "flow logs empty", mutate: func(value *LiveEvidenceObservation) { value.NAT.FlowLogRecords = 0 }},
		{name: "flow rejects", mutate: func(value *LiveEvidenceObservation) { value.NAT.RejectedFlowCount = 1 }},
		{name: "short endurance", mutate: func(value *LiveEvidenceObservation) { value.Endurance.Duration = MinimumTURNEndurance - time.Second }},
		{name: "discontinuous endurance", mutate: func(value *LiveEvidenceObservation) { value.Endurance.Continuous = false }},
		{name: "non relay endurance", mutate: func(value *LiveEvidenceObservation) { value.Endurance.RelayOnly = false }},
		{name: "empty endurance", mutate: func(value *LiveEvidenceObservation) { value.Endurance.RoundTrips = 0 }},
		{name: "zero endurance bytes", mutate: func(value *LiveEvidenceObservation) { value.Endurance.BytesRoundTripped = 0 }},
		{name: "turn allocation missing", mutate: func(value *LiveEvidenceObservation) { value.Endurance.TURNAllocations = 0 }},
		{name: "turn allocation failed", mutate: func(value *LiveEvidenceObservation) { value.Endurance.TURNAllocationFailures = 1 }},
		{name: "reconnect missing", mutate: func(value *LiveEvidenceObservation) { value.Endurance.ReconnectAttempts = 0 }},
		{name: "reconnect failed", mutate: func(value *LiveEvidenceObservation) { value.Endurance.ReconnectFailures = 1 }},
		{name: "credential expiry outside window", mutate: func(value *LiveEvidenceObservation) {
			value.CredentialLifecycle.CredentialExpiresAt = value.CredentialLifecycle.WindowEnd.Add(time.Second)
		}},
		{name: "established across expiry not observed", mutate: func(value *LiveEvidenceObservation) {
			value.CredentialLifecycle.EstablishedAcrossExpiryObserved = false
		}},
		{name: "established across expiry failed", mutate: func(value *LiveEvidenceObservation) {
			value.CredentialLifecycle.EstablishedAcrossExpirySucceeded = false
		}},
		{name: "new allocation not observed", mutate: func(value *LiveEvidenceObservation) { value.CredentialLifecycle.NewAllocationObserved = false }},
		{name: "new allocation failed", mutate: func(value *LiveEvidenceObservation) { value.CredentialLifecycle.NewAllocationSucceeded = false }},
		{name: "refresh not observed", mutate: func(value *LiveEvidenceObservation) { value.CredentialLifecycle.RefreshObserved = false }},
		{name: "refresh failed", mutate: func(value *LiveEvidenceObservation) { value.CredentialLifecycle.RefreshSucceeded = false }},
		{name: "restart not observed", mutate: func(value *LiveEvidenceObservation) { value.CredentialLifecycle.RestartObserved = false }},
		{name: "restart outcome invalid", mutate: func(value *LiveEvidenceObservation) {
			value.CredentialLifecycle.RestartOutcome = RestartOutcome("invalid")
		}},
		{name: "no restart cannot claim success", mutate: func(value *LiveEvidenceObservation) {
			value.CredentialLifecycle.RestartOutcome = RestartOutcomeNone
			value.CredentialLifecycle.RestartSucceeded = true
		}},
		{name: "restart failed", mutate: func(value *LiveEvidenceObservation) { value.CredentialLifecycle.RestartSucceeded = false }},
		{name: "backend regression", mutate: func(value *LiveEvidenceObservation) { value.BackendRegression.NoRegression = false }},
		{name: "backend baseline missing", mutate: func(value *LiveEvidenceObservation) { value.BackendRegression.BaselineObserved = false }},
		{name: "backend in-test missing", mutate: func(value *LiveEvidenceObservation) { value.BackendRegression.InTestObserved = false }},
		{name: "backend cleanup missing", mutate: func(value *LiveEvidenceObservation) { value.BackendRegression.PostCleanupObserved = false }},
		{name: "backend tests missing", mutate: func(value *LiveEvidenceObservation) { value.BackendRegression.TestsRun = 0 }},
		{name: "backend tests failed", mutate: func(value *LiveEvidenceObservation) { value.BackendRegression.TestsFailed = 1 }},
		{name: "bad digest", mutate: func(value *LiveEvidenceObservation) { value.NAT.ArtifactSHA256 = "bad" }},
		{name: "duplicate digest", mutate: func(value *LiveEvidenceObservation) {
			value.BackendRegression.ArtifactSHA256 = value.NAT.ArtifactSHA256
		}},
		{name: "stale artifact", mutate: func(value *LiveEvidenceObservation) {
			value.BackendRegression.ObservedAt = fixedProbeTime.Add(-3 * time.Hour)
		}},
		{name: "future artifact", mutate: func(value *LiveEvidenceObservation) { value.Endurance.ObservedAt = fixedProbeTime.Add(time.Second) }},
		{name: "non utc artifact", mutate: func(value *LiveEvidenceObservation) {
			value.NAT.ObservedAt = fixedProbeTime.In(time.FixedZone("zero", 0))
		}},
		{name: "incoherent window", mutate: func(value *LiveEvidenceObservation) {
			value.BackendRegression.WindowStart = value.BackendRegression.WindowStart.Add(time.Minute)
		}},
		{name: "window does not start with run", mutate: func(value *LiveEvidenceObservation) {
			start := fixedProbeTime.Add(time.Second)
			value.NAT.WindowStart, value.Endurance.WindowStart, value.CredentialLifecycle.WindowStart, value.BackendRegression.WindowStart = start, start, start, start
		}},
		{name: "window shorter than thirty minutes", mutate: func(value *LiveEvidenceObservation) {
			end := fixedProbeTime.Add(MinimumTURNEndurance - time.Second)
			value.NAT.WindowEnd, value.Endurance.WindowEnd, value.CredentialLifecycle.WindowEnd, value.BackendRegression.WindowEnd = end, end, end, end
			value.NAT.ObservedAt, value.Endurance.ObservedAt, value.CredentialLifecycle.ObservedAt, value.BackendRegression.ObservedAt = end, end, end, end
			value.Endurance.Duration = MinimumTURNEndurance - time.Second
		}},
		{name: "window ends after completion", mutate: func(value *LiveEvidenceObservation) {
			end := fixedProbeTime.Add(36 * time.Minute)
			value.NAT.WindowEnd, value.Endurance.WindowEnd, value.CredentialLifecycle.WindowEnd, value.BackendRegression.WindowEnd = end, end, end, end
			value.NAT.ObservedAt, value.Endurance.ObservedAt, value.CredentialLifecycle.ObservedAt, value.BackendRegression.ObservedAt = end, end, end, end
		}},
		{name: "credential expiry differs from this run", mutate: func(value *LiveEvidenceObservation) {
			value.CredentialLifecycle.CredentialExpiresAt = fixedProbeTime.Add(29 * time.Minute)
		}},
		{name: "credential expiry on window boundary", mutate: func(value *LiveEvidenceObservation) {
			value.CredentialLifecycle.CredentialExpiresAt = value.CredentialLifecycle.WindowEnd
		}},
	}

	valid := validLiveEvidenceObservation()
	ice, iceErr := RedactICE(validCredentials())
	if iceErr != nil {
		t.Fatalf("RedactICE() error = %v", iceErr)
	}
	completion := fixedProbeTime.Add(35 * time.Minute)
	if summary, err := ValidateLiveEvidence(fixedProbeTime, completion, 2*time.Hour, ice, valid); err != nil || !summary.NATHealthy || summary.EnduranceMinutes < 30 || !summary.CredentialLifecyclePassed || !summary.BackendRegressionPassed {
		t.Fatalf("ValidateLiveEvidence(valid) = %+v, %v", summary, err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := valid
			test.mutate(&value)
			if _, err := ValidateLiveEvidence(fixedProbeTime, completion, 2*time.Hour, ice, value); !errors.Is(err, ErrInvalidLiveEvidence) {
				t.Fatalf("ValidateLiveEvidence() error = %v, want invalid evidence", err)
			}
		})
	}
}

func TestValidateLiveEvidenceAllowsObservedNoRestart(t *testing.T) {
	t.Parallel()

	value := validLiveEvidenceObservation()
	value.CredentialLifecycle.RestartOutcome = RestartOutcomeNone
	value.CredentialLifecycle.RestartSucceeded = false
	ice, err := RedactICE(validCredentials())
	if err != nil {
		t.Fatalf("RedactICE() error = %v", err)
	}
	if _, err := ValidateLiveEvidence(fixedProbeTime, fixedProbeTime.Add(35*time.Minute), 2*time.Hour, ice, value); err != nil {
		t.Fatalf("ValidateLiveEvidence(no restart) error = %v", err)
	}
}

func TestProbeSyntheticRelayRoundTripIsByteExactAndRedacted(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("full fake probe requires the explicit test build tag")
	}

	deps := successfulFakeDependencies(t)
	probe := NewProbe(deps)

	result, err := probe.Run(context.Background(), validProbeConfig())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != ProbeStatusPassed || !result.RelayOnly || result.RoundTripBytes != ProbeNonceBytes || len(result.PathFingerprint) != 64 || len(result.ICE.HostSHA256) != 1 {
		t.Fatalf("Run() result = %+v, want passed %d-byte relay-only proof", result, ProbeNonceBytes)
	}
	if result.Envelope != nil {
		t.Fatal("synthetic result must never carry a signed live evidence envelope")
	}

	encoded, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		t.Fatalf("json.Marshal() error = %v", marshalErr)
	}
	views := []string{string(encoded), result.String(), fmt.Sprintf("%#v", result)}
	for _, view := range views {
		for _, sensitive := range []string{"turn-user-sensitive", "turn-password-sensitive", "123456789012", "voice-01", "subnet-private-a", "eni-nat-voice", "v-abc123"} {
			if strings.Contains(view, sensitive) {
				t.Fatalf("result representation leaked %q: %s", sensitive, view)
			}
		}
	}
}

func TestLiveProbeRequiresValidEvidenceBeforePrivateValidation(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("full fake probe requires the explicit test build tag")
	}

	config := validProbeConfig()
	config.EvidenceMode = EvidenceLive
	deps := successfulFakeDependencies(t)
	deps.liveObserver.(*testLiveObserver).collectFn = func(context.Context, EvidenceRequest) (LiveEvidenceObservation, error) {
		value := validLiveEvidenceObservation()
		value.BackendRegression.NoRegression = false
		return value, nil
	}
	result, err := NewProbe(deps).Run(context.Background(), config)
	if !errors.Is(err, ErrInvalidLiveEvidence) {
		t.Fatalf("Run() error = %v, want invalid live evidence", err)
	}
	if result.Envelope != nil {
		t.Fatal("invalid evidence yielded a signed envelope")
	}
}

func TestLiveProbeEvidenceCollectionIsBoundedAndContextSafe(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("full fake probe requires the explicit test build tag")
	}

	config := validProbeConfig()
	config.EvidenceMode = EvidenceLive
	config.EvidenceTimeout = 5 * time.Millisecond
	deps := successfulFakeDependencies(t)
	deps.liveObserver.(*testLiveObserver).collectFn = func(ctx context.Context, _ EvidenceRequest) (LiveEvidenceObservation, error) {
		<-ctx.Done()
		return validLiveEvidenceObservation(), nil
	}
	_, err := NewProbe(deps).Run(context.Background(), config)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrEvidenceCollectionFailed) {
		t.Fatalf("Run() error = %v, want evidence operation and deadline identities", err)
	}
}

func TestLiveProbeReadsCompletionClockAfterEvidenceCollection(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("full fake probe requires the explicit test build tag")
	}

	events := make([]string, 0, 6)
	deps := successfulFakeDependencies(t)
	observer := deps.liveObserver.(*testLiveObserver)
	var clockCalls atomic.Int64
	observer.clock = func() time.Time {
		events = append(events, "clock")
		switch clockCalls.Add(1) {
		case 1:
			return fixedProbeTime
		case 2:
			return fixedProbeTime.Add(time.Second)
		case 3:
			return fixedProbeTime.Add(2 * time.Second)
		case 4:
			return fixedProbeTime.Add(3 * time.Second)
		default:
			return fixedProbeTime.Add(35 * time.Minute)
		}
	}
	observer.collectFn = func(context.Context, EvidenceRequest) (LiveEvidenceObservation, error) {
		events = append(events, "evidence")
		return validLiveEvidenceObservation(), nil
	}
	config := validProbeConfig()
	config.EvidenceMode = EvidenceLive
	if _, err := NewProbe(deps).Run(context.Background(), config); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := strings.Join(events, ","); got != "clock,clock,clock,clock,evidence,clock" {
		t.Fatalf("clock/evidence order = %q, want validated operation clocks before evidence completion", got)
	}
}

func TestLiveProbeRejectsCompletionBeyondOverallWindow(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("full fake probe requires the explicit test build tag")
	}

	var calls atomic.Int64
	deps := successfulFakeDependencies(t)
	deps.liveObserver.(*testLiveObserver).clock = func() time.Time {
		switch calls.Add(1) {
		case 1:
			return fixedProbeTime
		case 2:
			return fixedProbeTime.Add(time.Second)
		case 3:
			return fixedProbeTime.Add(2 * time.Second)
		case 4:
			return fixedProbeTime.Add(3 * time.Second)
		default:
			return fixedProbeTime.Add(41 * time.Minute)
		}
	}
	config := validProbeConfig()
	config.EvidenceMode = EvidenceLive
	if _, err := NewProbe(deps).Run(context.Background(), config); !errors.Is(err, ErrInvalidLiveEvidence) {
		t.Fatalf("Run() error = %v, want completion outside overall window rejection", err)
	}
}

func TestProbeRejectsNonRelayOrChangedPayloadWithoutLeakingDependencyDetails(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("full fake probe requires the explicit test build tag")
	}

	tests := []struct {
		name  string
		relay func([]byte) RelayObservation
	}{
		{name: "changed payload", relay: func([]byte) RelayObservation {
			return RelayObservation{Payload: []byte("not-the-nonce"), LocalCandidateType: CandidateRelay, RemoteCandidateType: CandidateRelay}
		}},
		{name: "host candidate", relay: func(payload []byte) RelayObservation {
			return RelayObservation{Payload: bytes.Clone(payload), LocalCandidateType: CandidateHost, RemoteCandidateType: CandidateRelay}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deps := successfulFakeDependencies(t)
			deps.Relay = relayFunc(func(_ context.Context, request RelayRequest) (RelayObservation, error) {
				return test.relay(request.Payload), nil
			})

			_, err := NewProbe(deps).Run(context.Background(), validProbeConfig())
			if !errors.Is(err, ErrRelayRoundTripFailed) {
				t.Fatalf("Run() error = %v, want relay error", err)
			}
			for _, sensitive := range []string{"turn-password-sensitive", "123456789012", "voice-01"} {
				if strings.Contains(fmt.Sprint(err), sensitive) {
					t.Fatalf("error leaked %q: %v", sensitive, err)
				}
			}
		})
	}
}

func TestProbeBoundsEveryDependencyContext(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("full fake probe requires the explicit test build tag")
	}

	deps := successfulFakeDependencies(t)
	deps.Topology = topologyFunc(func(ctx context.Context, _ TopologyExpectation) (TopologyObservation, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("topology dependency did not receive a deadline")
		}
		<-ctx.Done()
		return TopologyObservation{}, errors.New("dependency timeout includes turn-password-sensitive")
	})
	config := validProbeConfig()
	config.TopologyTimeout = 5 * time.Millisecond

	started := time.Now()
	_, err := NewProbe(deps).Run(context.Background(), config)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want preserved context deadline", err)
	}
	if !errors.Is(err, ErrTopologyInspectionFailed) {
		t.Fatalf("Run() error = %v, want topology operation identity", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("Run() took %v, want bounded topology timeout", elapsed)
	}
	if strings.Contains(fmt.Sprint(err), "turn-password-sensitive") {
		t.Fatalf("Run() leaked dependency error: %v", err)
	}
}

func TestStablePathFingerprintIgnoresConfiguredPathOrdering(t *testing.T) {
	t.Parallel()

	topology := validProbeConfig().Topology
	reversed := topology
	reversed.Paths = []PrivatePath{topology.Paths[1], topology.Paths[0]}
	if stablePathFingerprint(topology) != stablePathFingerprint(reversed) {
		t.Fatal("reversing configured path order changed the runtime path identity")
	}
}

func TestProbeRejectsLateNilNetworkResultWithDeadlineIdentity(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("full fake probe requires the explicit test build tag")
	}

	deps := successfulFakeDependencies(t)
	deps.Network = networkFunc(func(ctx context.Context, targets []NetworkTarget) ([]ConnectivityObservation, error) {
		<-ctx.Done()
		return successfulNetworkObservations(targets), nil
	})
	config := validProbeConfig()
	config.NetworkTimeout = 5 * time.Millisecond
	_, err := NewProbe(deps).Run(context.Background(), config)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrNetworkCheckFailed) {
		t.Fatalf("Run() error = %v, want network operation and preserved deadline identities", err)
	}
}

func TestProbePreservesWrappedContextErrorWithoutProviderText(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("full fake probe requires the explicit test build tag")
	}

	deps := successfulFakeDependencies(t)
	deps.Topology = topologyFunc(func(context.Context, TopologyExpectation) (TopologyObservation, error) {
		return TopologyObservation{}, fmt.Errorf("provider secret turn-password-sensitive: %w", context.Canceled)
	})
	_, err := NewProbe(deps).Run(context.Background(), validProbeConfig())
	if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrTopologyInspectionFailed) {
		t.Fatalf("Run() error = %v, want topology operation and preserved cancellation identities", err)
	}
	if strings.Contains(fmt.Sprint(err), "turn-password-sensitive") {
		t.Fatalf("Run() leaked wrapped provider text: %v", err)
	}
}

func replaceWSSEndpoint(rawURL string) []TLSEndpoint {
	return []TLSEndpoint{
		{Protocol: EndpointHTTPS, URL: "https://kinesisvideo.ap-south-1.amazonaws.com:443"},
		{Protocol: EndpointWSS, URL: rawURL},
	}
}

func mutateCredentials(mutate func(*TURNCredentials)) TURNCredentials {
	value := validCredentials()
	value.URIs = append([]string(nil), value.URIs...)
	mutate(&value)
	return value
}

func expectedNetworkTargets() []NetworkTarget {
	return []NetworkTarget{
		{Kind: NetworkKVSControl, Host: "kinesisvideo.ap-south-1.amazonaws.com", Port: 443},
		{Kind: NetworkSecretsManager, Host: "secretsmanager.ap-south-1.amazonaws.com", Port: 443},
		{Kind: NetworkCloudWatchLogs, Host: "logs.ap-south-1.amazonaws.com", Port: 443},
		{Kind: NetworkSarvam, Host: "api.sarvam.ai", Port: 443},
		{Kind: NetworkBilleifBackend, Host: "backend.billeif.example", Port: 443},
		{Kind: NetworkKVSDiscovered, Host: "v-abc123.kinesisvideo.ap-south-1.amazonaws.com", Port: 443},
	}
}

func successfulNetworkObservations(targets []NetworkTarget) []ConnectivityObservation {
	observations := make([]ConnectivityObservation, len(targets))
	for index, target := range targets {
		observations[index] = ConnectivityObservation{
			Kind:            target.Kind,
			Host:            target.Host,
			Port:            target.Port,
			DNSResolved:     true,
			TCP443Reachable: true,
			ObservedAt:      fixedProbeTime,
			ArtifactSHA256:  hashString("network-artifact-" + target.Host),
		}
	}
	return observations
}

func mutateNetworkObservation(values []ConnectivityObservation, index int, mutate func(*ConnectivityObservation)) []ConnectivityObservation {
	cloned := append([]ConnectivityObservation(nil), values...)
	mutate(&cloned[index])
	return cloned
}

func validObservationArtifact(digestCharacter string) ObservationArtifact {
	return ObservationArtifact{
		ArtifactSHA256: strings.Repeat(digestCharacter, 64),
		WindowStart:    fixedProbeTime,
		WindowEnd:      fixedProbeTime.Add(35 * time.Minute),
		ObservedAt:     fixedProbeTime.Add(35 * time.Minute),
	}
}

func validLiveEvidenceObservation() LiveEvidenceObservation {
	return LiveEvidenceObservation{
		NAT: NATHealthObservation{
			ObservationArtifact:            validObservationArtifact("a"),
			CPUObserved:                    true,
			CPUUtilizationP95Percent:       42,
			CPUCreditsObserved:             true,
			CPUCreditThrottled:             false,
			CPUSurplusCreditsCharged:       false,
			NetworkObserved:                true,
			NetworkBytesProcessed:          1_000_000,
			NetworkErrorCount:              0,
			ConntrackObserved:              true,
			PeakConntrackEntries:           25,
			MaxConntrackUtilizationPercent: 40,
			DropsObserved:                  true,
			PacketDropCount:                0,
			StatusObserved:                 true,
			InstanceStatusOK:               true,
			SystemStatusOK:                 true,
			ENAObserved:                    true,
			ENAEnabled:                     true,
			FlowLogsObserved:               true,
			FlowLogsEnabled:                true,
			FlowLogRecords:                 100,
			RejectedFlowCount:              0,
		},
		Endurance: EnduranceObservation{
			ObservationArtifact:    validObservationArtifact("b"),
			Duration:               35 * time.Minute,
			Continuous:             true,
			RelayOnly:              true,
			RoundTrips:             2_100,
			BytesRoundTripped:      67_200,
			TURNAllocations:        2,
			TURNAllocationFailures: 0,
			ReconnectAttempts:      1,
			ReconnectFailures:      0,
		},
		CredentialLifecycle: CredentialLifecycleObservation{
			ObservationArtifact:              validObservationArtifact("c"),
			CredentialExpiresAt:              fixedProbeTime.Add(30 * time.Minute),
			EstablishedAcrossExpiryObserved:  true,
			EstablishedAcrossExpirySucceeded: true,
			NewAllocationObserved:            true,
			NewAllocationSucceeded:           true,
			RefreshObserved:                  true,
			RefreshSucceeded:                 true,
			RestartObserved:                  true,
			RestartOutcome:                   RestartOutcomeICE,
			RestartSucceeded:                 true,
		},
		BackendRegression: BackendRegressionObservation{
			ObservationArtifact: validObservationArtifact("d"),
			NoRegression:        true,
			BaselineObserved:    true,
			InTestObserved:      true,
			PostCleanupObserved: true,
			TestsRun:            100,
			TestsFailed:         0,
		},
	}
}

func validSecondPathEvidenceObservation() LiveEvidenceObservation {
	value := validLiveEvidenceObservation()
	value.NAT.ArtifactSHA256 = strings.Repeat("e", 64)
	value.Endurance.ArtifactSHA256 = strings.Repeat("f", 64)
	value.CredentialLifecycle.ArtifactSHA256 = strings.Repeat("0", 64)
	value.BackendRegression.ArtifactSHA256 = strings.Repeat("1", 64)
	return value
}

func poisonDependencies() Dependencies {
	panicIfCalled := func() { panic("dependency called before live-probe gate") }
	return Dependencies{
		Topology: topologyFunc(func(context.Context, TopologyExpectation) (TopologyObservation, error) {
			panicIfCalled()
			return TopologyObservation{}, nil
		}),
		Endpoints: endpointFunc(func(context.Context, string) ([]TLSEndpoint, error) { panicIfCalled(); return nil, nil }),
		Network: networkFunc(func(context.Context, []NetworkTarget) ([]ConnectivityObservation, error) {
			panicIfCalled()
			return nil, nil
		}),
		ICE: iceFunc(func(context.Context, string) (TURNCredentials, error) { panicIfCalled(); return TURNCredentials{}, nil }),
		Relay: relayFunc(func(context.Context, RelayRequest) (RelayObservation, error) {
			panicIfCalled()
			return RelayObservation{}, nil
		}),
		Metrics: metricsFunc(func(context.Context, ProbeMetric) error { panicIfCalled(); return nil }),
		Clock:   func() time.Time { panicIfCalled(); return time.Time{} },
	}
}

func successfulFakeDependencies(t *testing.T) Dependencies {
	t.Helper()
	clock := fakeProbeClock()
	observer := newTestLiveObserver("observer-primary", "observer-key-01", 7)
	observer.clock = fakeProbeClock()
	observer.collectFn = func(ctx context.Context, request EvidenceRequest) (LiveEvidenceObservation, error) {
		if len(request.RunID) != 64 || len(request.PathFingerprint) != 64 || len(request.ICE.HostSHA256) == 0 ||
			request.RunStartedAt != fixedProbeTime || request.CampaignID != "voice-turn-campaign-01" ||
			request.SourceRevision != strings.Repeat("a", 40) || request.ImageDigest != "sha256:"+strings.Repeat("b", 64) ||
			request.ObserverIdentity != "observer-primary" || request.ObserverKeyID != "observer-key-01" {
			return LiveEvidenceObservation{}, errors.New("invalid redacted evidence request")
		}
		return validLiveEvidenceObservation(), ctx.Err()
	}
	checkDeadline := func(ctx context.Context) {
		t.Helper()
		deadline, ok := ctx.Deadline()
		if !ok || !deadline.After(time.Now()) {
			t.Fatalf("dependency context deadline = %v, %v; want a future deadline", deadline, ok)
		}
	}
	return Dependencies{
		Topology: topologyFunc(func(ctx context.Context, _ TopologyExpectation) (TopologyObservation, error) {
			checkDeadline(ctx)
			return validTopologyObservation(), nil
		}),
		Endpoints: endpointFunc(func(ctx context.Context, _ string) ([]TLSEndpoint, error) {
			checkDeadline(ctx)
			return validEndpoints(), nil
		}),
		Network: networkFunc(func(ctx context.Context, targets []NetworkTarget) ([]ConnectivityObservation, error) {
			checkDeadline(ctx)
			if !slices.Equal(targets, expectedNetworkTargets()) {
				return nil, errors.New("network target allowlist mismatch")
			}
			return successfulNetworkObservations(targets), nil
		}),
		ICE: iceFunc(func(ctx context.Context, _ string) (TURNCredentials, error) {
			checkDeadline(ctx)
			return validCredentials(), nil
		}),
		Relay: relayFunc(func(ctx context.Context, request RelayRequest) (RelayObservation, error) {
			checkDeadline(ctx)
			if !request.RelayOnly || len(request.Payload) != ProbeNonceBytes {
				return RelayObservation{}, errors.New("relay-only request contract violated")
			}
			return RelayObservation{
				Payload:             bytes.Clone(request.Payload),
				LocalCandidateType:  CandidateRelay,
				RemoteCandidateType: CandidateRelay,
				ArtifactSHA256:      hashString("relay-artifact-private-a"),
			}, nil
		}),
		Metrics: metricsFunc(func(ctx context.Context, metric ProbeMetric) error {
			checkDeadline(ctx)
			if metric.Name != ProbeMetricName || metric.RoundTripBytes != ProbeNonceBytes || len(metric.PathFingerprint) != 64 || metric.NetworkHostsValidated != 6 {
				return errors.New("unexpected redacted metric")
			}
			if metric.EvidenceMode == EvidenceLive && (!metric.Evidence.NATHealthy || metric.Evidence.EnduranceMinutes < 30 || !metric.Evidence.CredentialLifecyclePassed || !metric.Evidence.BackendRegressionPassed) {
				return errors.New("live metric omitted safe evidence summary")
			}
			return nil
		}),
		Clock:        clock,
		liveObserver: observer,
	}
}

func fakeProbeClock() func() time.Time {
	var calls atomic.Int64
	return func() time.Time {
		switch calls.Add(1) {
		case 1:
			return fixedProbeTime
		case 2:
			return fixedProbeTime.Add(time.Second)
		case 3:
			return fixedProbeTime.Add(2 * time.Second)
		case 4:
			return fixedProbeTime.Add(3 * time.Second)
		default:
			return fixedProbeTime.Add(35 * time.Minute)
		}
	}
}

type topologyFunc func(context.Context, TopologyExpectation) (TopologyObservation, error)

func (function topologyFunc) Inspect(ctx context.Context, expectation TopologyExpectation) (TopologyObservation, error) {
	return function(ctx, expectation)
}

type endpointFunc func(context.Context, string) ([]TLSEndpoint, error)

func (function endpointFunc) Discover(ctx context.Context, channelARN string) ([]TLSEndpoint, error) {
	return function(ctx, channelARN)
}

type iceFunc func(context.Context, string) (TURNCredentials, error)

func (function iceFunc) GetTURN(ctx context.Context, channelARN string) (TURNCredentials, error) {
	return function(ctx, channelARN)
}

type networkFunc func(context.Context, []NetworkTarget) ([]ConnectivityObservation, error)

func (function networkFunc) Check(ctx context.Context, targets []NetworkTarget) ([]ConnectivityObservation, error) {
	return function(ctx, targets)
}

type relayFunc func(context.Context, RelayRequest) (RelayObservation, error)

func (function relayFunc) RoundTrip(ctx context.Context, request RelayRequest) (RelayObservation, error) {
	return function(ctx, request)
}

type metricsFunc func(context.Context, ProbeMetric) error

func (function metricsFunc) Record(ctx context.Context, metric ProbeMetric) error {
	return function(ctx, metric)
}
