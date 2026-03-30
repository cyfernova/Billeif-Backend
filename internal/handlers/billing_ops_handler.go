package handlers

import (
	"io"
	"net/http"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type BillingOpsHandler struct {
	svc *services.BillingOpsService
	log *logger.Logger
}

func NewBillingOpsHandler(svc *services.BillingOpsService, log *logger.Logger) *BillingOpsHandler {
	return &BillingOpsHandler{svc: svc, log: log}
}

// ListPriceLists returns all price lists for a business
// @Summary List price lists
// @Description Returns all price lists for the business
// @Tags Billing Ops
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /price-lists [get]
func (h *BillingOpsHandler) ListPriceLists(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page, limit := utils.ParsePagination(c)
	rows, total, err := h.svc.ListPriceLists(c.Request.Context(), businessID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total, "page": page, "limit": limit})
}

func (h *BillingOpsHandler) GetPriceList(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	detail, err := h.svc.GetPriceList(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "price list not found"})
		return
	}
	c.JSON(http.StatusOK, detail)
}

// CreatePriceList creates a new price list
// @Summary Create price list
// @Description Creates a new price list for the business
// @Tags Billing Ops
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.CreatePriceListInput true "Price list details"
// @Success 201 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /price-lists [post]
func (h *BillingOpsHandler) CreatePriceList(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreatePriceListInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID
	requestContextWithActor(c)
	detail, err := h.svc.CreatePriceList(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, detail)
}

func (h *BillingOpsHandler) UpdatePriceList(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpdatePriceListInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	requestContextWithActor(c)
	detail, err := h.svc.UpdatePriceList(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (h *BillingOpsHandler) DeletePriceList(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	requestContextWithActor(c)
	if err := h.svc.DeletePriceList(c.Request.Context(), businessID, c.Param("id")); err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

func (h *BillingOpsHandler) ListPartyGroups(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page, limit := utils.ParsePagination(c)
	rows, total, err := h.svc.ListPartyGroups(c.Request.Context(), businessID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total, "page": page, "limit": limit})
}

func (h *BillingOpsHandler) GetPartyGroup(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	group, err := h.svc.GetPartyGroup(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "party group not found"})
		return
	}
	c.JSON(http.StatusOK, group)
}

func (h *BillingOpsHandler) GetPartyGroupLedger(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	summary, err := h.svc.GetPartyGroupLedger(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, summary)
}

func (h *BillingOpsHandler) CreatePartyGroup(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreatePartyGroupInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID
	requestContextWithActor(c)
	group, err := h.svc.CreatePartyGroup(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, group)
}

func (h *BillingOpsHandler) UpdatePartyGroup(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpdatePartyGroupInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	requestContextWithActor(c)
	group, err := h.svc.UpdatePartyGroup(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, group)
}

