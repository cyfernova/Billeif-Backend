package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

const (
	a2aTestUserID         = "11111111-1111-1111-1111-111111111111"
	a2aTestOtherUserID    = "22222222-2222-2222-2222-222222222222"
	a2aTestBusinessID     = "33333333-3333-3333-3333-333333333333"
	a2aTestOtherBusiness  = "44444444-4444-4444-4444-444444444444"
	a2aTestBuyerAgentID   = "55555555-5555-5555-5555-555555555555"
	a2aTestSellerAgentID  = "66666666-6666-6666-6666-666666666666"
	a2aTestNegotiationID  = "77777777-7777-7777-7777-777777777777"
	a2aTestForeignAgentID = "88888888-8888-8888-8888-888888888888"
)

type a2aNegotiationRepoFake struct {
	interfaces.AP2Repository
	agents                 map[string]*models.Agent
	negotiations           map[string]*models.BargainingNegotiation
	createErr              error
	agentScopeErr          error
	scopedNegotiationErr   error
	scopedSessionErr       error
	exactTupleErr          error
	created                *models.BargainingNegotiation
	createHook             func(*models.BargainingNegotiation)
	stopCalls              int
	internalStopCalls      int
	internalStopHook       func(*models.BargainingNegotiation)
	scopedNegotiationReads int
	scopedSessionReads     int
	exactTupleReads        int
}

func (r *a2aNegotiationRepoFake) GetAgentByID(_ context.Context, id string) (*models.Agent, error) {
	agent := r.agents[id]
	if agent == nil {
		return nil, errors.New("agent not found")
	}
	copy := *agent
	return &copy, nil
}

func (r *a2aNegotiationRepoFake) GetAgentByIDForOwnerAndBusiness(
	_ context.Context,
	id,
	ownerID,
	businessID string,
) (*models.Agent, error) {
	if r.agentScopeErr != nil {
		return nil, r.agentScopeErr
	}
	agent := r.agents[id]
	if agent == nil || agent.OwnerID != ownerID || agent.BusinessID != businessID || !agent.IsActive {
		return nil, interfaces.ErrA2ANegotiationScopeNotFound
	}
	copy := *agent
	return &copy, nil
}

func (r *a2aNegotiationRepoFake) CreateBargainingNegotiation(_ context.Context, negotiation *models.BargainingNegotiation) error {
	if r.createHook != nil {
		r.createHook(negotiation)
	}
	if r.createErr != nil {
		return r.createErr
	}
	copy := *negotiation
	if copy.ID == "" {
		copy.ID = a2aTestNegotiationID
		negotiation.ID = copy.ID
	}
	r.created = &copy
	if r.negotiations == nil {
		r.negotiations = make(map[string]*models.BargainingNegotiation)
	}
	r.negotiations[copy.ID] = &copy
	return nil
}

func (r *a2aNegotiationRepoFake) GetBargainingNegotiationByID(_ context.Context, id string) (*models.BargainingNegotiation, error) {
	negotiation := r.negotiations[id]
	if negotiation == nil {
		return nil, errors.New("negotiation not found")
	}
	copy := *negotiation
	return &copy, nil
}

func (r *a2aNegotiationRepoFake) GetBargainingNegotiationByIDForScope(
	_ context.Context,
	id,
	userID,
	businessID string,
) (*models.BargainingNegotiation, error) {
	r.scopedNegotiationReads++
	if r.scopedNegotiationErr != nil {
		return nil, r.scopedNegotiationErr
	}
	negotiation := r.negotiations[id]
	if negotiation == nil || negotiation.UserID != userID || negotiation.BusinessID != businessID {
		return nil, interfaces.ErrA2ANegotiationScopeNotFound
	}
	copy := *negotiation
	return &copy, nil
}

