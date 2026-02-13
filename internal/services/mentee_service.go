package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"invoice-backend/pkg/logger"
)

type MenteeService struct {
	learningData map[string]*AgentLearningData
	mu           sync.RWMutex
	log          *logger.Logger
	decayFactor  float64
}

type AgentLearningData struct {
	AgentID             string
	AgentType           string
	LastUpdated         time.Time
	Outcomes            []NegotiationOutcome
	AverageDiscount     float64
	AverageMarkup       float64
	SuccessRate         float64
	PreferredStrategies []string
	LearnedParameters   map[string]float64
	Volatility          float64
	Confidence          float64
}

type NegotiationOutcome struct {
	NegotiationID    string
	InitialAmount    float64
	FinalAmount      float64
	Status           string
	Rounds           int
	OpponentID       string
	OpponentType     string
	Timestamp        time.Time
	Strategy         string
	VolatilityFactor float64
}

type BargainingDecision struct {
	Action         string
	ProposedAmount float64
	Reason         string
	Confidence     float64
	SuggestedRange *PriceRange
}

type PriceRange struct {
	Min float64
	Max float64
}

func NewMenteeService(log *logger.Logger) *MenteeService {
	return &MenteeService{
		learningData: make(map[string]*AgentLearningData),
		log:          log,
		decayFactor:  0.95,
	}
}

func (m *MenteeService) RecordNegotiationOutcome(ctx context.Context, outcome *NegotiationOutcome) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, exists := m.learningData[outcome.OpponentID]; exists {
		existing.Outcomes = append(existing.Outcomes, *outcome)
		m.updateLearningMetrics(existing)
	} else {
		data := &AgentLearningData{
			AgentID:           outcome.OpponentID,
			AgentType:         outcome.OpponentType,
			LastUpdated:       time.Now(),
			Outcomes:          []NegotiationOutcome{*outcome},
			LearnedParameters: make(map[string]float64),
			Volatility:        0.5,
			Confidence:        0.1,
		}
		m.updateLearningMetrics(data)
		m.learningData[outcome.OpponentID] = data
	}

	m.log.Info("recorded negotiation outcome", "agent_id", outcome.OpponentID, "status", outcome.Status, "final_amount", outcome.FinalAmount)

	return nil
}

func (m *MenteeService) GetBargainingDecision(ctx context.Context, agentID string, agentType string, currentAmount float64, initialAmount float64, round int, maxRounds int, opponentID string) (*BargainingDecision, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	decision := &BargainingDecision{
		Reason:     "",
		Confidence: 0.5,
	}

	opponentData, knowsOpponent := m.learningData[opponentID]
	selfData, knowsSelf := m.learningData[agentID]

	if knowsOpponent && opponentData.Confidence > 0.7 {
		decision = m.calculateInformedDecision(agentType, currentAmount, initialAmount, round, maxRounds, opponentData, selfData)
		decision.Reason = "Based on historical opponent patterns"
		decision.Confidence = math.Min(0.9, opponentData.Confidence+0.1)
	} else if knowsSelf && selfData.Confidence > 0.6 {
		decision = m.calculateSelfBasedDecision(agentType, currentAmount, initialAmount, round, maxRounds, selfData)
		decision.Reason = "Based on personal negotiation history"
		decision.Confidence = selfData.Confidence + 0.1
	} else {
		decision = m.calculateBaseDecision(agentType, currentAmount, initialAmount, round, maxRounds)
		decision.Reason = "Initial negotiation - using default strategy"
		decision.Confidence = 0.4
	}

	decision.SuggestedRange = m.calculateSuggestedRange(agentType, currentAmount, decision.Confidence, initialAmount, round, maxRounds)

	return decision, nil
}

func (m *MenteeService) calculateInformedDecision(agentType string, currentAmount float64, initialAmount float64, round int, maxRounds int, opponentData *AgentLearningData, selfData *AgentLearningData) *BargainingDecision {
	decision := &BargainingDecision{}

	if agentType == "buyer" {
		avgDiscount := m.calculateAverageDiscount(opponentData)
		targetDiscount := avgDiscount * 1.1
		if round < maxRounds {
			targetDiscount *= (1.0 - float64(round)/float64(maxRounds)*0.3)
		}

		targetAmount := initialAmount * (1.0 - math.Min(targetDiscount, 0.3))
		decision.ProposedAmount = math.Max(targetAmount, initialAmount*0.7)
		decision.Action = "counteroffer"

		if round >= maxRounds || currentAmount <= decision.ProposedAmount*1.02 {
			decision.Action = "accept"
			decision.ProposedAmount = currentAmount
		}

		if currentAmount <= initialAmount*0.75 {
			decision.Action = "accept"
			decision.ProposedAmount = currentAmount
		}
	} else {
		avgMarkup := m.calculateAverageMarkup(opponentData)
		targetMarkup := avgMarkup * 0.95
		if round < maxRounds {
			targetMarkup *= (1.0 - float64(round)/float64(maxRounds)*0.2)
		}

		targetAmount := initialAmount * (1.0 + math.Min(targetMarkup, 0.2))
		decision.ProposedAmount = math.Min(targetAmount, initialAmount*1.25)
		decision.Action = "counteroffer"

		if round >= maxRounds || currentAmount >= decision.ProposedAmount*0.98 {
			decision.Action = "accept"
			decision.ProposedAmount = currentAmount
		}

		if currentAmount >= initialAmount*1.15 {
			decision.Action = "accept"
			decision.ProposedAmount = currentAmount
		}
	}

	return decision
}

