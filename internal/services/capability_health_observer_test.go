package services

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type staticCapabilityProbeTargets struct {
	mu          sync.Mutex
	targets     []CapabilityProbeTarget
	activeCount int
}

func TestDBCapabilityProbeTargetSourceDiscoversConfiguredTenantsAndGSTAccounts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE business_profiles (id TEXT PRIMARY KEY, owner_id TEXT, name TEXT, email TEXT, currency TEXT, timezone TEXT, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE gst_integration_accounts (id TEXT PRIMARY KEY, business_id TEXT, provider TEXT, service_type TEXT, status TEXT, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`).Error)
	for _, id := range []string{"biz-1", "biz-2"} {
		require.NoError(t, db.Exec(`INSERT INTO business_profiles (id, owner_id, name, email, currency, timezone) VALUES (?, 'owner', ?, ?, 'INR', 'Asia/Kolkata')`, id, id, id+"@example.test").Error)
	}
	require.NoError(t, db.Exec(`INSERT INTO gst_integration_accounts (id, business_id, provider, service_type, status) VALUES ('gst-1', 'biz-1', 'cleartax', 'einvoice', ?)`, models.GSTJobStatusSucceeded).Error)
	require.NoError(t, db.Exec(`INSERT INTO gst_integration_accounts (id, business_id, provider, service_type, status) VALUES ('gst-2', 'biz-2', 'cleartax', 'einvoice', ?)`, models.GSTJobStatusFailed).Error)

	source := NewDBCapabilityProbeTargetSource(db, config.CapabilityConfiguration{Razorpay: true, GST: true, AI: true}, true)
	targets, err := source.DiscoverCapabilityProbeTargets(context.Background(), 20)
	require.NoError(t, err)
	require.ElementsMatch(t, []CapabilityProbeTarget{
		{BusinessID: "biz-1", HealthKey: CapabilityRazorpay, ProbeGroup: "global:razorpay"},
		{BusinessID: "biz-1", HealthKey: CapabilityAI, ProbeGroup: "global:ai"},
		{BusinessID: "biz-1", HealthKey: CapabilityGSTProvider, ProbeGroup: "gst:biz-1"},
		{BusinessID: "biz-2", HealthKey: CapabilityRazorpay, ProbeGroup: "global:razorpay"},
		{BusinessID: "biz-2", HealthKey: CapabilityAI, ProbeGroup: "global:ai"},
		{BusinessID: "biz-2", HealthKey: CapabilityGSTProvider, ProbeGroup: "gst:biz-2"},
	}, targets)
}

func TestDBCapabilityProbeTargetSourceRotatesBoundedTenantBatches(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE business_profiles (id TEXT PRIMARY KEY, deleted_at DATETIME)`).Error)
	for _, id := range []string{"biz-1", "biz-2", "biz-3", "biz-4"} {
		require.NoError(t, db.Exec(`INSERT INTO business_profiles (id) VALUES (?)`, id).Error)
	}
	source := NewDBCapabilityProbeTargetSource(db, config.CapabilityConfiguration{Razorpay: true}, false)
	first, err := source.DiscoverCapabilityProbeTargets(context.Background(), 3)
	require.NoError(t, err)
	require.Len(t, first, 3)
	second, err := source.DiscoverCapabilityProbeTargets(context.Background(), 3)
	require.NoError(t, err)
	require.Contains(t, second, CapabilityProbeTarget{BusinessID: "biz-4", HealthKey: CapabilityRazorpay, ProbeGroup: "global:razorpay"})
}

