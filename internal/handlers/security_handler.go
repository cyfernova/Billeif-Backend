package handlers

import (
	"net/http"
	"strings"
	"time"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/models"
	"invoice-backend/internal/services"

	"github.com/gin-gonic/gin"
)

type SecurityHandler struct {
	service *services.SecurityService
	auth    *services.AuthService
	uploads *services.PendingUploadService
	privacy *services.PrivacyService
}

func NewSecurityHandler(service *services.SecurityService, auth *services.AuthService, uploads *services.PendingUploadService, privacy *services.PrivacyService) *SecurityHandler {
	return &SecurityHandler{service: service, auth: auth, uploads: uploads, privacy: privacy}
}

// RequestPrivacyExport creates a durable export request.
// @Summary Request privacy export
// @Tags Privacy
// @Security BearerAuth
// @Param Idempotency-Key header string true "Idempotency key"
// @Success 202 {object} models.PrivacyRequest
// @Failure 400 {object} map[string]string
// @Router /privacy/exports [post]
func (h *SecurityHandler) RequestPrivacyExport(c *gin.Context) { h.requestPrivacy(c, "export") }

// RequestPrivacyDeletion creates a retention-held deletion request.
// @Summary Request privacy deletion
// @Tags Privacy
// @Security BearerAuth
// @Param Idempotency-Key header string true "Idempotency key"
// @Success 202 {object} models.PrivacyRequest
// @Failure 400 {object} map[string]string
// @Router /privacy/deletions [post]
func (h *SecurityHandler) RequestPrivacyDeletion(c *gin.Context) { h.requestPrivacy(c, "delete") }

func (h *SecurityHandler) requestPrivacy(c *gin.Context, kind string) {
	if h == nil || h.privacy == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "privacy workflow unavailable"})
		return
	}
	request, err := h.privacy.Request(c.Request.Context(), services.PrivacyRequestInput{
		BusinessID: middleware.GetBusinessID(c), Subject: middleware.GetUserID(c), Kind: kind,
		IdempotencyKey: strings.TrimSpace(c.GetHeader("Idempotency-Key")),
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid privacy request"})
		return
	}
	c.JSON(http.StatusAccepted, request)
}

// GetPrivacyRequest returns a tenant-and-subject scoped request.
// @Summary Get privacy request
// @Tags Privacy
// @Security BearerAuth
// @Param request_id path string true "Privacy request ID"
// @Success 200 {object} models.PrivacyRequest
// @Failure 404 {object} map[string]string
// @Router /privacy/requests/{request_id} [get]
func (h *SecurityHandler) GetPrivacyRequest(c *gin.Context) {
	request, err := h.privacy.Get(c.Request.Context(), c.Param("request_id"), middleware.GetBusinessID(c), middleware.GetUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "privacy request not found"})
		return
	}
	c.JSON(http.StatusOK, request)
}

