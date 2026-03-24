package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"
)

const (
	procurementResponseQuote        = "quote"
	procurementResponseCounteroffer = "counteroffer"
	procurementResponseAccept       = "accept"
	procurementResponseReject       = "reject"
	procurementResponseUnavailable  = "unavailable"
)

type sellerNegotiationRepository interface {
	GetAgentByID(ctx context.Context, id string) (*models.Agent, error)
	GetMarketplaceProductByID(ctx context.Context, id string) (*models.MarketplaceProduct, error)
	GetAgentRegistry(ctx context.Context, agentID string) (*models.AgentRegistry, error)
	ReserveMarketplaceInventory(ctx context.Context, productID string, quantity int) error
}

type SellerNegotiationService struct {
	repo        sellerNegotiationRepository
	agentConfig *AgentConfigService
	log         *logger.Logger
}

type SellerProcurementRequest struct {
	ProcurementRunID string    `json:"procurementRunId"`
	CandidateID      string    `json:"candidateId"`
	BuyerAgentID     string    `json:"buyerAgentId"`
	SellerAgentID    string    `json:"sellerAgentId"`
	ProductID        string    `json:"productId"`
	Quantity         int       `json:"quantity"`
	Currency         string    `json:"currency"`
	PaymentTerms     []string  `json:"paymentTerms"`
	RoundNumber      int       `json:"roundNumber"`
	ProposedAmount   float64   `json:"proposedAmount"`
	PreviousAmount   float64   `json:"previousAmount"`
	MaxBudget        float64   `json:"maxBudget"`
	MaxRounds        int       `json:"maxRounds"`
	DeadlineAt       time.Time `json:"deadlineAt"`
}

type SellerProcurementResponse struct {
	Status            string     `json:"status"`
	Amount            float64    `json:"amount"`
	Currency          string     `json:"currency"`
	AvailableQuantity int        `json:"availableQuantity"`
	Reason            string     `json:"reason,omitempty"`
	ExpiresAt         *time.Time `json:"expiresAt,omitempty"`
}

type sellerNegotiationContext struct {
	request        *SellerProcurementRequest
	seller         *models.Agent
	product        *models.MarketplaceProduct
	registry       *models.AgentRegistry
	sellerConfig   *models.AgentConfig
	totalListPrice float64
	hardFloor      float64
	maxRounds      int
}

func NewSellerNegotiationService(repo sellerNegotiationRepository, agentConfig *AgentConfigService, log *logger.Logger) *SellerNegotiationService {
	return &SellerNegotiationService{
		repo:        repo,
		agentConfig: agentConfig,
		log:         log,
	}
}

func (s *SellerNegotiationService) HandleTask(ctx context.Context, taskType string, metadata map[string]interface{}) (a2a.Message, *a2a.Artifact, error) {
	req, err := s.parseRequest(metadata)
	if err != nil {
		return a2a.Message{}, nil, err
	}

	context, err := s.loadContext(ctx, req)
	if err != nil {
		return s.buildResponse(taskType, req, &SellerProcurementResponse{
			Status:            procurementResponseReject,
			Amount:            req.ProposedAmount,
			Currency:          strings.ToUpper(req.Currency),
			AvailableQuantity: 0,
			Reason:            err.Error(),
		})
	}

	switch taskType {
	case taskTypeProcurementQuoteRequest:
		return s.handleQuoteRequest(ctx, context)
	case taskTypeProcurementNegotiationCounter:
		return s.handleCounteroffer(ctx, context)
	case taskTypeProcurementNegotiationAccept:
		return s.handleAccept(ctx, context)
	case taskTypeProcurementNegotiationReject:
		return s.buildResponse(taskType, req, &SellerProcurementResponse{
			Status:            procurementResponseReject,
			Amount:            req.ProposedAmount,
			Currency:          strings.ToUpper(req.Currency),
			AvailableQuantity: maxInt(0, context.product.InventoryCount-context.product.ReservedInventoryCount),
			Reason:            "buyer ended the negotiation",
		})
	default:
		return a2a.Message{}, nil, fmt.Errorf("unsupported seller negotiation task type %s", taskType)
	}
}

