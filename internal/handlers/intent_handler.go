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

// ProcessIntentRequest represents process intent request
type ProcessIntentRequest struct {
	Intent     string `json:"intent" binding:"required"`
	MaxResults int    `json:"max_results,omitempty"`
}

// ValidateIntentRequest represents validate intent request
type ValidateIntentRequest struct {
	Intent string `json:"intent" binding:"required"`
}

// ParseIntentRequest represents parse intent request
type ParseIntentRequest struct {
	Intent string `json:"intent" binding:"required"`
}

// ProcessIntent processes a natural language shopping intent
// @Summary Process shopping intent
// @Description Processes a natural language shopping intent and returns matched products
// @Tags Intent
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body ProcessIntentRequest true "Intent details"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Router /intent/process [post]
func (h *IntentHandler) ProcessIntent(c *gin.Context) {
	userID := c.GetString("user_id")

	var req ProcessIntentRequest
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
// @Summary Validate shopping intent
// @Description Validates if products exist for the given natural language intent
// @Tags Intent
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body ValidateIntentRequest true "Intent to validate"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Router /intent/validate [post]
func (h *IntentHandler) ValidateIntent(c *gin.Context) {
	userID := c.GetString("user_id")

	var req ValidateIntentRequest
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
	if !result.Success || result.ParseResult == nil || result.MatchResults == nil {
		c.JSON(http.StatusBadRequest, gin.H{"valid": false, "error": result.Error})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"valid":            result.Success,
		"confidence":       result.ParseResult.Confidence,
		"matched_products": result.MatchResults.TotalMatched,
		"error":            result.Error,
	})
}

// ParseIntent only parses the intent without searching for products
// @Summary Parse shopping intent
// @Description Parses a natural language intent without searching for products
// @Tags Intent
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body ParseIntentRequest true "Intent to parse"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /intent/parse [post]
func (h *IntentHandler) ParseIntent(c *gin.Context) {
	userID := c.GetString("user_id")

	var req ParseIntentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Parse the intent using ParseIntentOnly
	parseResult, err := h.intentProcessing.ParseIntentOnly(c.Request.Context(), req.Intent)
	if err != nil || parseResult == nil || !parseResult.Parsed || parseResult.Intent == nil {
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