// DownloadPrivacyExport returns a short-lived URL for a completed export.
// @Summary Download privacy export
// @Tags Privacy
// @Security BearerAuth
// @Param request_id path string true "Privacy request ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Router /privacy/requests/{request_id}/download [get]
func (h *SecurityHandler) DownloadPrivacyExport(c *gin.Context) {
	url, err := h.privacy.ExportDownload(c.Request.Context(), c.Param("request_id"), middleware.GetBusinessID(c), middleware.GetUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "privacy export not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"download_url": url, "expires_in_seconds": 300})
}

type ProcessPrivacyInput struct {
	BusinessID      string `json:"business_id" binding:"required"`
	Subject         string `json:"subject" binding:"required"`
	Kind            string `json:"kind" binding:"required,oneof=export delete"`
	CommandIdentity string `json:"command_identity" binding:"required"`
}

// ProcessPrivacyRequest executes one operator-approved durable request.
// @Summary Process privacy request
// @Tags Operator
// @Security BearerAuth
// @Param request_id path string true "Privacy request ID"
// @Param X-Step-Up-Token header string true "Scoped one-time step-up token"
// @Param input body ProcessPrivacyInput true "Processing scope"
// @Success 200 {object} map[string]interface{}
// @Failure 412 {object} map[string]string
// @Router /operator/privacy/requests/{request_id}/process [post]
func (h *SecurityHandler) ProcessPrivacyRequest(c *gin.Context) {
	var input ProcessPrivacyInput
	if h == nil || h.service == nil || h.privacy == nil || c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid privacy processing request"})
		return
	}
	requestID := strings.TrimSpace(c.Param("request_id"))
	if err := h.service.ConsumeStepUp(c.Request.Context(), services.StepUpConsumeRequest{
		Subject: middleware.GetUserID(c), BusinessID: input.BusinessID, Action: "privacy_process", Resource: requestID,
		CommandIdentity: input.CommandIdentity, Token: strings.TrimSpace(c.GetHeader("X-Step-Up-Token")),
	}); err != nil {
		c.JSON(http.StatusPreconditionRequired, gin.H{"error": "scoped step-up is required", "code": "step_up_required"})
		return
	}
	var request any
	var err error
	if input.Kind == models.PrivacyRequestExport {
		request, err = h.privacy.ProcessExport(c.Request.Context(), requestID, input.BusinessID, input.Subject)
	} else {
		request, err = h.privacy.PurgeDeletion(c.Request.Context(), requestID, input.BusinessID, input.Subject)
	}
	if request == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "privacy request could not be processed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"request": request, "reconciliation_required": err != nil})
}

type PendingUploadInput struct {
	Kind           string `json:"kind" binding:"required"`
	ContentType    string `json:"content_type" binding:"required"`
	SizeBytes      int64  `json:"size_bytes" binding:"required"`
	ChecksumSHA256 string `json:"checksum_sha256" binding:"required"`
}

// CreatePendingUpload creates a tenant-bound quarantine upload.
// @Summary Create pending upload
// @Tags Security
// @Security BearerAuth
// @Param input body PendingUploadInput true "Pending object metadata"
// @Success 201 {object} services.PendingUploadCreated
// @Failure 400 {object} map[string]string
// @Router /security/uploads [post]
func (h *SecurityHandler) CreatePendingUpload(c *gin.Context) {
	var input PendingUploadInput
	if h == nil || h.uploads == nil || c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pending upload"})
		return
	}
	created, err := h.uploads.Create(c.Request.Context(), services.PendingUploadCreateInput{
		BusinessID: middleware.GetBusinessID(c), UploaderID: middleware.GetUserID(c), Kind: input.Kind,
		ContentType: input.ContentType, SizeBytes: input.SizeBytes, ChecksumSHA256: input.ChecksumSHA256,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pending upload"})
		return
	}
	c.JSON(http.StatusCreated, created)
}

// CompletePendingUpload verifies metadata and scans the object.
// @Summary Complete pending upload
// @Tags Security
// @Security BearerAuth
// @Param upload_id path string true "Upload ID"
// @Success 200 {object} models.PendingUpload
// @Failure 409 {object} map[string]string
// @Router /security/uploads/{upload_id}/complete [post]
func (h *SecurityHandler) CompletePendingUpload(c *gin.Context) {
	upload, err := h.uploads.Complete(c.Request.Context(), c.Param("upload_id"), middleware.GetBusinessID(c), middleware.GetUserID(c))
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "pending upload could not be completed"})
		return
	}
	c.JSON(http.StatusOK, upload)
}

// DownloadPendingUpload returns a URL only for clean objects.
// @Summary Download verified upload
// @Tags Security
// @Security BearerAuth
// @Param upload_id path string true "Upload ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Router /security/uploads/{upload_id}/download [get]
func (h *SecurityHandler) DownloadPendingUpload(c *gin.Context) {
	value, err := h.uploads.Download(c.Request.Context(), c.Param("upload_id"), middleware.GetBusinessID(c), middleware.GetUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pending upload not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"download_url": value, "expires_in_seconds": 300})
}

