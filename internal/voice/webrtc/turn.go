package webrtc

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
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
)

const maxProbeTimeout = 45 * time.Minute

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
}

type TLSEndpoint struct {
	Protocol string
	URL      string
}

type TURNCredentials struct {
	URIs      []string
	Username  string
	Password  string
	ExpiresAt time.Time
}

func (credentials TURNCredentials) String() string {
	return fmt.Sprintf("turn_credentials{uri_count=%d,expires_at=%s}", len(credentials.URIs), credentials.ExpiresAt.UTC().Format(time.RFC3339))
}

func (credentials TURNCredentials) GoString() string {
	return credentials.String()
}

func (credentials TURNCredentials) MarshalJSON() ([]byte, error) {
	type redactedCredentials struct {
		URIcount  int       `json:"uri_count"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	return json.Marshal(redactedCredentials{URIcount: len(credentials.URIs), ExpiresAt: credentials.ExpiresAt})
}

type RelayRequest struct {
	URI       string
	Username  string
	Password  string
	Payload   []byte
	RelayOnly bool
}

func (request RelayRequest) String() string {
	hostFingerprint := ""
	if host, ok := parseUDP443TURNURI(MumbaiRegion, request.URI); ok {
		hostFingerprint = hashString(host)
	}
	return fmt.Sprintf(
		"relay_request{host_sha256=%s,transport=udp,port=443,payload_bytes=%d,relay_only=%t}",
		hostFingerprint,
		len(request.Payload),
		request.RelayOnly,
	)
}

func (request RelayRequest) GoString() string {
	return request.String()
}

func (request RelayRequest) MarshalJSON() ([]byte, error) {
	hostFingerprint := ""
	if host, ok := parseUDP443TURNURI(MumbaiRegion, request.URI); ok {
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
		PayloadBytes: len(request.Payload),
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
		HostSHA256: make([]string, 0, len(credentials.URIs)),
		Transport:  "udp",
		Port:       443,
		ExpiresAt:  credentials.ExpiresAt,
	}
	for _, rawURI := range credentials.URIs {
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
	Payload             []byte
	LocalCandidateType  string
	RemoteCandidateType string
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
	PathFingerprint string
	RunStartedAt    time.Time
	ICE             RedactedICE
}

type EvidenceSummary struct {
	NATHealthy                bool `json:"nat_healthy"`
	EnduranceMinutes          int  `json:"endurance_minutes"`
	CredentialLifecyclePassed bool `json:"credential_lifecycle_passed"`
	BackendRegressionPassed   bool `json:"backend_regression_passed"`
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
	RuntimeAcknowledgement string
	EvidenceMode           EvidenceMode
	Region                 string
	ChannelARN             string
	Topology               TopologyExpectation
	ExactTLSHosts          []string
	BilleifBackendHost     string
	OverallTimeout         time.Duration
	TopologyTimeout        time.Duration
	EndpointTimeout        time.Duration
	CredentialTimeout      time.Duration
	RelayTimeout           time.Duration
	MetricsTimeout         time.Duration
	NetworkTimeout         time.Duration
	EvidenceTimeout        time.Duration
	EvidenceMaxAge         time.Duration
	CredentialExpiryMargin time.Duration
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

type EvidenceObserver interface {
	Collect(context.Context, EvidenceRequest) (LiveEvidenceObservation, error)
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
	Evidence  EvidenceObserver
	Relay     RelayClient
	Metrics   MetricsSink
	Clock     func() time.Time
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
	Status                ProbeStatus     `json:"status"`
	EvidenceMode          EvidenceMode    `json:"evidence_mode"`
	PathFingerprint       string          `json:"path_fingerprint"`
	ValidatedPaths        int             `json:"validated_paths"`
	TLSEndpoints          int             `json:"tls_endpoints"`
	RoundTripBytes        int             `json:"round_trip_bytes"`
	RelayOnly             bool            `json:"relay_only"`
	CheckedAt             time.Time       `json:"checked_at"`
	ICE                   RedactedICE     `json:"ice"`
	NetworkHostsValidated int             `json:"network_hosts_validated"`
	Evidence              EvidenceSummary `json:"evidence"`

	validatedLiveEvidence *ValidatedLiveEvidence
}

type ValidatedLiveEvidence struct {
	pathFingerprint   string
	artifactSetSHA256 string
	checkedAt         time.Time
	validated         bool
}

func (result ProbeResult) ValidatedLiveEvidence() (ValidatedLiveEvidence, bool) {
	if result.validatedLiveEvidence == nil {
		return ValidatedLiveEvidence{}, false
	}
	return *result.validatedLiveEvidence, true
}

func (evidence ValidatedLiveEvidence) PathFingerprint() string {
	return evidence.pathFingerprint
}

func LiveReleaseGateSatisfied(evidence ...ValidatedLiveEvidence) bool {
	if len(evidence) != 2 {
		return false
	}
	seen := make(map[string]struct{}, 2)
	seenArtifacts := make(map[string]struct{}, 2)
	for _, item := range evidence {
		if !item.validated || item.checkedAt.IsZero() || !isSHA256(item.pathFingerprint) || !isSHA256(item.artifactSetSHA256) {
			return false
		}
		if _, duplicate := seen[item.pathFingerprint]; duplicate {
			return false
		}
		seen[item.pathFingerprint] = struct{}{}
		if _, duplicate := seenArtifacts[item.artifactSetSHA256]; duplicate {
			return false
		}
		seenArtifacts[item.artifactSetSHA256] = struct{}{}
	}
	return len(seen) == 2 && len(seenArtifacts) == 2
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
		Status                ProbeStatus     `json:"status"`
		EvidenceMode          EvidenceMode    `json:"evidence_mode"`
		PathFingerprint       string          `json:"path_fingerprint"`
		ValidatedPaths        int             `json:"validated_paths"`
		TLSEndpoints          int             `json:"tls_endpoints"`
		RoundTripBytes        int             `json:"round_trip_bytes"`
		RelayOnly             bool            `json:"relay_only"`
		CheckedAt             time.Time       `json:"checked_at"`
		ICE                   RedactedICE     `json:"ice"`
		NetworkHostsValidated int             `json:"network_hosts_validated"`
		Evidence              EvidenceSummary `json:"evidence"`
	}

	return json.Marshal(redactedResult{
		Status:                result.Status,
		EvidenceMode:          result.EvidenceMode,
		PathFingerprint:       result.PathFingerprint,
		ValidatedPaths:        result.ValidatedPaths,
		TLSEndpoints:          result.TLSEndpoints,
		RoundTripBytes:        result.RoundTripBytes,
		RelayOnly:             result.RelayOnly,
		CheckedAt:             result.CheckedAt,
		ICE:                   result.ICE,
		NetworkHostsValidated: result.NetworkHostsValidated,
		Evidence:              result.Evidence,
	})
}

func (probe *Probe) Run(parent context.Context, config Config) (ProbeResult, error) {
	failed := ProbeResult{Status: ProbeStatusFailed, EvidenceMode: config.EvidenceMode}
	if !liveProbeBuildEnabled {
		return failed, ErrLiveProbeBuildDisabled
	}
	if config.RuntimeAcknowledgement != RequiredLiveProbeAcknowledgement {
		return failed, ErrLiveProbeAcknowledgementRequired
	}
	if parent == nil || probe == nil || validateConfig(config) != nil || probe.validateDependencies() != nil {
		return failed, ErrInvalidProbeConfig
	}

	overallContext, cancelOverall := context.WithTimeout(parent, config.OverallTimeout)
	defer cancelOverall()
	if err := overallContext.Err(); err != nil {
		return failed, err
	}
	runStartedAt := probe.dependencies.Clock().UTC()

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
		return failed, ErrInvalidProbeConfig
	}
	networkContext, cancelNetwork := context.WithTimeout(overallContext, config.NetworkTimeout)
	network, err := probe.dependencies.Network.Check(networkContext, append([]NetworkTarget(nil), targets...))
	if err = finishDependency(networkContext, cancelNetwork, err, ErrNetworkCheckFailed); err != nil {
		return failed, err
	}
	if err := ValidateNetworkObservations(runStartedAt, config.EvidenceMaxAge, targets, network); err != nil {
		return failed, ErrInvalidNetworkObservation
	}

	credentialContext, cancelCredentials := context.WithTimeout(overallContext, config.CredentialTimeout)
	credentials, err := probe.dependencies.ICE.GetTURN(credentialContext, config.ChannelARN)
	if err = finishDependency(credentialContext, cancelCredentials, err, ErrTURNCredentialRequestFailed); err != nil {
		return failed, err
	}
	if err := ValidateTURNCredentials(config.Region, runStartedAt, config.CredentialExpiryMargin, credentials); err != nil {
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
		URI:       credentials.URIs[0],
		Username:  credentials.Username,
		Password:  credentials.Password,
		Payload:   bytes.Clone(payload),
		RelayOnly: true,
	})
	if err = finishDependency(relayContext, cancelRelay, err, ErrRelayRoundTripFailed); err != nil {
		return failed, err
	}
	if observation.LocalCandidateType != CandidateRelay || observation.RemoteCandidateType != CandidateRelay || !bytes.Equal(observation.Payload, payload) {
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
		CheckedAt:             runStartedAt,
		ICE:                   redactedICE,
		NetworkHostsValidated: len(network),
	}
	artifactSetSHA256 := ""
	if result.EvidenceMode == EvidenceLive {
		evidenceContext, cancelEvidence := context.WithTimeout(overallContext, config.EvidenceTimeout)
		evidence, evidenceErr := probe.dependencies.Evidence.Collect(evidenceContext, EvidenceRequest{
			PathFingerprint: result.PathFingerprint,
			RunStartedAt:    runStartedAt,
			ICE:             result.ICE,
		})
		if evidenceErr = finishDependency(evidenceContext, cancelEvidence, evidenceErr, ErrEvidenceCollectionFailed); evidenceErr != nil {
			return failed, evidenceErr
		}
		completedAt := probe.dependencies.Clock().UTC()
		if !completedAt.After(runStartedAt) || completedAt.Sub(runStartedAt) > config.OverallTimeout {
			return failed, ErrInvalidLiveEvidence
		}
		summary, validationErr := ValidateLiveEvidence(runStartedAt, completedAt, config.EvidenceMaxAge, result.ICE, evidence)
		if validationErr != nil {
			return failed, ErrInvalidLiveEvidence
		}
		result.Evidence = summary
		result.CheckedAt = completedAt
		artifactSetSHA256 = liveArtifactSetSHA256(evidence)
	}
	metricsContext, cancelMetrics := context.WithTimeout(overallContext, config.MetricsTimeout)
	err = probe.dependencies.Metrics.Record(metricsContext, ProbeMetric{
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
		return failed, err
	}
	if result.EvidenceMode == EvidenceLive {
		result.validatedLiveEvidence = &ValidatedLiveEvidence{
			pathFingerprint:   result.PathFingerprint,
			artifactSetSHA256: artifactSetSHA256,
			checkedAt:         result.CheckedAt,
			validated:         true,
		}
	}

	return result, nil
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

func (probe *Probe) validateDependencies() error {
	if probe.dependencies.Topology == nil ||
		probe.dependencies.Endpoints == nil ||
		probe.dependencies.ICE == nil ||
		probe.dependencies.Network == nil ||
		probe.dependencies.Evidence == nil ||
		probe.dependencies.Relay == nil ||
		probe.dependencies.Metrics == nil ||
		probe.dependencies.Clock == nil {
		return ErrInvalidProbeConfig
	}
	return nil
}

func validateConfig(config Config) error {
	if config.EvidenceMode != EvidenceSynthetic && config.EvidenceMode != EvidenceLive {
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
		observation.RuntimeSubnetID != expectation.RuntimeSubnetID {
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
	candidates := []NetworkTarget{
		{Kind: NetworkKVSControl, Host: kvsControlPlaneHost(config.Region), Port: 443},
		{Kind: NetworkSecretsManager, Host: "secretsmanager." + config.Region + ".amazonaws.com", Port: 443},
		{Kind: NetworkCloudWatchLogs, Host: "logs." + config.Region + ".amazonaws.com", Port: 443},
		{Kind: NetworkSarvam, Host: SarvamAPIHost, Port: 443},
		{Kind: NetworkBilleifBackend, Host: config.BilleifBackendHost, Port: 443},
	}
	for _, endpoint := range endpoints {
		parsed, err := url.Parse(endpoint.URL)
		if err != nil || parsed.Hostname() == "" {
			return nil, ErrInvalidTLSEndpoint
		}
		candidates = append(candidates, NetworkTarget{Kind: NetworkKVSDiscovered, Host: parsed.Hostname(), Port: 443})
	}
	targets := make([]NetworkTarget, 0, len(candidates))
	seenHosts := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if _, duplicate := seenHosts[candidate.Host]; duplicate {
			continue
		}
		seenHosts[candidate.Host] = struct{}{}
		targets = append(targets, candidate)
	}
	return targets, nil
}

func ValidateNetworkObservations(now time.Time, maxAge time.Duration, targets []NetworkTarget, observations []ConnectivityObservation) error {
	if !isUTC(now) || maxAge <= 0 || len(targets) == 0 || len(observations) != len(targets) {
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
			!validEvidencePoint(now, maxAge, observation.ObservedAt) || !isSHA256(observation.ArtifactSHA256) {
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

func validEvidencePoint(now time.Time, maxAge time.Duration, observedAt time.Time) bool {
	return isUTC(now) && isUTC(observedAt) && !observedAt.After(now) && !observedAt.Before(now.Add(-maxAge))
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

func liveArtifactSetSHA256(observation LiveEvidenceObservation) string {
	return hashString(strings.Join([]string{
		observation.NAT.ArtifactSHA256,
		observation.Endurance.ArtifactSHA256,
		observation.CredentialLifecycle.ArtifactSHA256,
		observation.BackendRegression.ArtifactSHA256,
	}, "\x00"))
}

func ValidateTURNCredentials(region string, now time.Time, expiryMargin time.Duration, credentials TURNCredentials) error {
	if region != MumbaiRegion || expiryMargin <= 0 || credentials.Username == "" || credentials.Password == "" || len(credentials.URIs) == 0 {
		return ErrInvalidTURNCredentials
	}
	if !credentials.ExpiresAt.After(now.Add(expiryMargin)) {
		return ErrInvalidTURNCredentials
	}
	seen := make(map[string]struct{}, len(credentials.URIs))
	for _, rawURI := range credentials.URIs {
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
	if !isRegionBoundKVSHost(region, host) {
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
		return isRegionBoundKVSHost(region, host)
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