func (s *SellerNegotiationService) parseRequest(metadata map[string]interface{}) (*SellerProcurementRequest, error) {
	payload, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal seller procurement metadata: %w", err)
	}

	var req SellerProcurementRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode seller procurement metadata: %w", err)
	}

	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	if req.Quantity <= 0 {
		return nil, fmt.Errorf("quantity must be greater than zero")
	}
	if strings.TrimSpace(req.SellerAgentID) == "" || strings.TrimSpace(req.ProductID) == "" {
		return nil, fmt.Errorf("seller agent and product are required")
	}
	if req.RoundNumber <= 0 {
		req.RoundNumber = 1
	}

	return &req, nil
}

func (s *SellerNegotiationService) loadContext(ctx context.Context, req *SellerProcurementRequest) (*sellerNegotiationContext, error) {
	if !req.DeadlineAt.IsZero() && time.Now().UTC().After(req.DeadlineAt.UTC()) {
		return nil, fmt.Errorf("request deadline exceeded")
	}

	seller, err := s.repo.GetAgentByID(ctx, req.SellerAgentID)
	if err != nil {
		return nil, fmt.Errorf("seller agent not found")
	}
	if NormalizeMarketplaceAgentType(seller.Type) != "merchant" || !seller.IsActive {
		return nil, fmt.Errorf("seller agent is not available")
	}

	product, err := s.repo.GetMarketplaceProductByID(ctx, req.ProductID)
	if err != nil {
		return nil, fmt.Errorf("seller product not found")
	}
	if product.AgentID != seller.ID {
		return nil, fmt.Errorf("product does not belong to seller agent")
	}

	registry, err := s.repo.GetAgentRegistry(ctx, seller.ID)
	if err != nil {
		return nil, fmt.Errorf("seller registry entry not found")
	}
	if !registry.IsActive || !registry.IsVerified {
		return nil, fmt.Errorf("seller registry entry is not eligible")
	}

	card, err := parseRegistryAgentCard(registry)
	if err != nil || !cardSupportsProcurement(card) {
		return nil, fmt.Errorf("seller registry card does not support procurement")
	}

	sellerConfig, err := s.ensureSellerConfig(ctx, seller)
	if err != nil {
		return nil, err
	}

	totalListPrice := roundCurrency(product.Price * float64(req.Quantity))
	hardFloor := roundCurrency(calculateSellerHardFloorFromConfig(product, sellerConfig.SellerConfig, req.Quantity))
	maxRounds := req.MaxRounds
	if maxRounds <= 0 {
		maxRounds = sellerConfig.SellerConfig.MaxRounds
	}
	if maxRounds <= 0 {
		maxRounds = 5
	}

	return &sellerNegotiationContext{
		request:        req,
		seller:         seller,
		product:        product,
		registry:       registry,
		sellerConfig:   sellerConfig,
		totalListPrice: totalListPrice,
		hardFloor:      hardFloor,
		maxRounds:      maxRounds,
	}, nil
}

func (s *SellerNegotiationService) ensureSellerConfig(ctx context.Context, seller *models.Agent) (*models.AgentConfig, error) {
	config, err := s.agentConfig.GetAgentConfig(ctx, seller.ID)
	if err == nil && config != nil && config.Config != nil && config.Config.SellerConfig != nil {
		return config.Config, nil
	}
	if err != nil && err != ErrAgentConfigNotFound {
		return nil, err
	}

	defaultConfig := s.agentConfig.CreateDefaultSellerConfig()
	saved, saveErr := s.agentConfig.SaveAgentConfig(ctx, seller, defaultConfig)
	if saveErr != nil {
		return nil, saveErr
	}
	return saved.Config, nil
}

