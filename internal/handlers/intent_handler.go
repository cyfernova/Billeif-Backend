package handlers

import (
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// IntentHandler handles intent processing endpoints
type IntentHandler struct {
	intentProcessing *services.IntentProcessingService
	log              *logger.Logger
}

// NewIntentHandler creates a new intent handler
func NewIntentHandler(intentProcessing *services.IntentProcessingService, log *logger.Logger) *IntentHandler {
	return &IntentHandler{
		intentProcessing: intentProcessing,
		log:              log,
	}
}

// ProcessIntent processes a natural language shopping intent
// POST /api/v1/intent/process
func (h *IntentHandler) ProcessIntent(c *gin.Context) {
	userID := c.GetString("user_id")

	var req struct {
		Intent     string `json:"intent" binding:"required"`
		MaxResults int    `json:"max_results,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.MaxResults == 0 {
		req.MaxResults = 20
	}

	// Process the intent
	processReq := &services.ProcessIntentRequest{
		NaturalLanguage: req.Intent,
		UserID:          userID,
		MaxResults:      req.MaxResults,
	}

	result := h.intentProcessing.ProcessIntent(c.Request.Context(), processReq)

	if !result.Success {
		h.log.Warn("intent processing failed", "user_id", userID, "error", result.Error)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": result.Error,
		})
		return
	}

	h.log.Info("intent processed successfully",
		"user_id", userID,
		"confidence", result.ParseResult.Confidence,
		"matched_products", result.MatchResults.TotalMatched)

	c.JSON(http.StatusOK, gin.H{
		"parse_result": result.ParseResult,
		"match_results": gin.H{
			"total_matched":  result.MatchResults.TotalMatched,
			"total_searched": result.MatchResults.TotalSearched,
			"products":       result.MatchResults.Products,
		},
	})
}

// ValidateIntent validates if products exist for the given intent
// POST /api/v1/intent/validate
func (h *IntentHandler) ValidateIntent(c *gin.Context) {
	userID := c.GetString("user_id")

	var req struct {
		Intent string `json:"intent" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Parse the intent
	parseReq := &services.ProcessIntentRequest{
		NaturalLanguage: req.Intent,
		UserID:          userID,
		MaxResults:      1, // Just check if any products exist
	}

	result := h.intentProcessing.ProcessIntent(c.Request.Context(), parseReq)

	c.JSON(http.StatusOK, gin.H{
		"valid":            result.Success,
		"confidence":       result.ParseResult.Confidence,
		"matched_products": result.MatchResults.TotalMatched,
		"error":            result.Error,
	})
}

// ParseIntent only parses the intent without searching for products
// POST /api/v1/intent/parse
func (h *IntentHandler) ParseIntent(c *gin.Context) {
	userID := c.GetString("user_id")

	var req struct {
		Intent string `json:"intent" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Parse the intent using ParseIntentOnly
	parseResult, err := h.intentProcessing.ParseIntentOnly(c.Request.Context(), req.Intent)
	if err != nil {
		h.log.Error("intent parsing failed", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to parse intent",
		})
		return
	}

	h.log.Info("intent parsed",
		"user_id", userID,
		"confidence", parseResult.Confidence,
		"categories", parseResult.Intent.Categories,
		"keywords", parseResult.Intent.Keywords)

	c.JSON(http.StatusOK, parseResult)
}
