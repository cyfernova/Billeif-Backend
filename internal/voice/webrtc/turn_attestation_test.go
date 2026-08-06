package webrtc

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testLiveEvidenceDomain = "billeif-agentcore-turn-live-evidence-v2\x00"

func TestVerifyLiveReleaseAcceptsPortableSignedEnvelopesOnce(t *testing.T) {
	t.Parallel()

	observer := newTestLiveObserver("observer-primary", "observer-key-01", 7)
	first := signedTestEnvelope(t, observer, "a", []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"})
	second := signedTestEnvelope(t, observer, "b", []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"})

	encoded, err := json.Marshal([]LiveEvidenceEnvelope{first, second})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var portable []LiveEvidenceEnvelope
	if err := json.Unmarshal(encoded, &portable); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	policy := validReleasePolicy(observer)
	replay := newMemoryReplayGuard()
	now := fixedProbeTime.Add(36 * time.Minute)
	if err := verifyLiveReleaseAt(context.Background(), now, policy, replay, portable...); err != nil {
		t.Fatalf("verifyLiveReleaseAt() error = %v", err)
	}
	if err := verifyLiveReleaseAt(context.Background(), now, policy, replay, portable...); !errors.Is(err, ErrLiveEvidenceReplay) {
		t.Fatalf("replayed verifyLiveReleaseAt() error = %v, want replay rejection", err)
	}
}

func TestVerifyLiveReleaseRejectsTamperedSignedClaims(t *testing.T) {
	t.Parallel()

	observer := newTestLiveObserver("observer-primary", "observer-key-01", 11)
	base := signedTestEnvelope(t, observer, "a", []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"})
	mutations := []struct {
		name   string
		mutate func(*LiveEvidenceEnvelope)
	}{
		{name: "campaign", mutate: func(value *LiveEvidenceEnvelope) { value.Claims.CampaignID = "voice-turn-other" }},
		{name: "source revision", mutate: func(value *LiveEvidenceEnvelope) { value.Claims.SourceRevision = strings.Repeat("f", 40) }},
		{name: "image digest", mutate: func(value *LiveEvidenceEnvelope) { value.Claims.ImageDigest = "sha256:" + strings.Repeat("f", 64) }},
		{name: "observer identity", mutate: func(value *LiveEvidenceEnvelope) { value.Claims.ObserverIdentity = "observer-other" }},
		{name: "observer key", mutate: func(value *LiveEvidenceEnvelope) { value.Claims.ObserverKeyID = "observer-key-other" }},
		{name: "path", mutate: func(value *LiveEvidenceEnvelope) { value.Claims.PathFingerprint = strings.Repeat("c", 64) }},
		{name: "ice expiry", mutate: func(value *LiveEvidenceEnvelope) {
			value.Claims.ICE.ExpiresAt = value.Claims.ICE.ExpiresAt.Add(time.Second)
		}},
		{name: "artifact", mutate: func(value *LiveEvidenceEnvelope) { value.Claims.Artifacts.NATSHA256 = strings.Repeat("f", 64) }},
		{name: "signature", mutate: func(value *LiveEvidenceEnvelope) {
			value.Signature = base64.RawStdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
		}},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			value := base
			test.mutate(&value)
			if err := verifyLiveReleaseAt(context.Background(), fixedProbeTime.Add(36*time.Minute), validReleasePolicy(observer), newMemoryReplayGuard(), value, signedTestEnvelope(t, observer, "b", []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"})); !errors.Is(err, ErrInvalidLiveEvidence) {
				t.Fatalf("verifyLiveReleaseAt() error = %v, want tamper rejection", err)
			}
		})
	}
}

func TestVerifyLiveReleaseRejectsPolicyAndBundleMixing(t *testing.T) {
	t.Parallel()

	observer := newTestLiveObserver("observer-primary", "observer-key-01", 17)
	first := signedTestEnvelope(t, observer, "a", []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"})
	second := signedTestEnvelope(t, observer, "b", []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"})
	duplicateRun := second
	duplicateRun.Claims.RunID = first.Claims.RunID
	duplicateRun = observer.signClaims(t, duplicateRun.Claims)
	overlappingArtifact := second
	overlappingArtifact.Claims.Artifacts.NATSHA256 = first.Claims.Artifacts.NATSHA256
	overlappingArtifact = observer.signClaims(t, overlappingArtifact.Claims)
	now := fixedProbeTime.Add(36 * time.Minute)

	tests := []struct {
		name      string
		policy    LiveReleasePolicy
		first     LiveEvidenceEnvelope
		second    LiveEvidenceEnvelope
		verifyNow time.Time
	}{
		{name: "cross campaign", policy: mutateReleasePolicy(validReleasePolicy(observer), func(value *LiveReleasePolicy) { value.CampaignID = "voice-turn-other" }), first: first, second: second, verifyNow: now},
		{name: "cross build", policy: mutateReleasePolicy(validReleasePolicy(observer), func(value *LiveReleasePolicy) { value.ImageDigest = "sha256:" + strings.Repeat("f", 64) }), first: first, second: second, verifyNow: now},
		{name: "untrusted observer", policy: mutateReleasePolicy(validReleasePolicy(observer), func(value *LiveReleasePolicy) { value.AllowedObservers = nil }), first: first, second: second, verifyNow: now},
		{name: "duplicate path", policy: validReleasePolicy(observer), first: first, second: first, verifyNow: now},
		{name: "duplicate run id", policy: validReleasePolicy(observer), first: first, second: duplicateRun, verifyNow: now},
		{name: "overlapping artifact", policy: validReleasePolicy(observer), first: first, second: overlappingArtifact, verifyNow: now},
		{name: "stale", policy: validReleasePolicy(observer), first: first, second: second, verifyNow: fixedProbeTime.Add(3 * time.Hour)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := verifyLiveReleaseAt(context.Background(), test.verifyNow, test.policy, newMemoryReplayGuard(), test.first, test.second); !errors.Is(err, ErrInvalidLiveEvidence) {
				t.Fatalf("verifyLiveReleaseAt() error = %v, want policy rejection", err)
			}
		})
	}
	reorderedPolicy := mutateReleasePolicy(validReleasePolicy(observer), func(value *LiveReleasePolicy) {
		value.ExpectedPaths[0], value.ExpectedPaths[1] = value.ExpectedPaths[1], value.ExpectedPaths[0]
	})
	if err := verifyLiveReleaseAt(context.Background(), now, reorderedPolicy, newMemoryReplayGuard(), second, first); err != nil {
		t.Fatalf("verifyLiveReleaseAt(reordered approved path set) error = %v", err)
	}
}

func TestVerifyLiveReleaseRequiresExactApprovedPathSet(t *testing.T) {
	t.Parallel()

	observer := newTestLiveObserver("observer-primary", "observer-key-01", 19)
	first := signedTestEnvelope(t, observer, "a", []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"})
	second := signedTestEnvelope(t, observer, "b", []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"})
	now := fixedProbeTime.Add(36 * time.Minute)

	tests := []struct {
		name   string
		mutate func(*LiveReleasePolicy)
	}{
		{name: "missing paths", mutate: func(value *LiveReleasePolicy) { value.ExpectedPaths = nil }},
		{name: "only one path", mutate: func(value *LiveReleasePolicy) { value.ExpectedPaths = value.ExpectedPaths[:1] }},
		{name: "three paths", mutate: func(value *LiveReleasePolicy) {
			value.ExpectedPaths = append(value.ExpectedPaths, LiveReleasePathPolicy{PathFingerprint: strings.Repeat("c", 64), ConnectivityTargets: expectedRedactedNetworkTargets()})
		}},
		{name: "duplicate paths", mutate: func(value *LiveReleasePolicy) {
			value.ExpectedPaths[1].PathFingerprint = value.ExpectedPaths[0].PathFingerprint
		}},
		{name: "unapproved path set", mutate: func(value *LiveReleasePolicy) { value.ExpectedPaths[1].PathFingerprint = strings.Repeat("c", 64) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := mutateReleasePolicy(validReleasePolicy(observer), test.mutate)
			if err := verifyLiveReleaseAt(context.Background(), now, policy, newMemoryReplayGuard(), first, second); !errors.Is(err, ErrInvalidLiveEvidence) {
				t.Fatalf("verifyLiveReleaseAt() error = %v, want exact approved path-set rejection", err)
			}
		})
	}
}

func TestVerifyLiveReleaseRequiresExactExpectedConnectivityTargetSet(t *testing.T) {
	t.Parallel()

	observer := newTestLiveObserver("observer-primary", "observer-key-01", 23)
	first := signedTestEnvelope(t, observer, "a", []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"})
	second := signedTestEnvelope(t, observer, "b", []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"})
	now := fixedProbeTime.Add(36 * time.Minute)

	policyTests := []struct {
		name   string
		mutate func(*LiveReleasePolicy)
	}{
		{name: "missing targets", mutate: func(value *LiveReleasePolicy) { value.ExpectedPaths[0].ConnectivityTargets = nil }},
		{name: "only five targets", mutate: func(value *LiveReleasePolicy) {
			value.ExpectedPaths[0].ConnectivityTargets = value.ExpectedPaths[0].ConnectivityTargets[:5]
		}},
		{name: "seven targets", mutate: func(value *LiveReleasePolicy) {
			value.ExpectedPaths[0].ConnectivityTargets = append(value.ExpectedPaths[0].ConnectivityTargets, RedactedNetworkTarget{Kind: NetworkKVSDiscovered, HostSHA256: hashString("extra.example"), Port: 443})
		}},
		{name: "duplicate targets", mutate: func(value *LiveReleasePolicy) {
			value.ExpectedPaths[0].ConnectivityTargets[1] = value.ExpectedPaths[0].ConnectivityTargets[0]
		}},
		{name: "wrong kind", mutate: func(value *LiveReleasePolicy) {
			value.ExpectedPaths[0].ConnectivityTargets[0].Kind = NetworkSarvam
		}},
		{name: "unapproved host", mutate: func(value *LiveReleasePolicy) {
			value.ExpectedPaths[0].ConnectivityTargets[0].HostSHA256 = hashString("unapproved.example")
		}},
		{name: "wrong port", mutate: func(value *LiveReleasePolicy) {
			value.ExpectedPaths[0].ConnectivityTargets[0].Port = 8443
		}},
	}
	for _, test := range policyTests {
		t.Run("policy "+test.name, func(t *testing.T) {
			policy := mutateReleasePolicy(validReleasePolicy(observer), test.mutate)
			if err := verifyLiveReleaseAt(context.Background(), now, policy, newMemoryReplayGuard(), first, second); !errors.Is(err, ErrInvalidLiveEvidence) {
				t.Fatalf("verifyLiveReleaseAt() error = %v, want exact connectivity policy rejection", err)
			}
		})
	}
	reorderedPolicy := mutateReleasePolicy(validReleasePolicy(observer), func(value *LiveReleasePolicy) {
		targets := value.ExpectedPaths[0].ConnectivityTargets
		for left, right := 0, len(targets)-1; left < right; left, right = left+1, right-1 {
			targets[left], targets[right] = targets[right], targets[left]
		}
	})
	if err := verifyLiveReleaseAt(context.Background(), now, reorderedPolicy, newMemoryReplayGuard(), first, second); err != nil {
		t.Fatalf("verifyLiveReleaseAt(reordered expected connectivity set) error = %v", err)
	}
	pathScopedPolicy := mutateReleasePolicy(validReleasePolicy(observer), func(value *LiveReleasePolicy) {
		value.ExpectedPaths[1].ConnectivityTargets[0].HostSHA256 = hashString("path-b-only.example")
	})
	pathScopedSecond := second
	pathScopedSecond.Claims.Artifacts.Connectivity = append([]LiveConnectivityEvidence(nil), second.Claims.Artifacts.Connectivity...)
	pathScopedSecond.Claims.Artifacts.Connectivity[0].HostSHA256 = hashString("path-b-only.example")
	pathScopedSecond = observer.signClaims(t, pathScopedSecond.Claims)
	if err := verifyLiveReleaseAt(context.Background(), now, pathScopedPolicy, newMemoryReplayGuard(), first, pathScopedSecond); err != nil {
		t.Fatalf("verifyLiveReleaseAt(path-scoped connectivity targets) error = %v", err)
	}
	permuted := first
	permuted.Claims.Artifacts.Connectivity = append([]LiveConnectivityEvidence(nil), first.Claims.Artifacts.Connectivity...)
	for left, right := 0, len(permuted.Claims.Artifacts.Connectivity)-1; left < right; left, right = left+1, right-1 {
		permuted.Claims.Artifacts.Connectivity[left], permuted.Claims.Artifacts.Connectivity[right] =
			permuted.Claims.Artifacts.Connectivity[right], permuted.Claims.Artifacts.Connectivity[left]
	}
	permuted = observer.signClaims(t, permuted.Claims)
	if err := verifyLiveReleaseAt(context.Background(), now, validReleasePolicy(observer), newMemoryReplayGuard(), permuted, second); err != nil {
		t.Fatalf("verifyLiveReleaseAt(permuted signed tuples) error = %v", err)
	}

	swapped := first
	swapped.Claims.Artifacts.Connectivity = append([]LiveConnectivityEvidence(nil), first.Claims.Artifacts.Connectivity...)
	swapped.Claims.Artifacts.Connectivity[0].HostSHA256, swapped.Claims.Artifacts.Connectivity[1].HostSHA256 =
		swapped.Claims.Artifacts.Connectivity[1].HostSHA256, swapped.Claims.Artifacts.Connectivity[0].HostSHA256
	swapped = observer.signClaims(t, swapped.Claims)
	if err := verifyLiveReleaseAt(context.Background(), now, validReleasePolicy(observer), newMemoryReplayGuard(), swapped, second); !errors.Is(err, ErrInvalidLiveEvidence) {
		t.Fatalf("verifyLiveReleaseAt(swapped host/artifact association) error = %v, want rejection", err)
	}
	missing := first
	missing.Claims.Artifacts.Connectivity = append([]LiveConnectivityEvidence(nil), first.Claims.Artifacts.Connectivity[:requiredConnectivityTargetCount-1]...)
	missing.Claims.NetworkHostsValidated = requiredConnectivityTargetCount - 1
	missing = observer.signClaims(t, missing.Claims)
	if err := verifyLiveReleaseAt(context.Background(), now, validReleasePolicy(observer), newMemoryReplayGuard(), missing, second); !errors.Is(err, ErrInvalidLiveEvidence) {
		t.Fatalf("verifyLiveReleaseAt(missing connectivity tuple) error = %v, want rejection", err)
	}
}

func TestPublicInjectedLiveDependenciesCannotMintEnvelope(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("live branch requires the explicit test build tag")
	}

	deps := successfulFakeDependencies(t)
	deps.liveObserver = nil
	config := validProbeConfig()
	config.EvidenceMode = EvidenceLive
	result, err := NewProbe(deps).Run(context.Background(), config)
	if !errors.Is(err, ErrInvalidProbeConfig) {
		t.Fatalf("Run() error = %v, want sealed live-observer requirement", err)
	}
	if result.Envelope != nil {
		t.Fatal("public injected dependencies minted a portable live envelope")
	}
}

func TestProbeValidatesCompleteArtifactSetBeforeAttestation(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("live branch requires the explicit test build tag")
	}

	dependencies := successfulFakeDependencies(t)
	observer := dependencies.liveObserver.(*testLiveObserver)
	dependencies.Topology = topologyFunc(func(ctx context.Context, _ TopologyExpectation) (TopologyObservation, error) {
		value := validTopologyObservation()
		value.ArtifactSHA256 = hashString("relay-artifact-private-a")
		return value, ctx.Err()
	})
	config := validProbeConfig()
	config.EvidenceMode = EvidenceLive

	result, err := NewProbe(dependencies).Run(context.Background(), config)
	if !errors.Is(err, ErrInvalidLiveEvidence) {
		t.Fatalf("Run() error = %v, want overlapping artifact rejection", err)
	}
	if result.Envelope != nil {
		t.Fatal("Run() returned an envelope for overlapping artifacts")
	}
	if calls := observer.attestCalls.Load(); calls != 0 {
		t.Fatalf("observer attest calls = %d, want validation before signing", calls)
	}
}

func TestProbeDoesNotAttestWhenMetricsFail(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("live branch requires the explicit test build tag")
	}

	dependencies := successfulFakeDependencies(t)
	observer := dependencies.liveObserver.(*testLiveObserver)
	dependencies.Metrics = metricsFunc(func(context.Context, ProbeMetric) error {
		return errors.New("test metrics failure")
	})
	config := validProbeConfig()
	config.EvidenceMode = EvidenceLive

	result, err := NewProbe(dependencies).Run(context.Background(), config)
	if !errors.Is(err, ErrMetricsRecordingFailed) {
		t.Fatalf("Run() error = %v, want metrics failure", err)
	}
	if result.Envelope != nil {
		t.Fatal("Run() returned an envelope after metrics failed")
	}
	if calls := observer.attestCalls.Load(); calls != 0 {
		t.Fatalf("observer attest calls = %d, want zero before required metrics succeed", calls)
	}
}

func TestProbeRevalidatesSealedClockAfterMetricsBeforeAttestation(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("live branch requires the explicit test build tag")
	}

	dependencies := successfulFakeDependencies(t)
	observer := dependencies.liveObserver.(*testLiveObserver)
	var clockCalls atomic.Int64
	observer.clock = func() time.Time {
		switch clockCalls.Add(1) {
		case 1:
			return fixedProbeTime
		case 2:
			return fixedProbeTime.Add(time.Second)
		case 3:
			return fixedProbeTime.Add(2 * time.Second)
		case 4:
			return fixedProbeTime.Add(3 * time.Second)
		case 5:
			return fixedProbeTime.Add(35 * time.Minute)
		default:
			return fixedProbeTime.Add(46 * time.Minute)
		}
	}
	config := validProbeConfig()
	config.EvidenceMode = EvidenceLive

	result, err := NewProbe(dependencies).Run(context.Background(), config)
	if !errors.Is(err, ErrInvalidLiveEvidence) {
		t.Fatalf("Run() error = %v, want post-metrics change-window rejection", err)
	}
	if result.Envelope != nil {
		t.Fatal("Run() returned an envelope after the approved time window elapsed")
	}
	if calls := observer.attestCalls.Load(); calls != 0 {
		t.Fatalf("observer attest calls = %d, want sealed-clock revalidation before signing", calls)
	}
}

func TestProbeOrdersCollectionValidationMetricsAndAttestation(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("live branch requires the explicit test build tag")
	}

	events := make([]string, 0, 4)
	dependencies := successfulFakeDependencies(t)
	observer := dependencies.liveObserver.(*testLiveObserver)
	observer.collectFn = func(context.Context, EvidenceRequest) (LiveEvidenceObservation, error) {
		events = append(events, "collect")
		return validLiveEvidenceObservation(), nil
	}
	dependencies.Metrics = metricsFunc(func(_ context.Context, metric ProbeMetric) error {
		if !metric.Evidence.NATHealthy || metric.Evidence.EnduranceMinutes < 30 || !metric.Evidence.CredentialLifecyclePassed || !metric.Evidence.BackendRegressionPassed {
			return errors.New("metric received evidence before validation")
		}
		events = append(events, "validate", "metrics")
		return nil
	})
	observer.onAttest = func() { events = append(events, "attest") }
	config := validProbeConfig()
	config.EvidenceMode = EvidenceLive

	if _, err := NewProbe(dependencies).Run(context.Background(), config); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := strings.Join(events, ","); got != "collect,validate,metrics,attest" {
		t.Fatalf("live proof order = %q, want collect,validate,metrics,attest", got)
	}
}

func TestProbeProducesCrossProcessEnvelopesAcceptedByCentralVerifier(t *testing.T) {
	if !liveProbeBuildEnabled {
		t.Skip("live branch requires the explicit test build tag")
	}

	firstConfig := validProbeConfig()
	firstConfig.EvidenceMode = EvidenceLive
	firstResult, err := NewProbe(successfulFakeDependencies(t)).Run(context.Background(), firstConfig)
	if err != nil || firstResult.Envelope == nil {
		t.Fatalf("first Run() = %+v, %v; want signed envelope", firstResult, err)
	}
	assertConnectivityTuplesMatchObservations(t, firstResult.Envelope.Claims.Artifacts.Connectivity, successfulNetworkObservations(expectedNetworkTargets()))

	secondConfig := validProbeConfig()
	secondConfig.EvidenceMode = EvidenceLive
	secondConfig.Topology.RuntimeSubnetID = "subnet-private-b"
	secondDependencies := successfulFakeDependencies(t)
	secondDependencies.Topology = topologyFunc(func(ctx context.Context, _ TopologyExpectation) (TopologyObservation, error) {
		value := validTopologyObservation()
		value.RuntimeSubnetID = "subnet-private-b"
		value.ArtifactSHA256 = hashString("topology-artifact-private-b")
		return value, ctx.Err()
	})
	secondDependencies.Network = networkFunc(func(ctx context.Context, targets []NetworkTarget) ([]ConnectivityObservation, error) {
		values := successfulNetworkObservations(targets)
		for index := range values {
			values[index].ArtifactSHA256 = hashString("second-path-network-artifact-" + values[index].Host)
		}
		return values, ctx.Err()
	})
	secondDependencies.Relay = relayFunc(func(ctx context.Context, request RelayRequest) (RelayObservation, error) {
		return RelayObservation{
			payload:             append([]byte(nil), request.payload...),
			LocalCandidateType:  CandidateRelay,
			RemoteCandidateType: CandidateRelay,
			ArtifactSHA256:      hashString("relay-artifact-private-b"),
		}, ctx.Err()
	})
	secondDependencies.liveObserver.(*testLiveObserver).collectFn = func(ctx context.Context, _ EvidenceRequest) (LiveEvidenceObservation, error) {
		return validSecondPathEvidenceObservation(), ctx.Err()
	}
	secondResult, err := NewProbe(secondDependencies).Run(context.Background(), secondConfig)
	if err != nil || secondResult.Envelope == nil {
		t.Fatalf("second Run() = %+v, %v; want signed envelope", secondResult, err)
	}

	encoded, err := json.Marshal([]LiveEvidenceEnvelope{*firstResult.Envelope, *secondResult.Envelope})
	if err != nil {
		t.Fatalf("json.Marshal(envelopes) error = %v", err)
	}
	var portable []LiveEvidenceEnvelope
	if err := json.Unmarshal(encoded, &portable); err != nil {
		t.Fatalf("json.Unmarshal(envelopes) error = %v", err)
	}
	observer := firstResultObserver(t, successfulFakeDependencies(t))
	policy := validReleasePolicyForPaths(observer, portable[0].Claims.PathFingerprint, portable[1].Claims.PathFingerprint)
	if err := verifyLiveReleaseAt(context.Background(), fixedProbeTime.Add(36*time.Minute), policy, newMemoryReplayGuard(), portable...); err != nil {
		t.Fatalf("verifyLiveReleaseAt(portable probe envelopes) error = %v", err)
	}
}

func assertConnectivityTuplesMatchObservations(t *testing.T, tuples []LiveConnectivityEvidence, observations []ConnectivityObservation) {
	t.Helper()
	if len(tuples) != requiredConnectivityTargetCount || len(tuples) != len(observations) {
		t.Fatalf("signed connectivity tuple count = %d, want %d", len(tuples), len(observations))
	}
	expected := make(map[string]string, len(observations))
	for _, observation := range observations {
		expected[redactedNetworkTargetKey(observation.Kind, hashString(observation.Host), observation.Port)] = observation.ArtifactSHA256
	}
	for _, tuple := range tuples {
		key := redactedNetworkTargetKey(tuple.Kind, tuple.HostSHA256, tuple.Port)
		if artifact, ok := expected[key]; !ok || artifact != tuple.ArtifactSHA256 || tuple.Port != 443 {
			t.Fatalf("signed connectivity tuple %+v does not bind an observed kind, host, and artifact", tuple)
		}
	}
}

func firstResultObserver(t *testing.T, dependencies Dependencies) *testLiveObserver {
	t.Helper()
	observer, ok := dependencies.liveObserver.(*testLiveObserver)
	if !ok {
		t.Fatal("test dependencies omitted sealed observer")
	}
	return observer
}

func validReleasePolicy(observer *testLiveObserver) LiveReleasePolicy {
	return validReleasePolicyForPaths(observer, strings.Repeat("a", 64), strings.Repeat("b", 64))
}

func validReleasePolicyForPaths(observer *testLiveObserver, pathFingerprints ...string) LiveReleasePolicy {
	expectedPaths := make([]LiveReleasePathPolicy, len(pathFingerprints))
	for index, pathFingerprint := range pathFingerprints {
		expectedPaths[index] = LiveReleasePathPolicy{PathFingerprint: pathFingerprint, ConnectivityTargets: expectedRedactedNetworkTargets()}
	}
	return LiveReleasePolicy{
		CampaignID:        "voice-turn-campaign-01",
		SourceRevision:    strings.Repeat("a", 40),
		ImageDigest:       "sha256:" + strings.Repeat("b", 64),
		ChangeWindowStart: fixedProbeTime.Add(-time.Minute),
		ChangeWindowEnd:   fixedProbeTime.Add(45 * time.Minute),
		MaxEvidenceAge:    20 * time.Minute,
		ExpectedPaths:     expectedPaths,
		AllowedObservers: []TrustedObserver{{
			Identity:  observer.identity,
			KeyID:     observer.keyID,
			PublicKey: append(ed25519.PublicKey(nil), observer.publicKey...),
		}},
	}
}

func mutateReleasePolicy(value LiveReleasePolicy, mutate func(*LiveReleasePolicy)) LiveReleasePolicy {
	value.AllowedObservers = append([]TrustedObserver(nil), value.AllowedObservers...)
	value.ExpectedPaths = append([]LiveReleasePathPolicy(nil), value.ExpectedPaths...)
	for index := range value.ExpectedPaths {
		value.ExpectedPaths[index].ConnectivityTargets = append([]RedactedNetworkTarget(nil), value.ExpectedPaths[index].ConnectivityTargets...)
	}
	mutate(&value)
	return value
}

func expectedRedactedNetworkTargets() []RedactedNetworkTarget {
	targets := expectedNetworkTargets()
	redacted := make([]RedactedNetworkTarget, len(targets))
	for index, target := range targets {
		redacted[index] = RedactedNetworkTarget{Kind: target.Kind, HostSHA256: hashString(target.Host), Port: target.Port}
	}
	return redacted
}

func signedTestEnvelope(t *testing.T, observer *testLiveObserver, pathCharacter string, artifactCharacters []string) LiveEvidenceEnvelope {
	t.Helper()
	if len(artifactCharacters) != 9 {
		t.Fatalf("artifact character count = %d, want 9", len(artifactCharacters))
	}
	connectivity := make([]LiveConnectivityEvidence, 0, len(expectedRedactedNetworkTargets()))
	for index, target := range expectedRedactedNetworkTargets() {
		connectivity = append(connectivity, LiveConnectivityEvidence{
			Kind:           target.Kind,
			HostSHA256:     target.HostSHA256,
			Port:           target.Port,
			ArtifactSHA256: hashString("signed-test-connectivity-" + artifactCharacters[1] + "-" + strconv.Itoa(index)),
		})
	}
	claims := LiveEvidenceClaims{
		Version:              LiveEvidenceEnvelopeVersion,
		RunID:                strings.Repeat(pathCharacter, 64),
		CampaignID:           "voice-turn-campaign-01",
		SourceRevision:       strings.Repeat("a", 40),
		ImageDigest:          "sha256:" + strings.Repeat("b", 64),
		ObserverIdentity:     observer.identity,
		ObserverKeyID:        observer.keyID,
		ChangeWindowStart:    fixedProbeTime.Add(-time.Minute),
		ChangeWindowEnd:      fixedProbeTime.Add(45 * time.Minute),
		RunStartedAt:         fixedProbeTime,
		NetworkCompletedAt:   fixedProbeTime.Add(time.Second),
		CredentialAcquiredAt: fixedProbeTime.Add(2 * time.Second),
		RelayCompletedAt:     fixedProbeTime.Add(3 * time.Second),
		CompletedAt:          fixedProbeTime.Add(35 * time.Minute),
		PathFingerprint:      strings.Repeat(pathCharacter, 64),
		ICE: RedactedICE{
			HostSHA256: []string{strings.Repeat("e", 64)},
			Transport:  "udp",
			Port:       443,
			ExpiresAt:  fixedProbeTime.Add(30 * time.Minute),
		},
		CredentialExpiryMargin: 5 * time.Minute,
		ValidatedPaths:         2,
		TLSEndpoints:           2,
		NetworkHostsValidated:  6,
		RoundTripBytes:         ProbeNonceBytes,
		RelayOnly:              true,
		Artifacts: LiveEvidenceArtifacts{
			TopologySHA256:            hashString("signed-test-artifact-" + artifactCharacters[0]),
			Connectivity:              connectivity,
			RelaySHA256:               hashString("signed-test-artifact-" + artifactCharacters[4]),
			NATSHA256:                 hashString("signed-test-artifact-" + artifactCharacters[5]),
			EnduranceSHA256:           hashString("signed-test-artifact-" + artifactCharacters[6]),
			CredentialLifecycleSHA256: hashString("signed-test-artifact-" + artifactCharacters[7]),
			BackendRegressionSHA256:   hashString("signed-test-artifact-" + artifactCharacters[8]),
		},
		Evidence: EvidenceSummary{NATHealthy: true, EnduranceMinutes: 35, CredentialLifecyclePassed: true, BackendRegressionPassed: true},
	}
	return observer.signClaims(t, claims)
}

type testLiveObserver struct {
	identity    string
	keyID       string
	privateKey  ed25519.PrivateKey
	publicKey   ed25519.PublicKey
	clock       func() time.Time
	collectFn   func(context.Context, EvidenceRequest) (LiveEvidenceObservation, error)
	onAttest    func()
	attestCalls atomic.Int64
}

func newTestLiveObserver(identity, keyID string, seedByte byte) *testLiveObserver {
	privateKey := ed25519.NewKeyFromSeed(bytesOf(seedByte, ed25519.SeedSize))
	return &testLiveObserver{
		identity:   identity,
		keyID:      keyID,
		privateKey: privateKey,
		publicKey:  append(ed25519.PublicKey(nil), privateKey.Public().(ed25519.PublicKey)...),
		clock:      time.Now,
	}
}

func (observer *testLiveObserver) signClaims(t *testing.T, claims LiveEvidenceClaims) LiveEvidenceEnvelope {
	t.Helper()
	envelope, err := observer.attest(context.Background(), claims)
	if err != nil {
		t.Fatalf("attest() error = %v", err)
	}
	return envelope
}

func (observer *testLiveObserver) collect(ctx context.Context, request EvidenceRequest) (LiveEvidenceObservation, error) {
	if observer.collectFn == nil {
		return LiveEvidenceObservation{}, errors.New("test observer collection is not configured")
	}
	return observer.collectFn(ctx, request)
}

func (observer *testLiveObserver) attest(_ context.Context, claims LiveEvidenceClaims) (LiveEvidenceEnvelope, error) {
	observer.attestCalls.Add(1)
	if observer.onAttest != nil {
		observer.onAttest()
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return LiveEvidenceEnvelope{}, err
	}
	signature := ed25519.Sign(observer.privateKey, append([]byte(testLiveEvidenceDomain), payload...))
	return LiveEvidenceEnvelope{Claims: claims, Signature: base64.RawStdEncoding.EncodeToString(signature)}, nil
}

func (observer *testLiveObserver) descriptor() TrustedObserver {
	return TrustedObserver{Identity: observer.identity, KeyID: observer.keyID, PublicKey: append(ed25519.PublicKey(nil), observer.publicKey...)}
}

func (observer *testLiveObserver) now() time.Time {
	return observer.clock()
}

func (*testLiveObserver) liveEvidenceObserverSeal() {}

func bytesOf(value byte, count int) []byte {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return result
}

type memoryReplayGuard struct {
	seen map[string]struct{}
}

func newMemoryReplayGuard() *memoryReplayGuard {
	return &memoryReplayGuard{seen: make(map[string]struct{})}
}

func (guard *memoryReplayGuard) Reserve(_ context.Context, _ string, envelopeSHA256 []string, _ time.Time) error {
	for _, digest := range envelopeSHA256 {
		if _, exists := guard.seen[digest]; exists {
			return ErrLiveEvidenceReplay
		}
	}
	for _, digest := range envelopeSHA256 {
		guard.seen[digest] = struct{}{}
	}
	return nil
}
