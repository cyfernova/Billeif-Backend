package unit

import (
	"context"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockBargainingAP2Repository is a mock for AP2Repository methods used by BargainingService
type MockBargainingAP2Repository struct {
	mock.Mock
}

func (m *MockBargainingAP2Repository) CreateBargainingNegotiation(ctx context.Context, negotiation *models.BargainingNegotiation) error {
	args := m.Called(ctx, negotiation)
	return args.Error(0)
}

func (m *MockBargainingAP2Repository) GetBargainingNegotiationByID(ctx context.Context, id string) (*models.BargainingNegotiation, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.BargainingNegotiation), args.Error(1)
}

func (m *MockBargainingAP2Repository) GetNegotiationsByUser(ctx context.Context, userID string, page, limit int) ([]*models.BargainingNegotiation, int64, error) {
	args := m.Called(ctx, userID, page, limit)
	return args.Get(0).([]*models.BargainingNegotiation), args.Get(1).(int64), args.Error(2)
}

func (m *MockBargainingAP2Repository) UpdateNegotiationStatus(ctx context.Context, id, status string) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}

func (m *MockBargainingAP2Repository) UpdateNegotiationAmountAndRounds(ctx context.Context, id string, amount float64, rounds int, status string) error {
	args := m.Called(ctx, id, amount, rounds, status)
	return args.Error(0)
}

func (m *MockBargainingAP2Repository) CompleteNegotiation(ctx context.Context, id, status string, finalAmount float64, completedAt *time.Time) error {
	args := m.Called(ctx, id, status, finalAmount, completedAt)
	return args.Error(0)
}

func (m *MockBargainingAP2Repository) CreateBargainingRound(ctx context.Context, round *models.BargainingRound) error {
	args := m.Called(ctx, round)
	return args.Error(0)
}

func (m *MockBargainingAP2Repository) GetBargainingRounds(ctx context.Context, negotiationID string) ([]*models.BargainingRound, error) {
	args := m.Called(ctx, negotiationID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.BargainingRound), args.Error(1)
}

func (m *MockBargainingAP2Repository) GetBargainingRoundsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.BargainingRound, int64, error) {
	args := m.Called(ctx, agentID, page, limit)
	return args.Get(0).([]*models.BargainingRound), args.Get(1).(int64), args.Error(2)
}

// MockAgentServiceForBargaining mocks AgentService methods used by BargainingService
type MockAgentServiceForBargaining struct {
	mock.Mock
}

func (m *MockAgentServiceForBargaining) GetAgentByID(ctx context.Context, id string) (*models.Agent, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Agent), args.Error(1)
}

// MockMenteeServiceForBargaining mocks MenteeService methods
type MockMenteeServiceForBargaining struct {
	mock.Mock
}

func (m *MockMenteeServiceForBargaining) GetBargainingDecision(ctx context.Context, agentID string, agentType string, currentAmount float64, initialAmount float64, round int, maxRounds int, opponentID string) (*services.BargainingDecision, error) {
	args := m.Called(ctx, agentID, agentType, currentAmount, initialAmount, round, maxRounds, opponentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.BargainingDecision), args.Error(1)
}

func (m *MockMenteeServiceForBargaining) RecordNegotiationOutcome(ctx context.Context, outcome *services.NegotiationOutcome) error {
	args := m.Called(ctx, outcome)
	return args.Error(0)
}

// TestCreateNegotiation_Success tests successful negotiation creation
func TestBargainingService_CreateNegotiation_Success(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	// Create service using reflection or a test constructor
	// For now, we'll test the pure functions
	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	ctx := context.Background()
	buyerAgentID := uuid.New().String()
	sellerAgentID := uuid.New().String()
	userID := uuid.New().String()

	buyerAgent := &models.Agent{
		ID:   buyerAgentID,
		Type: "shopping",
		Name: "Buyer Agent",
		Config: `{"volatility": 0.3}`,
	}

	sellerAgent := &models.Agent{
		ID:   sellerAgentID,
		Type: "merchant",
		Name: "Seller Agent",
		Config: `{"volatility": 0.5}`,
	}

	req := &services.CreateNegotiationRequest{
		BuyerAgentID:  buyerAgentID,
		SellerAgentID: sellerAgentID,
		UserID:       userID,
		InitialAmount: 1000.00,
		MaxRounds:    5,
	}

	mockAgent.On("GetAgentByID", ctx, buyerAgentID).Return(buyerAgent, nil)
	mockAgent.On("GetAgentByID", ctx, sellerAgentID).Return(sellerAgent, nil)
	mockAP2.On("CreateBargainingNegotiation", ctx, mock.AnythingOfType("*models.BargainingNegotiation")).Return(nil)

	negotiation, err := svc.CreateNegotiation(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, negotiation)
	assert.Equal(t, buyerAgentID, negotiation.BuyerAgentID)
	assert.Equal(t, sellerAgentID, negotiation.SellerAgentID)
	assert.Equal(t, userID, negotiation.UserID)
	assert.Equal(t, 1000.00, negotiation.InitialAmount)
	assert.Equal(t, 1000.00, negotiation.CurrentAmount)
	assert.Equal(t, "initiated", negotiation.Status)
	assert.Equal(t, 5, negotiation.MaxRounds)
	assert.Equal(t, 0, negotiation.Rounds)

	mockAgent.AssertExpectations(t)
	mockAP2.AssertExpectations(t)
}

