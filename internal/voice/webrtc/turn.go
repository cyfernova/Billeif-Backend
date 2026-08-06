package webrtc

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	RequiredLiveProbeAcknowledgement = "I_ACKNOWLEDGE_LIVE_AGENTCORE_TURN_PROBE_MAY_INCUR_COSTS"
	MumbaiRegion                     = "ap-south-1"
	ProbeNonceBytes                  = 32
	ProbeMetricName                  = "voice.turn_probe.round_trip"

	EndpointHTTPS = "HTTPS"
	EndpointWSS   = "WSS"

	CandidateRelay = "relay"
	CandidateHost  = "host"

	SarvamAPIHost = "api.sarvam.ai"

	LiveEvidenceEnvelopeVersion = "billeif-agentcore-turn-live-evidence-v2"
)

const liveEvidenceSignatureDomain = LiveEvidenceEnvelopeVersion + "\x00"

const maxProbeTimeout = 45 * time.Minute

const requiredConnectivityTargetCount = 6

const (
	// NATCPUUtilizationP95Limit is an exclusive p95 CPU pass threshold.
	NATCPUUtilizationP95Limit = 60.0
	// NATMaxConntrackUtilizationPercent is the reviewed peak conntrack pass threshold.
	NATMaxConntrackUtilizationPercent = 70.0
	// NATMaxNetworkErrorCount requires an error-free observed network window.
	NATMaxNetworkErrorCount uint64 = 0
	// NATMaxPacketDropCount requires an observed window without interface packet drops.
	NATMaxPacketDropCount uint64 = 0
	// NATMaxRejectedFlowCount requires the reviewed flow-log window to have no rejected flows.
	NATMaxRejectedFlowCount uint64 = 0
	// NATMinimumObservedTraffic requires the evidence to include real relay traffic.
	NATMinimumObservedTraffic uint64 = 1
	// MinimumTURNEndurance is the continuous relay-only endurance pass threshold.
	MinimumTURNEndurance = 30 * time.Minute
)

var (
	ErrLiveProbeBuildDisabled           = errors.New("voice TURN live probe build gate is disabled")
	ErrLiveProbeAcknowledgementRequired = errors.New("voice TURN live probe acknowledgement is required")
	ErrInvalidProbeConfig               = errors.New("voice TURN probe configuration is invalid")
	ErrInvalidTopology                  = errors.New("voice TURN probe topology is invalid")
	ErrInvalidTLSEndpoint               = errors.New("voice TURN probe TLS endpoint is invalid")
	ErrInvalidTURNCredentials           = errors.New("voice TURN probe credentials are invalid")
	ErrTopologyInspectionFailed         = errors.New("voice TURN probe topology inspection failed")
	ErrEndpointDiscoveryFailed          = errors.New("voice TURN probe endpoint discovery failed")
	ErrTURNCredentialRequestFailed      = errors.New("voice TURN probe credential request failed")
	ErrRelayRoundTripFailed             = errors.New("voice TURN probe relay round trip failed")
	ErrMetricsRecordingFailed           = errors.New("voice TURN probe metric recording failed")
	ErrNetworkCheckFailed               = errors.New("voice TURN probe network check failed")
	ErrInvalidNetworkObservation        = errors.New("voice TURN probe network observation is invalid")
	ErrEvidenceCollectionFailed         = errors.New("voice TURN probe evidence collection failed")
	ErrInvalidLiveEvidence              = errors.New("voice TURN probe live evidence is invalid")
	ErrLiveAttestationFailed            = errors.New("voice TURN probe live attestation failed")
	ErrLiveEvidenceReplay               = errors.New("voice TURN probe live evidence replay detected")
)

type EvidenceMode string

const (
	EvidenceSynthetic EvidenceMode = "SYNTHETIC"
	EvidenceLive      EvidenceMode = "LIVE"
)

type ProbeStatus string

const (
	ProbeStatusPassed ProbeStatus = "PASSED"
	ProbeStatusFailed ProbeStatus = "FAILED"
)

type PrivatePath struct {
	SubnetID           string
	RouteTableID       string
	AvailabilityZoneID string
}

type TopologyExpectation struct {
	Paths           []PrivatePath
	NATENIID        string
	RuntimeSubnetID string
}

type ObservedPrivatePath struct {
	PrivatePath
	DefaultRouteNATENIID string
}

type NATObservation struct {
	ENIID                   string
	SourceDestCheck         bool
	SourceDestCheckObserved bool
}

type TopologyObservation struct {
	Paths           []ObservedPrivatePath
	NAT             NATObservation
	RuntimeSubnetID string
	ArtifactSHA256  string
}

type TLSEndpoint struct {
	Protocol string
	URL      string
}

type TURNCredentials struct {
	uris      []string
	username  string
	password  string
	ExpiresAt time.Time
}

func (credentials TURNCredentials) String() string {
	return fmt.Sprintf("turn_credentials{uri_count=%d,expires_at=%s}", len(credentials.uris), credentials.ExpiresAt.UTC().Format(time.RFC3339))
}

func (credentials TURNCredentials) GoString() string {
	return credentials.String()
}

