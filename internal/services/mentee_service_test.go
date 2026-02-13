package services

import (
	"context"
	"testing"
	"time"

	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMenteeService(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	assert.NotNil(t, svc)
	assert.NotNil(t, svc.learningData)
	assert.NotNil(t, svc.log)
	assert.Equal(t, 0.95, svc.decayFactor)
}

func TestMenteeService_GetBargainingDecision_BaseDecision(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	decision, err := svc.GetBargainingDecision(
		context.Background(),
		"unknown-agent-1",
		"buyer",
		100.0,
		100.0,
		1,
		5,
		"unknown-agent-2",
	)

	assert.NoError(t, err)
	assert.NotNil(t, decision)
	assert.NotEmpty(t, decision.Action)
	assert.NotZero(t, decision.ProposedAmount)
	assert.NotEmpty(t, decision.Reason)
	assert.GreaterOrEqual(t, decision.Confidence, 0.0)
	assert.LessOrEqual(t, decision.Confidence, 1.0)
	assert.NotNil(t, decision.SuggestedRange)
}

func TestMenteeService_GetBargainingDecision_Buyer(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	tests := []struct {
		name               string
		agentType          string
		currentAmount      float64
		initialAmount      float64
		round              int
		maxRounds          int
		expectedAction     string
		expectedLowerPrice bool
	}{
		{
			name:               "First round buyer",
			agentType:          "buyer",
			currentAmount:      100.0,
			initialAmount:      100.0,
			round:              1,
			maxRounds:          5,
			expectedAction:     "counteroffer",
			expectedLowerPrice: true,
		},
		{
			name:               "Late round buyer",
			agentType:          "buyer",
			currentAmount:      85.0,
			initialAmount:      100.0,
			round:              4,
			maxRounds:          5,
			expectedAction:     "accept",
			expectedLowerPrice: false,
		},
		{
			name:               "Good price early",
			agentType:          "buyer",
			currentAmount:      75.0,
			initialAmount:      100.0,
			round:              1,
			maxRounds:          5,
			expectedAction:     "accept",
			expectedLowerPrice: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := svc.GetBargainingDecision(
				context.Background(),
				"buyer-agent",
				tt.agentType,
				tt.currentAmount,
				tt.initialAmount,
				tt.round,
				tt.maxRounds,
				"seller-agent",
			)

			assert.NoError(t, err)
			assert.NotNil(t, decision)
			assert.Equal(t, tt.expectedAction, decision.Action)

			if tt.expectedLowerPrice {
				assert.Less(t, decision.ProposedAmount, tt.currentAmount)
			} else {
				assert.Equal(t, tt.currentAmount, decision.ProposedAmount)
			}
		})
	}
}

func TestMenteeService_GetBargainingDecision_Seller(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	tests := []struct {
		name                string
		agentType           string
		currentAmount       float64
		initialAmount       float64
		round               int
		maxRounds           int
		expectedAction      string
		expectedHigherPrice bool
	}{
		{
			name:                "First round seller",
			agentType:           "seller",
			currentAmount:       100.0,
			initialAmount:       100.0,
			round:               1,
			maxRounds:           5,
			expectedAction:      "counteroffer",
			expectedHigherPrice: true,
		},
		{
			name:                "Late round seller",
			agentType:           "seller",
			currentAmount:       105.0,
			initialAmount:       100.0,
			round:               4,
			maxRounds:           5,
			expectedAction:      "accept",
			expectedHigherPrice: false,
		},
		{
			name:                "Good price early",
			agentType:           "seller",
			currentAmount:       115.0,
			initialAmount:       100.0,
			round:               1,
			maxRounds:           5,
			expectedAction:      "accept",
			expectedHigherPrice: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := svc.GetBargainingDecision(
				context.Background(),
				"seller-agent",
				tt.agentType,
				tt.currentAmount,
				tt.initialAmount,
				tt.round,
				tt.maxRounds,
				"buyer-agent",
			)

			assert.NoError(t, err)
			assert.NotNil(t, decision)
			assert.Equal(t, tt.expectedAction, decision.Action)

			if tt.expectedHigherPrice {
				assert.Greater(t, decision.ProposedAmount, tt.currentAmount)
			} else {
				assert.Equal(t, tt.currentAmount, decision.ProposedAmount)
			}
		})
	}
}

func TestMenteeService_RecordNegotiationOutcome(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	outcome := &NegotiationOutcome{
		NegotiationID:    "neg-1",
		InitialAmount:    100.0,
		FinalAmount:      85.0,
		Status:           "accepted",
		Rounds:           3,
		OpponentID:       "opponent-1",
		OpponentType:     "seller",
		Timestamp:        time.Now(),
		Strategy:         "counteroffer",
		VolatilityFactor: 0.05,
	}

	err := svc.RecordNegotiationOutcome(context.Background(), outcome)

	assert.NoError(t, err)

	data, err := svc.GetAgentLearningData(context.Background(), "opponent-1")
	assert.NoError(t, err)
	assert.Len(t, data.Outcomes, 1)
	assert.Equal(t, "seller", data.AgentType)
}