func (m *MenteeService) calculateSelfBasedDecision(agentType string, currentAmount float64, initialAmount float64, round int, maxRounds int, selfData *AgentLearningData) *BargainingDecision {
	decision := &BargainingDecision{}

	if agentType == "buyer" {
		targetDiscount := selfData.AverageDiscount * 1.05
		if round < maxRounds {
			targetDiscount *= (1.0 - float64(round)/float64(maxRounds)*0.25)
		}

		targetAmount := initialAmount * (1.0 - math.Min(targetDiscount, 0.25))
		decision.ProposedAmount = math.Max(targetAmount, initialAmount*0.75)
		decision.Action = "counteroffer"

		if round >= maxRounds-1 || currentAmount <= decision.ProposedAmount*1.03 {
			decision.Action = "accept"
			decision.ProposedAmount = currentAmount
		}
	} else {
		targetMarkup := selfData.AverageMarkup * 0.97
		if round < maxRounds {
			targetMarkup *= (1.0 - float64(round)/float64(maxRounds)*0.2)
		}

		targetAmount := initialAmount * (1.0 + math.Min(targetMarkup, 0.15))
		decision.ProposedAmount = math.Min(targetAmount, initialAmount*1.2)
		decision.Action = "counteroffer"

		if round >= maxRounds-1 || currentAmount >= decision.ProposedAmount*0.97 {
			decision.Action = "accept"
			decision.ProposedAmount = currentAmount
		}
	}

	return decision
}

func (m *MenteeService) calculateBaseDecision(agentType string, currentAmount float64, initialAmount float64, round int, maxRounds int) *BargainingDecision {
	decision := &BargainingDecision{}

	progress := float64(round) / float64(maxRounds)

	if agentType == "buyer" {
		maxDiscount := 0.20 - (progress * 0.05)
		targetAmount := initialAmount * (1.0 - maxDiscount)
		decision.ProposedAmount = math.Max(targetAmount, initialAmount*0.8)
		decision.Action = "counteroffer"

		if round >= maxRounds || currentAmount <= decision.ProposedAmount*1.05 {
			decision.Action = "accept"
			decision.ProposedAmount = currentAmount
		}
	} else {
		minMarkup := 0.05 + (progress * 0.03)
		targetAmount := initialAmount * (1.0 + minMarkup)
		decision.ProposedAmount = math.Min(targetAmount, initialAmount*1.15)
		decision.Action = "counteroffer"

		if round >= maxRounds || currentAmount >= decision.ProposedAmount*0.95 {
			decision.Action = "accept"
			decision.ProposedAmount = currentAmount
		}
	}

	return decision
}

func (m *MenteeService) calculateSuggestedRange(agentType string, currentAmount float64, confidence float64, initialAmount float64, round int, maxRounds int) *PriceRange {
	var min, max float64
	adjustment := (1.0 - confidence) * 0.1

	if agentType == "buyer" {
		min = currentAmount * (1.0 - adjustment)
		max = currentAmount * (1.0 + adjustment)
	} else {
		min = currentAmount * (1.0 - adjustment)
		max = currentAmount * (1.0 + adjustment)
	}

	return &PriceRange{
		Min: min,
		Max: max,
	}
}

func (m *MenteeService) calculateAverageDiscount(data *AgentLearningData) float64 {
	if len(data.Outcomes) == 0 {
		return 0.15
	}

	totalDiscount := 0.0
	weightedSum := 0.0

	for _, outcome := range data.Outcomes {
		if data.AgentType == "seller" {
			discount := 1.0 - (outcome.FinalAmount / outcome.InitialAmount)
			weight := math.Pow(m.decayFactor, time.Since(outcome.Timestamp).Hours()/24.0)
			totalDiscount += discount * weight
			weightedSum += weight
		}
	}

	if weightedSum > 0 {
		return totalDiscount / weightedSum
	}
	return 0.15
}