func TestDBCapabilityProbeCensusDeclaresWorstCaseBusinessSweep(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE business_profiles (id TEXT PRIMARY KEY, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE gst_integration_accounts (id TEXT PRIMARY KEY, business_id TEXT, deleted_at DATETIME)`).Error)
	for index := 0; index < 240; index++ {
		require.NoError(t, db.Exec(`INSERT INTO business_profiles (id) VALUES (?)`, fmt.Sprintf("biz-%03d", index)).Error)
	}
	source := NewDBCapabilityProbeTargetSource(db, config.CapabilityConfiguration{Razorpay: true, AI: true, GST: true}, true)

	census, err := source.CapabilityProbeCensus(context.Background(), 20)

	require.NoError(t, err)
	require.Equal(t, 480, census.ActiveTargets)
	require.Equal(t, 40, census.SweepCycles, "20-target pages must reserve for the possible third GST target per business")
}

func TestGSTCapabilityProbeValidatesStoredCredentialsWithoutMutatingAccount(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/validate", r.URL.Path)
		_, _ = w.Write([]byte(`{"valid":true}`))
	}))
	defer server.Close()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE gst_integration_accounts (id TEXT PRIMARY KEY, business_id TEXT, provider TEXT, service_type TEXT, encrypted_credentials TEXT, status TEXT, last_validated_at DATETIME, last_error TEXT, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`).Error)
	cfg := &config.Config{GST: config.GSTConfig{BaseURL: server.URL, ValidatePath: "/validate", Timeout: 1}}
	cfg.Credentials.EncryptionKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
	service := NewTaxComplianceService(cfg, db, nil, nil, nil, nil, nil, nil, nil, logger.New())
	ciphertext, _, err := service.encryptIntegrationCredentials(context.Background(), GSTIntegrationAccountCredentials{APIKey: "tenant-secret"})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`INSERT INTO gst_integration_accounts (id, business_id, provider, service_type, encrypted_credentials, status, last_error) VALUES ('gst-1', 'biz-1', 'cleartax', 'einvoice', ?, ?, '')`, ciphertext, models.GSTJobStatusSucceeded).Error)

	outcome := service.ProbeCapability(context.Background(), CapabilityProbeTarget{BusinessID: "biz-1", HealthKey: CapabilityGSTProvider})

	require.NoError(t, outcome.Err)
	require.Equal(t, 1, calls)
	var account models.GSTIntegrationAccount
	require.NoError(t, db.First(&account, "id = ?", "gst-1").Error)
	require.Equal(t, models.GSTJobStatusSucceeded, account.Status)
	require.Nil(t, account.LastValidatedAt)
	require.Empty(t, account.LastError)
}

func (s *staticCapabilityProbeTargets) DiscoverCapabilityProbeTargets(context.Context, int) ([]CapabilityProbeTarget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]CapabilityProbeTarget(nil), s.targets...), nil
}

func (s *staticCapabilityProbeTargets) set(targets []CapabilityProbeTarget) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targets = append([]CapabilityProbeTarget(nil), targets...)
}

func (s *staticCapabilityProbeTargets) CapabilityProbeCensus(context.Context, int) (CapabilityProbeCensus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeCount > 0 {
		return CapabilityProbeCensus{ActiveTargets: s.activeCount}, nil
	}
	return CapabilityProbeCensus{ActiveTargets: len(s.targets)}, nil
}

type recordingCapabilityProber struct {
	mu      sync.Mutex
	calls   int
	outcome CapabilityProviderOutcome
}

type failingCapabilityProbeTargets struct{ err error }

func (s failingCapabilityProbeTargets) DiscoverCapabilityProbeTargets(context.Context, int) ([]CapabilityProbeTarget, error) {
	return nil, s.err
}

type boundedCapabilityProber struct {
	mu        sync.Mutex
	active    int
	maxActive int
	calls     int
}

func (p *boundedCapabilityProber) ProbeCapability(ctx context.Context, _ CapabilityProbeTarget) CapabilityProviderOutcome {
	p.mu.Lock()
	p.active++
	p.calls++
	if p.active > p.maxActive {
		p.maxActive = p.active
	}
	p.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-time.After(10 * time.Millisecond):
	}
	p.mu.Lock()
	p.active--
	p.mu.Unlock()
	return CapabilityProviderOutcome{}
}

func (p *recordingCapabilityProber) ProbeCapability(context.Context, CapabilityProbeTarget) CapabilityProviderOutcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.outcome
}