func (r *a2aNegotiationRepoFake) GetBargainingNegotiationBySessionIDForScope(
	_ context.Context,
	sessionID,
	userID,
	businessID string,
) (*models.BargainingNegotiation, error) {
	r.scopedSessionReads++
	if r.scopedSessionErr != nil {
		return nil, r.scopedSessionErr
	}
	for _, negotiation := range r.negotiations {
		if negotiation.SessionID != nil && *negotiation.SessionID == sessionID &&
			negotiation.UserID == userID && negotiation.BusinessID == businessID {
			copy := *negotiation
			return &copy, nil
		}
	}
	return nil, interfaces.ErrA2ANegotiationScopeNotFound
}

func (r *a2aNegotiationRepoFake) GetBargainingNegotiationBySessionAndID(
	_ context.Context,
	sessionID,
	negotiationID string,
) (*models.BargainingNegotiation, error) {
	r.exactTupleReads++
	if r.exactTupleErr != nil {
		return nil, r.exactTupleErr
	}
	negotiation := r.negotiations[negotiationID]
	if negotiation == nil || negotiation.SessionID == nil || *negotiation.SessionID != sessionID {
		return nil, interfaces.ErrA2ANegotiationScopeNotFound
	}
	copy := *negotiation
	return &copy, nil
}

func (r *a2aNegotiationRepoFake) StopBargainingNegotiationForScope(
	_ context.Context,
	id,
	userID,
	businessID string,
	completedAt time.Time,
) (bool, error) {
	r.stopCalls++
	negotiation := r.negotiations[id]
	if negotiation == nil || negotiation.UserID != userID || negotiation.BusinessID != businessID {
		return false, nil
	}
	if negotiation.Status == "stopped" {
		return true, nil
	}
	if isTerminalNegotiationStatus(negotiation.Status) {
		return false, nil
	}
	negotiation.Status = "stopped"
	negotiation.CompletedAt = &completedAt
	return true, nil
}

func (r *a2aNegotiationRepoFake) StopBargainingNegotiationBySessionAndID(
	_ context.Context,
	sessionID,
	id string,
	completedAt time.Time,
) (bool, error) {
	r.internalStopCalls++
	negotiation := r.negotiations[id]
	if negotiation == nil || negotiation.SessionID == nil || *negotiation.SessionID != sessionID {
		return false, nil
	}
	if r.internalStopHook != nil {
		r.internalStopHook(negotiation)
	}
	if isTerminalNegotiationStatus(negotiation.Status) {
		return false, nil
	}
	negotiation.Status = "stopped"
	negotiation.CompletedAt = &completedAt
	return true, nil
}

func newA2ANegotiationServiceForTest(t *testing.T, repo *a2aNegotiationRepoFake) *A2ABargainingService {
	t.Helper()
	log := logger.New()
	agentService := NewAgentService(repo, nil, nil, log)
	bargaining := NewBargainingService(repo, nil, agentService, nil, nil, log)
	return NewA2ABargainingService(nil, bargaining, nil, repo, nil, nil, log)
}

func validA2ANegotiationRepo() *a2aNegotiationRepoFake {
	return &a2aNegotiationRepoFake{
		agents: map[string]*models.Agent{
			a2aTestBuyerAgentID: {
				ID: a2aTestBuyerAgentID, OwnerID: a2aTestUserID, BusinessID: a2aTestBusinessID,
				Type: "shopping", IsActive: true, Config: `{}`,
			},
			a2aTestSellerAgentID: {
				ID: a2aTestSellerAgentID, OwnerID: a2aTestBusinessID, BusinessID: a2aTestBusinessID,
				Type: "merchant", IsActive: true, IsPublic: true, Config: `{}`,
			},
		},
		negotiations: make(map[string]*models.BargainingNegotiation),
	}
}