// TestCreateNegotiation_BuyerAgentNotFound tests error when buyer agent doesn't exist
func TestBargainingService_CreateNegotiation_BuyerAgentNotFound(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	ctx := context.Background()
	buyerAgentID := uuid.New().String()
	sellerAgentID := uuid.New().String()
	userID := uuid.New().String()

	req := &services.CreateNegotiationRequest{
		BuyerAgentID:  buyerAgentID,
		SellerAgentID: sellerAgentID,
		UserID:       userID,
		InitialAmount: 1000.00,
	}

	mockAgent.On("GetAgentByID", ctx, buyerAgentID).Return(nil, assert.AnError)

	negotiation, err := svc.CreateNegotiation(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, negotiation)
	assert.Contains(t, err.Error(), "buyer agent not found")
	mockAgent.AssertExpectations(t)
}

// TestCreateNegotiation_InvalidBuyerAgentType tests error when buyer agent is not shopping type
func TestBargainingService_CreateNegotiation_InvalidBuyerAgentType(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	ctx := context.Background()
	buyerAgentID := uuid.New().String()
	sellerAgentID := uuid.New().String()
	userID := uuid.New().String()

	buyerAgent := &models.Agent{
		ID:   buyerAgentID,
		Type: "merchant", // Invalid - should be shopping
		Name: "Buyer Agent",
	}

	sellerAgent := &models.Agent{
		ID:   sellerAgentID,
		Type: "merchant",
		Name: "Seller Agent",
	}

	req := &services.CreateNegotiationRequest{
		BuyerAgentID:  buyerAgentID,
		SellerAgentID: sellerAgentID,
		UserID:       userID,
		InitialAmount: 1000.00,
	}

	mockAgent.On("GetAgentByID", ctx, buyerAgentID).Return(buyerAgent, nil)
	mockAgent.On("GetAgentByID", ctx, sellerAgentID).Return(sellerAgent, nil)

	negotiation, err := svc.CreateNegotiation(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, negotiation)
	assert.Contains(t, err.Error(), "buyer agent must be a shopping agent")
	mockAgent.AssertExpectations(t)
}

// TestCreateNegotiation_InvalidSellerAgentType tests error when seller agent is not merchant type
func TestBargainingService_CreateNegotiation_InvalidSellerAgentType(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	ctx := context.Background()
	buyerAgentID := uuid.New().String()
	sellerAgentID := uuid.New().String()
	userID := uuid.New().String()

	buyerAgent := &models.Agent{
		ID:   buyerAgentID,
		Type: "shopping",
		Name: "Buyer Agent",
	}

	sellerAgent := &models.Agent{
		ID:   sellerAgentID,
		Type: "shopping", // Invalid - should be merchant
		Name: "Seller Agent",
	}

	req := &services.CreateNegotiationRequest{
		BuyerAgentID:  buyerAgentID,
		SellerAgentID: sellerAgentID,
		UserID:       userID,
		InitialAmount: 1000.00,
	}

	mockAgent.On("GetAgentByID", ctx, buyerAgentID).Return(buyerAgent, nil)
	mockAgent.On("GetAgentByID", ctx, sellerAgentID).Return(sellerAgent, nil)

	negotiation, err := svc.CreateNegotiation(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, negotiation)
	assert.Contains(t, err.Error(), "seller agent must be a merchant agent")
	mockAgent.AssertExpectations(t)
}

