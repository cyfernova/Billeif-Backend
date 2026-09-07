package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/ap2"
	"invoice-backend/pkg/logger"
)

const (
	procurementStatusPending        = "pending"
	procurementStatusMatching       = "matching"
	procurementStatusNegotiating    = "negotiating"
	procurementStatusWinnerSelected = "winner_selected"
	procurementStatusCompleted      = "completed"
	procurementStatusCancelled      = "cancelled"
	procurementStatusFailed         = "failed"

	candidateStatusEligible = "eligible"
	candidateStatusFiltered = "filtered"
	candidateStatusRejected = "rejected"
	candidateStatusAccepted = "accepted"
	candidateStatusSelected = "selected"

	procurementA2ACallTimeout  = 3 * time.Second
	procurementRunTotalTimeout = 15 * time.Second
)

type ProcurementService struct {
	ap2Repo                     interfaces.AP2Repository
	agentSvc                    *AgentService
	intentSvc                   *IntentProcessingService
	shoppingSvc                 *ShoppingAgentService
	merchantSvc                 *MerchantAgentService
	bargaining                  *BargainingService
	agentConfig                 *AgentConfigService
	signer                      *ap2.SignatureService
	log                         *logger.Logger
	ungovernedExecutionDisabled bool
}

func (s *ProcurementService) DisableUngovernedExecution() *ProcurementService {
	if s != nil {
		s.ungovernedExecutionDisabled = true
	}
	return s
}

type CreateProcurementRunRequest struct {
	UserID          string
	ShoppingAgentID string
	Intent          string
	Quantity        int
	MaxBudget       float64
	Currency        string
	PaymentTerms    []string
	AutoBuy         bool
	MaxSellers      int
	MaxRounds       int
	IdempotencyKey  string
}

type procurementCandidateContext struct {
	candidate      *models.ProcurementCandidate
	product        *models.MarketplaceProduct
	merchant       *models.Agent
	registry       *models.AgentRegistry
	sellerConfig   *models.AgentConfig
	sellerCard     *a2a.AgentCard
	sellerEndpoint string
}

type procurementCandidateResult struct {
	candidate     *models.ProcurementCandidate
	product       *models.MarketplaceProduct
	registry      *models.AgentRegistry
	completedAt   time.Time
	internalError error
}

type sellerA2AResult struct {
	Status            string
	Amount            float64
	Currency          string
	AvailableQuantity int
	Reason            string
	ExpiresAt         *time.Time
	TaskID            string
	ResponseMessageID string
	Raw               map[string]interface{}
}

func NewProcurementService(
	ap2Repo interfaces.AP2Repository,
	agentSvc *AgentService,
	intentSvc *IntentProcessingService,
	shoppingSvc *ShoppingAgentService,
	merchantSvc *MerchantAgentService,
	bargaining *BargainingService,
	agentConfig *AgentConfigService,
	signer *ap2.SignatureService,
	log *logger.Logger,
) *ProcurementService {
	return &ProcurementService{
		ap2Repo:                     ap2Repo,
		agentSvc:                    agentSvc,
		intentSvc:                   intentSvc,
		shoppingSvc:                 shoppingSvc,
		merchantSvc:                 merchantSvc,
		bargaining:                  bargaining,
		agentConfig:                 agentConfig,
		signer:                      signer,
		log:                         log,
		ungovernedExecutionDisabled: true,
	}
}

func (s *ProcurementService) StartProcurement(ctx context.Context, req *CreateProcurementRunRequest) (*models.ProcurementRun, error) {
	if s.ungovernedExecutionDisabled {
		return nil, ErrA2AGovernanceRequired
	}
	ctx, cancel := withUpperBoundTimeout(ctx, procurementRunTotalTimeout)
	defer cancel()

	if strings.TrimSpace(req.Intent) == "" {
		return nil, fmt.Errorf("intent is required")
	}
	if req.Quantity <= 0 {
		req.Quantity = 1
	}
	if req.MaxSellers <= 0 {
		req.MaxSellers = 5
	}
	if req.MaxSellers > 20 {
		req.MaxSellers = 20
	}
	if req.MaxRounds <= 0 {
		req.MaxRounds = 5
	}
	if req.MaxRounds > 20 {
		req.MaxRounds = 20
	}
	if req.MaxBudget <= 0 {
		return nil, fmt.Errorf("max_budget must be greater than zero")
	}
	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	if req.Currency == "" {
		req.Currency = "INR"
	}

	shoppingAgent, err := s.agentSvc.GetAgentByID(ctx, req.ShoppingAgentID)
	if err != nil {
		return nil, err
	}
	if NormalizeMarketplaceAgentType(shoppingAgent.Type) != "shopping" {
		return nil, fmt.Errorf("agent is not a shopping agent")
	}

	if strings.TrimSpace(req.IdempotencyKey) != "" {
		existingRun, err := s.ap2Repo.GetProcurementRunByIdempotencyKey(ctx, req.UserID, req.ShoppingAgentID, req.IdempotencyKey)
		if err == nil {
			return existingRun, nil
		}
	}

	maxBudget := req.MaxBudget
	intentMandate, err := s.shoppingSvc.CreateIntentMandate(ctx, &ShoppingIntentRequest{
		UserID:          req.UserID,
		ShoppingAgentID: req.ShoppingAgentID,
		Query:           req.Intent,
		MaxAmount:       &maxBudget,
	})
	if err != nil {
		return nil, fmt.Errorf("create intent mandate: %w", err)
	}

	paymentTermsJSON, err := json.Marshal(req.PaymentTerms)
	if err != nil {
		return nil, fmt.Errorf("marshal payment terms: %w", err)
	}

	run := &models.ProcurementRun{
		UserID:          req.UserID,
		ShoppingAgentID: req.ShoppingAgentID,
		Intent:          req.Intent,
		Quantity:        req.Quantity,
		MaxBudget:       roundCurrency(req.MaxBudget),
		Currency:        req.Currency,
		PaymentTerms:    string(paymentTermsJSON),
		AutoBuy:         req.AutoBuy,
		MaxSellers:      req.MaxSellers,
		Status:          procurementStatusPending,
		IntentMandateID: &intentMandate.ID,
		Metadata: marshalJSON(map[string]interface{}{
			"marketplace_role": MarketplaceRoleBuyer,
			"max_rounds":       req.MaxRounds,
			"a2a_transport":    "/api/v1/a2a",
		}),
	}
	if strings.TrimSpace(req.IdempotencyKey) != "" {
		run.IdempotencyKey = &req.IdempotencyKey
	}

	if err := s.ap2Repo.CreateProcurementRun(ctx, run); err != nil {
		return nil, fmt.Errorf("create procurement run: %w", err)
	}

	if err := s.updateRunStatus(ctx, run, procurementStatusMatching, "", nil); err != nil {
		return nil, err
	}

	if _, err := s.ensureAgentConfig(ctx, shoppingAgent); err != nil {
		return nil, err
	}

	intentResponse := s.intentSvc.ProcessIntent(ctx, &ProcessIntentRequest{
		NaturalLanguage: req.Intent,
		UserID:          req.UserID,
		MaxResults:      maxInt(req.MaxSellers*4, 10),
	})

	if !intentResponse.Success || intentResponse.MatchResults == nil || len(intentResponse.MatchResults.Products) == 0 {
		reason := "no matching seller products found"
		if strings.TrimSpace(intentResponse.Error) != "" {
			reason = intentResponse.Error
		}
		if err := s.updateRunStatus(ctx, run, procurementStatusFailed, reason, nil); err != nil {
			return nil, err
		}
		return s.refreshRun(ctx, run)
	}

	candidates, err := s.prepareCandidates(ctx, run, req, intentResponse.MatchResults.Products)
	if err != nil {
		if updateErr := s.updateRunStatus(ctx, run, procurementStatusFailed, err.Error(), nil); updateErr != nil {
			return nil, updateErr
		}
		return s.refreshRun(ctx, run)
	}
	if len(candidates) == 0 {
		if err := s.updateRunStatus(ctx, run, procurementStatusFailed, "no eligible seller agents found", nil); err != nil {
			return nil, err
		}
		return s.refreshRun(ctx, run)
	}

	if err := s.updateRunStatus(ctx, run, procurementStatusNegotiating, "", nil); err != nil {
		return nil, err
	}

	results := s.evaluateCandidates(ctx, run, req, shoppingAgent, candidates)
	winner := s.selectWinningCandidate(results)
	if winner == nil {
		if releaseErr := s.releaseReservedCandidateInventory(ctx, results); releaseErr != nil {
			return nil, releaseErr
		}
		if err := s.updateRunStatus(ctx, run, procurementStatusFailed, "no seller accepted within buyer budget and seller policies", nil); err != nil {
			return nil, err
		}
		return s.refreshRun(ctx, run)
	}

	if err := s.finalizeWinnerSelection(ctx, run, winner, results, req.AutoBuy); err != nil {
		if updateErr := s.updateRunStatus(ctx, run, procurementStatusFailed, err.Error(), nil); updateErr != nil {
			return nil, updateErr
		}
		return s.refreshRun(ctx, run)
	}

	if !req.AutoBuy {
		return s.refreshRun(ctx, run)
	}

	if err := s.completePurchase(ctx, run, req, winner); err != nil {
		if winner.candidate.ReservedQuantity > 0 {
			_ = s.ap2Repo.ReleaseMarketplaceInventory(ctx, winner.product.ID, winner.candidate.ReservedQuantity)
		}
		if updateErr := s.updateRunStatus(ctx, run, procurementStatusFailed, err.Error(), nil); updateErr != nil {
			return nil, updateErr
		}
		return s.refreshRun(ctx, run)
	}

	return s.refreshRun(ctx, run)
}