func (credentials TURNCredentials) MarshalJSON() ([]byte, error) {
	type redactedCredentials struct {
		URIcount  int       `json:"uri_count"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	return json.Marshal(redactedCredentials{URIcount: len(credentials.uris), ExpiresAt: credentials.ExpiresAt})
}

type RelayRequest struct {
	uri       string
	username  string
	password  string
	payload   []byte
	RelayOnly bool
}

func (request RelayRequest) String() string {
	hostFingerprint := ""
	if host, ok := parseUDP443TURNURI(MumbaiRegion, request.uri); ok {
		hostFingerprint = hashString(host)
	}
	return fmt.Sprintf(
		"relay_request{host_sha256=%s,transport=udp,port=443,payload_bytes=%d,relay_only=%t}",
		hostFingerprint,
		len(request.payload),
		request.RelayOnly,
	)
}

func (request RelayRequest) GoString() string {
	return request.String()
}

func (request RelayRequest) MarshalJSON() ([]byte, error) {
	hostFingerprint := ""
	if host, ok := parseUDP443TURNURI(MumbaiRegion, request.uri); ok {
		hostFingerprint = hashString(host)
	}
	type redactedRequest struct {
		HostSHA256   string `json:"host_sha256,omitempty"`
		Transport    string `json:"transport"`
		Port         uint16 `json:"port"`
		PayloadBytes int    `json:"payload_bytes"`
		RelayOnly    bool   `json:"relay_only"`
	}
	return json.Marshal(redactedRequest{
		HostSHA256:   hostFingerprint,
		Transport:    "udp",
		Port:         443,
		PayloadBytes: len(request.payload),
		RelayOnly:    request.RelayOnly,
	})
}

type RedactedICE struct {
	HostSHA256 []string  `json:"host_sha256"`
	Transport  string    `json:"transport"`
	Port       uint16    `json:"port"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func RedactICE(credentials TURNCredentials) (RedactedICE, error) {
	redacted := RedactedICE{
		HostSHA256: make([]string, 0, len(credentials.uris)),
		Transport:  "udp",
		Port:       443,
		ExpiresAt:  credentials.ExpiresAt,
	}
	for _, rawURI := range credentials.uris {
		host, ok := parseUDP443TURNURI(MumbaiRegion, rawURI)
		if !ok {
			return RedactedICE{}, ErrInvalidTURNCredentials
		}
		redacted.HostSHA256 = append(redacted.HostSHA256, hashString(host))
	}
	sort.Strings(redacted.HostSHA256)
	return redacted, nil
}

type RelayObservation struct {
	payload             []byte
	LocalCandidateType  string
	RemoteCandidateType string
	ArtifactSHA256      string
}

func (observation RelayObservation) String() string {
	return fmt.Sprintf(
		"relay_observation{payload_bytes=%d,local_candidate_type=%s,remote_candidate_type=%s,artifact_sha256=%s}",
		len(observation.payload),
		observation.LocalCandidateType,
		observation.RemoteCandidateType,
		observation.ArtifactSHA256,
	)
}

func (observation RelayObservation) GoString() string {
	return observation.String()
}

func (observation RelayObservation) MarshalJSON() ([]byte, error) {
	type redactedObservation struct {
		PayloadBytes        int    `json:"payload_bytes"`
		LocalCandidateType  string `json:"local_candidate_type"`
		RemoteCandidateType string `json:"remote_candidate_type"`
		ArtifactSHA256      string `json:"artifact_sha256"`
	}
	return json.Marshal(redactedObservation{
		PayloadBytes:        len(observation.payload),
		LocalCandidateType:  observation.LocalCandidateType,
		RemoteCandidateType: observation.RemoteCandidateType,
		ArtifactSHA256:      observation.ArtifactSHA256,
	})
}

type NetworkTargetKind string

const (
	NetworkKVSControl     NetworkTargetKind = "KVS_CONTROL"
	NetworkSecretsManager NetworkTargetKind = "SECRETS_MANAGER"
	NetworkCloudWatchLogs NetworkTargetKind = "CLOUDWATCH_LOGS"
	NetworkSarvam         NetworkTargetKind = "SARVAM"
	NetworkBilleifBackend NetworkTargetKind = "BILLEIF_BACKEND"
	NetworkKVSDiscovered  NetworkTargetKind = "KVS_DISCOVERED"
)

type NetworkTarget struct {
	Kind NetworkTargetKind
	Host string
	Port uint16
}

type ConnectivityObservation struct {
	Kind            NetworkTargetKind
	Host            string
	Port            uint16
	DNSResolved     bool
	TCP443Reachable bool
	ObservedAt      time.Time
	ArtifactSHA256  string `json:"-"`
}

func (observation ConnectivityObservation) String() string {
	return fmt.Sprintf(
		"network_observation{kind=%s,host_sha256=%s,port=%d,dns_resolved=%t,tcp_443_reachable=%t,observed_at=%s}",
		observation.Kind,
		hashString(observation.Host),
		observation.Port,
		observation.DNSResolved,
		observation.TCP443Reachable,
		observation.ObservedAt.UTC().Format(time.RFC3339),
	)
}

func (observation ConnectivityObservation) GoString() string {
	return observation.String()
}

func (observation ConnectivityObservation) MarshalJSON() ([]byte, error) {
	type redactedObservation struct {
		Kind            NetworkTargetKind `json:"kind"`
		HostSHA256      string            `json:"host_sha256"`
		Port            uint16            `json:"port"`
		DNSResolved     bool              `json:"dns_resolved"`
		TCP443Reachable bool              `json:"tcp_443_reachable"`
		ObservedAt      time.Time         `json:"observed_at"`
	}
	return json.Marshal(redactedObservation{
		Kind:            observation.Kind,
		HostSHA256:      hashString(observation.Host),
		Port:            observation.Port,
		DNSResolved:     observation.DNSResolved,
		TCP443Reachable: observation.TCP443Reachable,
		ObservedAt:      observation.ObservedAt,
	})
}

type ObservationArtifact struct {
	ArtifactSHA256 string `json:"-"`
	WindowStart    time.Time
	WindowEnd      time.Time
	ObservedAt     time.Time
}

type NATHealthObservation struct {
	ObservationArtifact
	CPUObserved                    bool
	CPUUtilizationP95Percent       float64
	CPUCreditsObserved             bool
	CPUCreditThrottled             bool
	CPUSurplusCreditsCharged       bool
	NetworkObserved                bool
	NetworkBytesProcessed          uint64
	NetworkErrorCount              uint64
	ConntrackObserved              bool
	PeakConntrackEntries           uint64
	MaxConntrackUtilizationPercent float64
	DropsObserved                  bool
	PacketDropCount                uint64
	StatusObserved                 bool
	InstanceStatusOK               bool
	SystemStatusOK                 bool
	ENAObserved                    bool
	ENAEnabled                     bool
	FlowLogsObserved               bool
	FlowLogsEnabled                bool
	FlowLogRecords                 uint64
	RejectedFlowCount              uint64
}

type EnduranceObservation struct {
	ObservationArtifact
	Duration               time.Duration
	Continuous             bool
	RelayOnly              bool
	RoundTrips             uint64
	BytesRoundTripped      uint64
	TURNAllocations        uint64
	TURNAllocationFailures uint64
	ReconnectAttempts      uint64
	ReconnectFailures      uint64
}

type CredentialLifecycleObservation struct {
	ObservationArtifact
	CredentialExpiresAt              time.Time
	EstablishedAcrossExpiryObserved  bool
	EstablishedAcrossExpirySucceeded bool
	NewAllocationObserved            bool
	NewAllocationSucceeded           bool
	RefreshObserved                  bool
	RefreshSucceeded                 bool
	RestartObserved                  bool
	RestartOutcome                   RestartOutcome
	RestartSucceeded                 bool
}

type RestartOutcome string

const (
	RestartOutcomeNone RestartOutcome = "none"
	RestartOutcomeICE  RestartOutcome = "ice"
	RestartOutcomePeer RestartOutcome = "peer"
)

type BackendRegressionObservation struct {
	ObservationArtifact
	NoRegression        bool
	BaselineObserved    bool
	InTestObserved      bool
	PostCleanupObserved bool
	TestsRun            uint64
	TestsFailed         uint64
}

type LiveEvidenceObservation struct {
	NAT                 NATHealthObservation
	Endurance           EnduranceObservation
	CredentialLifecycle CredentialLifecycleObservation
	BackendRegression   BackendRegressionObservation
}

type EvidenceRequest struct {
	RunID             string
	CampaignID        string
	SourceRevision    string
	ImageDigest       string
	ObserverIdentity  string
	ObserverKeyID     string
	ChangeWindowStart time.Time
	ChangeWindowEnd   time.Time
	PathFingerprint   string
	RunStartedAt      time.Time
	ICE               RedactedICE
}

type EvidenceSummary struct {
	NATHealthy                bool `json:"nat_healthy"`
	EnduranceMinutes          int  `json:"endurance_minutes"`
	CredentialLifecyclePassed bool `json:"credential_lifecycle_passed"`
	BackendRegressionPassed   bool `json:"backend_regression_passed"`
}

type RedactedNetworkTarget struct {
	Kind       NetworkTargetKind `json:"kind"`
	HostSHA256 string            `json:"host_sha256"`
	Port       uint16            `json:"port"`
}

type LiveConnectivityEvidence struct {
	Kind           NetworkTargetKind `json:"kind"`
	HostSHA256     string            `json:"host_sha256"`
	Port           uint16            `json:"port"`
	ArtifactSHA256 string            `json:"artifact_sha256"`
}

type LiveEvidenceArtifacts struct {
	TopologySHA256            string                     `json:"topology_sha256"`
	Connectivity              []LiveConnectivityEvidence `json:"connectivity"`
	RelaySHA256               string                     `json:"relay_sha256"`
	NATSHA256                 string                     `json:"nat_sha256"`
	EnduranceSHA256           string                     `json:"endurance_sha256"`
	CredentialLifecycleSHA256 string                     `json:"credential_lifecycle_sha256"`
	BackendRegressionSHA256   string                     `json:"backend_regression_sha256"`
}

type LiveEvidenceClaims struct {
	Version                string                `json:"version"`
	RunID                  string                `json:"run_id"`
	CampaignID             string                `json:"campaign_id"`
	SourceRevision         string                `json:"source_revision"`
	ImageDigest            string                `json:"image_digest"`
	ObserverIdentity       string                `json:"observer_identity"`
	ObserverKeyID          string                `json:"observer_key_id"`
	ChangeWindowStart      time.Time             `json:"change_window_start"`
	ChangeWindowEnd        time.Time             `json:"change_window_end"`
	RunStartedAt           time.Time             `json:"run_started_at"`
	NetworkCompletedAt     time.Time             `json:"network_completed_at"`
	CredentialAcquiredAt   time.Time             `json:"credential_acquired_at"`
	RelayCompletedAt       time.Time             `json:"relay_completed_at"`
	CompletedAt            time.Time             `json:"completed_at"`
	PathFingerprint        string                `json:"path_fingerprint"`
	ICE                    RedactedICE           `json:"ice"`
	CredentialExpiryMargin time.Duration         `json:"credential_expiry_margin"`
	ValidatedPaths         int                   `json:"validated_paths"`
	TLSEndpoints           int                   `json:"tls_endpoints"`
	NetworkHostsValidated  int                   `json:"network_hosts_validated"`
	RoundTripBytes         int                   `json:"round_trip_bytes"`
	RelayOnly              bool                  `json:"relay_only"`
	Artifacts              LiveEvidenceArtifacts `json:"artifacts"`
	Evidence               EvidenceSummary       `json:"evidence"`
}

type LiveEvidenceEnvelope struct {
	Claims    LiveEvidenceClaims `json:"claims"`
	Signature string             `json:"signature"`
}

type TrustedObserver struct {
	Identity  string            `json:"identity"`
	KeyID     string            `json:"key_id"`
	PublicKey ed25519.PublicKey `json:"-"`
}

type LiveReleasePathPolicy struct {
	PathFingerprint     string
	ConnectivityTargets []RedactedNetworkTarget
}

type LiveReleasePolicy struct {
	CampaignID        string
	SourceRevision    string
	ImageDigest       string
	ChangeWindowStart time.Time
	ChangeWindowEnd   time.Time
	MaxEvidenceAge    time.Duration
	ExpectedPaths     []LiveReleasePathPolicy
	AllowedObservers  []TrustedObserver
}

type LiveEvidenceReplayGuard interface {
	Reserve(context.Context, string, []string, time.Time) error
}

type ProbeMetric struct {
	Name                  string          `json:"name"`
	EvidenceMode          EvidenceMode    `json:"evidence_mode"`
	PathFingerprint       string          `json:"path_fingerprint"`
	ValidatedPaths        int             `json:"validated_paths"`
	TLSEndpoints          int             `json:"tls_endpoints"`
	RoundTripBytes        int             `json:"round_trip_bytes"`
	RelayOnly             bool            `json:"relay_only"`
	NetworkHostsValidated int             `json:"network_hosts_validated"`
	Evidence              EvidenceSummary `json:"evidence"`
}

type Config struct {
	RuntimeAcknowledgement   string
	EvidenceMode             EvidenceMode
	CampaignID               string
	SourceRevision           string
	ImageDigest              string
	ExpectedObserverIdentity string
	ExpectedObserverKeyID    string
	ChangeWindowStart        time.Time
	ChangeWindowEnd          time.Time
	Region                   string
	ChannelARN               string
	Topology                 TopologyExpectation
	ExactTLSHosts            []string
	BilleifBackendHost       string
	OverallTimeout           time.Duration
	TopologyTimeout          time.Duration
	EndpointTimeout          time.Duration
	CredentialTimeout        time.Duration
	RelayTimeout             time.Duration
	MetricsTimeout           time.Duration
	NetworkTimeout           time.Duration
	EvidenceTimeout          time.Duration
	EvidenceMaxAge           time.Duration
	CredentialExpiryMargin   time.Duration
}

type TopologyInspector interface {
	Inspect(context.Context, TopologyExpectation) (TopologyObservation, error)
}

type EndpointSource interface {
	Discover(context.Context, string) ([]TLSEndpoint, error)
}

type ICECredentialSource interface {
	GetTURN(context.Context, string) (TURNCredentials, error)
}

type NetworkChecker interface {
	Check(context.Context, []NetworkTarget) ([]ConnectivityObservation, error)
}

type liveEvidenceObserver interface {
	collect(context.Context, EvidenceRequest) (LiveEvidenceObservation, error)
	attest(context.Context, LiveEvidenceClaims) (LiveEvidenceEnvelope, error)
	descriptor() TrustedObserver
	now() time.Time
	liveEvidenceObserverSeal()
}

type RelayClient interface {
	RoundTrip(context.Context, RelayRequest) (RelayObservation, error)
}

type MetricsSink interface {
	Record(context.Context, ProbeMetric) error
}

type Dependencies struct {
	Topology  TopologyInspector
	Endpoints EndpointSource
	ICE       ICECredentialSource
	Network   NetworkChecker
	Relay     RelayClient
	Metrics   MetricsSink
	Clock     func() time.Time

	liveObserver liveEvidenceObserver
}

type Probe struct {
	dependencies Dependencies
}

func NewProbe(dependencies Dependencies) *Probe {
	if dependencies.Clock == nil {
		dependencies.Clock = time.Now
	}
	return &Probe{dependencies: dependencies}
}

type ProbeResult struct {
	Status                ProbeStatus           `json:"status"`
	EvidenceMode          EvidenceMode          `json:"evidence_mode"`
	PathFingerprint       string                `json:"path_fingerprint"`
	ValidatedPaths        int                   `json:"validated_paths"`
	TLSEndpoints          int                   `json:"tls_endpoints"`
	RoundTripBytes        int                   `json:"round_trip_bytes"`
	RelayOnly             bool                  `json:"relay_only"`
	CheckedAt             time.Time             `json:"checked_at"`
	ICE                   RedactedICE           `json:"ice"`
	NetworkHostsValidated int                   `json:"network_hosts_validated"`
	Evidence              EvidenceSummary       `json:"evidence"`
	Envelope              *LiveEvidenceEnvelope `json:"envelope,omitempty"`
}

func (result ProbeResult) String() string {
	return fmt.Sprintf(
		"voice_turn_probe{status=%s,evidence=%s,path_fingerprint=%s,validated_paths=%d,tls_endpoints=%d,network_hosts=%d,round_trip_bytes=%d,relay_only=%t,nat_healthy=%t,endurance_minutes=%d,credential_lifecycle=%t,backend_regression=%t}",
		result.Status,
		result.EvidenceMode,
		result.PathFingerprint,
		result.ValidatedPaths,
		result.TLSEndpoints,
		result.NetworkHostsValidated,
		result.RoundTripBytes,
		result.RelayOnly,
		result.Evidence.NATHealthy,
		result.Evidence.EnduranceMinutes,
		result.Evidence.CredentialLifecyclePassed,
		result.Evidence.BackendRegressionPassed,
	)
}

func (result ProbeResult) GoString() string {
	return result.String()
}

func (result ProbeResult) MarshalJSON() ([]byte, error) {
	type redactedResult struct {
		Status                ProbeStatus           `json:"status"`
		EvidenceMode          EvidenceMode          `json:"evidence_mode"`
		PathFingerprint       string                `json:"path_fingerprint"`
		ValidatedPaths        int                   `json:"validated_paths"`
		TLSEndpoints          int                   `json:"tls_endpoints"`
		RoundTripBytes        int                   `json:"round_trip_bytes"`
		RelayOnly             bool                  `json:"relay_only"`
		CheckedAt             time.Time             `json:"checked_at"`
		ICE                   RedactedICE           `json:"ice"`
		NetworkHostsValidated int                   `json:"network_hosts_validated"`
		Evidence              EvidenceSummary       `json:"evidence"`
		Envelope              *LiveEvidenceEnvelope `json:"envelope,omitempty"`
	}

	return json.Marshal(redactedResult(result))
}

func (probe *Probe) Run(parent context.Context, config Config) (ProbeResult, error) {
	failed := ProbeResult{Status: ProbeStatusFailed, EvidenceMode: config.EvidenceMode}
	if !liveProbeBuildEnabled {
		return failed, ErrLiveProbeBuildDisabled
	}
	if config.RuntimeAcknowledgement != RequiredLiveProbeAcknowledgement {
		return failed, ErrLiveProbeAcknowledgementRequired
	}
	if parent == nil || probe == nil || validateConfig(config) != nil || probe.validateDependencies(config.EvidenceMode) != nil {
		return failed, ErrInvalidProbeConfig
	}

	overallContext, cancelOverall := context.WithTimeout(parent, config.OverallTimeout)
	defer cancelOverall()
	if err := overallContext.Err(); err != nil {
		return failed, err
	}
	runStartedAt := probe.now(config.EvidenceMode)

	topologyContext, cancelTopology := context.WithTimeout(overallContext, config.TopologyTimeout)
	topology, err := probe.dependencies.Topology.Inspect(topologyContext, cloneTopologyExpectation(config.Topology))
	if err = finishDependency(topologyContext, cancelTopology, err, ErrTopologyInspectionFailed); err != nil {
		return failed, err
	}
	if err := ValidateTopology(config.Topology, topology); err != nil {
		return failed, ErrInvalidTopology
	}

	endpointContext, cancelEndpoints := context.WithTimeout(overallContext, config.EndpointTimeout)
	endpoints, err := probe.dependencies.Endpoints.Discover(endpointContext, config.ChannelARN)
	if err = finishDependency(endpointContext, cancelEndpoints, err, ErrEndpointDiscoveryFailed); err != nil {
		return failed, err
	}
	if err := ValidateTLSEndpoints(config.Region, config.ExactTLSHosts, endpoints); err != nil {
		return failed, ErrInvalidTLSEndpoint
	}
	targets, err := buildNetworkTargets(config, endpoints)
	if err != nil {
		return failed, ErrInvalidTLSEndpoint
	}
	networkContext, cancelNetwork := context.WithTimeout(overallContext, config.NetworkTimeout)
	network, err := probe.dependencies.Network.Check(networkContext, append([]NetworkTarget(nil), targets...))
	if err = finishDependency(networkContext, cancelNetwork, err, ErrNetworkCheckFailed); err != nil {
		return failed, err
	}
	networkCompletedAt := probe.now(config.EvidenceMode)
	if networkCompletedAt.Before(runStartedAt) || networkCompletedAt.Sub(runStartedAt) > config.OverallTimeout {
		return failed, ErrInvalidNetworkObservation
	}
	if err := ValidateNetworkObservations(runStartedAt, networkCompletedAt, config.EvidenceMaxAge, targets, network); err != nil {
		return failed, ErrInvalidNetworkObservation
	}

	credentialContext, cancelCredentials := context.WithTimeout(overallContext, config.CredentialTimeout)
	credentials, err := probe.dependencies.ICE.GetTURN(credentialContext, config.ChannelARN)
	if err = finishDependency(credentialContext, cancelCredentials, err, ErrTURNCredentialRequestFailed); err != nil {
		return failed, err
	}
	credentialAcquiredAt := probe.now(config.EvidenceMode)
	if credentialAcquiredAt.Before(networkCompletedAt) || credentialAcquiredAt.Sub(runStartedAt) > config.OverallTimeout {
		return failed, ErrInvalidTURNCredentials
	}
	if err := ValidateTURNCredentials(config.Region, credentialAcquiredAt, config.CredentialExpiryMargin, credentials); err != nil {
		return failed, ErrInvalidTURNCredentials
	}
	redactedICE, err := RedactICE(credentials)
	if err != nil {
		return failed, ErrInvalidTURNCredentials
	}

	payload := make([]byte, ProbeNonceBytes)
	if _, err := rand.Read(payload); err != nil {
		return failed, ErrRelayRoundTripFailed
	}
	relayContext, cancelRelay := context.WithTimeout(overallContext, config.RelayTimeout)
	observation, err := probe.dependencies.Relay.RoundTrip(relayContext, RelayRequest{
		uri:       credentials.uris[0],
		username:  credentials.username,
		password:  credentials.password,
		payload:   bytes.Clone(payload),
		RelayOnly: true,
	})
	if err = finishDependency(relayContext, cancelRelay, err, ErrRelayRoundTripFailed); err != nil {
		return failed, err
	}
	relayCompletedAt := probe.now(config.EvidenceMode)
	if relayCompletedAt.Before(credentialAcquiredAt) || relayCompletedAt.Sub(runStartedAt) > config.OverallTimeout ||
		ValidateTURNCredentials(config.Region, relayCompletedAt, config.CredentialExpiryMargin, credentials) != nil {
		return failed, ErrInvalidTURNCredentials
	}
	if observation.LocalCandidateType != CandidateRelay || observation.RemoteCandidateType != CandidateRelay ||
		!bytes.Equal(observation.payload, payload) || !isSHA256(observation.ArtifactSHA256) {
		return failed, ErrRelayRoundTripFailed
	}

	pathFingerprint := stablePathFingerprint(config.Topology)
	result := ProbeResult{
		Status:                ProbeStatusPassed,
		EvidenceMode:          config.EvidenceMode,
		PathFingerprint:       pathFingerprint,
		ValidatedPaths:        len(topology.Paths),
		TLSEndpoints:          len(endpoints),
		RoundTripBytes:        len(payload),
		RelayOnly:             true,
		CheckedAt:             relayCompletedAt,
		ICE:                   redactedICE,
		NetworkHostsValidated: len(network),
	}
	if result.EvidenceMode == EvidenceLive {
		observer := probe.dependencies.liveObserver
		descriptor := observer.descriptor()
		if !validTrustedObserver(descriptor) || descriptor.Identity != config.ExpectedObserverIdentity || descriptor.KeyID != config.ExpectedObserverKeyID ||
			runStartedAt.Before(config.ChangeWindowStart) || !runStartedAt.Before(config.ChangeWindowEnd) {
			return failed, ErrInvalidLiveEvidence
		}
		runID, runIDErr := randomEvidenceID()
		if runIDErr != nil {
			return failed, ErrLiveAttestationFailed
		}
		evidenceContext, cancelEvidence := context.WithTimeout(overallContext, config.EvidenceTimeout)
		evidence, evidenceErr := observer.collect(evidenceContext, EvidenceRequest{
			RunID:             runID,
			CampaignID:        config.CampaignID,
			SourceRevision:    config.SourceRevision,
			ImageDigest:       config.ImageDigest,
			ObserverIdentity:  descriptor.Identity,
			ObserverKeyID:     descriptor.KeyID,
			ChangeWindowStart: config.ChangeWindowStart,
			ChangeWindowEnd:   config.ChangeWindowEnd,
			PathFingerprint:   result.PathFingerprint,
			RunStartedAt:      runStartedAt,
			ICE:               cloneRedactedICE(result.ICE),
		})
		if evidenceErr != nil || evidenceContext.Err() != nil {
			evidenceErr = finishDependency(evidenceContext, cancelEvidence, evidenceErr, ErrEvidenceCollectionFailed)
			return failed, evidenceErr
		}
		completedAt := probe.now(EvidenceLive)
		if !completedAt.After(relayCompletedAt) || completedAt.Sub(runStartedAt) > config.OverallTimeout || completedAt.After(config.ChangeWindowEnd) {
			cancelEvidence()
			return failed, ErrInvalidLiveEvidence
		}
		summary, validationErr := ValidateLiveEvidence(runStartedAt, completedAt, config.EvidenceMaxAge, result.ICE, evidence)
		if validationErr != nil {
			cancelEvidence()
			return failed, ErrInvalidLiveEvidence
		}
		result.Evidence = summary
		result.CheckedAt = completedAt
		claims := buildLiveEvidenceClaims(
			config,
			descriptor,
			runID,
			runStartedAt,
			networkCompletedAt,
			credentialAcquiredAt,
			relayCompletedAt,
			completedAt,
			result,
			topology,
			network,
			observation,
			evidence,
		)
		if !validateLiveEvidenceClaimShape(completedAt, config.EvidenceMaxAge, claims) {
			cancelEvidence()
			return failed, ErrInvalidLiveEvidence
		}
		if metricsErr := probe.recordMetrics(overallContext, config.MetricsTimeout, result); metricsErr != nil {
			cancelEvidence()
			return failed, metricsErr
		}
		if contextErr := overallContext.Err(); contextErr != nil {
			cancelEvidence()
			return failed, errors.Join(ErrLiveAttestationFailed, contextErr)
		}
		if contextErr := evidenceContext.Err(); contextErr != nil {
			cancelEvidence()
			return failed, errors.Join(ErrLiveAttestationFailed, contextErr)
		}
		attestationStartedAt := probe.now(EvidenceLive)
		if attestationStartedAt.Before(completedAt) || attestationStartedAt.Sub(runStartedAt) > config.OverallTimeout ||
			attestationStartedAt.After(config.ChangeWindowEnd) || !validateLiveEvidenceClaimShape(attestationStartedAt, config.EvidenceMaxAge, claims) {
			cancelEvidence()
			return failed, ErrInvalidLiveEvidence
		}
		envelope, attestationErr := observer.attest(evidenceContext, claims)
		if attestationErr = finishDependency(evidenceContext, cancelEvidence, attestationErr, ErrLiveAttestationFailed); attestationErr != nil {
			return failed, attestationErr
		}
		if !sameLiveEvidenceClaims(envelope.Claims, claims) || !verifyLiveEvidenceSignature(envelope, descriptor.PublicKey) {
			return failed, ErrInvalidLiveEvidence
		}
		result.Envelope = &envelope
		return result, nil
	}
	if err := probe.recordMetrics(overallContext, config.MetricsTimeout, result); err != nil {
		return failed, err
	}
	return result, nil
}

func (probe *Probe) recordMetrics(parent context.Context, timeout time.Duration, result ProbeResult) error {
	metricsContext, cancelMetrics := context.WithTimeout(parent, timeout)
	err := probe.dependencies.Metrics.Record(metricsContext, ProbeMetric{
		Name:                  ProbeMetricName,
		EvidenceMode:          result.EvidenceMode,
		PathFingerprint:       result.PathFingerprint,
		ValidatedPaths:        result.ValidatedPaths,
		TLSEndpoints:          result.TLSEndpoints,
		RoundTripBytes:        result.RoundTripBytes,
		RelayOnly:             result.RelayOnly,
		NetworkHostsValidated: result.NetworkHostsValidated,
		Evidence:              result.Evidence,
	})
	if err = finishDependency(metricsContext, cancelMetrics, err, ErrMetricsRecordingFailed); err != nil {
		return err
	}
	return nil
}

func finishDependency(ctx context.Context, cancel context.CancelFunc, providerErr, redactedErr error) error {
	contextErr := ctx.Err()
	cancel()
	if contextErr != nil {
		return errors.Join(redactedErr, contextErr)
	}
	if errors.Is(providerErr, context.Canceled) {
		return errors.Join(redactedErr, context.Canceled)
	}
	if errors.Is(providerErr, context.DeadlineExceeded) {
		return errors.Join(redactedErr, context.DeadlineExceeded)
	}
	if providerErr != nil {
		return redactedErr
	}
	return nil
}

func (probe *Probe) now(mode EvidenceMode) time.Time {
	if mode == EvidenceLive && probe.dependencies.liveObserver != nil {
		return probe.dependencies.liveObserver.now().UTC()
	}
	return probe.dependencies.Clock().UTC()
}

func (probe *Probe) validateDependencies(mode EvidenceMode) error {
	if probe.dependencies.Topology == nil ||
		probe.dependencies.Endpoints == nil ||
		probe.dependencies.ICE == nil ||
		probe.dependencies.Network == nil ||
		probe.dependencies.Relay == nil ||
		probe.dependencies.Metrics == nil ||
		probe.dependencies.Clock == nil {
		return ErrInvalidProbeConfig
	}
	if mode == EvidenceLive && probe.dependencies.liveObserver == nil {
		return ErrInvalidProbeConfig
	}
	return nil
}

func validateConfig(config Config) error {
	if config.EvidenceMode != EvidenceSynthetic && config.EvidenceMode != EvidenceLive {
		return ErrInvalidProbeConfig
	}
	if config.EvidenceMode == EvidenceLive && (!isSafeIdentifier(config.CampaignID) || !isSourceRevision(config.SourceRevision) ||
		!isImageDigest(config.ImageDigest) || !isSafeIdentifier(config.ExpectedObserverIdentity) || !isSafeIdentifier(config.ExpectedObserverKeyID) ||
		!isUTC(config.ChangeWindowStart) || !isUTC(config.ChangeWindowEnd) || !config.ChangeWindowEnd.After(config.ChangeWindowStart) ||
		config.ChangeWindowEnd.Sub(config.ChangeWindowStart) < MinimumTURNEndurance || config.ChangeWindowEnd.Sub(config.ChangeWindowStart) > 24*time.Hour) {
		return ErrInvalidProbeConfig
	}
	if config.Region != MumbaiRegion || config.ChannelARN == "" || len(config.ExactTLSHosts) == 0 ||
		!isStrictDNSName(config.BilleifBackendHost) || net.ParseIP(config.BilleifBackendHost) != nil {
		return ErrInvalidProbeConfig
	}
	if validateExpectedTopology(config.Topology) != nil {
		return ErrInvalidProbeConfig
	}
	if len(config.ExactTLSHosts) != 1 || config.ExactTLSHosts[0] != kvsControlPlaneHost(config.Region) {
		return ErrInvalidProbeConfig
	}
	if isMandatoryConnectivityHost(config.Region, config.BilleifBackendHost) {
		return ErrInvalidProbeConfig
	}
	if config.OverallTimeout <= 0 || config.OverallTimeout > maxProbeTimeout ||
		config.TopologyTimeout <= 0 || config.TopologyTimeout > config.OverallTimeout ||
		config.EndpointTimeout <= 0 || config.EndpointTimeout > config.OverallTimeout ||
		config.CredentialTimeout <= 0 || config.CredentialTimeout > config.OverallTimeout ||
		config.RelayTimeout <= 0 || config.RelayTimeout > config.OverallTimeout ||
		config.MetricsTimeout <= 0 || config.MetricsTimeout > config.OverallTimeout ||
		config.NetworkTimeout <= 0 || config.NetworkTimeout > config.OverallTimeout ||
		config.EvidenceTimeout <= 0 || config.EvidenceTimeout > config.OverallTimeout ||
		config.EvidenceMaxAge < MinimumTURNEndurance || config.EvidenceMaxAge > 24*time.Hour ||
		config.CredentialExpiryMargin <= 0 {
		return ErrInvalidProbeConfig
	}
	return nil
}

func ValidateTopology(expectation TopologyExpectation, observation TopologyObservation) error {
	if validateExpectedTopology(expectation) != nil || len(observation.Paths) != 2 {
		return ErrInvalidTopology
	}
	if observation.NAT.ENIID != expectation.NATENIID ||
		!observation.NAT.SourceDestCheckObserved ||
		observation.NAT.SourceDestCheck ||
		observation.RuntimeSubnetID != expectation.RuntimeSubnetID ||
		!isSHA256(observation.ArtifactSHA256) {
		return ErrInvalidTopology
	}

	expectedBySubnet := make(map[string]PrivatePath, len(expectation.Paths))
	for _, path := range expectation.Paths {
		expectedBySubnet[path.SubnetID] = path
	}
	seen := make(map[string]struct{}, len(observation.Paths))
	for _, observed := range observation.Paths {
		if _, duplicate := seen[observed.SubnetID]; duplicate {
			return ErrInvalidTopology
		}
		seen[observed.SubnetID] = struct{}{}
		expected, ok := expectedBySubnet[observed.SubnetID]
		if !ok || observed.PrivatePath != expected || observed.DefaultRouteNATENIID != expectation.NATENIID {
			return ErrInvalidTopology
		}
	}
	return nil
}

func validateExpectedTopology(expectation TopologyExpectation) error {
	if len(expectation.Paths) != 2 || expectation.NATENIID == "" || expectation.RuntimeSubnetID == "" {
		return ErrInvalidTopology
	}
	supportedZones := map[string]struct{}{"aps1-az1": {}, "aps1-az2": {}, "aps1-az3": {}}
	subnets := make(map[string]struct{}, 2)
	routeTables := make(map[string]struct{}, 2)
	zones := make(map[string]struct{}, 2)
	runtimeSubnetFound := false
	for _, path := range expectation.Paths {
		if path.SubnetID == "" || path.RouteTableID == "" {
			return ErrInvalidTopology
		}
		if _, ok := supportedZones[path.AvailabilityZoneID]; !ok {
			return ErrInvalidTopology
		}
		if _, duplicate := subnets[path.SubnetID]; duplicate {
			return ErrInvalidTopology
		}
		if _, duplicate := routeTables[path.RouteTableID]; duplicate {
			return ErrInvalidTopology
		}
		if _, duplicate := zones[path.AvailabilityZoneID]; duplicate {
			return ErrInvalidTopology
		}
		subnets[path.SubnetID] = struct{}{}
		routeTables[path.RouteTableID] = struct{}{}
		zones[path.AvailabilityZoneID] = struct{}{}
		if path.SubnetID == expectation.RuntimeSubnetID {
			runtimeSubnetFound = true
		}
	}
	if !runtimeSubnetFound {
		return ErrInvalidTopology
	}
	return nil
}

func ValidateTLSEndpoints(region string, exactHosts []string, endpoints []TLSEndpoint) error {
	if region != MumbaiRegion || len(endpoints) != 2 {
		return ErrInvalidTLSEndpoint
	}
	if len(exactHosts) != 1 || exactHosts[0] != kvsControlPlaneHost(region) {
		return ErrInvalidTLSEndpoint
	}
	requiredProtocols := map[string]bool{EndpointHTTPS: false, EndpointWSS: false}
	for _, endpoint := range endpoints {
		expectedScheme, ok := map[string]string{EndpointHTTPS: "https", EndpointWSS: "wss"}[endpoint.Protocol]
		if !ok || requiredProtocols[endpoint.Protocol] {
			return ErrInvalidTLSEndpoint
		}
		parsed, err := url.Parse(endpoint.URL)
		if err != nil || parsed.Scheme != expectedScheme || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" ||
			parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
			return ErrInvalidTLSEndpoint
		}
		host := parsed.Hostname()
		if !isStrictDNSName(host) || net.ParseIP(host) != nil || (parsed.Port() != "" && parsed.Port() != "443") {
			return ErrInvalidTLSEndpoint
		}
		if host != exactHosts[0] && !isRegionBoundKVSHost(region, host) {
			return ErrInvalidTLSEndpoint
		}
		requiredProtocols[endpoint.Protocol] = true
	}
	if !requiredProtocols[EndpointHTTPS] || !requiredProtocols[EndpointWSS] {
		return ErrInvalidTLSEndpoint
	}
	return nil
}

func buildNetworkTargets(config Config, endpoints []TLSEndpoint) ([]NetworkTarget, error) {
	if err := ValidateTLSEndpoints(config.Region, config.ExactTLSHosts, endpoints); err != nil {
		return nil, ErrInvalidTLSEndpoint
	}
	targets := []NetworkTarget{
		{Kind: NetworkKVSControl, Host: kvsControlPlaneHost(config.Region), Port: 443},
		{Kind: NetworkSecretsManager, Host: "secretsmanager." + config.Region + ".amazonaws.com", Port: 443},
		{Kind: NetworkCloudWatchLogs, Host: "logs." + config.Region + ".amazonaws.com", Port: 443},
		{Kind: NetworkSarvam, Host: SarvamAPIHost, Port: 443},
		{Kind: NetworkBilleifBackend, Host: config.BilleifBackendHost, Port: 443},
	}
	dynamicHosts := make(map[string]struct{}, 1)
	controlHost := kvsControlPlaneHost(config.Region)
	for _, endpoint := range endpoints {
		parsed, err := url.Parse(endpoint.URL)
		if err != nil || parsed.Hostname() == "" || (parsed.Port() != "" && parsed.Port() != "443") {
			return nil, ErrInvalidTLSEndpoint
		}
		host := parsed.Hostname()
		if host == controlHost {
			continue
		}
		if !isRegionBoundKVSHost(config.Region, host) {
			return nil, ErrInvalidTLSEndpoint
		}
		dynamicHosts[host] = struct{}{}
	}
	if len(dynamicHosts) != 1 {
		return nil, ErrInvalidTLSEndpoint
	}
	for host := range dynamicHosts {
		targets = append(targets, NetworkTarget{Kind: NetworkKVSDiscovered, Host: host, Port: 443})
	}
	if !validMandatoryNetworkTargets(targets) {
		return nil, ErrInvalidTLSEndpoint
	}
	return targets, nil
}

func validMandatoryNetworkTargets(targets []NetworkTarget) bool {
	if len(targets) != requiredConnectivityTargetCount {
		return false
	}
	seenKinds := make(map[NetworkTargetKind]struct{}, requiredConnectivityTargetCount)
	seenHosts := make(map[string]struct{}, requiredConnectivityTargetCount)
	for _, target := range targets {
		if !isMandatoryNetworkTargetKind(target.Kind) || !isStrictDNSName(target.Host) || target.Port != 443 {
			return false
		}
		if _, duplicate := seenKinds[target.Kind]; duplicate {
			return false
		}
		if _, duplicate := seenHosts[target.Host]; duplicate {
			return false
		}
		seenKinds[target.Kind] = struct{}{}
		seenHosts[target.Host] = struct{}{}
	}
	return true
}

func ValidateNetworkObservations(windowStart, windowEnd time.Time, maxAge time.Duration, targets []NetworkTarget, observations []ConnectivityObservation) error {
	if !isUTC(windowStart) || !isUTC(windowEnd) || windowEnd.Before(windowStart) || windowEnd.Sub(windowStart) > maxAge ||
		maxAge <= 0 || len(targets) == 0 || len(observations) != len(targets) {
		return ErrInvalidNetworkObservation
	}
	expected := make(map[string]NetworkTarget, len(targets))
	for _, target := range targets {
		if target.Port != 443 || !isStrictDNSName(target.Host) || net.ParseIP(target.Host) != nil {
			return ErrInvalidNetworkObservation
		}
		if _, duplicate := expected[target.Host]; duplicate {
			return ErrInvalidNetworkObservation
		}
		expected[target.Host] = target
	}
	seen := make(map[string]struct{}, len(observations))
	seenDigests := make(map[string]struct{}, len(observations))
	for _, observation := range observations {
		if observation.Port != 443 || !observation.DNSResolved || !observation.TCP443Reachable ||
			!isStrictDNSName(observation.Host) || net.ParseIP(observation.Host) != nil ||
			!validEvidencePoint(windowStart, windowEnd, observation.ObservedAt) || !isSHA256(observation.ArtifactSHA256) {
			return ErrInvalidNetworkObservation
		}
		if _, duplicate := seen[observation.Host]; duplicate {
			return ErrInvalidNetworkObservation
		}
		if _, duplicate := seenDigests[observation.ArtifactSHA256]; duplicate {
			return ErrInvalidNetworkObservation
		}
		seen[observation.Host] = struct{}{}
		seenDigests[observation.ArtifactSHA256] = struct{}{}
		target, ok := expected[observation.Host]
		if !ok || target.Kind != observation.Kind || target.Port != observation.Port {
			return ErrInvalidNetworkObservation
		}
	}
	return nil
}

func ValidateLiveEvidence(runStartedAt, completedAt time.Time, maxAge time.Duration, ice RedactedICE, observation LiveEvidenceObservation) (EvidenceSummary, error) {
	artifacts := []ObservationArtifact{
		observation.NAT.ObservationArtifact,
		observation.Endurance.ObservationArtifact,
		observation.CredentialLifecycle.ObservationArtifact,
		observation.BackendRegression.ObservationArtifact,
	}
	if !isUTC(runStartedAt) || !isUTC(completedAt) || !completedAt.After(runStartedAt) ||
		completedAt.Sub(runStartedAt) < MinimumTURNEndurance || completedAt.Sub(runStartedAt) > maxAge ||
		!isUTC(ice.ExpiresAt) {
		return EvidenceSummary{}, ErrInvalidLiveEvidence
	}
	seenDigests := make(map[string]struct{}, len(artifacts))
	windowStart := artifacts[0].WindowStart
	windowEnd := artifacts[0].WindowEnd
	for _, artifact := range artifacts {
		if !validateObservationArtifact(runStartedAt, completedAt, maxAge, artifact) || artifact.WindowStart != windowStart || artifact.WindowEnd != windowEnd {
			return EvidenceSummary{}, ErrInvalidLiveEvidence
		}
		if _, duplicate := seenDigests[artifact.ArtifactSHA256]; duplicate {
			return EvidenceSummary{}, ErrInvalidLiveEvidence
		}
		seenDigests[artifact.ArtifactSHA256] = struct{}{}
	}

	nat := observation.NAT
	if !nat.CPUObserved || math.IsNaN(nat.CPUUtilizationP95Percent) || math.IsInf(nat.CPUUtilizationP95Percent, 0) ||
		nat.CPUUtilizationP95Percent < 0 || nat.CPUUtilizationP95Percent >= NATCPUUtilizationP95Limit ||
		!nat.CPUCreditsObserved || nat.CPUCreditThrottled || nat.CPUSurplusCreditsCharged ||
		!nat.NetworkObserved || nat.NetworkBytesProcessed < NATMinimumObservedTraffic || nat.NetworkErrorCount > NATMaxNetworkErrorCount ||
		!nat.ConntrackObserved || nat.PeakConntrackEntries < NATMinimumObservedTraffic || math.IsNaN(nat.MaxConntrackUtilizationPercent) ||
		math.IsInf(nat.MaxConntrackUtilizationPercent, 0) || nat.MaxConntrackUtilizationPercent < 0 ||
		nat.MaxConntrackUtilizationPercent >= NATMaxConntrackUtilizationPercent ||
		!nat.DropsObserved || nat.PacketDropCount > NATMaxPacketDropCount ||
		!nat.StatusObserved || !nat.InstanceStatusOK || !nat.SystemStatusOK ||
		!nat.ENAObserved || !nat.ENAEnabled ||
		!nat.FlowLogsObserved || !nat.FlowLogsEnabled || nat.FlowLogRecords < NATMinimumObservedTraffic || nat.RejectedFlowCount > NATMaxRejectedFlowCount {
		return EvidenceSummary{}, ErrInvalidLiveEvidence
	}

	endurance := observation.Endurance
	if endurance.Duration < MinimumTURNEndurance || endurance.Duration > windowEnd.Sub(windowStart) ||
		!endurance.Continuous || !endurance.RelayOnly || endurance.RoundTrips == 0 || endurance.BytesRoundTripped == 0 ||
		endurance.TURNAllocations == 0 || endurance.TURNAllocationFailures != 0 ||
		endurance.ReconnectAttempts == 0 || endurance.ReconnectFailures != 0 {
		return EvidenceSummary{}, ErrInvalidLiveEvidence
	}

	lifecycle := observation.CredentialLifecycle
	if !isUTC(lifecycle.CredentialExpiresAt) || !lifecycle.CredentialExpiresAt.Equal(ice.ExpiresAt) ||
		!lifecycle.CredentialExpiresAt.After(windowStart) || !lifecycle.CredentialExpiresAt.Before(windowEnd) ||
		!lifecycle.EstablishedAcrossExpiryObserved || !lifecycle.EstablishedAcrossExpirySucceeded ||
		!lifecycle.NewAllocationObserved || !lifecycle.NewAllocationSucceeded ||
		!lifecycle.RefreshObserved || !lifecycle.RefreshSucceeded || !validRestartObservation(lifecycle) {
		return EvidenceSummary{}, ErrInvalidLiveEvidence
	}

	backend := observation.BackendRegression
	if !backend.NoRegression || !backend.BaselineObserved || !backend.InTestObserved || !backend.PostCleanupObserved ||
		backend.TestsRun == 0 || backend.TestsFailed != 0 {
		return EvidenceSummary{}, ErrInvalidLiveEvidence
	}

	return EvidenceSummary{
		NATHealthy:                true,
		EnduranceMinutes:          int(endurance.Duration / time.Minute),
		CredentialLifecyclePassed: true,
		BackendRegressionPassed:   true,
	}, nil
}

func validateObservationArtifact(runStartedAt, completedAt time.Time, maxAge time.Duration, artifact ObservationArtifact) bool {
	return isSHA256(artifact.ArtifactSHA256) &&
		isUTC(artifact.WindowStart) && isUTC(artifact.WindowEnd) && isUTC(artifact.ObservedAt) &&
		artifact.WindowStart.Equal(runStartedAt) && artifact.WindowEnd.Sub(artifact.WindowStart) >= MinimumTURNEndurance &&
		!artifact.WindowEnd.After(completedAt) && completedAt.Sub(artifact.WindowStart) <= maxAge &&
		!artifact.ObservedAt.Before(artifact.WindowEnd) && !artifact.ObservedAt.After(completedAt)
}

func validRestartObservation(lifecycle CredentialLifecycleObservation) bool {
	if !lifecycle.RestartObserved {
		return false
	}
	switch lifecycle.RestartOutcome {
	case RestartOutcomeNone:
		return !lifecycle.RestartSucceeded
	case RestartOutcomeICE, RestartOutcomePeer:
		return lifecycle.RestartSucceeded
	default:
		return false
	}
}

func validEvidencePoint(windowStart, windowEnd, observedAt time.Time) bool {
	return isUTC(windowStart) && isUTC(windowEnd) && isUTC(observedAt) &&
		!observedAt.Before(windowStart) && !observedAt.After(windowEnd)
}

func isUTC(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC
}

func isSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func buildLiveEvidenceClaims(
	config Config,
	observer TrustedObserver,
	runID string,
	runStartedAt time.Time,
	networkCompletedAt time.Time,
	credentialAcquiredAt time.Time,
	relayCompletedAt time.Time,
	completedAt time.Time,
	result ProbeResult,
	topology TopologyObservation,
	network []ConnectivityObservation,
	relay RelayObservation,
	evidence LiveEvidenceObservation,
) LiveEvidenceClaims {
	connectivity := make([]LiveConnectivityEvidence, 0, len(network))
	for _, item := range network {
		connectivity = append(connectivity, LiveConnectivityEvidence{
			Kind:           item.Kind,
			HostSHA256:     hashString(item.Host),
			Port:           item.Port,
			ArtifactSHA256: item.ArtifactSHA256,
		})
	}
	sort.Slice(connectivity, func(first, second int) bool {
		if connectivity[first].Kind != connectivity[second].Kind {
			return connectivity[first].Kind < connectivity[second].Kind
		}
		return connectivity[first].HostSHA256 < connectivity[second].HostSHA256
	})
	return LiveEvidenceClaims{
		Version:                LiveEvidenceEnvelopeVersion,
		RunID:                  runID,
		CampaignID:             config.CampaignID,
		SourceRevision:         config.SourceRevision,
		ImageDigest:            config.ImageDigest,
		ObserverIdentity:       observer.Identity,
		ObserverKeyID:          observer.KeyID,
		ChangeWindowStart:      config.ChangeWindowStart,
		ChangeWindowEnd:        config.ChangeWindowEnd,
		RunStartedAt:           runStartedAt,
		NetworkCompletedAt:     networkCompletedAt,
		CredentialAcquiredAt:   credentialAcquiredAt,
		RelayCompletedAt:       relayCompletedAt,
		CompletedAt:            completedAt,
		PathFingerprint:        result.PathFingerprint,
		ICE:                    cloneRedactedICE(result.ICE),
		CredentialExpiryMargin: config.CredentialExpiryMargin,
		ValidatedPaths:         result.ValidatedPaths,
		TLSEndpoints:           result.TLSEndpoints,
		NetworkHostsValidated:  result.NetworkHostsValidated,
		RoundTripBytes:         result.RoundTripBytes,
		RelayOnly:              result.RelayOnly,
		Artifacts: LiveEvidenceArtifacts{
			TopologySHA256:            topology.ArtifactSHA256,
			Connectivity:              connectivity,
			RelaySHA256:               relay.ArtifactSHA256,
			NATSHA256:                 evidence.NAT.ArtifactSHA256,
			EnduranceSHA256:           evidence.Endurance.ArtifactSHA256,
			CredentialLifecycleSHA256: evidence.CredentialLifecycle.ArtifactSHA256,
			BackendRegressionSHA256:   evidence.BackendRegression.ArtifactSHA256,
		},
		Evidence: result.Evidence,
	}
}

func cloneRedactedICE(value RedactedICE) RedactedICE {
	return RedactedICE{
		HostSHA256: append([]string(nil), value.HostSHA256...),
		Transport:  value.Transport,
		Port:       value.Port,
		ExpiresAt:  value.ExpiresAt,
	}
}

func randomEvidenceID() (string, error) {
	value := make([]byte, sha256.Size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", value), nil
}

func sameLiveEvidenceClaims(first, second LiveEvidenceClaims) bool {
	firstPayload, firstErr := json.Marshal(first)
	secondPayload, secondErr := json.Marshal(second)
	return firstErr == nil && secondErr == nil && bytes.Equal(firstPayload, secondPayload)
}

func canonicalLiveEvidenceMessage(claims LiveEvidenceClaims) ([]byte, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return nil, err
	}
	return append([]byte(liveEvidenceSignatureDomain), payload...), nil
}

func verifyLiveEvidenceSignature(envelope LiveEvidenceEnvelope, publicKey ed25519.PublicKey) bool {
	if len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	signature, err := base64.RawStdEncoding.DecodeString(envelope.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return false
	}
	message, err := canonicalLiveEvidenceMessage(envelope.Claims)
	return err == nil && ed25519.Verify(publicKey, message, signature)
}

func VerifyLiveRelease(ctx context.Context, policy LiveReleasePolicy, replay LiveEvidenceReplayGuard, envelopes ...LiveEvidenceEnvelope) error {
	return verifyLiveReleaseAt(ctx, time.Now().UTC(), policy, replay, envelopes...)
}

func verifyLiveReleaseAt(ctx context.Context, now time.Time, policy LiveReleasePolicy, replay LiveEvidenceReplayGuard, envelopes ...LiveEvidenceEnvelope) error {
	if ctx == nil || replay == nil || len(envelopes) != 2 || !validLiveReleasePolicy(now, policy) {
		return ErrInvalidLiveEvidence
	}
	trusted := make(map[string]TrustedObserver, len(policy.AllowedObservers))
	for _, observer := range policy.AllowedObservers {
		if !validTrustedObserver(observer) {
			return ErrInvalidLiveEvidence
		}
		key := observer.Identity + "\x00" + observer.KeyID
		if _, duplicate := trusted[key]; duplicate {
			return ErrInvalidLiveEvidence
		}
		observer.PublicKey = append(ed25519.PublicKey(nil), observer.PublicKey...)
		trusted[key] = observer
	}

	runIDs := make(map[string]struct{}, 2)
	paths := make(map[string]struct{}, 2)
	expectedPaths := make(map[string]struct{}, len(policy.ExpectedPaths))
	for _, expectedPath := range policy.ExpectedPaths {
		expectedPaths[expectedPath.PathFingerprint] = struct{}{}
	}
	artifacts := make(map[string]struct{})
	envelopeDigests := make([]string, 0, 2)
	for _, envelope := range envelopes {
		claims := envelope.Claims
		if !validateLiveEvidenceClaims(now, policy, claims) {
			return ErrInvalidLiveEvidence
		}
		observer, ok := trusted[claims.ObserverIdentity+"\x00"+claims.ObserverKeyID]
		if !ok || !verifyLiveEvidenceSignature(envelope, observer.PublicKey) {
			return ErrInvalidLiveEvidence
		}
		if _, duplicate := runIDs[claims.RunID]; duplicate {
			return ErrInvalidLiveEvidence
		}
		runIDs[claims.RunID] = struct{}{}
		if _, duplicate := paths[claims.PathFingerprint]; duplicate {
			return ErrInvalidLiveEvidence
		}
		paths[claims.PathFingerprint] = struct{}{}
		for _, digest := range allLiveArtifactDigests(claims.Artifacts) {
			if _, duplicate := artifacts[digest]; duplicate {
				return ErrInvalidLiveEvidence
			}
			artifacts[digest] = struct{}{}
		}
		digest, err := liveEnvelopeSHA256(envelope)
		if err != nil {
			return ErrInvalidLiveEvidence
		}
		envelopeDigests = append(envelopeDigests, digest)
	}
	if len(paths) != len(expectedPaths) || envelopeDigests[0] == envelopeDigests[1] {
		return ErrInvalidLiveEvidence
	}
	for pathFingerprint := range expectedPaths {
		if _, ok := paths[pathFingerprint]; !ok {
			return ErrInvalidLiveEvidence
		}
	}
	if err := replay.Reserve(ctx, policy.CampaignID, append([]string(nil), envelopeDigests...), policy.ChangeWindowEnd); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return errors.Join(ErrLiveEvidenceReplay, contextErr)
		}
		return ErrLiveEvidenceReplay
	}
	return nil
}