func (p *recordingCapabilityProber) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestCapabilityHealthObserverMovesColdTenantsFromUnknownWithoutMutation(t *testing.T) {
	now := time.Date(2026, 9, 1, 15, 30, 0, 0, time.UTC)
	cache := NewCapabilityHealthCache(CapabilityHealthCacheOptions{Now: func() time.Time { return now }})
	_, found := cache.CustomerFact("biz-1", CapabilityAI)
	require.False(t, found)
	prober := &recordingCapabilityProber{}
	observer := NewCapabilityHealthObserver(
		&staticCapabilityProbeTargets{targets: []CapabilityProbeTarget{
			{BusinessID: "biz-1", HealthKey: CapabilityAI, ProbeGroup: "global:ai"},
			{BusinessID: "biz-2", HealthKey: CapabilityAI, ProbeGroup: "global:ai"},
		}},
		map[CapabilityKey]CapabilityProviderProber{CapabilityAI: prober},
		NewCapabilityHealthRecorder(cache, func() time.Time { return now }),
		CapabilityHealthObserverOptions{MaxTargets: 10, Concurrency: 2, ProbeTimeout: time.Second, MaxAttempts: 1},
	)

	require.NoError(t, observer.ObserveOnce(context.Background()))

	for _, businessID := range []string{"biz-1", "biz-2"} {
		fact, ok := cache.CustomerFact(businessID, CapabilityAI)
		require.True(t, ok)
		require.Equal(t, CapabilityProviderHealthy, fact.Status)
		require.Equal(t, now, *fact.ObservedAt)
	}
	require.Equal(t, 1, prober.callCount(), "global provider probe must be shared across tenants")
	_, otherTenant := cache.CustomerFact("biz-3", CapabilityAI)
	require.False(t, otherTenant)
}