func (m *MenteeService) calculateAverageMarkup(data *AgentLearningData) float64 {
	if len(data.Outcomes) == 0 {
		return 0.10
	}

	totalMarkup := 0.0
	weightedSum := 0.0

	for _, outcome := range data.Outcomes {
		if data.AgentType == "buyer" {
			markup := (outcome.FinalAmount / outcome.InitialAmount) - 1.0
			weight := math.Pow(m.decayFactor, time.Since(outcome.Timestamp).Hours()/24.0)
			totalMarkup += markup * weight
			weightedSum += weight
		}
	}

	if weightedSum > 0 {
		return totalMarkup / weightedSum
	}
	return 0.10
}

func (m *MenteeService) updateLearningMetrics(data *AgentLearningData) {
	data.LastUpdated = time.Now()

	if len(data.Outcomes) == 0 {
		return
	}

	successfulOutcomes := 0
	totalDiscount := 0.0
	totalMarkup := 0.0

	for _, outcome := range data.Outcomes {
		if outcome.Status == "accepted" {
			successfulOutcomes++
			if data.AgentType == "seller" {
				discount := 1.0 - (outcome.FinalAmount / outcome.InitialAmount)
				totalDiscount += discount
			} else {
				markup := (outcome.FinalAmount / outcome.InitialAmount) - 1.0
				totalMarkup += markup
			}
		}
	}

	data.SuccessRate = float64(successfulOutcomes) / float64(len(data.Outcomes))

	if data.AgentType == "seller" && successfulOutcomes > 0 {
		data.AverageDiscount = totalDiscount / float64(successfulOutcomes)
	} else if data.AgentType == "buyer" && successfulOutcomes > 0 {
		data.AverageMarkup = totalMarkup / float64(successfulOutcomes)
	}

	totalOutcomes := float64(len(data.Outcomes))
	data.Confidence = math.Min(0.95, 0.1+totalOutcomes*0.25)

	data.Volatility = m.calculateVolatility(data)

	m.updateLearnedParameters(data)
}

func (m *MenteeService) calculateVolatility(data *AgentLearningData) float64 {
	if len(data.Outcomes) < 3 {
		return 0.5
	}

	variances := make([]float64, 0)

	for i := 1; i < len(data.Outcomes); i++ {
		prev := data.Outcomes[i-1]
		curr := data.Outcomes[i]
		diff := (curr.FinalAmount / curr.InitialAmount) - (prev.FinalAmount / prev.InitialAmount)
		variances = append(variances, diff*diff)
	}

	sum := 0.0
	for _, v := range variances {
		sum += v
	}

	variance := sum / float64(len(variances))
	volatility := math.Min(1.0, math.Sqrt(variance)*10.0)

	return volatility
}

func (m *MenteeService) updateLearnedParameters(data *AgentLearningData) {
	if data.LearnedParameters == nil {
		data.LearnedParameters = make(map[string]float64)
	}

	if len(data.Outcomes) > 0 {
		avgRounds := 0.0
		for _, outcome := range data.Outcomes {
			avgRounds += float64(outcome.Rounds)
		}
		avgRounds /= float64(len(data.Outcomes))
		data.LearnedParameters["avg_rounds"] = avgRounds

		data.LearnedParameters["success_rate"] = data.SuccessRate
		data.LearnedParameters["volatility"] = data.Volatility

		if data.AgentType == "seller" {
			data.LearnedParameters["avg_discount"] = data.AverageDiscount
		} else {
			data.LearnedParameters["avg_markup"] = data.AverageMarkup
		}
	}
}

func (m *MenteeService) GetAgentLearningData(ctx context.Context, agentID string) (*AgentLearningData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, exists := m.learningData[agentID]
	if !exists {
		return &AgentLearningData{
			AgentID:           agentID,
			Outcomes:          []NegotiationOutcome{},
			LearnedParameters: make(map[string]float64),
			Volatility:        0.5,
			Confidence:        0.1,
		}, nil
	}

	return data, nil
}

func (m *MenteeService) ExportLearningData(ctx context.Context) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	exportData := make(map[string]*AgentLearningData)
	for k, v := range m.learningData {
		exportData[k] = v
	}

	return json.MarshalIndent(exportData, "", "  ")
}

func (m *MenteeService) ImportLearningData(ctx context.Context, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var importData map[string]*AgentLearningData
	if err := json.Unmarshal(data, &importData); err != nil {
		return fmt.Errorf("failed to unmarshal learning data: %w", err)
	}

	for k, v := range importData {
		m.learningData[k] = v
	}

	m.log.Info("imported learning data", "agents", len(importData))

	return nil
}

func (m *MenteeService) ResetAgentLearning(ctx context.Context, agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.learningData, agentID)

	m.log.Info("reset agent learning data", "agent_id", agentID)

	return nil
}