// TestGetNegotiation_Success tests successful negotiation retrieval
func TestBargainingService_GetNegotiation_Success(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	ctx := context.Background()
	negotiationID := uuid.New().String()

	negotiation := &models.BargainingNegotiation{
		ID:             negotiationID,
		BuyerAgentID:   uuid.New().String(),
		SellerAgentID:  uuid.New().String(),
		UserID:        uuid.New().String(),
		InitialAmount: 1000.00,
		CurrentAmount: 1000.00,
		Status:        "initiated",
		ExpiresAt:     time.Now().Add(24 * time.Hour),
	}

	mockAP2.On("GetBargainingNegotiationByID", ctx, negotiationID).Return(negotiation, nil)

	result, err := svc.GetNegotiation(ctx, negotiationID)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, negotiationID, result.ID)
	mockAP2.AssertExpectations(t)
}

// TestGetNegotiation_NotFound tests error when negotiation doesn't exist
func TestBargainingService_GetNegotiation_NotFound(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	ctx := context.Background()
	negotiationID := uuid.New().String()

	mockAP2.On("GetBargainingNegotiationByID", ctx, negotiationID).Return(nil, assert.AnError)

	result, err := svc.GetNegotiation(ctx, negotiationID)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, services.ErrNegotiationNotFound, err)
	mockAP2.AssertExpectations(t)
}

// TestGetNegotiation_Expired tests expired negotiation handling
func TestBargainingService_GetNegotiation_Expired(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	ctx := context.Background()
	negotiationID := uuid.New().String()

	negotiation := &models.BargainingNegotiation{
		ID:             negotiationID,
		BuyerAgentID:   uuid.New().String(),
		SellerAgentID:  uuid.New().String(),
		UserID:        uuid.New().String(),
		InitialAmount: 1000.00,
		CurrentAmount: 950.00,
		Status:        "in_progress",
		ExpiresAt:     time.Now().Add(-1 * time.Hour), // Expired
	}

	mockAP2.On("GetBargainingNegotiationByID", ctx, negotiationID).Return(negotiation, nil)
	mockAP2.On("UpdateNegotiationStatus", ctx, negotiationID, "expired").Return(nil)

	result, err := svc.GetNegotiation(ctx, negotiationID)

	assert.Error(t, err)
	assert.Equal(t, services.ErrNegotiationExpired, err)
	assert.NotNil(t, result)
	assert.Equal(t, "expired", result.Status)
	mockAP2.AssertExpectations(t)
}

