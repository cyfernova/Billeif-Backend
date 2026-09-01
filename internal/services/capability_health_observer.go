package services

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"

	"gorm.io/gorm"
)

var ErrCapabilityProbeUnsupported = errors.New("capability provider probe is unsupported")

type CapabilityProbeTarget struct {
	BusinessID string
	HealthKey  CapabilityKey
	ProbeGroup string
}

type CapabilityProbeTargetSource interface {
	DiscoverCapabilityProbeTargets(ctx context.Context, limit int) ([]CapabilityProbeTarget, error)
}

type CapabilityProbeCensus struct {
	ActiveTargets int
	SweepCycles   int
}

type CapabilityProbeTargetCensus interface {
	CapabilityProbeCensus(ctx context.Context, limit int) (CapabilityProbeCensus, error)
}

type CapabilityOutcomeCapacity interface {
	EnsureActiveCapacity(activeTargets int)
}

type CapabilityProviderProber interface {
	ProbeCapability(ctx context.Context, target CapabilityProbeTarget) CapabilityProviderOutcome
}

type CapabilityHealthObserverOptions struct {
	MaxTargets       int
	Concurrency      int
	ProbeTimeout     time.Duration
	MaxAttempts      int
	RetryDelay       time.Duration
	RefreshInterval  time.Duration
	MaxCycleDuration time.Duration
	OnCycleIssue     func(CapabilityHealthCycleIssue)
}

type CapabilityHealthCycleIssue struct {
	Code string
}

type capabilityHealthCycleError struct {
	code  string
	cause error
}

func (e *capabilityHealthCycleError) Error() string { return e.code }
func (e *capabilityHealthCycleError) Unwrap() error { return e.cause }

type CapabilityHealthObserver struct {
	targets  CapabilityProbeTargetSource
	probers  map[CapabilityKey]CapabilityProviderProber
	recorder CapabilityOutcomeRecorder
	options  CapabilityHealthObserverOptions
}

type DBCapabilityProbeTargetSource struct {
	db                *gorm.DB
	configuration     config.CapabilityConfiguration
	gstProbeSupported bool
	mu                sync.Mutex
	afterBusinessID   string
}

func NewDBCapabilityProbeTargetSource(db *gorm.DB, configuration config.CapabilityConfiguration, gstProbeSupported bool) *DBCapabilityProbeTargetSource {
	return &DBCapabilityProbeTargetSource{db: db, configuration: configuration, gstProbeSupported: gstProbeSupported}
}

func (s *DBCapabilityProbeTargetSource) DiscoverCapabilityProbeTargets(ctx context.Context, limit int) ([]CapabilityProbeTarget, error) {
	if s == nil || s.db == nil || limit <= 0 {
		return nil, nil
	}
	perBusiness := 0
	if s.configuration.Razorpay {
		perBusiness++
	}
	if s.configuration.AI {
		perBusiness++
	}
	if s.configuration.GST && s.gstProbeSupported {
		perBusiness++
	}
	if perBusiness == 0 {
		return nil, nil
	}
	businessLimit := max(1, limit/perBusiness)
	s.mu.Lock()
	defer s.mu.Unlock()
	var businessIDs []string
	query := s.db.WithContext(ctx).Model(&models.BusinessProfile{}).Where("deleted_at IS NULL")
	if s.afterBusinessID != "" {
		query = query.Where("id > ?", s.afterBusinessID)
	}
	if err := query.Order("id ASC").Limit(businessLimit).Pluck("id", &businessIDs).Error; err != nil {
		return nil, err
	}
	if len(businessIDs) < businessLimit && s.afterBusinessID != "" {
		var wrapped []string
		if err := s.db.WithContext(ctx).Model(&models.BusinessProfile{}).
			Where("deleted_at IS NULL AND id <= ?", s.afterBusinessID).
			Order("id ASC").Limit(businessLimit-len(businessIDs)).Pluck("id", &wrapped).Error; err != nil {
			return nil, err
		}
		businessIDs = append(businessIDs, wrapped...)
	}
	if len(businessIDs) > 0 {
		s.afterBusinessID = businessIDs[len(businessIDs)-1]
	}
	gstBusinesses := make(map[string]struct{})
	if s.configuration.GST && s.gstProbeSupported && len(businessIDs) > 0 {
		var ids []string
		if err := s.db.WithContext(ctx).Model(&models.GSTIntegrationAccount{}).
			Where("business_id IN ? AND deleted_at IS NULL", businessIDs).
			Distinct().Pluck("business_id", &ids).Error; err != nil {
			return nil, err
		}
		for _, id := range ids {
			gstBusinesses[id] = struct{}{}
		}
	}
	targets := make([]CapabilityProbeTarget, 0, min(limit, len(businessIDs)*perBusiness))
	for _, businessID := range businessIDs {
		if s.configuration.Razorpay && len(targets) < limit {
			targets = append(targets, CapabilityProbeTarget{BusinessID: businessID, HealthKey: CapabilityRazorpay, ProbeGroup: "global:razorpay"})
		}
		if s.configuration.AI && len(targets) < limit {
			targets = append(targets, CapabilityProbeTarget{BusinessID: businessID, HealthKey: CapabilityAI, ProbeGroup: "global:ai"})
		}
		if _, ok := gstBusinesses[businessID]; ok && len(targets) < limit {
			targets = append(targets, CapabilityProbeTarget{BusinessID: businessID, HealthKey: CapabilityGSTProvider, ProbeGroup: "gst:" + businessID})
		}
	}
	return targets, nil
}