func validLiveReleasePolicy(now time.Time, policy LiveReleasePolicy) bool {
	return isUTC(now) && isSafeIdentifier(policy.CampaignID) && isSourceRevision(policy.SourceRevision) && isImageDigest(policy.ImageDigest) &&
		isUTC(policy.ChangeWindowStart) && isUTC(policy.ChangeWindowEnd) && policy.ChangeWindowEnd.After(policy.ChangeWindowStart) &&
		!now.Before(policy.ChangeWindowStart) && !now.After(policy.ChangeWindowEnd) &&
		policy.MaxEvidenceAge > 0 && policy.MaxEvidenceAge <= policy.ChangeWindowEnd.Sub(policy.ChangeWindowStart) &&
		validExpectedPaths(policy.ExpectedPaths) && len(policy.AllowedObservers) > 0
}

func validateLiveEvidenceClaims(now time.Time, policy LiveReleasePolicy, claims LiveEvidenceClaims) bool {
	expectedPath, ok := findExpectedPath(policy.ExpectedPaths, claims.PathFingerprint)
	if !ok {
		return false
	}
	return validateLiveEvidenceClaimShape(now, policy.MaxEvidenceAge, claims) &&
		claims.CampaignID == policy.CampaignID && claims.SourceRevision == policy.SourceRevision && claims.ImageDigest == policy.ImageDigest &&
		claims.ChangeWindowStart.Equal(policy.ChangeWindowStart) && claims.ChangeWindowEnd.Equal(policy.ChangeWindowEnd) &&
		connectivityTargetsMatchPolicy(expectedPath.ConnectivityTargets, claims.Artifacts.Connectivity)
}