func TestA2ABargainingStartPersistsAuthenticatedScopeBeforePublishingSession(t *testing.T) {
	repo := validA2ANegotiationRepo()
	service := newA2ANegotiationServiceForTest(t, repo)
	repo.createHook = func(negotiation *models.BargainingNegotiation) {
		if len(service.sessions) != 0 {
			t.Fatal("session became visible before its ownership scope was durable")
		}
		if negotiation.UserID != a2aTestUserID || negotiation.BusinessID != a2aTestBusinessID {
			t.Fatalf("persisted scope = %q/%q", negotiation.UserID, negotiation.BusinessID)
		}
		if negotiation.SessionID == nil || *negotiation.SessionID == "" {
			t.Fatal("persisted negotiation is missing its immutable session ID")
		}
	}

	session, err := service.StartAutonomousNegotiation(
		context.Background(),
		A2ANegotiationScope{UserID: a2aTestUserID, BusinessID: a2aTestBusinessID},
		&AutonomousNegotiationRequest{
			BuyerAgentID: a2aTestBuyerAgentID, SellerAgentID: a2aTestSellerAgentID,
			InitialAmount: 100, MaxRounds: 3, UserID: a2aTestOtherUserID,
		},
	)
	if err != nil {
		t.Fatalf("start autonomous negotiation: %v", err)
	}
	if session.UserID != a2aTestUserID || session.BusinessID != a2aTestBusinessID {
		t.Fatalf("session scope = %q/%q", session.UserID, session.BusinessID)
	}
	if repo.created == nil || repo.created.UserID != a2aTestUserID || repo.created.BusinessID != a2aTestBusinessID {
		t.Fatalf("created negotiation scope = %#v", repo.created)
	}
}

func TestA2ABargainingRawStartRejectsForeignBuyerAndCrossBusinessSeller(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*a2aNegotiationRepoFake)
	}{
		{
			name: "buyer belongs to another user",
			mutate: func(repo *a2aNegotiationRepoFake) {
				repo.agents[a2aTestBuyerAgentID].OwnerID = a2aTestOtherUserID
			},
		},
		{
			name: "buyer belongs to another business",
			mutate: func(repo *a2aNegotiationRepoFake) {
				repo.agents[a2aTestBuyerAgentID].BusinessID = a2aTestOtherBusiness
			},
		},
		{
			name: "public seller belongs to another business",
			mutate: func(repo *a2aNegotiationRepoFake) {
				repo.agents[a2aTestSellerAgentID].OwnerID = a2aTestOtherBusiness
				repo.agents[a2aTestSellerAgentID].BusinessID = a2aTestOtherBusiness
				repo.agents[a2aTestSellerAgentID].IsPublic = true
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := validA2ANegotiationRepo()
			test.mutate(repo)
			service := newA2ANegotiationServiceForTest(t, repo)

			_, err := service.StartNegotiation(
				context.Background(),
				A2ANegotiationScope{UserID: a2aTestUserID, BusinessID: a2aTestBusinessID},
				a2aTestBuyerAgentID,
				a2aTestSellerAgentID,
				100,
			)
			if !errors.Is(err, ErrA2ANegotiationNotFound) {
				t.Fatalf("StartNegotiation() error = %v, want not found", err)
			}
			if repo.created != nil || len(service.sessions) != 0 {
				t.Fatal("unauthorized start persisted or published a session")
			}
		})
	}
}

func TestBargainingServicePreservesProcurementCrossBusinessSellerPolicy(t *testing.T) {
	repo := validA2ANegotiationRepo()
	repo.agents[a2aTestSellerAgentID].OwnerID = a2aTestOtherBusiness
	repo.agents[a2aTestSellerAgentID].BusinessID = a2aTestOtherBusiness
	log := logger.New()
	agents := NewAgentService(repo, nil, nil, log)
	bargaining := NewBargainingService(repo, nil, agents, nil, nil, log)

	negotiation, err := bargaining.CreateNegotiation(context.Background(), &CreateNegotiationRequest{
		BuyerAgentID: a2aTestBuyerAgentID, SellerAgentID: a2aTestSellerAgentID,
		UserID: a2aTestUserID, InitialAmount: 100, MaxRounds: 3,
		Metadata: map[string]interface{}{"procurement_managed_a2a": true},
	})
	if err != nil {
		t.Fatalf("procurement cross-business seller negotiation: %v", err)
	}
	if negotiation.BusinessID != a2aTestBusinessID {
		t.Fatalf("negotiation business scope = %q, want buyer business %q", negotiation.BusinessID, a2aTestBusinessID)
	}
}

