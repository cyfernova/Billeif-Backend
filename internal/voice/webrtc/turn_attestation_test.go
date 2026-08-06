package webrtc

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testLiveEvidenceDomain = "billeif-agentcore-turn-live-evidence-v1\x00"

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
			Payload:             append([]byte(nil), request.Payload...),
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
	if err := verifyLiveReleaseAt(context.Background(), fixedProbeTime.Add(36*time.Minute), validReleasePolicy(observer), newMemoryReplayGuard(), portable...); err != nil {
		t.Fatalf("verifyLiveReleaseAt(portable probe envelopes) error = %v", err)
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
	return LiveReleasePolicy{
		CampaignID:        "voice-turn-campaign-01",
		SourceRevision:    strings.Repeat("a", 40),
		ImageDigest:       "sha256:" + strings.Repeat("b", 64),
		ChangeWindowStart: fixedProbeTime.Add(-time.Minute),
		ChangeWindowEnd:   fixedProbeTime.Add(45 * time.Minute),
		MaxEvidenceAge:    20 * time.Minute,
		AllowedObservers: []TrustedObserver{{
			Identity:  observer.identity,
			KeyID:     observer.keyID,
			PublicKey: append(ed25519.PublicKey(nil), observer.publicKey...),
		}},
	}
}

func mutateReleasePolicy(value LiveReleasePolicy, mutate func(*LiveReleasePolicy)) LiveReleasePolicy {
	value.AllowedObservers = append([]TrustedObserver(nil), value.AllowedObservers...)
	mutate(&value)
	return value
}

func signedTestEnvelope(t *testing.T, observer *testLiveObserver, pathCharacter string, artifactCharacters []string) LiveEvidenceEnvelope {
	t.Helper()
	if len(artifactCharacters) != 9 {
		t.Fatalf("artifact character count = %d, want 9", len(artifactCharacters))
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
		NetworkHostsValidated:  3,
		RoundTripBytes:         ProbeNonceBytes,
		RelayOnly:              true,
		Artifacts: LiveEvidenceArtifacts{
			TopologySHA256:            hashString("signed-test-artifact-" + artifactCharacters[0]),
			ConnectivityHostSHA256:    []string{hashString("signed-test-host-1"), hashString("signed-test-host-2"), hashString("signed-test-host-3")},
			ConnectivitySHA256:        []string{hashString("signed-test-artifact-" + artifactCharacters[1]), hashString("signed-test-artifact-" + artifactCharacters[2]), hashString("signed-test-artifact-" + artifactCharacters[3])},
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