func validateLiveEvidenceClaimShape(now time.Time, maxEvidenceAge time.Duration, claims LiveEvidenceClaims) bool {
	if claims.Version != LiveEvidenceEnvelopeVersion || !isSHA256(claims.RunID) || !isSHA256(claims.PathFingerprint) ||
		!isSafeIdentifier(claims.CampaignID) || !isSourceRevision(claims.SourceRevision) || !isImageDigest(claims.ImageDigest) ||
		!isSafeIdentifier(claims.ObserverIdentity) || !isSafeIdentifier(claims.ObserverKeyID) ||
		!isUTC(claims.ChangeWindowStart) || !isUTC(claims.ChangeWindowEnd) || !claims.ChangeWindowEnd.After(claims.ChangeWindowStart) ||
		!isUTC(claims.RunStartedAt) || !isUTC(claims.NetworkCompletedAt) || !isUTC(claims.CredentialAcquiredAt) ||
		!isUTC(claims.RelayCompletedAt) || !isUTC(claims.CompletedAt) ||
		claims.RunStartedAt.Before(claims.ChangeWindowStart) || !claims.RunStartedAt.Before(claims.ChangeWindowEnd) ||
		claims.NetworkCompletedAt.Before(claims.RunStartedAt) || claims.CredentialAcquiredAt.Before(claims.NetworkCompletedAt) ||
		claims.RelayCompletedAt.Before(claims.CredentialAcquiredAt) || !claims.CompletedAt.After(claims.RelayCompletedAt) ||
		claims.CompletedAt.After(claims.ChangeWindowEnd) || claims.CompletedAt.Sub(claims.RunStartedAt) < MinimumTURNEndurance ||
		claims.CompletedAt.Sub(claims.RunStartedAt) > maxProbeTimeout || !isUTC(now) || maxEvidenceAge <= 0 ||
		claims.CompletedAt.After(now) || now.Sub(claims.CompletedAt) > maxEvidenceAge {
		return false
	}
	if claims.ValidatedPaths != 2 || claims.TLSEndpoints != 2 || claims.NetworkHostsValidated != requiredConnectivityTargetCount ||
		claims.RoundTripBytes != ProbeNonceBytes || !claims.RelayOnly || claims.CredentialExpiryMargin <= 0 ||
		claims.ICE.Transport != "udp" || claims.ICE.Port != 443 || !isUTC(claims.ICE.ExpiresAt) ||
		!claims.ICE.ExpiresAt.After(claims.RelayCompletedAt.Add(claims.CredentialExpiryMargin)) || !claims.ICE.ExpiresAt.Before(claims.CompletedAt) ||
		len(claims.ICE.HostSHA256) == 0 || len(claims.Artifacts.Connectivity) != requiredConnectivityTargetCount ||
		!claims.Evidence.NATHealthy || claims.Evidence.EnduranceMinutes < int(MinimumTURNEndurance/time.Minute) ||
		!claims.Evidence.CredentialLifecyclePassed || !claims.Evidence.BackendRegressionPassed {
		return false
	}
	seenICEHosts := make(map[string]struct{}, len(claims.ICE.HostSHA256))
	for _, digest := range claims.ICE.HostSHA256 {
		if !isSHA256(digest) {
			return false
		}
		if _, duplicate := seenICEHosts[digest]; duplicate {
			return false
		}
		seenICEHosts[digest] = struct{}{}
	}
	seenConnectivityHosts := make(map[string]struct{}, requiredConnectivityTargetCount)
	seenConnectivityKinds := make(map[NetworkTargetKind]struct{}, requiredConnectivityTargetCount)
	seenConnectivityTargets := make(map[string]struct{}, requiredConnectivityTargetCount)
	for _, item := range claims.Artifacts.Connectivity {
		if !isMandatoryNetworkTargetKind(item.Kind) || !isSHA256(item.HostSHA256) || item.Port != 443 || !isSHA256(item.ArtifactSHA256) {
			return false
		}
		if _, duplicate := seenConnectivityHosts[item.HostSHA256]; duplicate {
			return false
		}
		if _, duplicate := seenConnectivityKinds[item.Kind]; duplicate {
			return false
		}
		targetKey := redactedNetworkTargetKey(item.Kind, item.HostSHA256, item.Port)
		if _, duplicate := seenConnectivityTargets[targetKey]; duplicate {
			return false
		}
		seenConnectivityHosts[item.HostSHA256] = struct{}{}
		seenConnectivityKinds[item.Kind] = struct{}{}
		seenConnectivityTargets[targetKey] = struct{}{}
	}
	seenArtifacts := make(map[string]struct{})
	for _, digest := range allLiveArtifactDigests(claims.Artifacts) {
		if !isSHA256(digest) {
			return false
		}
		if _, duplicate := seenArtifacts[digest]; duplicate {
			return false
		}
		seenArtifacts[digest] = struct{}{}
	}
	return true
}