func TestA2ABargainingStartDoesNotPublishSessionWhenPersistenceFails(t *testing.T) {
	repo := validA2ANegotiationRepo()
	repo.createErr = errors.New("database unavailable")
	service := newA2ANegotiationServiceForTest(t, repo)

	_, err := service.StartNegotiation(
		context.Background(),
		A2ANegotiationScope{UserID: a2aTestUserID, BusinessID: a2aTestBusinessID},
		a2aTestBuyerAgentID,
		a2aTestSellerAgentID,
		100,
	)
	if err == nil {
		t.Fatal("StartNegotiation() returned nil error")
	}
	if len(service.sessions) != 0 {
		t.Fatal("failed persistence left a visible in-memory session")
	}
}

func TestA2ABargainingStartPropagatesScopedRepositoryFailureWithoutPublishing(t *testing.T) {
	repositoryFailure := errors.New("database unavailable")
	repo := validA2ANegotiationRepo()
	repo.agentScopeErr = repositoryFailure
	service := newA2ANegotiationServiceForTest(t, repo)

	_, err := service.StartNegotiation(
		context.Background(),
		A2ANegotiationScope{UserID: a2aTestUserID, BusinessID: a2aTestBusinessID},
		a2aTestBuyerAgentID,
		a2aTestSellerAgentID,
		100,
	)
	if !errors.Is(err, repositoryFailure) {
		t.Fatalf("StartNegotiation() error = %v, want repository failure", err)
	}
	if errors.Is(err, ErrA2ANegotiationNotFound) {
		t.Fatalf("repository failure was collapsed to not found: %v", err)
	}
	if repo.created != nil || len(service.sessions) != 0 {
		t.Fatal("repository failure persisted or published a negotiation")
	}
}

func TestA2ABargainingScopedReadFailuresPropagateWithoutMutation(t *testing.T) {
	repositoryFailure := errors.New("database unavailable")
	sessionID := "a2a_session_storage_failure"
	ownerScope := A2ANegotiationScope{UserID: a2aTestUserID, BusinessID: a2aTestBusinessID}

	t.Run("progress by session", func(t *testing.T) {
		repo := validA2ANegotiationRepo()
		repo.scopedSessionErr = repositoryFailure
		service := newA2ANegotiationServiceForTest(t, repo)
		if _, err := service.GetSessionProgress(context.Background(), ownerScope, sessionID); !errors.Is(err, repositoryFailure) {
			t.Fatalf("progress error = %v, want repository failure", err)
		}
	})

	t.Run("result by negotiation", func(t *testing.T) {
		repo := validA2ANegotiationRepo()
		repo.scopedNegotiationErr = repositoryFailure
		service := newA2ANegotiationServiceForTest(t, repo)
		if _, err := service.GetSessionProgressByNegotiationID(context.Background(), ownerScope, a2aTestNegotiationID); !errors.Is(err, repositoryFailure) {
			t.Fatalf("result error = %v, want repository failure", err)
		}
	})

	t.Run("stop", func(t *testing.T) {
		repo := validA2ANegotiationRepo()
		repo.scopedSessionErr = repositoryFailure
		service := newA2ANegotiationServiceForTest(t, repo)
		service.sessions[sessionID] = &A2ASession{
			NegotiationID: sessionID, DBNegotiationID: a2aTestNegotiationID,
			Status: "running", MaxRounds: 5,
		}
		if err := service.StopNegotiation(context.Background(), ownerScope, sessionID); !errors.Is(err, repositoryFailure) {
			t.Fatalf("stop error = %v, want repository failure", err)
		}
		if repo.stopCalls != 0 {
			t.Fatalf("repository failure reached stop mutation %d times", repo.stopCalls)
		}
		if got := service.sessions[sessionID].ToProgressResponse().Status; got != "running" {
			t.Fatalf("repository failure changed cached status to %q", got)
		}
	})
}