func (s *SellerNegotiationService) handleQuoteRequest(ctx context.Context, negotiation *sellerNegotiationContext) (a2a.Message, *a2a.Artifact, error) {
	if response := s.validateAvailability(negotiation); response != nil {
		return s.buildResponse(taskTypeProcurementQuoteRequest, negotiation.request, response)
	}

	if negotiation.request.ProposedAmount >= negotiation.hardFloor {
		if err := s.repo.ReserveMarketplaceInventory(ctx, negotiation.product.ID, negotiation.request.Quantity); err != nil {
			return s.buildResponse(taskTypeProcurementQuoteRequest, negotiation.request, &SellerProcurementResponse{
				Status:            procurementResponseUnavailable,
				Amount:            negotiation.request.ProposedAmount,
				Currency:          negotiation.product.Currency,
				AvailableQuantity: maxInt(0, negotiation.product.InventoryCount-negotiation.product.ReservedInventoryCount),
				Reason:            "inventory reservation failed",
			})
		}
		return s.buildResponse(taskTypeProcurementQuoteRequest, negotiation.request, &SellerProcurementResponse{
			Status:            procurementResponseAccept,
			Amount:            roundCurrency(negotiation.request.ProposedAmount),
			Currency:          negotiation.product.Currency,
			AvailableQuantity: maxInt(0, negotiation.product.InventoryCount-negotiation.product.ReservedInventoryCount-negotiation.request.Quantity),
			ExpiresAt:         timePointer(time.Now().UTC().Add(5 * time.Minute)),
		})
	}

	quoteAmount := sellerCounterAmountForRound(negotiation.request.RoundNumber, negotiation.maxRounds, negotiation.totalListPrice, negotiation.hardFloor)
	return s.buildResponse(taskTypeProcurementQuoteRequest, negotiation.request, &SellerProcurementResponse{
		Status:            procurementResponseQuote,
		Amount:            quoteAmount,
		Currency:          negotiation.product.Currency,
		AvailableQuantity: maxInt(0, negotiation.product.InventoryCount-negotiation.product.ReservedInventoryCount),
		Reason:            "quoted based on seller floor and round policy",
		ExpiresAt:         timePointer(time.Now().UTC().Add(5 * time.Minute)),
	})
}

func (s *SellerNegotiationService) handleCounteroffer(ctx context.Context, negotiation *sellerNegotiationContext) (a2a.Message, *a2a.Artifact, error) {
	if response := s.validateAvailability(negotiation); response != nil {
		return s.buildResponse(taskTypeProcurementNegotiationCounter, negotiation.request, response)
	}

	if negotiation.request.ProposedAmount >= negotiation.hardFloor {
		if err := s.repo.ReserveMarketplaceInventory(ctx, negotiation.product.ID, negotiation.request.Quantity); err != nil {
			return s.buildResponse(taskTypeProcurementNegotiationCounter, negotiation.request, &SellerProcurementResponse{
				Status:            procurementResponseUnavailable,
				Amount:            negotiation.request.ProposedAmount,
				Currency:          negotiation.product.Currency,
				AvailableQuantity: maxInt(0, negotiation.product.InventoryCount-negotiation.product.ReservedInventoryCount),
				Reason:            "inventory reservation failed",
			})
		}
		return s.buildResponse(taskTypeProcurementNegotiationCounter, negotiation.request, &SellerProcurementResponse{
			Status:            procurementResponseAccept,
			Amount:            roundCurrency(negotiation.request.ProposedAmount),
			Currency:          negotiation.product.Currency,
			AvailableQuantity: maxInt(0, negotiation.product.InventoryCount-negotiation.product.ReservedInventoryCount-negotiation.request.Quantity),
			ExpiresAt:         timePointer(time.Now().UTC().Add(5 * time.Minute)),
		})
	}

	if negotiation.request.RoundNumber >= negotiation.maxRounds {
		return s.buildResponse(taskTypeProcurementNegotiationCounter, negotiation.request, &SellerProcurementResponse{
			Status:            procurementResponseReject,
			Amount:            roundCurrency(negotiation.request.ProposedAmount),
			Currency:          negotiation.product.Currency,
			AvailableQuantity: maxInt(0, negotiation.product.InventoryCount-negotiation.product.ReservedInventoryCount),
			Reason:            "seller max rounds reached",
		})
	}

	counteroffer := sellerCounterAmountForRound(negotiation.request.RoundNumber, negotiation.maxRounds, negotiation.totalListPrice, negotiation.hardFloor)
	return s.buildResponse(taskTypeProcurementNegotiationCounter, negotiation.request, &SellerProcurementResponse{
		Status:            procurementResponseCounteroffer,
		Amount:            counteroffer,
		Currency:          negotiation.product.Currency,
		AvailableQuantity: maxInt(0, negotiation.product.InventoryCount-negotiation.product.ReservedInventoryCount),
		Reason:            "seller counteroffer generated from negotiation policy",
		ExpiresAt:         timePointer(time.Now().UTC().Add(5 * time.Minute)),
	})
}