func validExpectedPaths(paths []LiveReleasePathPolicy) bool {
	if len(paths) != 2 {
		return false
	}
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if !isSHA256(path.PathFingerprint) || !validExpectedConnectivityTargets(path.ConnectivityTargets) {
			return false
		}
		if _, duplicate := seen[path.PathFingerprint]; duplicate {
			return false
		}
		seen[path.PathFingerprint] = struct{}{}
	}
	return true
}

func validExpectedConnectivityTargets(targets []RedactedNetworkTarget) bool {
	if len(targets) != requiredConnectivityTargetCount {
		return false
	}
	seenHosts := make(map[string]struct{}, requiredConnectivityTargetCount)
	seenKinds := make(map[NetworkTargetKind]struct{}, requiredConnectivityTargetCount)
	seenTargets := make(map[string]struct{}, requiredConnectivityTargetCount)
	for _, target := range targets {
		if !isMandatoryNetworkTargetKind(target.Kind) || !isSHA256(target.HostSHA256) || target.Port != 443 {
			return false
		}
		if _, duplicate := seenHosts[target.HostSHA256]; duplicate {
			return false
		}
		if _, duplicate := seenKinds[target.Kind]; duplicate {
			return false
		}
		key := redactedNetworkTargetKey(target.Kind, target.HostSHA256, target.Port)
		if _, duplicate := seenTargets[key]; duplicate {
			return false
		}
		seenHosts[target.HostSHA256] = struct{}{}
		seenKinds[target.Kind] = struct{}{}
		seenTargets[key] = struct{}{}
	}
	return true
}