func TestA2ABargainingTupleReadFailuresRemainRetryableBeforeWork(t *testing.T) {
	repositoryFailure := errors.New("database unavailable")
	repo := validA2ANegotiationRepo()
	repo.exactTupleErr = repositoryFailure
	service := newA2ANegotiationServiceForTest(t, repo)
	service.sessions["session-1"] = &A2ASession{
		NegotiationID:   "session-1",
		DBNegotiationID: a2aTestNegotiationID,
		Status:          "running",
		MaxRounds:       5,
	}
	service.sessionLocks["session-1"] = &sync.Mutex{}

	operations := []struct {
		name string
		run  func() error
	}{
		{
			name: "autonomous loop",
			run: func() error {
				return service.RunAutonomousNegotiation(context.Background(), "session-1")
			},
		},
		{
			name: "progress",
			run: func() error {
				_, err := service.GetAutonomousNegotiationProgress(context.Background(), "session-1", a2aTestNegotiationID)
				return err
			},
		},
		{
			name: "enqueue",
			run: func() error {
				return service.EnqueueNegotiationRound(context.Background(), "session-1", a2aTestNegotiationID, 0)
			},
		},
		{
			name: "round",
			run: func() error {
				return service.RunAutonomousNegotiationRound(context.Background(), "session-1", a2aTestNegotiationID, 1)
			},
		},
		{
			name: "internal stop",
			run: func() error {
				return service.StopAutonomousNegotiation(context.Background(), "session-1", a2aTestNegotiationID)
			},
		},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			err := operation.run()
			if !errors.Is(err, repositoryFailure) {
				t.Fatalf("error = %v, want repository failure", err)
			}
			if errors.Is(err, ErrA2ANegotiationNotFound) {
				t.Fatalf("repository failure was collapsed to not found: %v", err)
			}
		})
	}
	if repo.internalStopCalls != 0 {
		t.Fatalf("tuple read failure reached internal stop mutation %d times", repo.internalStopCalls)
	}
}

func TestA2ABargainingLifecycleRequiresPersistedUserAndBusinessScope(t *testing.T) {
	repo := validA2ANegotiationRepo()
	sessionID := "a2a_session_owned"
	repo.negotiations[a2aTestNegotiationID] = &models.BargainingNegotiation{
		ID: a2aTestNegotiationID, SessionID: &sessionID,
		BuyerAgentID: a2aTestBuyerAgentID, SellerAgentID: a2aTestSellerAgentID,
		UserID: a2aTestUserID, BusinessID: a2aTestBusinessID,
		InitialAmount: 100, CurrentAmount: 100, MaxRounds: 5, Status: "in_progress",
	}
	service := newA2ANegotiationServiceForTest(t, repo)
	service.sessions[sessionID] = &A2ASession{
		NegotiationID: sessionID, DBNegotiationID: a2aTestNegotiationID,
		BuyerAgentID: a2aTestBuyerAgentID, SellerAgentID: a2aTestSellerAgentID,
		UserID: a2aTestUserID, BusinessID: a2aTestBusinessID,
		InitialAmount: 100, CurrentAmount: 100, MaxRounds: 5, Status: "running",
	}

	foreignScopes := []A2ANegotiationScope{
		{UserID: a2aTestOtherUserID, BusinessID: a2aTestBusinessID},
		{UserID: a2aTestUserID, BusinessID: a2aTestOtherBusiness},
	}
	for _, scope := range foreignScopes {
		if _, err := service.GetSessionProgress(context.Background(), scope, sessionID); !errors.Is(err, ErrA2ANegotiationNotFound) {
			t.Fatalf("foreign session progress error = %v", err)
		}
		if _, err := service.GetSessionProgressByNegotiationID(context.Background(), scope, a2aTestNegotiationID); !errors.Is(err, ErrA2ANegotiationNotFound) {
			t.Fatalf("foreign result error = %v", err)
		}
		if err := service.StopNegotiation(context.Background(), scope, sessionID); !errors.Is(err, ErrA2ANegotiationNotFound) {
			t.Fatalf("foreign stop error = %v", err)
		}
	}
	if repo.stopCalls != 0 {
		t.Fatalf("foreign stop reached mutation %d times", repo.stopCalls)
	}
	if got := service.sessions[sessionID].ToProgressResponse().Status; got != "running" {
		t.Fatalf("foreign stop changed cached status to %q", got)
	}

	ownerScope := A2ANegotiationScope{UserID: a2aTestUserID, BusinessID: a2aTestBusinessID}
	if err := service.StopNegotiation(context.Background(), ownerScope, sessionID); err != nil {
		t.Fatalf("owner stop: %v", err)
	}
	progress, err := service.GetSessionProgress(context.Background(), ownerScope, sessionID)
	if err != nil {
		t.Fatalf("owner progress after stop: %v", err)
	}
	if progress.Status != "stopped" || repo.negotiations[a2aTestNegotiationID].Status != "stopped" {
		t.Fatalf("cached/database statuses = %q/%q", progress.Status, repo.negotiations[a2aTestNegotiationID].Status)
	}
}