func (s *SellerNegotiationService) handleAccept(ctx context.Context, negotiation *sellerNegotiationContext) (a2a.Message, *a2a.Artifact, error) {
	if response := s.validateAvailability(negotiation); response != nil {
		return s.buildResponse(taskTypeProcurementNegotiationAccept, negotiation.request, response)
	}

	if negotiation.request.ProposedAmount < negotiation.hardFloor {
		return s.buildResponse(taskTypeProcurementNegotiationAccept, negotiation.request, &SellerProcurementResponse{
			Status:            procurementResponseReject,
			Amount:            roundCurrency(negotiation.request.ProposedAmount),
			Currency:          negotiation.product.Currency,
			AvailableQuantity: maxInt(0, negotiation.product.InventoryCount-negotiation.product.ReservedInventoryCount),
			Reason:            "accepted amount is below seller floor",
		})
	}

	if err := s.repo.ReserveMarketplaceInventory(ctx, negotiation.product.ID, negotiation.request.Quantity); err != nil {
		return s.buildResponse(taskTypeProcurementNegotiationAccept, negotiation.request, &SellerProcurementResponse{
			Status:            procurementResponseUnavailable,
			Amount:            roundCurrency(negotiation.request.ProposedAmount),
			Currency:          negotiation.product.Currency,
			AvailableQuantity: maxInt(0, negotiation.product.InventoryCount-negotiation.product.ReservedInventoryCount),
			Reason:            "inventory reservation failed",
		})
	}

	return s.buildResponse(taskTypeProcurementNegotiationAccept, negotiation.request, &SellerProcurementResponse{
		Status:            procurementResponseAccept,
		Amount:            roundCurrency(negotiation.request.ProposedAmount),
		Currency:          negotiation.product.Currency,
		AvailableQuantity: maxInt(0, negotiation.product.InventoryCount-negotiation.product.ReservedInventoryCount-negotiation.request.Quantity),
		ExpiresAt:         timePointer(time.Now().UTC().Add(5 * time.Minute)),
	})
}