func TestCapabilityHealthObserverRunDiscoversNewTenantOnLaterCycle(t *testing.T) {
	cache := NewCapabilityHealthCache(CapabilityHealthCacheOptions{})
	targets := &staticCapabilityProbeTargets{targets: []CapabilityProbeTarget{{BusinessID: "biz-1", HealthKey: CapabilityAI, ProbeGroup: "global:ai"}}}
	observer := NewCapabilityHealthObserver(targets, map[CapabilityKey]CapabilityProviderProber{
		CapabilityAI: &recordingCapabilityProber{},
	}, NewCapabilityHealthRecorder(cache, nil), CapabilityHealthObserverOptions{MaxTargets: 10, Concurrency: 1, ProbeTimeout: time.Second, MaxAttempts: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- observer.Run(ctx, 10*time.Millisecond) }()
	require.Eventually(t, func() bool {
		_, ok := cache.CustomerFact("biz-1", CapabilityAI)
		return ok
	}, time.Second, 5*time.Millisecond)
	targets.set([]CapabilityProbeTarget{
		{BusinessID: "biz-1", HealthKey: CapabilityAI, ProbeGroup: "global:ai"},
		{BusinessID: "biz-2", HealthKey: CapabilityAI, ProbeGroup: "global:ai"},
	})
	require.Eventually(t, func() bool {
		_, ok := cache.CustomerFact("biz-2", CapabilityAI)
		return ok
	}, time.Second, 5*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestCapabilityHealthObserverDoesNotRetryTypedRateLimit(t *testing.T) {
	now := time.Date(2026, 9, 1, 16, 20, 0, 0, time.UTC)
	cache := NewCapabilityHealthCache(CapabilityHealthCacheOptions{Now: func() time.Time { return now }})
	prober := &recordingCapabilityProber{outcome: CapabilityProviderOutcome{Err: &providerHTTPError{status: http.StatusTooManyRequests}}}
	observer := NewCapabilityHealthObserver(
		&staticCapabilityProbeTargets{targets: []CapabilityProbeTarget{{BusinessID: "biz-1", HealthKey: CapabilityRazorpay}}},
		map[CapabilityKey]CapabilityProviderProber{CapabilityRazorpay: prober},
		NewCapabilityHealthRecorder(cache, func() time.Time { return now }),
		CapabilityHealthObserverOptions{MaxAttempts: 2, RetryDelay: time.Millisecond, ProbeTimeout: time.Second},
	)

	require.NoError(t, observer.ObserveOnce(context.Background()))
	require.Equal(t, 1, prober.callCount())
	fact, ok := cache.CustomerFact("biz-1", CapabilityRazorpay)
	require.True(t, ok)
	require.Equal(t, CapabilityProviderDegraded, fact.Status)
	require.NotNil(t, fact.Degradation)
	require.Equal(t, "provider_rate_limited", fact.Degradation.Code)
}

func TestCapabilityHealthObserverBoundsConcurrencyAndLeavesUnsupportedUnknown(t *testing.T) {
	cache := NewCapabilityHealthCache(CapabilityHealthCacheOptions{})
	prober := &boundedCapabilityProber{}
	targets := make([]CapabilityProbeTarget, 0, 6)
	for index := 0; index < 6; index++ {
		targets = append(targets, CapabilityProbeTarget{BusinessID: fmt.Sprintf("biz-%d", index), HealthKey: CapabilityRazorpay})
	}
	observer := NewCapabilityHealthObserver(
		&staticCapabilityProbeTargets{targets: targets},
		map[CapabilityKey]CapabilityProviderProber{CapabilityRazorpay: prober},
		NewCapabilityHealthRecorder(cache, nil),
		CapabilityHealthObserverOptions{MaxTargets: 6, Concurrency: 2, ProbeTimeout: time.Second, MaxAttempts: 1},
	)
	require.NoError(t, observer.ObserveOnce(context.Background()))
	prober.mu.Lock()
	require.Equal(t, 6, prober.calls)
	require.LessOrEqual(t, prober.maxActive, 2)
	prober.mu.Unlock()

	unsupported := NewCapabilityHealthObserver(
		&staticCapabilityProbeTargets{targets: []CapabilityProbeTarget{{BusinessID: "biz-unsupported", HealthKey: CapabilityAI}}},
		map[CapabilityKey]CapabilityProviderProber{CapabilityAI: &recordingCapabilityProber{outcome: CapabilityProviderOutcome{Err: ErrCapabilityProbeUnsupported}}},
		NewCapabilityHealthRecorder(cache, nil), CapabilityHealthObserverOptions{MaxAttempts: 1},
	)
	require.NoError(t, unsupported.ObserveOnce(context.Background()))
	_, found := cache.CustomerFact("biz-unsupported", CapabilityAI)
	require.False(t, found)
}

func TestCapabilityHealthObserverDefaultTargetBoundLeavesCycleDeadlineMargin(t *testing.T) {
	observer := NewCapabilityHealthObserver(nil, nil, nil, CapabilityHealthObserverOptions{})

	require.Equal(t, 20, observer.options.MaxTargets)
	worstCaseProbeTime := time.Duration((observer.options.MaxTargets+observer.options.Concurrency-1)/observer.options.Concurrency) *
		time.Duration(observer.options.MaxAttempts) * observer.options.ProbeTimeout
	require.Less(t, worstCaseProbeTime, observer.options.MaxCycleDuration,
		"the default target bound must leave time for census, discovery and recording")
}

func TestCapabilityHealthObserverDeclaresSweepFreshnessBeyond170Businesses(t *testing.T) {
	now := time.Date(2026, 9, 1, 16, 30, 0, 0, time.UTC)
	cache := NewCapabilityHealthCache(CapabilityHealthCacheOptions{MaxAge: 5 * time.Minute, MaxEntries: 2, Now: func() time.Time { return now }})
	targets := make([]CapabilityProbeTarget, 0, 600)
	for index := 0; index < 600; index++ {
		targets = append(targets, CapabilityProbeTarget{BusinessID: fmt.Sprintf("biz-%03d", index), HealthKey: CapabilityRazorpay})
	}
	observer := NewCapabilityHealthObserver(
		&staticCapabilityProbeTargets{targets: targets, activeCount: 600},
		map[CapabilityKey]CapabilityProviderProber{CapabilityRazorpay: &recordingCapabilityProber{}},
		NewCapabilityHealthRecorder(cache, func() time.Time { return now }),
		CapabilityHealthObserverOptions{
			MaxTargets: 20, Concurrency: 4, ProbeTimeout: time.Second, MaxAttempts: 1,
			RefreshInterval: 2 * time.Minute, MaxCycleDuration: time.Minute,
		},
	)

	require.NoError(t, observer.ObserveOnce(context.Background()))
	now = now.Add(6 * time.Minute)
	fact, found := cache.CustomerFact("biz-000", CapabilityRazorpay)
	require.True(t, found)
	require.False(t, fact.Stale, "rotation scale must be reflected in the declared full-sweep freshness SLA")
	for index := 0; index < 600; index++ {
		require.NoError(t, cache.Record(fmt.Sprintf("pressure-%03d", index), CapabilityEmail, CapabilityHealthObservation{
			Status: CapabilityProviderHealthy, ObservedAt: now.Add(time.Duration(index) * time.Millisecond),
		}))
	}
	_, stillPresent := cache.CustomerFact("biz-000", CapabilityRazorpay)
	require.True(t, stillPresent, "active target must survive cache pressure")
}

func TestCapabilityHealthObserverRefreshesEveryTenantAcrossBoundedGlobalProviderPages(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE business_profiles (id TEXT PRIMARY KEY, deleted_at DATETIME)`).Error)
	for index := 0; index < 240; index++ {
		require.NoError(t, db.Exec(`INSERT INTO business_profiles (id) VALUES (?)`, fmt.Sprintf("biz-%03d", index)).Error)
	}
	now := time.Date(2026, 9, 1, 17, 0, 0, 0, time.UTC)
	cache := NewCapabilityHealthCache(CapabilityHealthCacheOptions{MaxAge: 5 * time.Minute, MaxEntries: 2, Now: func() time.Time { return now }})
	razorpayProbe := &recordingCapabilityProber{}
	aiProbe := &recordingCapabilityProber{}
	observer := NewCapabilityHealthObserver(
		NewDBCapabilityProbeTargetSource(db, config.CapabilityConfiguration{Razorpay: true, AI: true}, false),
		map[CapabilityKey]CapabilityProviderProber{CapabilityRazorpay: razorpayProbe, CapabilityAI: aiProbe},
		NewCapabilityHealthRecorder(cache, func() time.Time { return now }),
		CapabilityHealthObserverOptions{MaxTargets: 20, Concurrency: 4, MaxAttempts: 1, RefreshInterval: 2 * time.Minute, MaxCycleDuration: time.Minute},
	)
	for cycle := 0; cycle < 24; cycle++ {
		require.NoError(t, observer.ObserveOnce(context.Background()))
		now = now.Add(2 * time.Minute)
	}
	for index := 0; index < 240; index++ {
		for _, capability := range []CapabilityKey{CapabilityRazorpay, CapabilityAI} {
			fact, found := cache.CustomerFact(fmt.Sprintf("biz-%03d", index), capability)
			require.True(t, found, "business %d capability %s was starved", index, capability)
			require.False(t, fact.Stale, "business %d capability %s became stale inside the sweep SLA", index, capability)
		}
	}
	require.Equal(t, 24, razorpayProbe.callCount(), "global Razorpay call count must stay one per bounded page")
	require.Equal(t, 24, aiProbe.callCount(), "global AI call count must stay one per bounded page")
	_, leaked := cache.CustomerFact("biz-outside", CapabilityRazorpay)
	require.False(t, leaked)
}

func TestCapabilityHealthObserverRefreshesGSTTenantsAcrossBoundedProbePages(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE business_profiles (id TEXT PRIMARY KEY, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE gst_integration_accounts (id TEXT PRIMARY KEY, business_id TEXT, deleted_at DATETIME)`).Error)
	for index := 0; index < 180; index++ {
		businessID := fmt.Sprintf("biz-%03d", index)
		require.NoError(t, db.Exec(`INSERT INTO business_profiles (id) VALUES (?)`, businessID).Error)
		require.NoError(t, db.Exec(`INSERT INTO gst_integration_accounts (id, business_id) VALUES (?, ?)`, fmt.Sprintf("gst-%03d", index), businessID).Error)
	}
	now := time.Date(2026, 9, 1, 17, 0, 0, 0, time.UTC)
	cache := NewCapabilityHealthCache(CapabilityHealthCacheOptions{MaxAge: 5 * time.Minute, MaxEntries: 2, Now: func() time.Time { return now }})
	gstProbe := &recordingCapabilityProber{}
	observer := NewCapabilityHealthObserver(
		NewDBCapabilityProbeTargetSource(db, config.CapabilityConfiguration{GST: true}, true),
		map[CapabilityKey]CapabilityProviderProber{CapabilityGSTProvider: gstProbe},
		NewCapabilityHealthRecorder(cache, func() time.Time { return now }),
		CapabilityHealthObserverOptions{MaxTargets: 20, Concurrency: 4, MaxAttempts: 1, RefreshInterval: 2 * time.Minute, MaxCycleDuration: time.Minute},
	)
	for cycle := 0; cycle < 9; cycle++ {
		require.NoError(t, observer.ObserveOnce(context.Background()))
		now = now.Add(2 * time.Minute)
	}
	for index := 0; index < 180; index++ {
		fact, found := cache.CustomerFact(fmt.Sprintf("biz-%03d", index), CapabilityGSTProvider)
		require.True(t, found, "GST business %d was starved", index)
		require.False(t, fact.Stale)
	}
	require.GreaterOrEqual(t, gstProbe.callCount(), 180)
	require.LessOrEqual(t, gstProbe.callCount(), 9*20, "GST provider calls must remain bounded by page size")
	_, leaked := cache.CustomerFact("biz-outside", CapabilityGSTProvider)
	require.False(t, leaked)
}

func TestCapabilityHealthObserverReportsSanitizedCycleErrorsWithoutBusyLoop(t *testing.T) {
	issues := make(chan CapabilityHealthCycleIssue, 4)
	observer := NewCapabilityHealthObserver(
		failingCapabilityProbeTargets{err: errors.New("database secret credential raw failure")}, nil,
		NewCapabilityHealthRecorder(NewCapabilityHealthCache(CapabilityHealthCacheOptions{}), nil),
		CapabilityHealthObserverOptions{OnCycleIssue: func(issue CapabilityHealthCycleIssue) { issues <- issue }},
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	started := time.Now()
	go func() { done <- observer.Run(ctx, 25*time.Millisecond) }()
	first := <-issues
	second := <-issues
	if first.Code != "target_discovery_failed" || second.Code != first.Code {
		t.Fatalf("cycle issues = %#v, %#v", first, second)
	}
	if strings.Contains(fmt.Sprintf("%#v", first), "secret") || strings.Contains(fmt.Sprintf("%#v", first), "raw failure") {
		t.Fatalf("cycle issue leaked source error: %#v", first)
	}
	require.GreaterOrEqual(t, time.Since(started), 20*time.Millisecond, "recurring errors must wait for the refresh interval")
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestCapabilityHealthObserverRunCannotExceedDeclaredRefreshInterval(t *testing.T) {
	prober := &recordingCapabilityProber{}
	observer := NewCapabilityHealthObserver(
		&staticCapabilityProbeTargets{targets: []CapabilityProbeTarget{{BusinessID: "biz-1", HealthKey: CapabilityRazorpay}}},
		map[CapabilityKey]CapabilityProviderProber{CapabilityRazorpay: prober},
		NewCapabilityHealthRecorder(NewCapabilityHealthCache(CapabilityHealthCacheOptions{}), nil),
		CapabilityHealthObserverOptions{RefreshInterval: 10 * time.Millisecond, MaxAttempts: 1},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- observer.Run(ctx, time.Hour) }()
	require.Eventually(t, func() bool { return prober.callCount() >= 2 }, time.Second, 5*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}