func TestA2ABargainingEnqueueChecksExactDurableStateBeforeSQS(t *testing.T) {
	repo := validA2ANegotiationRepo()
	sessionID := "a2a_session_stopped"
	repo.negotiations[a2aTestNegotiationID] = &models.BargainingNegotiation{
		ID: a2aTestNegotiationID, SessionID: &sessionID,
		UserID: a2aTestUserID, BusinessID: a2aTestBusinessID,
		Status: "stopped", Rounds: 1, MaxRounds: 5,
	}
	service := newA2ANegotiationServiceForTest(t, repo)

	if err := service.EnqueueNegotiationRound(
		context.Background(),
		sessionID,
		a2aTestNegotiationID,
		1,
	); err != nil {
		t.Fatalf("enqueue stopped negotiation should no-op before SQS: %v", err)
	}
	if err := service.EnqueueNegotiationRound(
		context.Background(),
		"wrong-session",
		a2aTestNegotiationID,
		1,
	); !errors.Is(err, ErrA2ANegotiationNotFound) {
		t.Fatalf("enqueue mismatched tuple error = %v", err)
	}
}

func TestRunAutonomousNegotiationRoundNoOpsStoppedStateBeforeLLM(t *testing.T) {
	repo := validA2ANegotiationRepo()
	sessionID := "a2a_session_stopped_worker"
	repo.negotiations[a2aTestNegotiationID] = &models.BargainingNegotiation{
		ID: a2aTestNegotiationID, SessionID: &sessionID,
		BuyerAgentID: a2aTestBuyerAgentID, SellerAgentID: a2aTestSellerAgentID,
		UserID: a2aTestUserID, BusinessID: a2aTestBusinessID,
		InitialAmount: 100, CurrentAmount: 90, Rounds: 1, MaxRounds: 5, Status: "stopped",
	}
	service := newA2ANegotiationServiceForTest(t, repo)
	service.sessions[sessionID] = &A2ASession{
		NegotiationID: sessionID, DBNegotiationID: a2aTestNegotiationID,
		BuyerAgentID: a2aTestBuyerAgentID, SellerAgentID: a2aTestSellerAgentID,
		CurrentAmount: 100, Round: 0, MaxRounds: 5, Status: "running",
	}

	if err := service.RunAutonomousNegotiationRound(
		context.Background(),
		sessionID,
		a2aTestNegotiationID,
		2,
	); err != nil {
		t.Fatalf("stopped round should no-op before unavailable LLM: %v", err)
	}
	if got := service.sessions[sessionID].ToProgressResponse(); got.Status != "stopped" || got.Round != 1 {
		t.Fatalf("cached stopped progress = %#v", got)
	}
	if service.sessions[sessionID].ProcessingRound != 0 {
		t.Fatalf("stopped round claimed in memory: %d", service.sessions[sessionID].ProcessingRound)
	}
}