func (s *SellerNegotiationService) validateAvailability(negotiation *sellerNegotiationContext) *SellerProcurementResponse {
	product := negotiation.product
	req := negotiation.request

	if !product.IsAvailable || product.InventoryCount-product.ReservedInventoryCount < req.Quantity {
		return &SellerProcurementResponse{
			Status:            procurementResponseUnavailable,
			Amount:            roundCurrency(req.ProposedAmount),
			Currency:          product.Currency,
			AvailableQuantity: maxInt(0, product.InventoryCount-product.ReservedInventoryCount),
			Reason:            "insufficient available inventory",
		}
	}
	if strings.ToUpper(product.Currency) != req.Currency {
		return &SellerProcurementResponse{
			Status:            procurementResponseReject,
			Amount:            roundCurrency(req.ProposedAmount),
			Currency:          product.Currency,
			AvailableQuantity: maxInt(0, product.InventoryCount-product.ReservedInventoryCount),
			Reason:            "seller does not support requested currency",
		}
	}

	sellerCfg := negotiation.sellerConfig.SellerConfig
	if sellerCfg == nil {
		return &SellerProcurementResponse{
			Status:            procurementResponseReject,
			Amount:            roundCurrency(req.ProposedAmount),
			Currency:          product.Currency,
			AvailableQuantity: maxInt(0, product.InventoryCount-product.ReservedInventoryCount),
			Reason:            "seller configuration is unavailable",
		}
	}
	if len(req.PaymentTerms) > 0 && !hasIntersection(req.PaymentTerms, sellerCfg.PaymentTerms) {
		return &SellerProcurementResponse{
			Status:            procurementResponseReject,
			Amount:            roundCurrency(req.ProposedAmount),
			Currency:          product.Currency,
			AvailableQuantity: maxInt(0, product.InventoryCount-product.ReservedInventoryCount),
			Reason:            "requested payment terms are unsupported by seller",
		}
	}

	return nil
}

func (s *SellerNegotiationService) buildResponse(taskType string, req *SellerProcurementRequest, response *SellerProcurementResponse) (a2a.Message, *a2a.Artifact, error) {
	if response == nil {
		return a2a.Message{}, nil, fmt.Errorf("seller procurement response is required")
	}

	payload := map[string]interface{}{
		"status":            response.Status,
		"amount":            roundCurrency(response.Amount),
		"currency":          strings.ToUpper(response.Currency),
		"availableQuantity": response.AvailableQuantity,
		"reason":            response.Reason,
		"taskType":          taskType,
		"procurementRunId":  req.ProcurementRunID,
		"candidateId":       req.CandidateID,
		"sellerAgentId":     req.SellerAgentID,
		"productId":         req.ProductID,
		"roundNumber":       req.RoundNumber,
	}
	if response.ExpiresAt != nil {
		payload["expiresAt"] = response.ExpiresAt.UTC().Format(time.RFC3339)
	}

	message := a2a.NewTextMessage(a2a.RoleAgent, sellerResponseMessage(response))
	artifact := a2a.NewDataArtifact("procurement-response", payload)
	return message, &artifact, nil
}

func sellerResponseMessage(response *SellerProcurementResponse) string {
	switch response.Status {
	case procurementResponseAccept:
		return fmt.Sprintf("Seller accepted at %.2f %s.", roundCurrency(response.Amount), strings.ToUpper(response.Currency))
	case procurementResponseQuote, procurementResponseCounteroffer:
		return fmt.Sprintf("Seller responded with %.2f %s.", roundCurrency(response.Amount), strings.ToUpper(response.Currency))
	case procurementResponseUnavailable:
		return "Seller cannot fulfill the requested quantity."
	default:
		if response.Reason != "" {
			return response.Reason
		}
		return "Seller rejected the procurement request."
	}
}

func sellerCounterAmountForRound(roundNumber, maxRounds int, listPrice, hardFloor float64) float64 {
	if maxRounds <= 1 {
		return roundCurrency(hardFloor)
	}
	progress := float64(roundNumber) / float64(maxRounds)
	counter := listPrice - (listPrice-hardFloor)*progress
	return roundCurrency(maxFloat(hardFloor, minFloat(listPrice, counter)))
}

func calculateSellerHardFloorFromConfig(product *models.MarketplaceProduct, seller *models.SellerConfig, quantity int) float64 {
	if seller == nil {
		return roundCurrency(product.Price * float64(quantity))
	}

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

func timePointer(value time.Time) *time.Time {
	return &value
}