func (s *ProcurementService) GetProcurementRun(ctx context.Context, userID, shoppingAgentID, runID string) (*models.ProcurementRun, error) {
	run, err := s.ap2Repo.GetProcurementRunByID(ctx, runID, userID)
	if err != nil {
		return nil, err
	}
	if run.ShoppingAgentID != shoppingAgentID {
		return nil, fmt.Errorf("procurement run not found")
	}
	return run, nil
}

func (s *ProcurementService) CancelProcurement(ctx context.Context, userID, shoppingAgentID, runID string) (*models.ProcurementRun, error) {
	run, err := s.GetProcurementRun(ctx, userID, shoppingAgentID, runID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	run.Status = procurementStatusCancelled
	run.CancelledAt = &now
	reason := "cancelled by user"
	run.FailureReason = &reason

	candidates, err := s.ap2Repo.GetProcurementCandidatesByRun(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		if candidate.ReservedQuantity > 0 {
			if releaseErr := s.ap2Repo.ReleaseMarketplaceInventory(ctx, candidate.MarketplaceProductID, candidate.ReservedQuantity); releaseErr != nil {
				s.log.Warn("failed to release inventory during procurement cancellation", "candidate_id", candidate.ID, "error", releaseErr)
			}
			candidate.ReservedQuantity = 0
		}
		if candidate.SelectionReason == nil {
			candidate.SelectionReason = stringPtr("procurement run cancelled")
		}
		if updateErr := s.ap2Repo.UpdateProcurementCandidate(ctx, candidate); updateErr != nil {
			return nil, updateErr
		}
	}

	if err := s.ap2Repo.UpdateProcurementRun(ctx, run); err != nil {
		return nil, err
	}
	return s.refreshRun(ctx, run)
}

func (s *ProcurementService) prepareCandidates(ctx context.Context, run *models.ProcurementRun, req *CreateProcurementRunRequest, matches []*MatchedProduct) ([]*procurementCandidateContext, error) {
	grouped := make(map[string]*MatchedProduct)
	for _, match := range matches {
		if match == nil || match.Product == nil {
			continue
		}

		merchantID := match.Product.AgentID
		current, exists := grouped[merchantID]
		if !exists || match.RelevanceScore > current.RelevanceScore || (match.RelevanceScore == current.RelevanceScore && match.Product.Price < current.Product.Price) {
			grouped[merchantID] = match
		}
	}

	merchantIDs := make([]string, 0, len(grouped))
	for merchantID := range grouped {
		merchantIDs = append(merchantIDs, merchantID)
	}
	sort.Slice(merchantIDs, func(i, j int) bool {
		left := grouped[merchantIDs[i]]
		right := grouped[merchantIDs[j]]
		if left.RelevanceScore == right.RelevanceScore {
			return left.Product.Price < right.Product.Price
		}
		return left.RelevanceScore > right.RelevanceScore
	})
	results := make([]*procurementCandidateContext, 0, len(merchantIDs))
	for _, merchantID := range merchantIDs {
		if len(results) >= req.MaxSellers {
			break
		}

		match := grouped[merchantID]
		merchant, err := s.agentSvc.GetAgentByID(ctx, merchantID)
		if err != nil {
			return nil, fmt.Errorf("load merchant agent %s: %w", merchantID, err)
		}
		if NormalizeMarketplaceAgentType(merchant.Type) != "merchant" {
			continue
		}
		var registry *models.AgentRegistry
		registry, _ = s.ap2Repo.GetAgentRegistry(ctx, merchantID)
		if registry == nil {
			continue
		}
		ApplyMarketplaceRoleToRegistry(registry)
		if !registry.IsActive || !registry.IsVerified {
			continue
		}

		sellerCard, sellerEndpoint, err := s.resolveProcurementSellerInterface(ctx, registry)
		if err != nil {
			s.log.Warn("skipping seller without usable procurement agent card", "merchant_agent_id", merchantID, "error", err)
			continue
		}

		sellerConfig, err := s.ensureAgentConfig(ctx, merchant)
		if err != nil {
			return nil, err
		}

		candidate := &models.ProcurementCandidate{
			ProcurementRunID:     run.ID,
			MerchantAgentID:      merchantID,
			MarketplaceProductID: match.Product.ID,
			MatchScore:           match.RelevanceScore,
			EligibilityStatus:    candidateStatusEligible,
			Metadata: marshalJSON(map[string]interface{}{
				"match_reasons":            match.MatchReasons,
				"list_price":               roundCurrency(match.Product.Price * float64(req.Quantity)),
				"currency":                 strings.ToUpper(match.Product.Currency),
				"is_verified":              registry != nil && registry.IsVerified,
				"average_rating":           registryMetric(registry, func(r *models.AgentRegistry) float64 { return r.AverageRating }),
				"total_reviews":            registryIntMetric(registry, func(r *models.AgentRegistry) int { return r.TotalReviews }),
				"average_response_time_ms": registryMetric(registry, func(r *models.AgentRegistry) float64 { return r.AverageResponseTimeMs }),
				"registry_id":              registry.ID.String(),
				"seller_endpoint":          sellerEndpoint,
				"seller_card_version":      strings.TrimSpace(sellerCard.Version),
				"supports_procurement":     true,
				"a2a_exchanges":            []map[string]interface{}{},
			}),
		}
		if err := s.ap2Repo.CreateProcurementCandidate(ctx, candidate); err != nil {
			return nil, fmt.Errorf("create procurement candidate: %w", err)
		}

		results = append(results, &procurementCandidateContext{
			candidate:      candidate,
			product:        match.Product,
			merchant:       merchant,
			registry:       registry,
			sellerConfig:   sellerConfig,
			sellerCard:     sellerCard,
			sellerEndpoint: sellerEndpoint,
		})
	}

	return results, nil
}

func (s *ProcurementService) evaluateCandidates(ctx context.Context, run *models.ProcurementRun, req *CreateProcurementRunRequest, shoppingAgent *models.Agent, candidates []*procurementCandidateContext) []*procurementCandidateResult {
	results := make([]*procurementCandidateResult, 0, len(candidates))
	resultCh := make(chan *procurementCandidateResult, len(candidates))
	var wg sync.WaitGroup

	for _, candidate := range candidates {
		wg.Add(1)
		go func(candidate *procurementCandidateContext) {
			defer wg.Done()
			resultCh <- s.evaluateCandidate(ctx, run, req, shoppingAgent, candidate)
		}(candidate)
	}

	wg.Wait()
	close(resultCh)

	for result := range resultCh {
		results = append(results, result)
	}

	return results
}

func (s *ProcurementService) evaluateCandidate(ctx context.Context, run *models.ProcurementRun, req *CreateProcurementRunRequest, shoppingAgent *models.Agent, candidateCtx *procurementCandidateContext) *procurementCandidateResult {
	candidate := candidateCtx.candidate
	product := candidateCtx.product
	registry := candidateCtx.registry
	result := &procurementCandidateResult{
		candidate: candidate,
		product:   product,
		registry:  registry,
	}

	listPrice := roundCurrency(product.Price * float64(req.Quantity))
	if !product.IsAvailable || product.InventoryCount-product.ReservedInventoryCount < req.Quantity {
		return s.rejectCandidate(ctx, result, candidateStatusFiltered, "insufficient available inventory")
	}
	if strings.ToUpper(product.Currency) != req.Currency {
		return s.rejectCandidate(ctx, result, candidateStatusFiltered, "seller does not support requested currency")
	}

	sellerCfg := candidateCtx.sellerConfig.SellerConfig
	if sellerCfg != nil {
		if containsString(sellerCfg.BlacklistedBuyers, req.UserID) || containsString(sellerCfg.BlacklistedBuyers, req.ShoppingAgentID) {
			return s.rejectCandidate(ctx, result, candidateStatusFiltered, "buyer is blacklisted by seller policy")
		}
		if len(req.PaymentTerms) > 0 && !hasIntersection(req.PaymentTerms, sellerCfg.PaymentTerms) {
			return s.rejectCandidate(ctx, result, candidateStatusFiltered, "requested payment terms are unsupported by seller")
		}
	}
	if registry != nil && !registry.IsActive {
		return s.rejectCandidate(ctx, result, candidateStatusFiltered, "seller discovery entry is inactive")
	}
	if registry == nil || !registry.IsVerified {
		return s.rejectCandidate(ctx, result, candidateStatusFiltered, "seller discovery entry is not verified")
	}
	if candidateCtx.sellerCard == nil || !cardSupportsProcurement(candidateCtx.sellerCard) {
		return s.rejectCandidate(ctx, result, candidateStatusFiltered, "seller discovery card does not support procurement")
	}
	if strings.TrimSpace(candidateCtx.sellerEndpoint) == "" {
		return s.rejectCandidate(ctx, result, candidateStatusFiltered, "seller A2A endpoint is unavailable")
	}

	buyerCfg, err := s.ensureAgentConfig(ctx, shoppingAgent)
	if err != nil {
		result.internalError = err
		return result
	}

	eval := s.newNegotiationEvaluation(req, candidateCtx, buyerCfg, listPrice)
	if eval.hardFloor > listPrice {
		return s.rejectCandidate(ctx, result, candidateStatusFiltered, "seller floor exceeds listed marketplace price")
	}
	if eval.maxWillingness < eval.openingOffer {
		eval.openingOffer = eval.maxWillingness
	}
	if eval.maxWillingness <= 0 {
		return s.rejectCandidate(ctx, result, candidateStatusRejected, "buyer budget is below viable negotiation range")
	}

	negotiation, err := s.bargaining.CreateNegotiation(ctx, &CreateNegotiationRequest{
		BuyerAgentID:  req.ShoppingAgentID,
		SellerAgentID: candidate.MerchantAgentID,
		UserID:        req.UserID,
		InitialAmount: eval.openingOffer,
		MaxRounds:     eval.negotiationMaxRounds,
		Metadata: map[string]interface{}{
			"procurement_run_id":      run.ID,
			"candidate_id":            candidate.ID,
			"marketplace_product_id":  product.ID,
			"list_price":              listPrice,
			"currency":                req.Currency,
			"seller_endpoint":         candidateCtx.sellerEndpoint,
			"procurement_managed_a2a": true,
		},
	})
	if err != nil {
		result.internalError = fmt.Errorf("create negotiation: %w", err)
		return result
	}

	candidate.NegotiationID = &negotiation.ID
	if updateErr := s.ap2Repo.UpdateProcurementCandidate(ctx, candidate); updateErr != nil {
		result.internalError = updateErr
		return result
	}

	currentBuyerOffer := eval.openingOffer
	previousSellerAmount := listPrice
	for cycle := 1; cycle <= eval.cycles; cycle++ {
		taskType := taskTypeProcurementQuoteRequest
		if cycle > 1 {
			if _, _, err := s.bargaining.SubmitCounterOffer(ctx, negotiation.ID, &CounterOfferRequest{
				AgentID:        req.ShoppingAgentID,
				ProposedAmount: currentBuyerOffer,
				Action:         "counteroffer",
			}); err != nil {
				result.internalError = fmt.Errorf("buyer counteroffer: %w", err)
				return result
			}
			taskType = taskTypeProcurementNegotiationCounter
		}

		sellerRequest := &SellerProcurementRequest{
			ProcurementRunID: run.ID,
			CandidateID:      candidate.ID,
			BuyerAgentID:     req.ShoppingAgentID,
			SellerAgentID:    candidate.MerchantAgentID,
			ProductID:        product.ID,
			Quantity:         req.Quantity,
			Currency:         req.Currency,
			PaymentTerms:     req.PaymentTerms,
			RoundNumber:      cycle,
			ProposedAmount:   currentBuyerOffer,
			PreviousAmount:   previousSellerAmount,
			MaxBudget:        req.MaxBudget,
			MaxRounds:        eval.cycles,
			DeadlineAt:       time.Now().UTC().Add(procurementA2ACallTimeout),
		}

		sellerResponse, err := s.sendSellerNegotiationRequest(ctx, candidate, candidateCtx.sellerEndpoint, taskType, sellerRequest)
		if err != nil {
			s.log.Warn("seller negotiation request failed", "candidate_id", candidate.ID, "task_type", taskType, "error", err)
			return s.rejectCandidate(ctx, result, candidateStatusRejected, "seller A2A negotiation failed")
		}

		switch sellerResponse.Status {
		case procurementResponseAccept:
			if _, _, err := s.bargaining.SubmitCounterOffer(ctx, negotiation.ID, &CounterOfferRequest{
				AgentID:        candidate.MerchantAgentID,
				ProposedAmount: sellerResponse.Amount,
				Action:         "accept",
			}); err != nil {
				result.internalError = fmt.Errorf("seller accept: %w", err)
				return result
			}
			return s.acceptCandidate(ctx, result, sellerResponse.Amount, req.Quantity)
		case procurementResponseQuote, procurementResponseCounteroffer:
			previousSellerAmount = sellerResponse.Amount
			reason := sellerResponse.Reason
			if _, _, err := s.bargaining.SubmitCounterOffer(ctx, negotiation.ID, &CounterOfferRequest{
				AgentID:        candidate.MerchantAgentID,
				ProposedAmount: sellerResponse.Amount,
				Action:         "counteroffer",
				Reason:         stringPtr(reason),
			}); err != nil {
				result.internalError = fmt.Errorf("seller counteroffer: %w", err)
				return result
			}

			if sellerResponse.Amount <= eval.maxWillingness {
				acceptRequest := *sellerRequest
				acceptRequest.ProposedAmount = sellerResponse.Amount
				acceptRequest.PreviousAmount = sellerResponse.Amount
				acceptRequest.DeadlineAt = time.Now().UTC().Add(procurementA2ACallTimeout)

				acceptResponse, err := s.sendSellerNegotiationRequest(ctx, candidate, candidateCtx.sellerEndpoint, taskTypeProcurementNegotiationAccept, &acceptRequest)
				if err != nil {
					s.log.Warn("seller acceptance confirmation failed", "candidate_id", candidate.ID, "error", err)
					return s.rejectCandidate(ctx, result, candidateStatusRejected, "seller failed to confirm buyer acceptance")
				}

				if acceptResponse.Status != procurementResponseAccept {
					if acceptResponse.Status == procurementResponseReject || acceptResponse.Status == procurementResponseUnavailable {
						if _, _, err := s.bargaining.SubmitCounterOffer(ctx, negotiation.ID, &CounterOfferRequest{
							AgentID:        candidate.MerchantAgentID,
							ProposedAmount: maxFloat(acceptResponse.Amount, sellerResponse.Amount),
							Action:         "reject",
							Reason:         stringPtr(candidateTerminalReason(acceptResponse, "seller did not confirm the quoted amount")),
						}); err != nil {
							result.internalError = fmt.Errorf("seller reject after accept request: %w", err)
							return result
						}
						return s.rejectCandidate(ctx, result, candidateStatusRejected, candidateTerminalReason(acceptResponse, "seller did not confirm the quoted amount"))
					}
					return s.rejectCandidate(ctx, result, candidateStatusRejected, "seller returned an unsupported acceptance response")
				}

				if _, _, err := s.bargaining.SubmitCounterOffer(ctx, negotiation.ID, &CounterOfferRequest{
					AgentID:        req.ShoppingAgentID,
					ProposedAmount: sellerResponse.Amount,
					Action:         "accept",
				}); err != nil {
					result.internalError = fmt.Errorf("buyer accept: %w", err)
					return result
				}
				return s.acceptCandidate(ctx, result, sellerResponse.Amount, req.Quantity)
			}

			if cycle == eval.cycles {
				break
			}

			nextBuyerOffer := eval.buyerAmountAt(cycle + 1)
			if nextBuyerOffer <= currentBuyerOffer {
				break
			}
			currentBuyerOffer = nextBuyerOffer
		case procurementResponseReject, procurementResponseUnavailable:
			if _, _, err := s.bargaining.SubmitCounterOffer(ctx, negotiation.ID, &CounterOfferRequest{
				AgentID:        candidate.MerchantAgentID,
				ProposedAmount: maxFloat(sellerResponse.Amount, currentBuyerOffer),
				Action:         "reject",
				Reason:         stringPtr(candidateTerminalReason(sellerResponse, "seller rejected the procurement request")),
			}); err != nil {
				result.internalError = fmt.Errorf("seller reject: %w", err)
				return result
			}
			return s.rejectCandidate(ctx, result, candidateStatusRejected, candidateTerminalReason(sellerResponse, "seller rejected the procurement request"))
		default:
			return s.rejectCandidate(ctx, result, candidateStatusRejected, "seller returned an unsupported procurement response")
		}
	}

	rejectRequest := &SellerProcurementRequest{
		ProcurementRunID: run.ID,
		CandidateID:      candidate.ID,
		BuyerAgentID:     req.ShoppingAgentID,
		SellerAgentID:    candidate.MerchantAgentID,
		ProductID:        product.ID,
		Quantity:         req.Quantity,
		Currency:         req.Currency,
		PaymentTerms:     req.PaymentTerms,
		RoundNumber:      eval.cycles,
		ProposedAmount:   currentBuyerOffer,
		PreviousAmount:   previousSellerAmount,
		MaxBudget:        req.MaxBudget,
		MaxRounds:        eval.cycles,
		DeadlineAt:       time.Now().UTC().Add(procurementA2ACallTimeout),
	}
	if _, err := s.sendSellerNegotiationRequest(ctx, candidate, candidateCtx.sellerEndpoint, taskTypeProcurementNegotiationReject, rejectRequest); err != nil {
		s.log.Warn("failed to notify seller of procurement rejection", "candidate_id", candidate.ID, "error", err)
	}

	if _, _, err := s.bargaining.SubmitCounterOffer(ctx, negotiation.ID, &CounterOfferRequest{
		AgentID:        req.ShoppingAgentID,
		ProposedAmount: currentBuyerOffer,
		Action:         "reject",
		Reason:         stringPtr("maximum negotiation rounds reached without acceptable price"),
	}); err != nil {
		result.internalError = fmt.Errorf("reject negotiation: %w", err)
		return result
	}

	return s.rejectCandidate(ctx, result, candidateStatusRejected, "max negotiation rounds exhausted")
}

func (s *ProcurementService) finalizeWinnerSelection(ctx context.Context, run *models.ProcurementRun, winner *procurementCandidateResult, results []*procurementCandidateResult, autoBuy bool) error {
	for _, result := range results {
		if result == nil || result.candidate == nil {
			continue
		}

		switch {
		case result.candidate.ID == winner.candidate.ID:
			reason := "selected by lowest accepted amount and marketplace ranking policy"
			if !autoBuy {
				reason = "selected as best seller; inventory released because auto_buy is disabled"
				if winner.candidate.ReservedQuantity > 0 {
					if err := s.ap2Repo.ReleaseMarketplaceInventory(ctx, winner.product.ID, winner.candidate.ReservedQuantity); err != nil {
						return fmt.Errorf("release winner reservation: %w", err)
					}
					winner.candidate.ReservedQuantity = 0
				}
				winner.candidate.EligibilityStatus = procurementStatusWinnerSelected
			} else {
				winner.candidate.EligibilityStatus = candidateStatusSelected
			}
			result.candidate.SelectionReason = &reason
		default:
			reason := "better seller outcome selected by ranking policy"
			result.candidate.SelectionReason = &reason
			if result.candidate.ReservedQuantity > 0 {
				if err := s.ap2Repo.ReleaseMarketplaceInventory(ctx, result.product.ID, result.candidate.ReservedQuantity); err != nil {
					return fmt.Errorf("release losing reservation: %w", err)
				}
				result.candidate.ReservedQuantity = 0
			}
		}

		if err := s.ap2Repo.UpdateProcurementCandidate(ctx, result.candidate); err != nil {
			return err
		}
	}

	run.WinningCandidateID = &winner.candidate.ID
	run.WinningNegotiationID = winner.candidate.NegotiationID
	status := procurementStatusWinnerSelected
	if autoBuy {
		status = procurementStatusNegotiating
	}
	return s.updateRunStatus(ctx, run, status, "", nil)
}

func (s *ProcurementService) completePurchase(ctx context.Context, run *models.ProcurementRun, req *CreateProcurementRunRequest, winner *procurementCandidateResult) error {
	if winner.candidate.FinalAmount == nil {
		return fmt.Errorf("winning candidate has no accepted amount")
	}

	merchantID := winner.candidate.MerchantAgentID
	unitPrice := *winner.candidate.FinalAmount / float64(req.Quantity)
	shoppingAgent, err := s.ap2Repo.GetAgentByID(ctx, req.ShoppingAgentID)
	if err != nil {
		return fmt.Errorf("load shopping agent scope: %w", err)
	}
	cartMandate, err := s.shoppingSvc.CreateCartMandate(ctx, &CreateCartMandateRequest{
		UserID:          req.UserID,
		BusinessID:      shoppingAgent.BusinessID,
		ShoppingAgentID: req.ShoppingAgentID,
		MerchantID:      &merchantID,
		IntentMandateID: run.IntentMandateID,
		Items: []ap2.CartItem{
			{
				ProductID: winner.product.ID,
				Name:      winner.product.Name,
				Quantity:  req.Quantity,
				UnitPrice: roundCurrency(unitPrice),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("create cart mandate: %w", err)
	}
	run.CartMandateID = &cartMandate.ID

	merchantSigned := false
	if sellerEndpoint := candidateA2AEndpoint(winner.candidate, winner.registry); sellerEndpoint != "" {
		if _, err := s.shoppingSvc.ProcessCartWithMerchantA2A(ctx, sellerEndpoint, cartMandate, req.UserID); err != nil {
			s.log.Warn("merchant A2A cart processing failed, falling back to direct merchant signature", "cart_id", cartMandate.ID, "error", err)
		} else {
			merchantSigned = true
		}
	}

	if !merchantSigned {
		if err := s.merchantSvc.RespondToCart(ctx, cartMandate.ID, merchantID, "signed"); err != nil {
			return fmt.Errorf("merchant sign cart: %w", err)
		}
	}

	paymentMandate, err := s.shoppingSvc.CompleteCheckout(ctx, &CheckoutRequest{
		UserID:        req.UserID,
		BusinessID:    shoppingAgent.BusinessID,
		CartMandateID: cartMandate.ID,
	})
	if err != nil {
		return fmt.Errorf("create payment mandate: %w", err)
	}
	run.PaymentMandateID = &paymentMandate.ID

	order := &models.MarketplaceOrder{
		UserID:           req.UserID,
		ShoppingAgentID:  req.ShoppingAgentID,
		MerchantAgentID:  merchantID,
		CartMandateID:    cartMandate.ID,
		PaymentMandateID: &paymentMandate.ID,
		TotalAmount:      *winner.candidate.FinalAmount,
		Currency:         req.Currency,
		Status:           "pending",
	}
	if err := s.ap2Repo.CreateOrder(ctx, order); err != nil {
		return fmt.Errorf("create order: %w", err)
	}
	run.OrderID = &order.ID

	if winner.candidate.ReservedQuantity > 0 {
		if err := s.ap2Repo.CommitMarketplaceInventory(ctx, winner.product.ID, winner.candidate.ReservedQuantity); err != nil {
			return fmt.Errorf("commit inventory: %w", err)
		}
		winner.candidate.ReservedQuantity = 0
		if err := s.ap2Repo.UpdateProcurementCandidate(ctx, winner.candidate); err != nil {
			return err
		}
	}

	return s.updateRunStatus(ctx, run, procurementStatusCompleted, "", map[string]interface{}{
		"order_id":           order.ID,
		"payment_mandate_id": paymentMandate.ID,
		"cart_mandate_id":    cartMandate.ID,
	})
}

func (s *ProcurementService) acceptCandidate(ctx context.Context, result *procurementCandidateResult, finalAmount float64, quantity int) *procurementCandidateResult {
	result.completedAt = time.Now().UTC()
	result.candidate.EligibilityStatus = candidateStatusAccepted
	finalAmount = roundCurrency(finalAmount)
	result.candidate.FinalAmount = &finalAmount
	reason := "seller accepted negotiated amount"
	result.candidate.TerminalReason = &reason

	result.candidate.ReservedQuantity = quantity
	if err := s.ap2Repo.UpdateProcurementCandidate(ctx, result.candidate); err != nil {
		result.internalError = err
	}
	return result
}

func (s *ProcurementService) rejectCandidate(ctx context.Context, result *procurementCandidateResult, status, reason string) *procurementCandidateResult {
	result.completedAt = time.Now().UTC()
	result.candidate.EligibilityStatus = status
	result.candidate.TerminalReason = &reason
	if err := s.ap2Repo.UpdateProcurementCandidate(ctx, result.candidate); err != nil {
		result.internalError = err
	}
	return result
}

func (s *ProcurementService) selectWinningCandidate(results []*procurementCandidateResult) *procurementCandidateResult {
	accepted := make([]*procurementCandidateResult, 0, len(results))
	for _, result := range results {
		if result == nil || result.internalError != nil || result.candidate == nil || result.candidate.FinalAmount == nil {
			continue
		}
		if result.candidate.EligibilityStatus != candidateStatusAccepted {
			continue
		}
		accepted = append(accepted, result)
	}
	if len(accepted) == 0 {
		return nil
	}

	sort.SliceStable(accepted, func(i, j int) bool {
		left := accepted[i]
		right := accepted[j]
		if compareFloat(*left.candidate.FinalAmount, *right.candidate.FinalAmount) != 0 {
			return *left.candidate.FinalAmount < *right.candidate.FinalAmount
		}

		leftVerified := left.registry != nil && left.registry.IsVerified
		rightVerified := right.registry != nil && right.registry.IsVerified
		if leftVerified != rightVerified {
			return leftVerified
		}

		leftRating := registryMetric(left.registry, func(r *models.AgentRegistry) float64 { return r.AverageRating })
		rightRating := registryMetric(right.registry, func(r *models.AgentRegistry) float64 { return r.AverageRating })
		if compareFloat(leftRating, rightRating) != 0 {
			return leftRating > rightRating
		}

		leftReviews := registryIntMetric(left.registry, func(r *models.AgentRegistry) int { return r.TotalReviews })
		rightReviews := registryIntMetric(right.registry, func(r *models.AgentRegistry) int { return r.TotalReviews })
		if leftReviews != rightReviews {
			return leftReviews > rightReviews
		}

		leftResponse := registryMetric(left.registry, func(r *models.AgentRegistry) float64 { return r.AverageResponseTimeMs })
		rightResponse := registryMetric(right.registry, func(r *models.AgentRegistry) float64 { return r.AverageResponseTimeMs })
		if compareFloat(leftResponse, rightResponse) != 0 {
			return leftResponse < rightResponse
		}

		return left.completedAt.Before(right.completedAt)
	})

	return accepted[0]
}

func (s *ProcurementService) ensureAgentConfig(ctx context.Context, agent *models.Agent) (*models.AgentConfig, error) {
	config, err := s.agentConfig.GetAgentConfig(ctx, agent.ID)
	if err == nil {
		return config.Config, nil
	}
	if err != ErrAgentConfigNotFound {
		return nil, err
	}

	defaultConfig, err := s.agentConfig.CreateDefaultConfigForAgentType(agent.Type)
	if err != nil {
		return nil, err
	}
	saved, err := s.agentConfig.SaveAgentConfig(ctx, agent, defaultConfig)
	if err != nil {
		return nil, err
	}
	return saved.Config, nil
}

func (s *ProcurementService) releaseReservedCandidateInventory(ctx context.Context, results []*procurementCandidateResult) error {
	for _, result := range results {
		if result == nil || result.candidate == nil || result.candidate.ReservedQuantity <= 0 || result.product == nil {
			continue
		}
		if err := s.ap2Repo.ReleaseMarketplaceInventory(ctx, result.product.ID, result.candidate.ReservedQuantity); err != nil {
			return err
		}
		result.candidate.ReservedQuantity = 0
		if updateErr := s.ap2Repo.UpdateProcurementCandidate(ctx, result.candidate); updateErr != nil {
			return updateErr
		}
	}
	return nil
}

func (s *ProcurementService) updateRunStatus(ctx context.Context, run *models.ProcurementRun, status, failureReason string, metadata map[string]interface{}) error {
	run.Status = status
	if failureReason != "" {
		run.FailureReason = &failureReason
	} else {
		run.FailureReason = nil
	}
	if metadata != nil {
		run.Metadata = marshalJSON(metadata)
	}
	return s.ap2Repo.UpdateProcurementRun(ctx, run)
}

func (s *ProcurementService) refreshRun(ctx context.Context, run *models.ProcurementRun) (*models.ProcurementRun, error) {
	return s.ap2Repo.GetProcurementRunByID(ctx, run.ID, run.UserID)
}

type negotiationEvaluation struct {
	cycles               int
	negotiationMaxRounds int
	openingOffer         float64
	preferredTarget      float64
	maxWillingness       float64
	hardFloor            float64
	listPrice            float64
}

func (s *ProcurementService) newNegotiationEvaluation(req *CreateProcurementRunRequest, candidateCtx *procurementCandidateContext, buyerCfg *models.AgentConfig, listPrice float64) *negotiationEvaluation {
	buyer := buyerCfg.BuyerConfig
	seller := candidateCtx.sellerConfig.SellerConfig

	cycles := minPositive(req.MaxRounds, buyer.MaxRounds, seller.MaxRounds)
	if cycles == 0 {
		cycles = 5
	}

	openingOffer := roundCurrency(listPrice * (1 - buyer.MaxDiscountPercent/100))
	preferredTarget := roundCurrency(listPrice * (1 - buyer.TargetDiscount/100))
	maxWillingness := roundCurrency(minFloat(req.MaxBudget, listPrice*(1-buyer.MinDiscountPercent/100)))
	hardFloor := roundCurrency(s.calculateSellerHardFloor(candidateCtx.product, seller, req.Quantity))

	if preferredTarget < openingOffer {
		preferredTarget = openingOffer
	}
	if preferredTarget > maxWillingness {
		preferredTarget = maxWillingness
	}
	if openingOffer < 0 {
		openingOffer = 0
	}
	if maxWillingness > listPrice {
		maxWillingness = listPrice
	}

	return &negotiationEvaluation{
		cycles:               cycles,
		negotiationMaxRounds: minInt(20, cycles*2),
		openingOffer:         openingOffer,
		preferredTarget:      preferredTarget,
		maxWillingness:       maxWillingness,
		hardFloor:            hardFloor,
		listPrice:            listPrice,
	}
}

func (e *negotiationEvaluation) buyerAmountAt(cycle int) float64 {
	if e.cycles <= 1 {
		return roundCurrency(e.maxWillingness)
	}

	progress := float64(cycle-1) / float64(e.cycles-1)
	targetWeight := math.Min(progress*1.4, 1)
	offer := e.openingOffer + (e.preferredTarget-e.openingOffer)*targetWeight
	if progress > 0.6 {
		lateProgress := (progress - 0.6) / 0.4
		offer = e.preferredTarget + (e.maxWillingness-e.preferredTarget)*lateProgress
	}
	return roundCurrency(minFloat(e.maxWillingness, maxFloat(e.openingOffer, offer)))
}

func (e *negotiationEvaluation) sellerAmountAt(cycle int) float64 {
	if e.cycles <= 1 {
		return roundCurrency(e.hardFloor)
	}
	progress := float64(cycle) / float64(e.cycles)
	counter := e.listPrice - (e.listPrice-e.hardFloor)*progress
	return roundCurrency(maxFloat(e.hardFloor, minFloat(e.listPrice, counter)))
}

func (s *ProcurementService) calculateSellerHardFloor(product *models.MarketplaceProduct, seller *models.SellerConfig, quantity int) float64 {
	listPrice := product.Price * float64(quantity)

	floor := seller.MinAcceptablePrice * float64(quantity)
	seasonal := listPrice
	if seller.SeasonalAdjustments != nil {
		now := time.Now().UTC()
		if (seller.SeasonalAdjustments.EffectiveFrom.IsZero() || !now.Before(seller.SeasonalAdjustments.EffectiveFrom)) &&
			(seller.SeasonalAdjustments.EffectiveTo.IsZero() || !now.After(seller.SeasonalAdjustments.EffectiveTo)) &&
			seller.SeasonalAdjustments.Multiplier > 0 {
			seasonal = listPrice * seller.SeasonalAdjustments.Multiplier
		}
	}

	volumeDiscount := 0.0
	for _, tier := range seller.VolumeDiscountTiers {
		if quantity >= tier.MinQuantity && tier.DiscountPercent > volumeDiscount {
			volumeDiscount = tier.DiscountPercent
		}
	}
	if volumeDiscount > 0 {
		seasonal = seasonal * (1 - volumeDiscount/100)
	}

	return maxFloat(floor, seasonal)
}

func marshalJSON(value interface{}) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func roundCurrency(value float64) float64 {
	return math.Round(value*100) / 100
}

func compareFloat(left, right float64) int {
	diff := roundCurrency(left - right)
	switch {
	case diff < 0:
		return -1
	case diff > 0:
		return 1
	default:
		return 0
	}
}

func minFloat(values ...float64) float64 {
	if len(values) == 0 {
		return 0
	}
	minValue := values[0]
	for _, value := range values[1:] {
		if value < minValue {
			minValue = value
		}
	}
	return minValue
}

func minPositive(values ...int) int {
	best := 0
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if best == 0 || value < best {
			best = value
		}
	}
	return best
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(strings.ToLower(target))
	for _, value := range values {
		if strings.TrimSpace(strings.ToLower(value)) == target {
			return true
		}
	}
	return false
}

func hasIntersection(left, right []string) bool {
	lookup := make(map[string]struct{}, len(right))
	for _, value := range right {
		lookup[strings.TrimSpace(strings.ToLower(value))] = struct{}{}
	}
	for _, value := range left {
		if _, ok := lookup[strings.TrimSpace(strings.ToLower(value))]; ok {
			return true
		}
	}
	return false
}

func registryMetric(registry *models.AgentRegistry, getter func(*models.AgentRegistry) float64) float64 {
	if registry == nil {
		return 0
	}
	return getter(registry)
}

func registryIntMetric(registry *models.AgentRegistry, getter func(*models.AgentRegistry) int) int {
	if registry == nil {
		return 0
	}
	return getter(registry)
}

func (s *ProcurementService) resolveProcurementSellerInterface(ctx context.Context, registry *models.AgentRegistry) (*a2a.AgentCard, string, error) {
	if registry == nil {
		return nil, "", fmt.Errorf("seller registry entry not found")
	}

	var (
		card      *a2a.AgentCard
		storedErr error
	)
	card, storedErr = parseRegistryAgentCard(registry)

	if registry.WellKnownURI != nil && strings.TrimSpace(*registry.WellKnownURI) != "" {
		refreshCtx, cancel := context.WithTimeout(ctx, procurementA2ACallTimeout)
		fetchedCard, err := fetchA2AAgentCard(refreshCtx, *registry.WellKnownURI)
		cancel()
		if err != nil {
			s.log.Warn("failed to refresh seller agent card from well-known URI", "agent_id", registry.AgentID, "well_known_uri", *registry.WellKnownURI, "error", err)
		} else if fetchedCard != nil {
			card = normalizeParsedAgentCard(fetchedCard, registry)
		}
	}

	if card == nil {
		return nil, "", storedErr
	}
	if !cardSupportsProcurement(card) {
		return nil, "", fmt.Errorf("seller agent card does not advertise procurement skills")
	}

	endpoint := resolveRegistryA2AEndpoint(card, registry)
	if endpoint == "" {
		return nil, "", fmt.Errorf("seller A2A endpoint is unavailable")
	}

	return normalizeParsedAgentCard(card, registry), endpoint, nil
}

func (s *ProcurementService) sendSellerNegotiationRequest(ctx context.Context, candidate *models.ProcurementCandidate, endpoint, taskType string, payload *SellerProcurementRequest) (*sellerA2AResult, error) {
	if s.shoppingSvc == nil || s.shoppingSvc.a2aClient == nil {
		return nil, fmt.Errorf("A2A client is unavailable")
	}

	requestMessage := a2a.NewTextMessage(a2a.RoleUser, fmt.Sprintf("Handle %s for procurement candidate %s.", taskType, candidate.ID))
	requestMessage.Metadata = map[string]interface{}{
		"taskType": taskType,
	}

	requestMetadata := map[string]interface{}{
		"taskType":         taskType,
		"procurementRunId": payload.ProcurementRunID,
		"candidateId":      payload.CandidateID,
		"buyerAgentId":     payload.BuyerAgentID,
		"sellerAgentId":    payload.SellerAgentID,
		"productId":        payload.ProductID,
		"quantity":         payload.Quantity,
		"currency":         payload.Currency,
		"paymentTerms":     payload.PaymentTerms,
		"roundNumber":      payload.RoundNumber,
		"proposedAmount":   roundCurrency(payload.ProposedAmount),
		"previousAmount":   roundCurrency(payload.PreviousAmount),
		"maxBudget":        roundCurrency(payload.MaxBudget),
		"maxRounds":        payload.MaxRounds,
		"deadlineAt":       payload.DeadlineAt.UTC().Format(time.RFC3339),
	}

	request := &a2a.SendMessageRequest{
		Message:  requestMessage,
		Metadata: requestMetadata,
	}

	callCtx, cancel := context.WithTimeout(ctx, procurementA2ACallTimeout)
	defer cancel()

	response, err := s.shoppingSvc.a2aClient.SendMessage(callCtx, endpoint, request)
	if err != nil {
		_ = s.appendCandidateA2AExchange(ctx, candidate, map[string]interface{}{
			"task_type":           taskType,
			"request_message_id":  requestMessage.MessageID,
			"status":              "error",
			"timestamp":           time.Now().UTC().Format(time.RFC3339),
			"request":             requestMetadata,
			"response_error":      err.Error(),
			"seller_a2a_endpoint": endpoint,
		})
		return nil, err
	}

	result, parseErr := s.parseSellerA2AResponse(response)
	if parseErr != nil {
		_ = s.appendCandidateA2AExchange(ctx, candidate, map[string]interface{}{
			"task_type":           taskType,
			"request_message_id":  requestMessage.MessageID,
			"status":              "error",
			"timestamp":           time.Now().UTC().Format(time.RFC3339),
			"request":             requestMetadata,
			"response_error":      parseErr.Error(),
			"seller_a2a_endpoint": endpoint,
		})
		return nil, parseErr
	}

	if err := s.appendCandidateA2AExchange(ctx, candidate, map[string]interface{}{
		"task_type":           taskType,
		"task_id":             result.TaskID,
		"request_message_id":  requestMessage.MessageID,
		"response_message_id": result.ResponseMessageID,
		"status":              result.Status,
		"timestamp":           time.Now().UTC().Format(time.RFC3339),
		"request":             requestMetadata,
		"response":            result.Raw,
		"seller_a2a_endpoint": endpoint,
	}); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *ProcurementService) parseSellerA2AResponse(response *a2a.SendMessageResponse) (*sellerA2AResult, error) {
	if response == nil || response.Task == nil {
		return nil, fmt.Errorf("seller A2A response did not include a task")
	}
	if response.Task.Status.State != a2a.TaskStateCompleted {
		return nil, fmt.Errorf("seller A2A task finished in state %s", response.Task.Status.State)
	}

	var payload map[string]interface{}
	for i := len(response.Task.Artifacts) - 1; i >= 0; i-- {
		artifact := response.Task.Artifacts[i]
		for _, part := range artifact.Parts {
			if len(part.Data) == 0 {
				continue
			}
			payload = make(map[string]interface{}, len(part.Data))
			for key, value := range part.Data {
				payload[key] = value
			}
			break
		}
		if payload != nil {
			break
		}
	}
	if payload == nil {
		return nil, fmt.Errorf("seller A2A response artifact is missing")
	}

	status := strings.TrimSpace(fmt.Sprintf("%v", payload["status"]))
	if status == "" {
		return nil, fmt.Errorf("seller A2A response status is missing")
	}

	amount, err := interfaceToFloat(payload["amount"])
	if err != nil {
		return nil, fmt.Errorf("seller A2A response amount is invalid: %w", err)
	}
	if amount < 0 {
		return nil, fmt.Errorf("seller A2A response amount cannot be negative")
	}

	availableQuantity, err := interfaceToInt(payload["availableQuantity"])
	if err != nil {
		availableQuantity = 0
	}

	var expiresAt *time.Time
	if rawExpiresAt := strings.TrimSpace(fmt.Sprintf("%v", payload["expiresAt"])); rawExpiresAt != "" && rawExpiresAt != "<nil>" {
		parsed, err := time.Parse(time.RFC3339, rawExpiresAt)
		if err == nil {
			expiresAt = &parsed
		}
	}

	result := &sellerA2AResult{
		Status:            status,
		Amount:            roundCurrency(amount),
		Currency:          strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", payload["currency"]))),
		AvailableQuantity: availableQuantity,
		Reason:            strings.TrimSpace(fmt.Sprintf("%v", payload["reason"])),
		ExpiresAt:         expiresAt,
		TaskID:            response.Task.ID,
		Raw:               payload,
	}

	if historyLen := len(response.Task.History); historyLen > 0 {
		result.ResponseMessageID = response.Task.History[historyLen-1].MessageID
	}

	return result, nil
}

func (s *ProcurementService) appendCandidateA2AExchange(ctx context.Context, candidate *models.ProcurementCandidate, exchange map[string]interface{}) error {
	metadata := candidateMetadataMap(candidate)
	exchanges := make([]interface{}, 0)
	if rawExchanges, ok := metadata["a2a_exchanges"].([]interface{}); ok {
		exchanges = append(exchanges, rawExchanges...)
	}
	exchanges = append(exchanges, exchange)
	metadata["a2a_exchanges"] = exchanges
	candidate.Metadata = marshalJSON(metadata)
	return s.ap2Repo.UpdateProcurementCandidate(ctx, candidate)
}

func candidateMetadataMap(candidate *models.ProcurementCandidate) map[string]interface{} {
	if candidate == nil || strings.TrimSpace(candidate.Metadata) == "" {
		return map[string]interface{}{}
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(candidate.Metadata), &metadata); err != nil || metadata == nil {
		return map[string]interface{}{}
	}
	return metadata
}

func candidateA2AEndpoint(candidate *models.ProcurementCandidate, registry *models.AgentRegistry) string {
	metadata := candidateMetadataMap(candidate)
	if endpoint := strings.TrimSpace(interfaceToString(metadata["seller_endpoint"])); endpoint != "" {
		return endpoint
	}
	if registry != nil && registry.A2AEndpoint != nil {
		return strings.TrimSpace(*registry.A2AEndpoint)
	}
	return ""
}

func candidateTerminalReason(response *sellerA2AResult, fallback string) string {
	if response == nil {
		return fallback
	}
	if strings.TrimSpace(response.Reason) != "" && response.Reason != "<nil>" {
		return strings.TrimSpace(response.Reason)
	}
	return fallback
}

func interfaceToFloat(value interface{}) (float64, error) {
	switch typed := value.(type) {
	case nil:
		return 0, nil
	case float64:
		return typed, nil
	case float32:
		return float64(typed), nil
	case int:
		return float64(typed), nil
	case int32:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	case json.Number:
		return typed.Float64()
	case string:
		if strings.TrimSpace(typed) == "" {
			return 0, nil
		}
		var parsed json.Number = json.Number(strings.TrimSpace(typed))
		return parsed.Float64()
	default:
		return 0, fmt.Errorf("unsupported numeric value %T", value)
	}
}

func interfaceToInt(value interface{}) (int, error) {
	switch typed := value.(type) {
	case nil:
		return 0, nil
	case int:
		return typed, nil
	case int32:
		return int(typed), nil
	case int64:
		return int(typed), nil
	case float64:
		return int(typed), nil
	case float32:
		return int(typed), nil
	case json.Number:
		parsed, err := typed.Int64()
		return int(parsed), err
	case string:
		if strings.TrimSpace(typed) == "" {
			return 0, nil
		}
		var parsed json.Number = json.Number(strings.TrimSpace(typed))
		value, err := parsed.Int64()
		return int(value), err
	default:
		return 0, fmt.Errorf("unsupported integer value %T", value)
	}
}

func interfaceToString(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}

func withUpperBoundTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= timeout {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}