func (s *DBCapabilityProbeTargetSource) CapabilityProbeCensus(ctx context.Context, limit int) (CapabilityProbeCensus, error) {
	if s == nil || s.db == nil {
		return CapabilityProbeCensus{}, nil
	}
	var businessCount int64
	if err := s.db.WithContext(ctx).Model(&models.BusinessProfile{}).Where("deleted_at IS NULL").Count(&businessCount).Error; err != nil {
		return CapabilityProbeCensus{}, err
	}
	perBusiness := int64(0)
	if s.configuration.Razorpay {
		perBusiness++
	}
	if s.configuration.AI {
		perBusiness++
	}
	active := businessCount * perBusiness
	if s.configuration.GST && s.gstProbeSupported {
		var gstBusinessCount int64
		if err := s.db.WithContext(ctx).Model(&models.GSTIntegrationAccount{}).
			Where("deleted_at IS NULL").Distinct("business_id").Count(&gstBusinessCount).Error; err != nil {
			return CapabilityProbeCensus{}, err
		}
		active += gstBusinessCount
	}
	perBusinessMax := int(perBusiness)
	if s.configuration.GST && s.gstProbeSupported {
		perBusinessMax++
	}
	businessesPerCycle := max(1, limit/max(1, perBusinessMax))
	sweepCycles := max(1, (int(businessCount)+businessesPerCycle-1)/businessesPerCycle)
	return CapabilityProbeCensus{ActiveTargets: int(active), SweepCycles: sweepCycles}, nil
}

func NewCapabilityHealthObserver(
	targets CapabilityProbeTargetSource,
	probers map[CapabilityKey]CapabilityProviderProber,
	recorder CapabilityOutcomeRecorder,
	options CapabilityHealthObserverOptions,
) *CapabilityHealthObserver {
	if options.MaxTargets <= 0 || options.MaxTargets > 4096 {
		// Twenty worst-case tenant-specific groups use at most five worker
		// waves. Two five-second attempts therefore leave ten seconds of the
		// one-minute cycle for census, discovery, recording, and shutdown.
		options.MaxTargets = 20
	}
	if options.Concurrency <= 0 || options.Concurrency > 16 {
		options.Concurrency = 4
	}
	if options.ProbeTimeout <= 0 || options.ProbeTimeout > 30*time.Second {
		options.ProbeTimeout = 5 * time.Second
	}
	if options.MaxAttempts <= 0 || options.MaxAttempts > 3 {
		options.MaxAttempts = 2
	}
	if options.RetryDelay < 0 || options.RetryDelay > time.Second {
		options.RetryDelay = 100 * time.Millisecond
	}
	if options.RefreshInterval <= 0 {
		options.RefreshInterval = 2 * time.Minute
	}
	if options.MaxCycleDuration <= 0 || options.MaxCycleDuration > 5*time.Minute {
		options.MaxCycleDuration = time.Minute
	}
	return &CapabilityHealthObserver{targets: targets, probers: probers, recorder: recorder, options: options}
}

