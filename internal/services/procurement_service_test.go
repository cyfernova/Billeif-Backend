package services

import (
	"testing"
	"time"

	"invoice-backend/internal/models"
)

func TestNegotiationEvaluationRespectsBuyerAndSellerBounds(t *testing.T) {
	service := &ProcurementService{}

	budget := 95.0
	buyerCfg := &models.AgentConfig{
		Type: models.AgentTypeBuyer,
		BuyerConfig: &models.BuyerConfig{
			MaxDiscountPercent: 20,
			MinDiscountPercent: 5,
			TargetDiscount:     10,
			MaxRounds:          4,
		},
	}
	sellerCfg := &models.AgentConfig{
		Type: models.AgentTypeSeller,
		SellerConfig: &models.SellerConfig{
			MinAcceptablePrice: 24,
			MaxRounds:          4,
			VolumeDiscountTiers: []models.DiscountTier{
				{MinQuantity: 3, DiscountPercent: 5},
			},
			SeasonalAdjustments: &models.SeasonalAdj{
				Multiplier:    0.95,
				EffectiveFrom: time.Now().Add(-time.Hour),
				EffectiveTo:   time.Now().Add(time.Hour),
			},
		},
	}

	eval := service.newNegotiationEvaluation(
		&CreateProcurementRunRequest{Quantity: 3, MaxBudget: budget},
		&procurementCandidateContext{
			product:      &models.MarketplaceProduct{Price: 30},
			sellerConfig: sellerCfg,
		},
		buyerCfg,
		90,
	)

	if eval.openingOffer != 72 {
		t.Fatalf("expected opening offer 72, got %.2f", eval.openingOffer)
	}
	if eval.preferredTarget != 81 {
		t.Fatalf("expected preferred target 81, got %.2f", eval.preferredTarget)
	}
	if eval.maxWillingness != 85.5 {
		t.Fatalf("expected max willingness 85.5, got %.2f", eval.maxWillingness)
	}
	if eval.hardFloor < 81 {
		t.Fatalf("expected hard floor to incorporate seasonal and volume adjustments, got %.2f", eval.hardFloor)
	}
	if eval.negotiationMaxRounds != 8 {
		t.Fatalf("expected 8 total bargaining actions, got %d", eval.negotiationMaxRounds)
	}

	firstBuyer := eval.buyerAmountAt(1)
	lastBuyer := eval.buyerAmountAt(eval.cycles)
	if firstBuyer >= lastBuyer {
		t.Fatalf("expected buyer offers to increase over time: %.2f >= %.2f", firstBuyer, lastBuyer)
	}

	firstSeller := eval.sellerAmountAt(1)
	lastSeller := eval.sellerAmountAt(eval.cycles)
	if firstSeller <= lastSeller {
		t.Fatalf("expected seller counters to decrease over time: %.2f <= %.2f", firstSeller, lastSeller)
	}
	if lastSeller < eval.hardFloor {
		t.Fatalf("expected seller final counter to stay above floor %.2f, got %.2f", eval.hardFloor, lastSeller)
	}
}

func TestSelectWinningCandidateUsesRankingPolicy(t *testing.T) {
	service := &ProcurementService{}
	finalAmount := 80.0
	now := time.Now()

	makeResult := func(id string, verified bool, rating float64, reviews int, responseMs float64, completedAt time.Time) *procurementCandidateResult {
		return &procurementCandidateResult{
			candidate: &models.ProcurementCandidate{
				ID:                id,
				EligibilityStatus: candidateStatusAccepted,
				FinalAmount:       &finalAmount,
			},
			registry: &models.AgentRegistry{
				IsVerified:            verified,
				AverageRating:         rating,
				TotalReviews:          reviews,
				AverageResponseTimeMs: responseMs,
			},
			completedAt: completedAt,
		}
	}

	results := []*procurementCandidateResult{
		makeResult("slow-verified", true, 4.8, 100, 300, now.Add(2*time.Second)),
		makeResult("fast-unverified", false, 5.0, 200, 50, now),
		makeResult("fast-verified", true, 4.9, 120, 60, now.Add(time.Second)),
	}

	winner := service.selectWinningCandidate(results)
	if winner == nil {
		t.Fatal("expected a winning candidate")
	}
	if winner.candidate.ID != "fast-verified" {
		t.Fatalf("expected verified higher-rated candidate to win tie-break, got %s", winner.candidate.ID)
	}
}