func connectivityTargetsMatchPolicy(expected []RedactedNetworkTarget, observed []LiveConnectivityEvidence) bool {
	if len(expected) != len(observed) {
		return false
	}
	expectedSet := make(map[string]struct{}, len(expected))
	for _, target := range expected {
		expectedSet[redactedNetworkTargetKey(target.Kind, target.HostSHA256, target.Port)] = struct{}{}
	}
	for _, item := range observed {
		if _, ok := expectedSet[redactedNetworkTargetKey(item.Kind, item.HostSHA256, item.Port)]; !ok {
			return false
		}
	}
	return true
}

func isMandatoryNetworkTargetKind(kind NetworkTargetKind) bool {
	switch kind {
	case NetworkKVSControl, NetworkSecretsManager, NetworkCloudWatchLogs, NetworkSarvam, NetworkBilleifBackend, NetworkKVSDiscovered:
		return true
	default:
		return false
	}
}

func redactedNetworkTargetKey(kind NetworkTargetKind, hostSHA256 string, port uint16) string {
	return fmt.Sprintf("%s\x00%s\x00%d", kind, hostSHA256, port)
}

func findExpectedPath(paths []LiveReleasePathPolicy, pathFingerprint string) (LiveReleasePathPolicy, bool) {
	for _, path := range paths {
		if path.PathFingerprint == pathFingerprint {
			return path, true
		}
	}
	return LiveReleasePathPolicy{}, false
}