func TestMenteeService_GetAgentLearningData_NewAgent(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	data, err := svc.GetAgentLearningData(context.Background(), "new-agent-xyz")

	assert.NoError(t, err)
	assert.NotNil(t, data)
	assert.Equal(t, "new-agent-xyz", data.AgentID)
	assert.Empty(t, data.Outcomes)
	assert.NotNil(t, data.LearnedParameters)
	assert.Equal(t, 0.5, data.Volatility)
	assert.Equal(t, 0.1, data.Confidence)
}

func TestMenteeService_GetAgentLearningData_ExistingAgent(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	outcome := &NegotiationOutcome{
		NegotiationID:    "neg-2",
		InitialAmount:    200.0,
		FinalAmount:      180.0,
		Status:           "accepted",
		Rounds:           2,
		OpponentID:       "existing-agent",
		OpponentType:     "buyer",
		Timestamp:        time.Now(),
		Strategy:         "accept",
		VolatilityFactor: 0.03,
	}

	err := svc.RecordNegotiationOutcome(context.Background(), outcome)
	require.NoError(t, err)

	data, err := svc.GetAgentLearningData(context.Background(), "existing-agent")

	assert.NoError(t, err)
	assert.Equal(t, "existing-agent", data.AgentID)
	assert.Len(t, data.Outcomes, 1)
	assert.Greater(t, data.Confidence, 0.1)
}

func TestMenteeService_InformedDecision(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	outcomes := []NegotiationOutcome{
		{
			NegotiationID: "neg-1",
			InitialAmount: 100.0,
			FinalAmount:   85.0,
			Status:        "accepted",
			Rounds:        3,
			OpponentID:    "opponent-1",
			OpponentType:  "seller",
			Timestamp:     time.Now(),
			Strategy:      "counteroffer",
		},
		{
			NegotiationID: "neg-2",
			InitialAmount: 150.0,
			FinalAmount:   127.5,
			Status:        "accepted",
			Rounds:        2,
			OpponentID:    "opponent-1",
			OpponentType:  "seller",
			Timestamp:     time.Now(),
			Strategy:      "counteroffer",
		},
		{
			NegotiationID: "neg-3",
			InitialAmount: 200.0,
			FinalAmount:   180.0,
			Status:        "accepted",
			Rounds:        4,
			OpponentID:    "opponent-1",
			OpponentType:  "seller",
			Timestamp:     time.Now(),
			Strategy:      "accept",
		},
	}

	for _, outcome := range outcomes {
		err := svc.RecordNegotiationOutcome(context.Background(), &outcome)
		require.NoError(t, err)
	}

	decision, err := svc.GetBargainingDecision(
		context.Background(),
		"buyer-1",
		"buyer",
		100.0,
		100.0,
		2,
		5,
		"opponent-1",
	)

	assert.NoError(t, err)
	assert.NotNil(t, decision)
	assert.Contains(t, decision.Reason, "historical opponent patterns")
	assert.Greater(t, decision.Confidence, 0.7)
}

func TestMenteeService_CalculateSuggestedRange(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	t.Run("Buyer range", func(t *testing.T) {
		rng := svc.calculateSuggestedRange("buyer", 100.0, 0.5, 100.0, 2, 5)
		assert.NotNil(t, rng)
		assert.Less(t, rng.Min, 100.0)
		assert.Greater(t, rng.Max, 100.0)
	})

	t.Run("Seller range", func(t *testing.T) {
		rng := svc.calculateSuggestedRange("seller", 100.0, 0.5, 100.0, 2, 5)
		assert.NotNil(t, rng)
		assert.Less(t, rng.Min, 100.0)
		assert.Greater(t, rng.Max, 100.0)
	})

	t.Run("High confidence", func(t *testing.T) {
		rng := svc.calculateSuggestedRange("buyer", 100.0, 0.9, 100.0, 2, 5)
		assert.NotNil(t, rng)
		delta := rng.Max - rng.Min
		assert.Less(t, delta, 20.0)
	})

	t.Run("Low confidence", func(t *testing.T) {
		rng := svc.calculateSuggestedRange("buyer", 100.0, 0.3, 100.0, 2, 5)
		assert.NotNil(t, rng)
		delta := rng.Max - rng.Min
		assert.Greater(t, delta, 10.0)
	})
}

func TestMenteeService_ResetAgentLearning(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	outcome := &NegotiationOutcome{
		NegotiationID: "neg-reset",
		InitialAmount: 100.0,
		FinalAmount:   90.0,
		Status:        "accepted",
		Rounds:        2,
		OpponentID:    "reset-agent",
		OpponentType:  "seller",
		Timestamp:     time.Now(),
	}

	err := svc.RecordNegotiationOutcome(context.Background(), outcome)
	require.NoError(t, err)

	data, err := svc.GetAgentLearningData(context.Background(), "reset-agent")
	require.NoError(t, err)
	assert.Len(t, data.Outcomes, 1)

	err = svc.ResetAgentLearning(context.Background(), "reset-agent")
	assert.NoError(t, err)

	data, err = svc.GetAgentLearningData(context.Background(), "reset-agent")
	assert.NoError(t, err)
	assert.Empty(t, data.Outcomes)
	assert.Equal(t, 0.1, data.Confidence)
}

