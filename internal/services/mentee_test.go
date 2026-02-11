package services

import (
	"context"
	"testing"
	"time"

	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMentee_CalculateVolatility(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	t.Run("Stable outcomes should have low volatility", func(t *testing.T) {
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

	t.Run("Volatile outcomes should have high volatility", func(t *testing.T) {
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

	t.Run("Insufficient data returns default volatility", func(t *testing.T) {
		data := &AgentLearningData{
			Outcomes: []NegotiationOutcome{
				{InitialAmount: 100.0, FinalAmount: 85.0, Timestamp: time.Now()},
			},
		}
		volatility := svc.calculateVolatility(data)
		assert.Equal(t, 0.5, volatility)
	})
}

func TestMentee_GetBargainingDecision(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	t.Run("First round buyer should counteroffer", func(t *testing.T) {
		decision, err := svc.GetBargainingDecision(
			context.Background(),
			"buyer-1",
			"buyer",
			100.0,
			100.0,
			1,
			5,
			"seller-1",
		)
		assert.NoError(t, err)
		assert.NotNil(t, decision)
		assert.Equal(t, "counteroffer", decision.Action)
		assert.Less(t, decision.ProposedAmount, 100.0)
	})

	t.Run("First round seller should counteroffer", func(t *testing.T) {
		decision, err := svc.GetBargainingDecision(
			context.Background(),
			"seller-1",
			"seller",
			100.0,
			100.0,
			1,
			5,
			"buyer-1",
		)
		assert.NoError(t, err)
		assert.NotNil(t, decision)
		assert.Equal(t, "counteroffer", decision.Action)
		assert.Greater(t, decision.ProposedAmount, 100.0)
	})

	t.Run("Late round with good price should accept", func(t *testing.T) {
		decision, err := svc.GetBargainingDecision(
			context.Background(),
			"buyer-1",
			"buyer",
			75.0,
			100.0,
			4,
			5,
			"seller-1",
		)
		assert.NoError(t, err)
		assert.NotNil(t, decision)
		assert.Equal(t, "accept", decision.Action)
		assert.Equal(t, 75.0, decision.ProposedAmount)
	})
}

func TestMentee_RecordNegotiationOutcome(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	t.Run("Record outcome and retrieve", func(t *testing.T) {
		outcome := &NegotiationOutcome{
			NegotiationID: "neg-test-1",
			InitialAmount: 100.0,
			FinalAmount:   85.0,
			Status:        "accepted",
			Rounds:        3,
			OpponentID:    "test-opponent",
			OpponentType:  "seller",
			Timestamp:     time.Now(),
		}

		err := svc.RecordNegotiationOutcome(context.Background(), outcome)
		assert.NoError(t, err)

		data, err := svc.GetAgentLearningData(context.Background(), "test-opponent")
		assert.NoError(t, err)
		assert.Equal(t, "test-opponent", data.AgentID)
		assert.Len(t, data.Outcomes, 1)
	})
}

func TestMentee_GetAgentLearningData(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	t.Run("New agent returns default data", func(t *testing.T) {
		data, err := svc.GetAgentLearningData(context.Background(), "new-agent-xyz")
		assert.NoError(t, err)
		assert.NotNil(t, data)
		assert.Equal(t, "new-agent-xyz", data.AgentID)
		assert.Empty(t, data.Outcomes)
		assert.Equal(t, 0.5, data.Volatility)
		assert.Equal(t, 0.1, data.Confidence)
	})
}

func TestMentee_ResetAgentLearning(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	t.Run("Reset clears learning data", func(t *testing.T) {
		outcome := &NegotiationOutcome{
			NegotiationID: "neg-reset-test",
			InitialAmount: 100.0,
			FinalAmount:   90.0,
			Status:        "accepted",
			Rounds:        2,
			OpponentID:    "reset-test-agent",
			OpponentType:  "seller",
			Timestamp:     time.Now(),
		}

		err := svc.RecordNegotiationOutcome(context.Background(), outcome)
		assert.NoError(t, err)

		data, err := svc.GetAgentLearningData(context.Background(), "reset-test-agent")
		require.NoError(t, err)
		assert.Len(t, data.Outcomes, 1)

		err = svc.ResetAgentLearning(context.Background(), "reset-test-agent")
		assert.NoError(t, err)

		data, err = svc.GetAgentLearningData(context.Background(), "reset-test-agent")
		assert.NoError(t, err)
		assert.Empty(t, data.Outcomes)
		assert.Equal(t, 0.1, data.Confidence)
	})
}

func TestMentee_ExportImportLearningData(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	t.Run("Export and import learning data", func(t *testing.T) {
		outcome := &NegotiationOutcome{
			NegotiationID: "neg-export-test",
			InitialAmount: 100.0,
			FinalAmount:   85.0,
			Status:        "accepted",
			Rounds:        3,
			OpponentID:    "export-test-agent",
			OpponentType:  "seller",
			Timestamp:     time.Now(),
		}

		err := svc.RecordNegotiationOutcome(context.Background(), outcome)
		assert.NoError(t, err)

		data, err := svc.ExportLearningData(context.Background())
		assert.NoError(t, err)
		assert.NotNil(t, data)
		assert.Contains(t, string(data), "export-test-agent")

		svc2 := NewMenteeService(log)
		err = svc2.ImportLearningData(context.Background(), data)
		assert.NoError(t, err)

		importedData, err := svc2.GetAgentLearningData(context.Background(), "export-test-agent")
		assert.NoError(t, err)
		assert.Len(t, importedData.Outcomes, 1)
	})
}

func TestMentee_CalculateAverageDiscount(t *testing.T) {
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

func TestMentee_CalculateAverageMarkup(t *testing.T) {
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

func TestMentee_UpdateLearningMetrics(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	t.Run("Update metrics with accepted outcomes", func(t *testing.T) {
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
	})

	t.Run("Update metrics with empty outcomes", func(t *testing.T) {
		data := &AgentLearningData{
			AgentType: "buyer",
			Outcomes:  []NegotiationOutcome{},
		}

		svc.updateLearningMetrics(data)

		assert.Equal(t, 0.0, data.SuccessRate)
	})
}

func TestMentee_CalculateSuggestedRange(t *testing.T) {
	log := logger.New()
	svc := NewMenteeService(log)

	t.Run("Buyer range with low confidence", func(t *testing.T) {
		rng := svc.calculateSuggestedRange("buyer", 100.0, 0.3, 100.0, 2, 5)
		assert.NotNil(t, rng)
		assert.Less(t, rng.Min, 100.0)
		assert.Greater(t, rng.Max, 100.0)
		delta := rng.Max - rng.Min
		assert.Greater(t, delta, 10.0)
	})

	t.Run("Buyer range with high confidence", func(t *testing.T) {
		rng := svc.calculateSuggestedRange("buyer", 100.0, 0.9, 100.0, 2, 5)
		assert.NotNil(t, rng)
		assert.Less(t, rng.Min, 100.0)
		assert.Greater(t, rng.Max, 100.0)
		delta := rng.Max - rng.Min
		assert.Less(t, delta, 20.0)
	})

	t.Run("Seller range", func(t *testing.T) {
		rng := svc.calculateSuggestedRange("seller", 100.0, 0.5, 100.0, 2, 5)
		assert.NotNil(t, rng)
		assert.Less(t, rng.Min, 100.0)
		assert.Greater(t, rng.Max, 100.0)
	})
}