func (h *BillingOpsHandler) DeletePartyGroup(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	requestContextWithActor(c)
	if err := h.svc.DeletePartyGroup(c.Request.Context(), businessID, c.Param("id")); err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// ListActivityLogs returns activity logs for a business
// @Summary List activity logs
// @Description Returns activity logs for the business
// @Tags Billing Ops
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /activity-logs [get]
func (h *BillingOpsHandler) ListActivityLogs(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page, limit := utils.ParsePagination(c)
	rows, total, err := h.svc.ListActivityLogs(c.Request.Context(), services.ActivityLogFilter{
		BusinessID: businessID,
		EntityType: c.Query("entity_type"),
		EntityID:   c.Query("entity_id"),
		Action:     c.Query("action"),
		Page:       page,
		Limit:      limit,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total, "page": page, "limit": limit})
}

func (h *BillingOpsHandler) ListSignatureProfiles(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	rows, err := h.svc.ListSignatureProfiles(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func (h *BillingOpsHandler) GetSignatureProfile(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	profile, err := h.svc.GetSignatureProfile(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "signature profile not found"})
		return
	}
	c.JSON(http.StatusOK, profile)
}

func (h *BillingOpsHandler) CreateSignatureProfile(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	fh, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer fh.Close()
	content, err := io.ReadAll(fh)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	requestContextWithActor(c)
	profile, err := h.svc.CreateSignatureProfile(c.Request.Context(), services.CreateSignatureProfileInput{
		BusinessID:    businessID,
		Name:          c.PostForm("name"),
		Provider:      c.PostForm("provider"),
		SignerName:    c.PostForm("signer_name"),
		CertificateSN: c.PostForm("certificate_sn"),
		FileName:      file.Filename,
		FileContent:   content,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, profile)
}

func (h *BillingOpsHandler) SignInvoice(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var body struct {
		SignatureProfileID string `json:"signature_profile_id" binding:"required"`
		Passphrase         string `json:"passphrase"`
		Reason             string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	requestContextWithActor(c)
	artifact, err := h.svc.SignInvoice(c.Request.Context(), businessID, c.Param("id"), body.SignatureProfileID, body.Passphrase, body.Reason)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, artifact)
}

func (h *BillingOpsHandler) SignDocument(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var body struct {
		SignatureProfileID string `json:"signature_profile_id" binding:"required"`
		Passphrase         string `json:"passphrase"`
		Reason             string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	requestContextWithActor(c)
	artifact, err := h.svc.SignDocument(c.Request.Context(), businessID, c.Param("id"), body.SignatureProfileID, body.Passphrase, body.Reason)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, artifact)
}

func (h *BillingOpsHandler) ListBulkJobs(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page, limit := utils.ParsePagination(c)
	rows, total, err := h.svc.ListBulkJobs(c.Request.Context(), businessID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total, "page": page, "limit": limit})
}

func (h *BillingOpsHandler) GetBulkJob(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	job, err := h.svc.GetBulkJob(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bulk job not found"})
		return
	}
	c.JSON(http.StatusOK, job)
}

func (h *BillingOpsHandler) createImportJob(c *gin.Context, jobType string) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	fh, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer fh.Close()
	content, err := io.ReadAll(fh)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	requestContextWithActor(c)
	job, err := h.svc.CreateBulkJob(c.Request.Context(), services.CreateBulkJobInput{
		BusinessID:  businessID,
		CreatedBy:   userID,
		JobType:     jobType,
		FileName:    file.Filename,
		ContentType: file.Header.Get("Content-Type"),
		FileContent: content,
		RequestPayload: map[string]interface{}{
			"source": "imports_api",
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (h *BillingOpsHandler) CreateCustomerImportJob(c *gin.Context) {
	h.createImportJob(c, models.BulkJobTypeImportCustomers)
}
func (h *BillingOpsHandler) CreateVendorImportJob(c *gin.Context) {
	h.createImportJob(c, models.BulkJobTypeImportVendors)
}
func (h *BillingOpsHandler) CreateProductImportJob(c *gin.Context) {
	h.createImportJob(c, models.BulkJobTypeImportProducts)
}
func (h *BillingOpsHandler) CreateInvoiceImportJob(c *gin.Context) {
	h.createImportJob(c, models.BulkJobTypeImportInvoices)
}
func (h *BillingOpsHandler) CreateDocumentImportJob(c *gin.Context) {
	h.createImportJob(c, models.BulkJobTypeImportDocuments)
}

func (h *BillingOpsHandler) CreateInvoiceBulkAction(c *gin.Context) {
	h.createBulkAction(c, models.BulkJobTypeBulkInvoices)
}

func (h *BillingOpsHandler) CreateDocumentBulkAction(c *gin.Context) {
	h.createBulkAction(c, models.BulkJobTypeBulkDocuments)
}

func (h *BillingOpsHandler) createBulkAction(c *gin.Context, jobType string) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var body struct {
		Action             string                 `json:"action" binding:"required"`
		EntityIDs          []string               `json:"entity_ids" binding:"required,min=1"`
		RequestPayload     map[string]interface{} `json:"request_payload,omitempty"`
		SignatureProfileID string                 `json:"signature_profile_id,omitempty"`
		Reason             string                 `json:"reason,omitempty"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	payload := body.RequestPayload
	if payload == nil {
		payload = map[string]interface{}{}
	}
	if body.SignatureProfileID != "" {
		payload["signature_profile_id"] = body.SignatureProfileID
	}
	if body.Reason != "" {
		payload["reason"] = body.Reason
	}
	requestContextWithActor(c)
	job, err := h.svc.CreateBulkJob(c.Request.Context(), services.CreateBulkJobInput{
		BusinessID:     businessID,
		CreatedBy:      userID,
		JobType:        jobType,
		Action:         body.Action,
		EntityIDs:      body.EntityIDs,
		RequestPayload: payload,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (h *BillingOpsHandler) ListInvoiceSubscriptions(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page, limit := utils.ParsePagination(c)
	rows, total, err := h.svc.ListInvoiceSubscriptions(c.Request.Context(), businessID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total, "page": page, "limit": limit})
}

func (h *BillingOpsHandler) GetInvoiceSubscription(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	subscription, err := h.svc.GetInvoiceSubscription(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "invoice subscription not found"})
		return
	}
	c.JSON(http.StatusOK, subscription)
}

func (h *BillingOpsHandler) CreateInvoiceSubscription(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateInvoiceSubscriptionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID
	requestContextWithActor(c)
	subscription, err := h.svc.CreateInvoiceSubscription(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, subscription)
}

func (h *BillingOpsHandler) UpdateInvoiceSubscription(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpdateInvoiceSubscriptionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	requestContextWithActor(c)
	subscription, err := h.svc.UpdateInvoiceSubscription(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, subscription)
}

func (h *BillingOpsHandler) PauseInvoiceSubscription(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	requestContextWithActor(c)
	subscription, err := h.svc.PauseInvoiceSubscription(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, subscription)
}

func (h *BillingOpsHandler) ResumeInvoiceSubscription(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	requestContextWithActor(c)
	subscription, err := h.svc.ResumeInvoiceSubscription(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, subscription)
}

func (h *BillingOpsHandler) GenerateInvoiceSubscriptionNow(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	requestContextWithActor(c)
	run, err := h.svc.GenerateInvoiceSubscriptionNow(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, run)
}

func (h *BillingOpsHandler) ListInvoiceSubscriptionRuns(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page, limit := utils.ParsePagination(c)
	rows, total, err := h.svc.ListInvoiceSubscriptionRuns(c.Request.Context(), businessID, c.Param("id"), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total, "page": page, "limit": limit})
}