func TestMenteeService_ExportImportLearningData(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	outcomes := []NegotiationOutcome{
		{
			NegotiationID: "neg-export-1",
			InitialAmount: 100.0,
			FinalAmount:   85.0,
			Status:        "accepted",
			Rounds:        3,
			OpponentID:    "export-agent",
			OpponentType:  "seller",
			Timestamp:     time.Now(),
		},
	}

	for _, outcome := range outcomes {
		err := svc.RecordNegotiationOutcome(context.Background(), &outcome)
		require.NoError(t, err)
	}

	data, err := svc.ExportLearningData(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, data)
	assert.Contains(t, string(data), "export-agent")

	svc2 := NewMenteeService(log)
	err = svc2.ImportLearningData(context.Background(), data)
	assert.NoError(t, err)

	importedData, err := svc2.GetAgentLearningData(context.Background(), "export-agent")
	assert.NoError(t, err)
	assert.Len(t, importedData.Outcomes, 1)
}

func TestMenteeService_CalculateAverageDiscount(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	data := &AgentLearningData{
		AgentType: "seller",
		Outcomes: []NegotiationOutcome{
			{InitialAmount: 100.0, FinalAmount: 85.0, Status: "accepted", Timestamp: time.Now()},
			{InitialAmount: 200.0, FinalAmount: 170.0, Status: "accepted", Timestamp: time.Now()},
			{InitialAmount: 150.0, FinalAmount: 127.5, Status: "accepted", Timestamp: time.Now()},
		},
	}

	avgDiscount := svc.calculateAverageDiscount(data)
	assert.Greater(t, avgDiscount, 0.0)
	assert.Less(t, avgDiscount, 1.0)
	assert.InDelta(t, 0.15, avgDiscount, 0.01)
}

func TestMenteeService_CalculateAverageMarkup(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	data := &AgentLearningData{
		AgentType: "buyer",
		Outcomes: []NegotiationOutcome{
			{InitialAmount: 85.0, FinalAmount: 100.0, Status: "accepted", Timestamp: time.Now()},
			{InitialAmount: 170.0, FinalAmount: 200.0, Status: "accepted", Timestamp: time.Now()},
		},
	}

	avgMarkup := svc.calculateAverageMarkup(data)
	assert.Greater(t, avgMarkup, 0.0)
	assert.Less(t, avgMarkup, 1.0)
	assert.InDelta(t, 0.176, avgMarkup, 0.01)
}

func TestMenteeService_CalculateVolatility(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	t.Run("Stable outcomes", func(t *testing.T) {
		data := &AgentLearningData{
			Outcomes: []NegotiationOutcome{
				{InitialAmount: 100.0, FinalAmount: 85.0, Timestamp: time.Now()},
				{InitialAmount: 100.0, FinalAmount: 84.0, Timestamp: time.Now()},
				{InitialAmount: 100.0, FinalAmount: 86.0, Timestamp: time.Now()},
			},
		}

		volatility := svc.calculateVolatility(data)
		assert.Less(t, volatility, 0.3)
	})

	t.Run("Volatile outcomes", func(t *testing.T) {
		data := &AgentLearningData{
			Outcomes: []NegotiationOutcome{
				{InitialAmount: 100.0, FinalAmount: 70.0, Timestamp: time.Now()},
				{InitialAmount: 100.0, FinalAmount: 95.0, Timestamp: time.Now()},
				{InitialAmount: 100.0, FinalAmount: 60.0, Timestamp: time.Now()},
			},
		}

		volatility := svc.calculateVolatility(data)
		assert.Greater(t, volatility, 0.3)
	})

	t.Run("Insufficient data", func(t *testing.T) {
		data := &AgentLearningData{
			Outcomes: []NegotiationOutcome{
				{InitialAmount: 100.0, FinalAmount: 85.0, Timestamp: time.Now()},
			},
		}

		volatility := svc.calculateVolatility(data)
		assert.Equal(t, 0.5, volatility)
	})
}

func TestMenteeService_UpdateLearningMetrics(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	data := &AgentLearningData{
		AgentType: "seller",
		Outcomes: []NegotiationOutcome{
			{InitialAmount: 100.0, FinalAmount: 85.0, Status: "accepted", Timestamp: time.Now()},
			{InitialAmount: 100.0, FinalAmount: 90.0, Status: "accepted", Timestamp: time.Now()},
			{InitialAmount: 100.0, FinalAmount: 75.0, Status: "rejected", Timestamp: time.Now()},
		},
	}

	svc.updateLearningMetrics(data)

	assert.Equal(t, 2.0/3.0, data.SuccessRate)
	assert.Greater(t, data.AverageDiscount, 0.0)
	assert.Greater(t, data.Confidence, 0.1)
	assert.NotNil(t, data.LearnedParameters)
	assert.Contains(t, data.LearnedParameters, "avg_rounds")
	assert.Contains(t, data.LearnedParameters, "success_rate")
}