func TestA2ASessionStoppedStateWinsConcurrentProgressRefreshes(t *testing.T) {
	session := &A2ASession{
		NegotiationID: "session-1", DBNegotiationID: "negotiation-1",
		CurrentAmount: 100, MaxRounds: 5, Status: "running",
	}
	stopped := &A2ASessionProgress{
		NegotiationID: "session-1", NegotiationUUID: "negotiation-1",
		CurrentAmount: 90, Round: 1, MaxRounds: 5, Status: "stopped",
	}
	stale := &A2ASessionProgress{
		NegotiationID: "session-1", NegotiationUUID: "negotiation-1",
		CurrentAmount: 80, Round: 2, MaxRounds: 5, Status: "in_progress",
	}

	var workers sync.WaitGroup
	workers.Add(3)
	go func() {
		defer workers.Done()
		for range 1_000 {
			session.applyProgress(stopped)
		}
	}()
	go func() {
		defer workers.Done()
		for range 1_000 {
			session.applyProgress(stale)
		}
	}()
	go func() {
		defer workers.Done()
		for range 1_000 {
			_ = session.ToProgressResponse()
		}
	}()
	workers.Wait()

	got := session.ToProgressResponse()
	if got.Status != "stopped" || got.Round != 1 || got.CurrentAmount != 90 {
		t.Fatalf("terminal cached state was revived: %#v", got)
	}
}

func TestA2ABargainingInternalStopKeepsConcurrentDurableTerminalWinner(t *testing.T) {
	repo := validA2ANegotiationRepo()
	sessionID := "a2a_session_concurrent_terminal"
	repo.negotiations[a2aTestNegotiationID] = &models.BargainingNegotiation{
		ID: a2aTestNegotiationID, SessionID: &sessionID,
		UserID: a2aTestUserID, BusinessID: a2aTestBusinessID,
		Status: "in_progress", Rounds: 1, MaxRounds: 5,
	}
	repo.internalStopHook = func(negotiation *models.BargainingNegotiation) {
		negotiation.Status = "accepted"
	}
	service := newA2ANegotiationServiceForTest(t, repo)
	service.sessions[sessionID] = &A2ASession{
		NegotiationID: sessionID, DBNegotiationID: a2aTestNegotiationID,
		Status: "running", Round: 1, MaxRounds: 5,
	}

	if err := service.StopAutonomousNegotiation(
		context.Background(),
		sessionID,
		a2aTestNegotiationID,
	); err != nil {
		t.Fatalf("internal stop with concurrent terminal transition: %v", err)
	}
	if got := service.sessions[sessionID].ToProgressResponse().Status; got != "accepted" {
		t.Fatalf("cached terminal winner = %q, want accepted", got)
	}
}

func TestA2ABargainingSessionLockLookupIsRaceSafe(t *testing.T) {
	repo := validA2ANegotiationRepo()
	sessionID := "a2a_session_lock_race"
	repo.negotiations[a2aTestNegotiationID] = &models.BargainingNegotiation{
		ID: a2aTestNegotiationID, SessionID: &sessionID,
		UserID: a2aTestUserID, BusinessID: a2aTestBusinessID,
		Status: "stopped", MaxRounds: 5,
	}
	service := newA2ANegotiationServiceForTest(t, repo)
	service.log = logger.NewWithConfig(logger.Config{Level: "fatal"})
	service.sessions[sessionID] = &A2ASession{
		NegotiationID: sessionID, DBNegotiationID: a2aTestNegotiationID,
		Status: "stopped", MaxRounds: 5,
	}
	service.sessionLocks[sessionID] = &sync.Mutex{}

	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for range 1_000 {
			service.sessionsLock.Lock()
			service.sessionLocks[sessionID] = &sync.Mutex{}
			service.sessionsLock.Unlock()
		}
	}()
	go func() {
		defer workers.Done()
		for range 1_000 {
			if err := service.RunAutonomousNegotiation(context.Background(), sessionID); err != nil {
				t.Errorf("run stopped negotiation: %v", err)
				return
			}
		}
	}()
	workers.Wait()
}