// TestIsValidCounterOffer tests counteroffer validation
func TestBargainingService_IsValidCounterOffer(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	negotiation := &models.BargainingNegotiation{
		InitialAmount: 1000.00,
		CurrentAmount: 1000.00,
	}

	tests := []struct {
		name          string
		agentType     string
		proposedAmount float64
		expected      bool
	}{
		// Buyer tests
		{"buyer_valid_counteroffer", "buyer", 900.00, true},
		{"buyer_at_current_amount", "buyer", 1000.00, true},
		{"buyer_above_current", "buyer", 1100.00, false},
		{"buyer_below_30_percent", "buyer", 200.00, false}, // Below 30% of initial (300)

		// Seller tests
		{"seller_valid_counteroffer", "seller", 1100.00, true},
		{"seller_at_current_amount", "seller", 1000.00, true},
		{"seller_below_current", "seller", 900.00, false},
		{"seller_above_150_percent", "seller", 1600.00, false}, // Above 150% of initial (1500)

		// Edge cases
		{"zero_amount", "buyer", 0, false},
		{"negative_amount", "buyer", -100.00, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := svc.IsValidCounterOffer(negotiation, tt.agentType, tt.proposedAmount)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestCalculateVolatilityFactor tests volatility factor calculation
func TestBargainingService_CalculateVolatilityFactor(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	tests := []struct {
		name          string
		volatility    float64
		rounds        int
		initialAmount float64
		currentAmount float64
	}{
		{"round_1_big_change", 0.5, 1, 1000.00, 800.00},
		{"round_5_small_change", 0.3, 5, 1000.00, 950.00},
		{"round_10_no_change", 0.5, 10, 1000.00, 1000.00},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factor := svc.CalculateVolatilityFactor(tt.volatility, tt.rounds, tt.initialAmount, tt.currentAmount)
			assert.GreaterOrEqual(t, factor, 0.0)
			assert.Less(t, factor, 1.0)
		})
	}
}

// TestCalculateFallbackCounterOffer tests fallback calculation
func TestBargainingService_CalculateFallbackCounterOffer(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	negotiation := &models.BargainingNegotiation{
		InitialAmount:     1000.00,
		CurrentAmount:     900.00,
		BuyerVolatility:   0.5,
		SellerVolatility:  0.5,
	}

	buyerOffer := svc.CalculateFallbackCounterOffer(negotiation, "buyer")
	sellerOffer := svc.CalculateFallbackCounterOffer(negotiation, "seller")

	// Buyer should offer less than current
	assert.Less(t, buyerOffer, negotiation.CurrentAmount)
	// Seller should ask more than current
	assert.Greater(t, sellerOffer, negotiation.CurrentAmount)
}

// TestGetAgentVolatility tests volatility extraction from agent config
func TestBargainingService_GetAgentVolatility(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	tests := []struct {
		name     string
		config   string
		expected float64
	}{
		{"valid_config", `{"volatility": 0.7}`, 0.7},
		{"empty_config", "", 0.5},
		{"invalid_json", `{invalid}`, 0.5},
		{"volatility_out_of_range_high", `{"volatility": 1.5}`, 0.5},
		{"volatility_out_of_range_low", `{"volatility": -0.5}`, 0.5},
		{"no_volatility_key", `{"other": "value"}`, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := &models.Agent{Config: tt.config}
			result := svc.GetAgentVolatility(agent)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNegotiationStatusTransitions tests status transitions
func TestBargainingService_NegotiationStatusTransitions(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	// Test expired status auto-update
	t.Run("expired_negotiation", func(t *testing.T) {
		ctx := context.Background()
		negotiationID := uuid.New().String()

		negotiation := &models.BargainingNegotiation{
			ID:             negotiationID,
			BuyerAgentID:   uuid.New().String(),
			SellerAgentID:  uuid.New().String(),
			UserID:        uuid.New().String(),
			InitialAmount: 1000.00,
			CurrentAmount: 950.00,
			Status:        "in_progress",
			ExpiresAt:     time.Now().Add(-1 * time.Hour),
		}

		mockAP2.On("GetBargainingNegotiationByID", ctx, negotiationID).Return(negotiation, nil)
		mockAP2.On("UpdateNegotiationStatus", ctx, negotiationID, "expired").Return(nil)

		result, err := svc.GetNegotiation(ctx, negotiationID)

		assert.Error(t, err)
		assert.Equal(t, services.ErrNegotiationExpired, err)
		assert.Equal(t, "expired", result.Status)
		mockAP2.AssertExpectations(t)
	})

	// Test already completed negotiation
	t.Run("already_completed", func(t *testing.T) {
		ctx := context.Background()
		negotiationID := uuid.New().String()

		negotiation := &models.BargainingNegotiation{
			ID:             negotiationID,
			BuyerAgentID:   uuid.New().String(),
			SellerAgentID:  uuid.New().String(),
			UserID:        uuid.New().String(),
			InitialAmount: 1000.00,
			CurrentAmount: 900.00,
			Status:        "accepted", // Already completed
			ExpiresAt:     time.Now().Add(-1 * time.Hour),
		}

		mockAP2.On("GetBargainingNegotiationByID", ctx, negotiationID).Return(negotiation, nil)

		result, err := svc.GetNegotiation(ctx, negotiationID)

		assert.NoError(t, err) // No error for already completed negotiations
		assert.Equal(t, "accepted", result.Status)
		mockAP2.AssertExpectations(t)
	})
}

// TestSubmitCounterOffer_AlreadyCompleted tests counteroffer on completed negotiation
func TestBargainingService_SubmitCounterOffer_AlreadyCompleted(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	ctx := context.Background()
	negotiationID := uuid.New().String()
	agentID := uuid.New().String()

	negotiation := &models.BargainingNegotiation{
		ID:             negotiationID,
		BuyerAgentID:   agentID,
		SellerAgentID:  uuid.New().String(),
		UserID:        uuid.New().String(),
		InitialAmount: 1000.00,
		CurrentAmount: 900.00,
		Status:        "accepted", // Already completed
		ExpiresAt:     time.Now().Add(24 * time.Hour),
	}

	req := &services.CounterOfferRequest{
		AgentID:        agentID,
		ProposedAmount: 850.00,
		Action:         "counteroffer",
	}

	mockAP2.On("GetBargainingNegotiationByID", ctx, negotiationID).Return(negotiation, nil)

	round, _, err := svc.SubmitCounterOffer(ctx, negotiationID, req)

	assert.Error(t, err)
	assert.Nil(t, round)
	assert.Contains(t, err.Error(), "already completed")
	mockAP2.AssertExpectations(t)
}

// TestSubmitCounterOffer_MaxRoundsExceeded tests counteroffer when max rounds reached
func TestBargainingService_SubmitCounterOffer_MaxRoundsExceeded(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	ctx := context.Background()
	negotiationID := uuid.New().String()
	agentID := uuid.New().String()

	negotiation := &models.BargainingNegotiation{
		ID:             negotiationID,
		BuyerAgentID:   agentID,
		SellerAgentID:  uuid.New().String(),
		UserID:        uuid.New().String(),
		InitialAmount: 1000.00,
		CurrentAmount: 900.00,
		Status:        "in_progress",
		Rounds:        5,        // Max reached
		MaxRounds:    5,        // Max allowed
		ExpiresAt:     time.Now().Add(24 * time.Hour),
	}

	req := &services.CounterOfferRequest{
		AgentID:        agentID,
		ProposedAmount: 850.00,
		Action:         "counteroffer",
	}

	mockAP2.On("GetBargainingNegotiationByID", ctx, negotiationID).Return(negotiation, nil)
	mockAP2.On("UpdateNegotiationStatus", ctx, negotiationID, "expired").Return(nil)

	round, _, err := svc.SubmitCounterOffer(ctx, negotiationID, req)

	assert.Error(t, err)
	assert.Nil(t, round)
	assert.Equal(t, services.ErrMaxRoundsExceeded, err)
	mockAP2.AssertExpectations(t)
}

// TestSubmitCounterOffer_InvalidAgent tests counteroffer from non-participating agent
func TestBargainingService_SubmitCounterOffer_InvalidAgent(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	ctx := context.Background()
	negotiationID := uuid.New().String()
	validAgentID := uuid.New().String()
	invalidAgentID := uuid.New().String()

	negotiation := &models.BargainingNegotiation{
		ID:             negotiationID,
		BuyerAgentID:   validAgentID,
		SellerAgentID:  uuid.New().String(),
		UserID:        uuid.New().String(),
		InitialAmount: 1000.00,
		CurrentAmount: 900.00,
		Status:        "in_progress",
		Rounds:        0,
		MaxRounds:    5,
		ExpiresAt:     time.Now().Add(24 * time.Hour),
		BuyerAgent: &models.Agent{
			ID:   validAgentID,
			Type: "shopping",
		},
		SellerAgent: &models.Agent{
			ID:   uuid.New().String(),
			Type: "merchant",
		},
	}

	req := &services.CounterOfferRequest{
		AgentID:        invalidAgentID, // Not a participant
		ProposedAmount: 850.00,
		Action:         "counteroffer",
	}

	mockAP2.On("GetBargainingNegotiationByID", ctx, negotiationID).Return(negotiation, nil)

	round, _, err := svc.SubmitCounterOffer(ctx, negotiationID, req)

	assert.Error(t, err)
	assert.Nil(t, round)
	assert.Contains(t, err.Error(), "not part of this negotiation")
	mockAP2.AssertExpectations(t)
}

// TestNegotiationOutcome tests successful negotiation completion
func TestBargainingService_SubmitCounterOffer_Accept(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	ctx := context.Background()
	negotiationID := uuid.New().String()
	buyerAgentID := uuid.New().String()
	sellerAgentID := uuid.New().String()

	negotiation := &models.BargainingNegotiation{
		ID:              negotiationID,
		BuyerAgentID:    buyerAgentID,
		SellerAgentID:   sellerAgentID,
		UserID:          uuid.New().String(),
		InitialAmount:  1000.00,
		CurrentAmount:   900.00,
		Status:          "in_progress",
		Rounds:         1,
		MaxRounds:      5,
		ExpiresAt:       time.Now().Add(24 * time.Hour),
		BuyerAgent: &models.Agent{
			ID:   buyerAgentID,
			Type: "shopping",
		},
		SellerAgent: &models.Agent{
			ID:   sellerAgentID,
			Type: "merchant",
		},
	}

	req := &services.CounterOfferRequest{
		AgentID:        buyerAgentID,
		ProposedAmount: 900.00,
		Action:         "accept",
	}

	mockAP2.On("GetBargainingNegotiationByID", ctx, negotiationID).Return(negotiation, nil)
	mockAP2.On("CreateBargainingRound", ctx, mock.AnythingOfType("*models.BargainingRound")).Return(nil)
	mockAP2.On("CompleteNegotiation", ctx, negotiationID, "accepted", 900.00, mock.AnythingOfType("*time.Time")).Return(nil)

	round, negotiation, err := svc.SubmitCounterOffer(ctx, negotiationID, req)

	assert.NoError(t, err)
	assert.NotNil(t, round)
	assert.NotNil(t, negotiation)
	assert.Equal(t, "accept", round.Action)
	assert.Equal(t, "accepted", negotiation.Status)
	mockAP2.AssertExpectations(t)
}

// TestSubmitCounterOffer_Reject tests rejection
func TestBargainingService_SubmitCounterOffer_Reject(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	ctx := context.Background()
	negotiationID := uuid.New().String()
	sellerAgentID := uuid.New().String()

	negotiation := &models.BargainingNegotiation{
		ID:              negotiationID,
		BuyerAgentID:    uuid.New().String(),
		SellerAgentID:   sellerAgentID,
		UserID:          uuid.New().String(),
		InitialAmount:  1000.00,
		CurrentAmount:   900.00,
		Status:          "in_progress",
		Rounds:         1,
		MaxRounds:      5,
		ExpiresAt:       time.Now().Add(24 * time.Hour),
		BuyerAgent: &models.Agent{
			ID:   uuid.New().String(),
			Type: "shopping",
		},
		SellerAgent: &models.Agent{
			ID:   sellerAgentID,
			Type: "merchant",
		},
	}

	reason := "Price too high"
	req := &services.CounterOfferRequest{
		AgentID:        sellerAgentID,
		ProposedAmount: 900.00,
		Action:         "reject",
		Reason:         &reason,
	}

	mockAP2.On("GetBargainingNegotiationByID", ctx, negotiationID).Return(negotiation, nil)
	mockAP2.On("CreateBargainingRound", ctx, mock.AnythingOfType("*models.BargainingRound")).Return(nil)
	mockAP2.On("UpdateNegotiationStatus", ctx, negotiationID, "rejected").Return(nil)

	round, negotiation, err := svc.SubmitCounterOffer(ctx, negotiationID, req)

	assert.NoError(t, err)
	assert.NotNil(t, round)
	assert.NotNil(t, negotiation)
	assert.Equal(t, "reject", round.Action)
	assert.Equal(t, "rejected", negotiation.Status)
	assert.Equal(t, &reason, round.Reason)
	mockAP2.AssertExpectations(t)
}

// TestGetNegotiationsByUser tests retrieving negotiations for a user
func TestBargainingService_GetNegotiationsByUser(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	ctx := context.Background()
	userID := uuid.New().String()

	expectedNegotiations := []*models.BargainingNegotiation{
		{ID: uuid.New().String(), InitialAmount: 1000.00},
		{ID: uuid.New().String(), InitialAmount: 2000.00},
	}

	mockAP2.On("GetNegotiationsByUser", ctx, userID, 1, 10).Return(expectedNegotiations, int64(2), nil)

	negotiations, total, err := svc.GetNegotiationsByUser(ctx, userID, 1, 10)

	assert.NoError(t, err)
	assert.Len(t, negotiations, 2)
	assert.Equal(t, int64(2), total)
	mockAP2.AssertExpectations(t)
}

// TestGetNegotiationRounds tests retrieving rounds for a negotiation
func TestBargainingService_GetNegotiationRounds(t *testing.T) {
	mockAP2 := new(MockBargainingAP2Repository)
	mockAgent := new(MockAgentServiceForBargaining)
	mockMentee := new(MockMenteeServiceForBargaining)
	log := logger.New()

	svc := services.NewBargainingServiceForTesting(mockAP2, nil, mockAgent, mockMentee, nil, log)

	ctx := context.Background()
	negotiationID := uuid.New().String()

	expectedRounds := []*models.BargainingRound{
		{RoundNumber: 1, Action: "counteroffer", ProposedAmount: 950.00},
		{RoundNumber: 2, Action: "counteroffer", ProposedAmount: 900.00},
	}

	mockAP2.On("GetBargainingRounds", ctx, negotiationID).Return(expectedRounds, nil)

	rounds, err := svc.GetNegotiationRounds(ctx, negotiationID)

	assert.NoError(t, err)
	assert.Len(t, rounds, 2)
	mockAP2.AssertExpectations(t)
}
