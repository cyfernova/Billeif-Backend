package handlers

import (
	"net/http"

	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type ProcurementHandler struct {
	svc     *services.ProcurementService
	ap2Repo interfaces.AP2Repository
	log     *logger.Logger
}

func NewProcurementHandler(svc *services.ProcurementService, ap2Repo interfaces.AP2Repository, log *logger.Logger) *ProcurementHandler {
	return &ProcurementHandler{
		svc:     svc,
		ap2Repo: ap2Repo,
		log:     log,
	}
}

type CreateProcurementRunRequest struct {
	Intent       string   `json:"intent" binding:"required"`
	Quantity     int      `json:"quantity" binding:"omitempty,gte=1"`
	MaxBudget    float64  `json:"max_budget" binding:"required,gt=0"`
	Currency     string   `json:"currency" binding:"omitempty,len=3"`
	PaymentTerms []string `json:"payment_terms"`
	AutoBuy      *bool    `json:"auto_buy"`
	MaxSellers   int      `json:"max_sellers" binding:"omitempty,gte=1,lte=20"`
	MaxRounds    int      `json:"max_rounds" binding:"omitempty,gte=1,lte=20"`
}

func (h *ProcurementHandler) StartAgent(c *gin.Context) {
	h.startProcurement(c)
}

func (h *ProcurementHandler) CreateProcurementRun(c *gin.Context) {
	h.startProcurement(c)
}

// startProcurement starts a procurement run
// @Summary Start procurement run
// @Description Starts a procurement run for a shopping agent
// @Tags Procurement
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Shopping Agent ID"
// @Param input body CreateProcurementRunRequest true "Procurement details"
// @Success 201 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/{id}/procurement-runs [post]
func (h *ProcurementHandler) startProcurement(c *gin.Context) {
	shoppingAgentID := c.Param("id")
	userID := c.GetString("user_id")

	agent, ok := requireOwnedAgent(c, h.ap2Repo, shoppingAgentID)
	if !ok {
		return
	}
	if services.NormalizeMarketplaceAgentType(agent.Type) != "shopping" {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	var req CreateProcurementRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	autoBuy := true
	if req.AutoBuy != nil {
		autoBuy = *req.AutoBuy
	}

	run, err := h.svc.StartProcurement(c.Request.Context(), &services.CreateProcurementRunRequest{
		UserID:          userID,
		ShoppingAgentID: shoppingAgentID,
		Intent:          req.Intent,
		Quantity:        req.Quantity,
		MaxBudget:       req.MaxBudget,
		Currency:        req.Currency,
		PaymentTerms:    req.PaymentTerms,
		AutoBuy:         autoBuy,
		MaxSellers:      req.MaxSellers,
		MaxRounds:       req.MaxRounds,
		IdempotencyKey:  c.GetHeader("Idempotency-Key"),
	})
	if err != nil {
		h.log.Error("failed to create procurement run", "shopping_agent_id", shoppingAgentID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, run)
}

// GetProcurementRun retrieves a procurement run
// @Summary Get procurement run
// @Description Returns a procurement run by ID
// @Tags Procurement
// @Produce json
// @Security BearerAuth
// @Param id path string true "Shopping Agent ID"
// @Param run_id path string true "Procurement Run ID"
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/{id}/procurement-runs/{run_id} [get]
func (h *ProcurementHandler) GetProcurementRun(c *gin.Context) {
	shoppingAgentID := c.Param("id")
	runID := c.Param("run_id")
	userID := c.GetString("user_id")

	if _, ok := requireOwnedAgent(c, h.ap2Repo, shoppingAgentID); !ok {
		return
	}

	run, err := h.svc.GetProcurementRun(c.Request.Context(), userID, shoppingAgentID, runID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "procurement run not found"})
		return
	}

	c.JSON(http.StatusOK, run)
}

// CancelProcurementRun cancels a procurement run
// @Summary Cancel procurement run
// @Description Cancels a procurement run by ID
// @Tags Procurement
// @Produce json
// @Security BearerAuth
// @Param id path string true "Shopping Agent ID"
// @Param run_id path string true "Procurement Run ID"
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/{id}/procurement-runs/{run_id}/cancel [post]
func (h *ProcurementHandler) CancelProcurementRun(c *gin.Context) {
	shoppingAgentID := c.Param("id")
	runID := c.Param("run_id")
	userID := c.GetString("user_id")

	if _, ok := requireOwnedAgent(c, h.ap2Repo, shoppingAgentID); !ok {
		return
	}

	run, err := h.svc.CancelProcurement(c.Request.Context(), userID, shoppingAgentID, runID)
	if err != nil {
		h.log.Error("failed to cancel procurement run", "shopping_agent_id", shoppingAgentID, "run_id", runID, "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "procurement run not found"})
		return
	}

	c.JSON(http.StatusOK, run)
}