// CleanupPendingUploads deletes a bounded batch of expired quarantine objects.
// @Summary Cleanup expired pending uploads
// @Tags Operator
// @Security BearerAuth
// @Success 200 {object} services.PendingUploadCleanupResult
// @Failure 409 {object} services.PendingUploadCleanupResult
// @Router /operator/security/uploads/cleanup [post]
func (h *SecurityHandler) CleanupPendingUploads(c *gin.Context) {
	if h == nil || h.uploads == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "upload cleanup unavailable"})
		return
	}
	result, err := h.uploads.CleanupExpired(c.Request.Context(), 100)
	if err != nil {
		c.JSON(http.StatusConflict, result)
		return
	}
	c.JSON(http.StatusOK, result)
}

type IssueStepUpInput struct {
	BusinessID      string `json:"business_id"`
	Action          string `json:"action" binding:"required"`
	Resource        string `json:"resource" binding:"required"`
	CommandIdentity string `json:"command_identity" binding:"required"`
	TTLSeconds      int    `json:"ttl_seconds"`
	MFASession      string `json:"mfa_session" binding:"required"`
	MFACode         string `json:"mfa_code" binding:"required,len=6"`
	MFAUsername     string `json:"mfa_username" binding:"required"`
}

// IssueStepUp exchanges a recent Cognito-authenticated TOTP assurance for one
// narrowly scoped, single-use backend grant.
// @Summary Issue scoped step-up grant
// @Tags Authentication
// @Security BearerAuth
// @Param input body IssueStepUpInput true "MFA challenge and command scope"
// @Success 201 {object} services.StepUpIssued
// @Failure 428 {object} map[string]string
// @Router /auth/step-up [post]
func (h *SecurityHandler) IssueStepUp(c *gin.Context) {
	h.issueStepUp(c, false)
}

// IssueOperatorStepUp issues an operator-scoped grant for one tenant command.
// @Summary Issue operator step-up grant
// @Tags Operator
// @Security BearerAuth
// @Param input body IssueStepUpInput true "MFA challenge and command scope"
// @Success 201 {object} services.StepUpIssued
// @Failure 428 {object} map[string]string
// @Router /operator/step-up [post]
func (h *SecurityHandler) IssueOperatorStepUp(c *gin.Context) {
	h.issueStepUp(c, true)
}

func (h *SecurityHandler) issueStepUp(c *gin.Context, operator bool) {
	var input IssueStepUpInput
	if h == nil || h.service == nil || h.auth == nil || c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid step-up request"})
		return
	}
	mfaResult, err := h.auth.CompleteLoginMFA(c.Request.Context(), services.LoginMFAInput{
		Username: input.MFAUsername, Session: input.MFASession, Code: input.MFACode,
	})
	verifiedSubject, subjectErr := "", error(nil)
	if err == nil {
		verifiedSubject, subjectErr = services.CognitoAccessTokenSubject(mfaResult.AccessToken)
	}
	if err != nil || subjectErr != nil || verifiedSubject != middleware.GetUserID(c) {
		c.JSON(http.StatusPreconditionRequired, gin.H{"error": "recent TOTP assurance is required", "code": "totp_assurance_required"})
		return
	}
	authenticatedAt := time.Now().UTC()
	ttl := time.Duration(input.TTLSeconds) * time.Second
	if ttl == 0 {
		ttl = 5 * time.Minute
	}
	businessID := middleware.GetBusinessID(c)
	if operator {
		businessID = strings.TrimSpace(input.BusinessID)
	}
	issued, err := h.service.IssueStepUp(c.Request.Context(), services.StepUpIssueRequest{
		Subject: middleware.GetUserID(c), BusinessID: businessID,
		Action: input.Action, Resource: input.Resource, CommandIdentity: input.CommandIdentity, Assurance: services.AssuranceTOTP,
		AuthenticatedAt: authenticatedAt, TTL: ttl,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid step-up request"})
		return
	}
	c.JSON(http.StatusCreated, issued)
}