func allLiveArtifactDigests(artifacts LiveEvidenceArtifacts) []string {
	values := make([]string, 0, len(artifacts.Connectivity)+6)
	values = append(values, artifacts.TopologySHA256)
	for _, item := range artifacts.Connectivity {
		values = append(values, item.ArtifactSHA256)
	}
	values = append(values,
		artifacts.RelaySHA256,
		artifacts.NATSHA256,
		artifacts.EnduranceSHA256,
		artifacts.CredentialLifecycleSHA256,
		artifacts.BackendRegressionSHA256,
	)
	return values
}

func liveEnvelopeSHA256(envelope LiveEvidenceEnvelope) (string, error) {
	message, err := canonicalLiveEvidenceMessage(envelope.Claims)
	if err != nil {
		return "", err
	}
	signature, err := base64.RawStdEncoding.DecodeString(envelope.Signature)
	if err != nil {
		return "", err
	}
	return hashString(string(message) + "\x00" + string(signature)), nil
}

func validTrustedObserver(observer TrustedObserver) bool {
	return isSafeIdentifier(observer.Identity) && isSafeIdentifier(observer.KeyID) && len(observer.PublicKey) == ed25519.PublicKeySize
}

func ValidateTURNCredentials(region string, now time.Time, expiryMargin time.Duration, credentials TURNCredentials) error {
	if region != MumbaiRegion || expiryMargin <= 0 || credentials.username == "" || credentials.password == "" || len(credentials.uris) == 0 {
		return ErrInvalidTURNCredentials
	}
	if !credentials.ExpiresAt.After(now.Add(expiryMargin)) {
		return ErrInvalidTURNCredentials
	}
	seen := make(map[string]struct{}, len(credentials.uris))
	for _, rawURI := range credentials.uris {
		if _, duplicate := seen[rawURI]; duplicate || !isUDP443TURNURI(region, rawURI) {
			return ErrInvalidTURNCredentials
		}
		seen[rawURI] = struct{}{}
	}
	return nil
}