func (o *CapabilityHealthObserver) ObserveOnce(ctx context.Context) error {
	if o == nil || o.targets == nil || o.recorder == nil {
		return nil
	}
	cycleCtx, cancel := context.WithTimeout(ctx, o.options.MaxCycleDuration)
	defer cancel()
	censusResult := CapabilityProbeCensus{}
	if census, ok := o.targets.(CapabilityProbeTargetCensus); ok {
		var err error
		censusResult, err = census.CapabilityProbeCensus(cycleCtx, o.options.MaxTargets)
		if err != nil {
			return newCapabilityHealthCycleError("target_census_failed", err)
		}
		if capacity, ok := o.recorder.(CapabilityOutcomeCapacity); ok {
			capacity.EnsureActiveCapacity(censusResult.ActiveTargets)
		}
	}
	targets, err := o.targets.DiscoverCapabilityProbeTargets(cycleCtx, o.options.MaxTargets)
	if err != nil {
		return newCapabilityHealthCycleError("target_discovery_failed", err)
	}
	if censusResult.ActiveTargets == 0 {
		censusResult.ActiveTargets = len(targets)
	}
	if censusResult.SweepCycles == 0 {
		censusResult.SweepCycles = max(1, (censusResult.ActiveTargets+o.options.MaxTargets-1)/o.options.MaxTargets)
	}
	freshFor := o.fullSweepFreshness(censusResult.SweepCycles)
	groups := groupCapabilityProbeTargets(targets, o.options.MaxTargets)
	if len(groups) == 0 {
		return nil
	}
	jobs := make(chan []CapabilityProbeTarget)
	errorsOut := make(chan error, len(groups))
	var wait sync.WaitGroup
	workerCount := min(o.options.Concurrency, len(groups))
	for index := 0; index < workerCount; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for group := range jobs {
				if err := o.observeGroup(cycleCtx, group, freshFor); err != nil {
					errorsOut <- err
				}
			}
		}()
	}
	for _, group := range groups {
		select {
		case <-cycleCtx.Done():
			close(jobs)
			wait.Wait()
			return newCapabilityHealthCycleError("cycle_deadline_exceeded", cycleCtx.Err())
		case jobs <- group:
		}
	}
	close(jobs)
	wait.Wait()
	close(errorsOut)
	for observeErr := range errorsOut {
		return observeErr
	}
	return nil
}

func newCapabilityHealthCycleError(code string, cause error) error {
	return &capabilityHealthCycleError{code: code, cause: cause}
}

func (o *CapabilityHealthObserver) fullSweepFreshness(cycles int) time.Duration {
	return time.Duration(cycles)*(o.options.RefreshInterval+o.options.MaxCycleDuration) + o.options.MaxCycleDuration
}

// Run refreshes provider observations outside customer requests. Cycles never
// overlap, and every probe remains bounded by ObserveOnce's worker and timeout
// limits.
func (o *CapabilityHealthObserver) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = o.options.RefreshInterval
	}
	if interval > o.options.RefreshInterval {
		interval = o.options.RefreshInterval
	}
	for {
		if err := o.ObserveOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if o.options.OnCycleIssue != nil {
				code := "observation_cycle_failed"
				var cycleError *capabilityHealthCycleError
				if errors.As(err, &cycleError) {
					code = cycleError.code
				}
				o.options.OnCycleIssue(CapabilityHealthCycleIssue{Code: code})
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (o *CapabilityHealthObserver) observeGroup(ctx context.Context, targets []CapabilityProbeTarget, freshFor time.Duration) error {
	if len(targets) == 0 {
		return nil
	}
	prober := o.probers[targets[0].HealthKey]
	if prober == nil {
		return nil
	}
	var outcome CapabilityProviderOutcome
	for attempt := 1; attempt <= o.options.MaxAttempts; attempt++ {
		probeCtx, cancel := context.WithTimeout(ctx, o.options.ProbeTimeout)
		outcome = prober.ProbeCapability(probeCtx, targets[0])
		cancel()
		if outcome.Err == nil || providerOutcomeHTTPStatus(outcome) != 0 || errors.Is(outcome.Err, ErrCapabilityProbeUnsupported) {
			break
		}
		if attempt < o.options.MaxAttempts && o.options.RetryDelay > 0 {
			timer := time.NewTimer(o.options.RetryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	if ctx.Err() != nil {
		return newCapabilityHealthCycleError("cycle_deadline_exceeded", ctx.Err())
	}
	if errors.Is(outcome.Err, ErrCapabilityProbeUnsupported) {
		return nil
	}
	outcome.FreshFor = freshFor
	for _, target := range targets {
		if err := o.recorder.RecordOutcome(target.BusinessID, target.HealthKey, outcome); err != nil {
			return newCapabilityHealthCycleError("observation_record_failed", err)
		}
	}
	return nil
}

func groupCapabilityProbeTargets(targets []CapabilityProbeTarget, limit int) [][]CapabilityProbeTarget {
	grouped := make(map[string][]CapabilityProbeTarget)
	keys := make([]string, 0)
	accepted := 0
	for _, target := range targets {
		if accepted >= limit || strings.TrimSpace(target.BusinessID) == "" || target.HealthKey == "" {
			continue
		}
		groupKey := strings.TrimSpace(target.ProbeGroup)
		if groupKey == "" {
			groupKey = target.BusinessID + "\x00" + string(target.HealthKey)
		}
		if _, exists := grouped[groupKey]; !exists {
			keys = append(keys, groupKey)
		}
		grouped[groupKey] = append(grouped[groupKey], target)
		accepted++
	}
	sort.Strings(keys)
	result := make([][]CapabilityProbeTarget, 0, len(keys))
	for _, key := range keys {
		result = append(result, grouped[key])
	}
	return result
}