func isUDP443TURNURI(region, rawURI string) bool {
	_, ok := parseUDP443TURNURI(region, rawURI)
	return ok
}

func parseUDP443TURNURI(region, rawURI string) (string, bool) {
	if !strings.HasPrefix(rawURI, "turn:") || strings.HasPrefix(rawURI, "turn://") || !isASCII(rawURI) {
		return "", false
	}
	body := strings.TrimPrefix(rawURI, "turn:")
	if strings.Count(body, "?") != 1 {
		return "", false
	}
	address, query, _ := strings.Cut(body, "?")
	if query != "transport=udp" || strings.ContainsAny(address, "/@#%") {
		return "", false
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "443" || net.ParseIP(host) != nil || !isStrictDNSName(host) {
		return "", false
	}
	if !isRegionBoundKVSTURNHost(region, host) {
		return "", false
	}
	return host, true
}

func isRegionBoundKVSHost(region, host string) bool {
	suffix := ".kinesisvideo." + region + ".amazonaws.com"
	if !strings.HasSuffix(host, suffix) {
		return false
	}
	label := strings.TrimSuffix(host, suffix)
	return label != "" && !strings.Contains(label, ".") && isDNSLabel(label)
}

// AWS Kinesis Video Streams documents signaling endpoints with one dynamic
// label and TURN endpoints with two dynamic labels before the regional
// service suffix. Keep the two host classes distinct so accepting a real TURN
// URI does not widen the HTTPS/WSS signaling boundary.
func isRegionBoundKVSTURNHost(region, host string) bool {
	suffix := ".kinesisvideo." + region + ".amazonaws.com"
	if !strings.HasSuffix(host, suffix) {
		return false
	}
	prefix := strings.TrimSuffix(host, suffix)
	labels := strings.Split(prefix, ".")
	return len(labels) == 2 && isDNSLabel(labels[0]) && isDNSLabel(labels[1])
}

func kvsControlPlaneHost(region string) string {
	return "kinesisvideo." + region + ".amazonaws.com"
}

func isMandatoryConnectivityHost(region, host string) bool {
	switch host {
	case kvsControlPlaneHost(region),
		"secretsmanager." + region + ".amazonaws.com",
		"logs." + region + ".amazonaws.com",
		SarvamAPIHost:
		return true
	default:
		return isRegionBoundKVSHost(region, host) || isRegionBoundKVSTURNHost(region, host)
	}
}

func isStrictDNSName(host string) bool {
	if host == "" || len(host) > 253 || !isASCII(host) || host != strings.ToLower(host) || strings.HasSuffix(host, ".") {
		return false
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if !isDNSLabel(label) {
			return false
		}
	}
	return true
}

func isDNSLabel(label string) bool {
	if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, character := range label {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

func isASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] > 0x7f {
			return false
		}
	}
	return true
}

func isSafeIdentifier(value string) bool {
	if len(value) < 3 || len(value) > 128 || !isASCII(value) {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' && character != '.' && character != ':' {
			return false
		}
	}
	return true
}

func isSourceRevision(value string) bool {
	return (len(value) == 40 || len(value) == 64) && isLowerHex(value)
}

func isImageDigest(value string) bool {
	return strings.HasPrefix(value, "sha256:") && len(value) == len("sha256:")+64 && isLowerHex(strings.TrimPrefix(value, "sha256:"))
}

func isLowerHex(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func cloneTopologyExpectation(expectation TopologyExpectation) TopologyExpectation {
	return TopologyExpectation{
		Paths:           append([]PrivatePath(nil), expectation.Paths...),
		NATENIID:        expectation.NATENIID,
		RuntimeSubnetID: expectation.RuntimeSubnetID,
	}
}

func stablePathFingerprint(expectation TopologyExpectation) string {
	for _, path := range expectation.Paths {
		if path.SubnetID == expectation.RuntimeSubnetID {
			return hashString(strings.Join([]string{
				"voice-turn-runtime-path-v1",
				path.SubnetID,
				path.RouteTableID,
				path.AvailabilityZoneID,
				expectation.NATENIID,
			}, "\x00"))
		}
	}
	return ""
}

func hashString(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest)
}
