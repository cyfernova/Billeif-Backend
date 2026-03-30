package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/skip2/go-qrcode"
	"gorm.io/gorm"
)

const (
	eInvoiceBackdateWindow = 30 * 24 * time.Hour
	eInvoiceCancelWindow   = 24 * time.Hour
	eWayBillBackdateWindow = 180 * 24 * time.Hour
)

type UpsertGSTIntegrationAccountInput struct {
	Provider       string                           `json:"provider"`
	ServiceType    string                           `json:"service_type" binding:"required"`
	GSPName        string                           `json:"gsp_name,omitempty"`
	PortalUsername string                           `json:"portal_username,omitempty"`
	Credentials    GSTIntegrationAccountCredentials `json:"credentials"`
	Metadata       map[string]interface{}           `json:"metadata,omitempty"`
}

type GenerateEInvoiceInput struct {
	Source string `json:"source,omitempty"`
}

type CancelEInvoiceInput struct {
	Reason string `json:"reason" binding:"required"`
	Source string `json:"source,omitempty"`
}

type GenerateEWayBillInput struct {
	Source       string                 `json:"source,omitempty"`
	DispatchFrom map[string]interface{} `json:"dispatch_from,omitempty"`
	DispatchTo   map[string]interface{} `json:"dispatch_to,omitempty"`
	DistanceKM   float64                `json:"distance_km,omitempty"`
	Transporter  map[string]interface{} `json:"transporter,omitempty"`
	Vehicle      map[string]interface{} `json:"vehicle,omitempty"`
}

type UpdateEWayPartBInput struct {
	Source      string                 `json:"source,omitempty"`
	Transporter map[string]interface{} `json:"transporter,omitempty"`
	Vehicle     map[string]interface{} `json:"vehicle,omitempty"`
	ReasonCode  string                 `json:"reason_code,omitempty"`
}

type MultiVehicleInput struct {
	Source         string                 `json:"source,omitempty"`
	MovementType   string                 `json:"movement_type,omitempty"`
	VehicleNo      string                 `json:"vehicle_no,omitempty"`
	TransportDocNo string                 `json:"transport_doc_no,omitempty"`
	FromPlace      string                 `json:"from_place,omitempty"`
	FromState      string                 `json:"from_state,omitempty"`
	ReasonCode     string                 `json:"reason_code,omitempty"`
	Payload        map[string]interface{} `json:"payload,omitempty"`
}

type ComplianceStatusResponse struct {
	ComplianceStatus   string                   `json:"compliance_status"`
	PortalStatus       string                   `json:"portal_status,omitempty"`
	RetryCount         int                      `json:"retry_count"`
	IRN                string                   `json:"irn,omitempty"`
	AckNumber          string                   `json:"ack_number,omitempty"`
	AckDate            *time.Time               `json:"ack_date,omitempty"`
	QRCodeURL          string                   `json:"qr_code_url,omitempty"`
	EWayBillNumber     string                   `json:"eway_bill_number,omitempty"`
	EWayBillValidUntil *time.Time               `json:"eway_bill_valid_until,omitempty"`
	LastError          string                   `json:"last_error,omitempty"`
	Job                *models.GSTSubmissionJob `json:"job,omitempty"`
	EInvoice           *models.EInvoiceRecord   `json:"einvoice,omitempty"`
	EWayBill           *models.EWayBillRecord   `json:"ewaybill,omitempty"`
}

type gstQueueMessage struct {
	JobID string `json:"job_id"`
}

func (s *TaxComplianceService) EnqueueDocumentCompliance(ctx context.Context, document *models.Document, source string) error {
	if document == nil || document.ID == "" {
		return nil
	}
	if document.GenerateEInvoice {
		if _, err := s.GenerateEInvoiceByDocument(ctx, document.BusinessID, document.ID, defaultIdempotencyKey(document.ID, models.GSTOperationGenerateEInvoice, source), GenerateEInvoiceInput{Source: source}); err != nil {
			return err
		}
	}
	if document.GenerateEWayBill {
		if _, err := s.GenerateEWayBillByDocument(ctx, document.BusinessID, document.ID, defaultIdempotencyKey(document.ID, models.GSTOperationGenerateEWayBill, source), GenerateEWayBillInput{Source: source}); err != nil {
			return err
		}
	}
	return nil
}

func (s *TaxComplianceService) ListIntegrationAccounts(ctx context.Context, businessID string) ([]*models.GSTIntegrationAccount, error) {
	var rows []models.GSTIntegrationAccount
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Order("updated_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*models.GSTIntegrationAccount, 0, len(rows))
	for i := range rows {
		row := rows[i]
		result = append(result, &row)
	}
	return result, nil
}

func (s *TaxComplianceService) UpsertIntegrationAccount(ctx context.Context, businessID, id string, input UpsertGSTIntegrationAccountInput) (*models.GSTIntegrationAccount, error) {
	account := &models.GSTIntegrationAccount{
		BusinessID:     businessID,
		Provider:       coalesceString(input.Provider, fallbackProviderName(s.cfg)),
		ServiceType:    strings.ToLower(strings.TrimSpace(input.ServiceType)),
		GSPName:        coalesceString(input.GSPName, gstConfigValue(s.cfg, func(cfg *config.Config) string { return cfg.GST.GSPName })),
		PortalUsername: input.PortalUsername,
		Status:         "pending",
		Metadata:       mustMarshalMap(input.Metadata),
	}
	if account.Provider == "" {
		account.Provider = "simulated"
	}
	encrypted, hint, err := s.encryptIntegrationCredentials(input.Credentials)
	if err != nil {
		return nil, err
	}
	account.EncryptedCredentials = encrypted
	account.CredentialHint = hint

	if id != "" {
		var existing models.GSTIntegrationAccount
		if err := s.db.WithContext(ctx).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
			First(&existing).Error; err != nil {
			return nil, err
		}
		existing.Provider = account.Provider
		existing.ServiceType = account.ServiceType
		existing.GSPName = account.GSPName
		existing.PortalUsername = account.PortalUsername
		existing.EncryptedCredentials = account.EncryptedCredentials
		existing.CredentialHint = account.CredentialHint
		existing.Metadata = account.Metadata
		existing.Status = "pending"
		existing.LastError = ""
		if err := s.db.WithContext(ctx).Save(&existing).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if err := s.db.WithContext(ctx).Create(account).Error; err != nil {
		return nil, err
	}
	return account, nil
}

func (s *TaxComplianceService) ValidateIntegrationAccount(ctx context.Context, businessID, id string) (*models.GSTIntegrationAccount, error) {
	var account models.GSTIntegrationAccount
	if err := s.db.WithContext(ctx).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		First(&account).Error; err != nil {
		return nil, err
	}
	credentials, err := s.decryptIntegrationCredentials(account.EncryptedCredentials)
	if err != nil {
		return nil, err
	}
	credentials.PortalUsername = coalesceString(account.PortalUsername, credentials.PortalUsername)
	if err := s.provider.ValidateCredentials(ctx, &credentials); err != nil {
		account.Status = models.GSTJobStatusFailed
		account.LastError = err.Error()
		_ = s.db.WithContext(ctx).Save(&account).Error
		return &account, err
	}
	now := time.Now().UTC()
	account.Status = models.GSTJobStatusSucceeded
	account.LastValidatedAt = &now
	account.LastError = ""
	if err := s.db.WithContext(ctx).Save(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func (s *TaxComplianceService) GetEInvoiceByDocument(ctx context.Context, businessID, documentID string) (*models.EInvoiceRecord, error) {
	var record models.EInvoiceRecord
	err := s.db.WithContext(ctx).
		Where("business_id = ? AND document_id = ? AND deleted_at IS NULL", businessID, documentID).
		Order("updated_at DESC").
		First(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *TaxComplianceService) GetEWayBillByDocument(ctx context.Context, businessID, documentID string) (*models.EWayBillRecord, error) {
	var record models.EWayBillRecord
	err := s.db.WithContext(ctx).
		Where("business_id = ? AND document_id = ? AND deleted_at IS NULL", businessID, documentID).
		Order("updated_at DESC").
		First(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *TaxComplianceService) GetComplianceStatus(ctx context.Context, businessID, documentID string) (*ComplianceStatusResponse, error) {
	response := &ComplianceStatusResponse{ComplianceStatus: "idle"}
	var job models.GSTSubmissionJob
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND document_id = ? AND deleted_at IS NULL", businessID, documentID).
		Order("updated_at DESC").
		First(&job).Error; err == nil {
		response.Job = &job
		response.PortalStatus = job.Status
		response.RetryCount = job.AttemptCount
		response.LastError = job.LastError
		response.ComplianceStatus = job.Status
	}
	if record, err := s.GetEInvoiceByDocument(ctx, businessID, documentID); err == nil {
		response.EInvoice = record
		response.IRN = record.IRN
		response.AckNumber = record.AckNumber
		response.AckDate = record.AckDate
		response.QRCodeURL = record.QRCodeURL
		if record.Status == models.EInvoiceStatusGenerated {
			response.ComplianceStatus = record.Status
		}
	}
	if record, err := s.GetEWayBillByDocument(ctx, businessID, documentID); err == nil {
		response.EWayBill = record
		response.EWayBillNumber = record.EWayBillNumber
		response.EWayBillValidUntil = record.ValidUntil
		if record.Status == models.EWayBillStatusGenerated || record.Status == models.EWayBillStatusPartB {
			response.ComplianceStatus = record.Status
		}
	}
	return response, nil
}

func (s *TaxComplianceService) GenerateEInvoiceByDocument(ctx context.Context, businessID, documentID, idempotencyKey string, input GenerateEInvoiceInput) (*models.GSTSubmissionJob, error) {
	if err := s.entitlements.EnsureFeature(ctx, businessID, FeatureEInvoice); err != nil {
		return nil, err
	}
	document, err := s.getDocumentForCompliance(ctx, businessID, documentID)
	if err != nil {
		return nil, err
	}
	if err := s.validateDocumentForEInvoice(document); err != nil {
		return nil, err
	}
	if current, err := s.GetEInvoiceByDocument(ctx, businessID, documentID); err == nil && current.Status == models.EInvoiceStatusGenerated {
		return nil, fmt.Errorf("e-invoice already exists for this document")
	}
	payload, err := s.buildEInvoicePayload(ctx, document)
	if err != nil {
		return nil, err
	}
	return s.enqueueGSTJob(ctx, document, models.GSTOperationGenerateEInvoice, input.Source, idempotencyKey, payload)
}

func (s *TaxComplianceService) CancelEInvoiceByDocument(ctx context.Context, businessID, documentID, idempotencyKey string, input CancelEInvoiceInput) (*models.GSTSubmissionJob, error) {
	document, err := s.getDocumentForCompliance(ctx, businessID, documentID)
	if err != nil {
		return nil, err
	}
	record, err := s.GetEInvoiceByDocument(ctx, businessID, documentID)
	if err != nil {
		return nil, err
	}
	if record.Status != models.EInvoiceStatusGenerated {
		return nil, fmt.Errorf("no generated e-invoice exists for this document")
	}
	if record.GeneratedAt != nil && time.Since(*record.GeneratedAt) > eInvoiceCancelWindow {
		return nil, fmt.Errorf("e-invoice cancel window has expired")
	}
	payload := map[string]interface{}{
		"document_id": document.ID,
		"irn":         record.IRN,
		"reason":      input.Reason,
	}
	return s.enqueueGSTJob(ctx, document, models.GSTOperationCancelEInvoice, input.Source, idempotencyKey, payload)
}

func (s *TaxComplianceService) GenerateEWayBillByDocument(ctx context.Context, businessID, documentID, idempotencyKey string, input GenerateEWayBillInput) (*models.GSTSubmissionJob, error) {
	if err := s.entitlements.EnsureFeature(ctx, businessID, FeatureEWayBill); err != nil {
		return nil, err
	}
	document, err := s.getDocumentForCompliance(ctx, businessID, documentID)
	if err != nil {
		return nil, err
	}
	if err := s.validateDocumentForEWayBill(document); err != nil {
		return nil, err
	}
	payload, err := s.buildEWayBillPayload(ctx, document, input)
	if err != nil {
		return nil, err
	}
	return s.enqueueGSTJob(ctx, document, models.GSTOperationGenerateEWayBill, input.Source, idempotencyKey, payload)
}

func (s *TaxComplianceService) UpdateEWayPartBByDocument(ctx context.Context, businessID, documentID, idempotencyKey string, input UpdateEWayPartBInput) (*models.GSTSubmissionJob, error) {
	document, err := s.getDocumentForCompliance(ctx, businessID, documentID)
	if err != nil {
		return nil, err
	}
	record, err := s.GetEWayBillByDocument(ctx, businessID, documentID)
	if err != nil {
		return nil, err
	}
	payload := map[string]interface{}{
		"document_id":      document.ID,
		"eway_bill_number": record.EWayBillNumber,
		"transporter":      firstNonNilMap(input.Transporter, unmarshalJSONMap(record.Transporter)),
		"vehicle":          firstNonNilMap(input.Vehicle, unmarshalJSONMap(record.Vehicle)),
		"reason_code":      input.ReasonCode,
	}
	return s.enqueueGSTJob(ctx, document, models.GSTOperationUpdateEWayPartB, input.Source, idempotencyKey, payload)
}

func (s *TaxComplianceService) InitiateMultiVehicleByDocument(ctx context.Context, businessID, documentID, idempotencyKey string, input MultiVehicleInput) (*models.GSTSubmissionJob, error) {
	document, err := s.getDocumentForCompliance(ctx, businessID, documentID)
	if err != nil {
		return nil, err
	}
	record, err := s.GetEWayBillByDocument(ctx, businessID, documentID)
	if err != nil {
		return nil, err
	}
	payload := map[string]interface{}{
		"document_id":      document.ID,
		"eway_bill_number": record.EWayBillNumber,
		"movement_type":    input.MovementType,
		"vehicle_no":       input.VehicleNo,
		"transport_doc_no": input.TransportDocNo,
		"from_place":       input.FromPlace,
		"from_state":       input.FromState,
		"reason_code":      input.ReasonCode,
		"payload":          input.Payload,
	}
	return s.enqueueGSTJob(ctx, document, models.GSTOperationMultiVehicle, input.Source, idempotencyKey, payload)
}

func (s *TaxComplianceService) GetEWayBillPDFByDocument(ctx context.Context, businessID, documentID string) (string, error) {
	record, err := s.GetEWayBillByDocument(ctx, businessID, documentID)
	if err != nil {
		return "", err
	}
	if record.PDFURL != "" {
		return record.PDFURL, nil
	}
	result, err := s.provider.FetchEWayBillPDF(ctx, GSTEWayBillPDFRequest{
		BusinessID: businessID,
		DocumentID: documentID,
		EWayBillNo: record.EWayBillNumber,
	})
	if err != nil {
		return "", err
	}
	if result.PDFURL != "" {
		record.PDFURL = result.PDFURL
		_ = s.db.WithContext(ctx).Save(record).Error
		return result.PDFURL, nil
	}
	if len(result.PDFContent) > 0 {
		if s.s3 == nil || s.cfg == nil {
			return "", fmt.Errorf("s3 storage is not configured for e-way bill pdf upload")
		}
		key := path.Join("gst", documentID, "ewaybill.pdf")
		if err := s.s3.Upload(ctx, s.cfg.S3.BucketInvoices, key, result.PDFContent, "application/pdf"); err != nil {
			return "", err
		}
		record.PDFURL = s.s3.GetObjectURL(s.cfg.S3.BucketInvoices, key)
		_ = s.db.WithContext(ctx).Save(record).Error
		return record.PDFURL, nil
	}
	return "", fmt.Errorf("eway bill pdf is not available")
}

func (s *TaxComplianceService) ProcessJobByID(ctx context.Context, jobID string) error {
	var job models.GSTSubmissionJob
	if err := s.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", jobID).
		First(&job).Error; err != nil {
		return err
	}
	return s.processGSTJob(ctx, &job)
}

func (s *TaxComplianceService) enqueueGSTJob(ctx context.Context, document *models.Document, operation, source, idempotencyKey string, payload map[string]interface{}) (*models.GSTSubmissionJob, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		idempotencyKey = defaultIdempotencyKey(document.ID, operation, source)
	}
	var existing models.GSTSubmissionJob
	if err := s.db.WithContext(ctx).
		Where("idempotency_key = ? AND deleted_at IS NULL", idempotencyKey).
		First(&existing).Error; err == nil {
		return &existing, nil
	}

	job := &models.GSTSubmissionJob{
		BusinessID:     document.BusinessID,
		DocumentID:     document.ID,
		Operation:      operation,
		Status:         models.GSTJobStatusQueued,
		IdempotencyKey: idempotencyKey,
		RequestPayload: mustMarshalMap(payload),
		Source:         coalesceString(source, "api"),
	}
	if err := s.db.WithContext(ctx).Create(job).Error; err != nil {
		return nil, err
	}
	if err := s.dispatchGSTJob(ctx, job); err != nil {
		job.Status = models.GSTJobStatusFailed
		job.LastError = err.Error()
		_ = s.db.WithContext(ctx).Save(job).Error
		return nil, err
	}
	return job, nil
}

func (s *TaxComplianceService) dispatchGSTJob(ctx context.Context, job *models.GSTSubmissionJob) error {
	message := gstQueueMessage{JobID: job.ID}
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if s.sqs != nil && strings.TrimSpace(gstQueueURL(s.cfg)) != "" {
		resp, err := s.sqs.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:    aws.String(gstQueueURL(s.cfg)),
			MessageBody: aws.String(string(raw)),
		})
		if err != nil {
			return err
		}
		if resp.MessageId != nil {
			job.QueueMessageID = *resp.MessageId
			_ = s.db.WithContext(ctx).Save(job).Error
		}
		return nil
	}

	go func(jobID string) {
		background := ContextWithActor(context.Background(), actorFromContext(ctx))
		if err := s.ProcessJobByID(background, jobID); err != nil {
			s.log.Error("failed to process gst job inline", "job_id", jobID, "error", err)
		}
	}(job.ID)
	return nil
}

func (s *TaxComplianceService) processGSTJob(ctx context.Context, job *models.GSTSubmissionJob) error {
	document, err := s.getDocumentForCompliance(ctx, job.BusinessID, job.DocumentID)
	if err != nil {
		return err
	}
	account, _, err := s.resolveIntegrationAccount(ctx, job.BusinessID, job.Operation)
	if err != nil {
		return s.failJob(ctx, job, models.GSTErrorClassCredentials, err)
	}
	now := time.Now().UTC()
	job.Status = models.GSTJobStatusProcessing
	job.AttemptCount++
	job.LastAttemptAt = &now
	if err := s.db.WithContext(ctx).Save(job).Error; err != nil {
		return err
	}

	reqPayload := unmarshalJSONMap(job.RequestPayload)
	attempt := &models.GSTSubmissionAttempt{
		GSTJobID:       job.ID,
		BusinessID:     job.BusinessID,
		DocumentID:     job.DocumentID,
		AttemptNumber:  job.AttemptCount,
		Status:         models.GSTJobStatusProcessing,
		RequestPayload: mustMarshalMap(reqPayload),
	}
	if err := s.db.WithContext(ctx).Create(attempt).Error; err != nil {
		return err
	}

	var opErr error
	var resultPayload map[string]interface{}
	switch job.Operation {
	case models.GSTOperationGenerateEInvoice:
		var result *GSTEInvoiceResult
		result, opErr = s.provider.GenerateEInvoice(ctx, GSTEInvoiceRequest{
			BusinessID: job.BusinessID,
			DocumentID: job.DocumentID,
			SerialNo:   document.SerialNumber,
			Payload:    reqPayload,
		})
		if opErr == nil {
			opErr = s.applyEInvoiceResult(ctx, document, job, account, result)
			resultPayload = result.RawResponse
		}
	case models.GSTOperationCancelEInvoice:
		record, getErr := s.GetEInvoiceByDocument(ctx, job.BusinessID, job.DocumentID)
		if getErr != nil {
			opErr = getErr
			break
		}
		var result *GSTCancelEInvoiceResult
		result, opErr = s.provider.CancelEInvoice(ctx, GSTCancelEInvoiceRequest{
			BusinessID: job.BusinessID,
			DocumentID: job.DocumentID,
			IRN:        record.IRN,
			Reason:     readStringCandidate(reqPayload, "reason"),
			Payload:    reqPayload,
		})
		if opErr == nil {
			opErr = s.applyCancelledEInvoice(ctx, document, job, record, result)
			resultPayload = result.RawResponse
		}
	case models.GSTOperationGenerateEWayBill:
		var result *GSTEWayBillResult
		result, opErr = s.provider.GenerateEWayBill(ctx, GSTEWayBillRequest{
			BusinessID: job.BusinessID,
			DocumentID: job.DocumentID,
			SerialNo:   document.SerialNumber,
			Payload:    reqPayload,
		})
		if opErr == nil {
			opErr = s.applyEWayBillResult(ctx, document, job, account, result, false)
			resultPayload = result.RawResponse
		}
	case models.GSTOperationUpdateEWayPartB:
		record, getErr := s.GetEWayBillByDocument(ctx, job.BusinessID, job.DocumentID)
		if getErr != nil {
			opErr = getErr
			break
		}
		var result *GSTEWayBillResult
		result, opErr = s.provider.UpdateEWayPartB(ctx, GSTEWayPartBRequest{
			BusinessID: job.BusinessID,
			DocumentID: job.DocumentID,
			EWayBillNo: record.EWayBillNumber,
			Payload:    reqPayload,
		})
		if opErr == nil {
			opErr = s.applyEWayBillResult(ctx, document, job, account, result, true)
			if opErr == nil {
				opErr = s.recordVehicleMovement(ctx, document, record, reqPayload, "part_b_update")
			}
			resultPayload = result.RawResponse
		}
	case models.GSTOperationMultiVehicle:
		record, getErr := s.GetEWayBillByDocument(ctx, job.BusinessID, job.DocumentID)
		if getErr != nil {
			opErr = getErr
			break
		}
		var result *GSTMultiVehicleResult
		result, opErr = s.provider.InitiateMultiVehicle(ctx, GSTMultiVehicleRequest{
			BusinessID: job.BusinessID,
			DocumentID: job.DocumentID,
			EWayBillNo: record.EWayBillNumber,
			Payload:    reqPayload,
		})
		if opErr == nil {
			opErr = s.recordVehicleMovement(ctx, document, record, reqPayload, coalesceString(readStringCandidate(reqPayload, "movement_type"), "multi_vehicle"))
			resultPayload = result.RawResponse
			if opErr == nil {
				job.Status = models.GSTJobStatusSucceeded
				job.SucceededAt = timePointer(time.Now().UTC())
				job.ResultPayload = mustMarshalMap(result.RawResponse)
				opErr = s.db.WithContext(ctx).Save(job).Error
			}
		}
	default:
		opErr = fmt.Errorf("unsupported gst job operation: %s", job.Operation)
	}

	if opErr != nil {
		return s.handleJobError(ctx, job, attempt, opErr)
	}

	attempt.Status = models.GSTJobStatusSucceeded
	attempt.ResultPayload = mustMarshalMap(resultPayload)
	if err := s.db.WithContext(ctx).Save(attempt).Error; err != nil {
		return err
	}
	return nil
}

func (s *TaxComplianceService) applyEInvoiceResult(ctx context.Context, document *models.Document, job *models.GSTSubmissionJob, account *models.GSTIntegrationAccount, result *GSTEInvoiceResult) error {
	record := &models.EInvoiceRecord{
		BusinessID:          document.BusinessID,
		DocumentID:          document.ID,
		Status:              models.EInvoiceStatusGenerated,
		IRN:                 result.IRN,
		AckNumber:           result.AckNumber,
		AckDate:             result.AckDate,
		SignedQRCodePayload: result.SignedQRCodePayload,
		QRCodeURL:           result.QRCodeURL,
		ProviderReferenceID: result.ProviderReferenceID,
		ProviderName:        fallbackProviderName(s.cfg),
		RequestPayload:      job.RequestPayload,
		ResponsePayload:     mustMarshalMap(result.RawResponse),
		GeneratedAt:         timePointer(time.Now().UTC()),
	}
	if account != nil && account.ID != "" {
		record.IntegrationAccountID = &account.ID
	}
	if result.SignedQRCodePayload != "" && s.s3 != nil {
		if qrURL, err := s.persistQRCode(ctx, document.ID, result.SignedQRCodePayload); err == nil {
			record.QRCodeURL = qrURL
		}
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("document_id = ? AND deleted_at IS NULL", document.ID).Delete(&models.EInvoiceRecord{}).Error; err != nil {
			return err
		}
		if err := tx.Create(record).Error; err != nil {
			return err
		}
		job.Status = models.GSTJobStatusSucceeded
		job.SucceededAt = timePointer(time.Now().UTC())
		job.ErrorClass = ""
		job.LastError = ""
		job.ResultPayload = mustMarshalMap(result.RawResponse)
		if err := tx.Save(job).Error; err != nil {
			return err
		}
		return tx.Model(&models.Document{}).
			Where("id = ?", document.ID).
			Updates(map[string]interface{}{
				"current_einvoice_id": record.ID,
			}).Error
	}); err != nil {
		return err
	}
	_ = s.emitComplianceEvent(ctx, document.BusinessID, "einvoice.generated", map[string]interface{}{
		"document_id": document.ID,
		"record_id":   record.ID,
		"irn":         record.IRN,
		"job_id":      job.ID,
	})
	return nil
}

func (s *TaxComplianceService) applyCancelledEInvoice(ctx context.Context, document *models.Document, job *models.GSTSubmissionJob, record *models.EInvoiceRecord, result *GSTCancelEInvoiceResult) error {
	now := time.Now().UTC()
	record.Status = models.EInvoiceStatusCancelled
	record.CancelledAt = &now
	record.ResponsePayload = mustMarshalMap(result.RawResponse)
	job.Status = models.GSTJobStatusSucceeded
	job.SucceededAt = &now
	job.ErrorClass = ""
	job.LastError = ""
	job.ResultPayload = mustMarshalMap(result.RawResponse)
	if err := s.db.WithContext(ctx).Save(record).Error; err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Save(job).Error; err != nil {
		return err
	}
	_ = s.emitComplianceEvent(ctx, document.BusinessID, "einvoice.cancelled", map[string]interface{}{
		"document_id": document.ID,
		"record_id":   record.ID,
		"status":      record.Status,
		"job_id":      job.ID,
	})
	return nil
}

func (s *TaxComplianceService) applyEWayBillResult(ctx context.Context, document *models.Document, job *models.GSTSubmissionJob, account *models.GSTIntegrationAccount, result *GSTEWayBillResult, partBOnly bool) error {
	jobPayload := unmarshalJSONMap(job.RequestPayload)
	record := &models.EWayBillRecord{
		BusinessID:          document.BusinessID,
		DocumentID:          document.ID,
		Status:              models.EWayBillStatusGenerated,
		EWayBillNumber:      result.EWayBillNumber,
		EWayBillDate:        result.EWayBillDate,
		ValidUntil:          result.ValidUntil,
		SupplyType:          document.Direction,
		PartAStatus:         "generated",
		PartBStatus:         "generated",
		DistanceKM:          document.DistanceKM,
		DistanceSource:      coalesceString(readStringCandidate(jobPayload, "distance_source"), models.DistanceSourceAuto),
		Transporter:         document.Transporter,
		Vehicle:             document.Vehicle,
		DispatchFrom:        document.DispatchFrom,
		DispatchTo:          document.DispatchTo,
		PDFURL:              result.PDFURL,
		ProviderReferenceID: result.ProviderReferenceID,
		ProviderName:        fallbackProviderName(s.cfg),
		RequestPayload:      job.RequestPayload,
		ResponsePayload:     mustMarshalMap(result.RawResponse),
		GeneratedAt:         timePointer(time.Now().UTC()),
	}
	if partBOnly {
		record.Status = models.EWayBillStatusPartB
		record.UpdatedPartBAt = timePointer(time.Now().UTC())
	}
	if account != nil && account.ID != "" {
		record.IntegrationAccountID = &account.ID
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.EWayBillRecord
		err := tx.Where("document_id = ? AND deleted_at IS NULL", document.ID).Order("updated_at DESC").First(&existing).Error
		if err == nil {
			record.ID = existing.ID
		}
		if record.ID == "" {
			if err := tx.Create(record).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Model(&existing).Updates(map[string]interface{}{
				"status":                record.Status,
				"eway_bill_number":      record.EWayBillNumber,
				"eway_bill_date":        record.EWayBillDate,
				"valid_until":           record.ValidUntil,
				"supply_type":           record.SupplyType,
				"part_a_status":         record.PartAStatus,
				"part_b_status":         record.PartBStatus,
				"distance_km":           record.DistanceKM,
				"distance_source":       record.DistanceSource,
				"transporter":           record.Transporter,
				"vehicle":               record.Vehicle,
				"dispatch_from":         record.DispatchFrom,
				"dispatch_to":           record.DispatchTo,
				"pdf_url":               record.PDFURL,
				"provider_reference_id": record.ProviderReferenceID,
				"provider_name":         record.ProviderName,
				"request_payload":       record.RequestPayload,
				"response_payload":      record.ResponsePayload,
				"generated_at":          record.GeneratedAt,
				"updated_part_b_at":     record.UpdatedPartBAt,
			}).Error; err != nil {
				return err
			}
			record.ID = existing.ID
		}
		job.Status = models.GSTJobStatusSucceeded
		job.SucceededAt = timePointer(time.Now().UTC())
		job.ErrorClass = ""
		job.LastError = ""
		job.ResultPayload = mustMarshalMap(result.RawResponse)
		if err := tx.Save(job).Error; err != nil {
			return err
		}
		return tx.Model(&models.Document{}).
			Where("id = ?", document.ID).
			Updates(map[string]interface{}{
				"current_ewaybill_id": record.ID,
			}).Error
	}); err != nil {
		return err
	}
	event := "ewaybill.generated"
	if partBOnly {
		event = "ewaybill.updated"
	}
	_ = s.emitComplianceEvent(ctx, document.BusinessID, event, map[string]interface{}{
		"document_id":      document.ID,
		"record_id":        record.ID,
		"eway_bill_number": record.EWayBillNumber,
		"job_id":           job.ID,
		"part_b_only":      partBOnly,
	})
	return nil
}

func (s *TaxComplianceService) recordVehicleMovement(ctx context.Context, document *models.Document, record *models.EWayBillRecord, payload map[string]interface{}, movementType string) error {
	movement := &models.EWayBillVehicleMovement{
		EWayBillID:     record.ID,
		BusinessID:     document.BusinessID,
		DocumentID:     document.ID,
		MovementType:   movementType,
		VehicleNo:      readStringCandidate(payload, "vehicle.vehicle_no", "vehicle_no"),
		TransportDocNo: readStringCandidate(payload, "vehicle.transport_doc_no", "transport_doc_no"),
		FromPlace:      readStringCandidate(payload, "from_place"),
		FromState:      readStringCandidate(payload, "from_state"),
		ReasonCode:     readStringCandidate(payload, "reason_code"),
		IsActive:       true,
		Payload:        mustMarshalMap(payload),
	}
	if err := s.db.WithContext(ctx).Create(movement).Error; err != nil {
		return err
	}
	return nil
}

func (s *TaxComplianceService) handleJobError(ctx context.Context, job *models.GSTSubmissionJob, attempt *models.GSTSubmissionAttempt, err error) error {
	class := classifyGSTError(err)
	attempt.Status = models.GSTJobStatusFailed
	attempt.ErrorClass = class
	attempt.ErrorMessage = err.Error()
	_ = s.db.WithContext(ctx).Save(attempt).Error

	if class == models.GSTErrorClassRetriable && job.AttemptCount < 20 {
		delay := calculateRetryDelay(job.AttemptCount)
		next := time.Now().UTC().Add(delay)
		job.Status = models.GSTJobStatusRetrying
		job.ErrorClass = class
		job.LastError = err.Error()
		job.NextAttemptAt = &next
		if saveErr := s.db.WithContext(ctx).Save(job).Error; saveErr != nil {
			return saveErr
		}
		_ = s.emitComplianceEvent(ctx, job.BusinessID, "gst_job.retrying", map[string]interface{}{
			"document_id": job.DocumentID,
			"job_id":      job.ID,
			"error":       err.Error(),
			"retry_at":    next,
		})
		return s.dispatchGSTJob(ctx, job)
	}

	status := models.GSTJobStatusFailed
	if class == models.GSTErrorClassCredentials || class == models.GSTErrorClassValidation || class == models.GSTErrorClassRule {
		status = models.GSTJobStatusNeedsAttention
	}
	job.Status = status
	job.ErrorClass = class
	job.LastError = err.Error()
	if saveErr := s.db.WithContext(ctx).Save(job).Error; saveErr != nil {
		return saveErr
	}
	_ = s.emitComplianceEvent(ctx, job.BusinessID, failureEventForOperation(job.Operation), map[string]interface{}{
		"document_id": job.DocumentID,
		"job_id":      job.ID,
		"status":      job.Status,
		"operation":   job.Operation,
		"error":       err.Error(),
	})
	return err
}

func (s *TaxComplianceService) failJob(ctx context.Context, job *models.GSTSubmissionJob, class string, err error) error {
	job.Status = models.GSTJobStatusNeedsAttention
	job.ErrorClass = class
	job.LastError = err.Error()
	_ = s.db.WithContext(ctx).Save(job).Error
	return err
}

func (s *TaxComplianceService) getDocumentForCompliance(ctx context.Context, businessID, documentID string) (*models.Document, error) {
	var document models.Document
	err := s.db.WithContext(ctx).
		Preload("Lines").
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", documentID, businessID).
		First(&document).Error
	if err != nil {
		return nil, err
	}
	return &document, nil
}

func (s *TaxComplianceService) validateDocumentForEInvoice(document *models.Document) error {
	if document == nil {
		return fmt.Errorf("document is required")
	}
	if strings.TrimSpace(document.PartyGSTIN) == "" {
		return fmt.Errorf("e-invoice is only supported for B2B documents with a GSTIN")
	}
	if document.TaxMode != models.DocumentTaxModeGST {
		return fmt.Errorf("e-invoice requires GST mode")
	}
	if document.DraftState != models.DocumentDraftStateFinal && document.Status == models.DocumentStatusDraft {
		return fmt.Errorf("document must be final before generating an e-invoice")
	}
	if time.Since(document.IssueDate) > eInvoiceBackdateWindow {
		return fmt.Errorf("document is outside the allowed e-invoice backdated window")
	}
	return nil
}

func (s *TaxComplianceService) validateDocumentForEWayBill(document *models.Document) error {
	if document == nil {
		return fmt.Errorf("document is required")
	}
	if document.TaxMode != models.DocumentTaxModeGST {
		return fmt.Errorf("e-way bill requires GST mode")
	}
	if time.Since(document.IssueDate) > eWayBillBackdateWindow {
		return fmt.Errorf("document is outside the allowed e-way bill window")
	}
	return nil
}

func (s *TaxComplianceService) buildEInvoicePayload(ctx context.Context, document *models.Document) (map[string]interface{}, error) {
	business, err := s.businessRepo.GetByID(ctx, document.BusinessID)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]interface{}, 0, len(document.Lines))
	for _, line := range document.Lines {
		items = append(items, map[string]interface{}{
			"description":     line.Description,
			"hsn_sac_code":    line.HSNSACCode,
			"uqc_code":        line.UQCCode,
			"quantity":        line.Quantity,
			"unit_price":      line.UnitPrice,
			"line_total":      line.LineTotal,
			"tax_rate":        line.TaxRate,
			"cgst_amount":     line.CGSTAmount,
			"sgst_amount":     line.SGSTAmount,
			"igst_amount":     line.IGSTAmount,
			"cess_amount":     line.CessAmount,
			"discount_amount": line.DiscountAmount,
		})
	}
	return map[string]interface{}{
		"document_id":           document.ID,
		"document_type":         document.DocumentType,
		"serial_number":         document.SerialNumber,
		"issue_date":            document.IssueDate.Format("2006-01-02"),
		"supplier_gstin":        business.GSTIN,
		"supplier_name":         business.Name,
		"party_gstin":           document.PartyGSTIN,
		"party_name":            documentPartyName(document),
		"place_of_supply":       document.PlaceOfSupply,
		"currency":              document.Currency,
		"subtotal":              document.Subtotal,
		"discount_total":        document.DiscountTotal,
		"tax_total":             document.TaxTotal,
		"cess_total":            document.CessTotal,
		"total":                 document.Total,
		"reverse_charge":        boolYN(document.ReverseCharge),
		"reverse_charge_reason": document.ReverseChargeReason,
		"items":                 items,
		"source_linkage":        unmarshalJSONMap(document.SourceLinkage),
	}, nil
}

func (s *TaxComplianceService) buildEWayBillPayload(ctx context.Context, document *models.Document, input GenerateEWayBillInput) (map[string]interface{}, error) {
	dispatchFrom := firstNonNilMap(input.DispatchFrom, unmarshalJSONMap(document.DispatchFrom))
	dispatchTo := firstNonNilMap(input.DispatchTo, unmarshalJSONMap(document.DispatchTo))
	transporter := firstNonNilMap(input.Transporter, unmarshalJSONMap(document.Transporter))
	vehicle := firstNonNilMap(input.Vehicle, unmarshalJSONMap(document.Vehicle))
	distanceKM := input.DistanceKM
	if distanceKM <= 0 {
		distanceKM = document.DistanceKM
	}
	if distanceKM <= 0 {
		fromPin := readStringCandidate(dispatchFrom, "postal_code", "pincode")
		toPin := readStringCandidate(dispatchTo, "postal_code", "pincode")
		if fromPin != "" && toPin != "" {
			if result, err := s.provider.FetchDistance(ctx, GSTDistanceRequest{FromPincode: fromPin, ToPincode: toPin}); err == nil && result.DistanceKM > 0 {
				distanceKM = float64(result.DistanceKM)
			}
		}
	}
	if distanceKM <= 0 {
		return nil, fmt.Errorf("distance_km is required when pincode-based distance lookup is unavailable")
	}
	distanceSource := models.DistanceSourceManual
	if input.DistanceKM <= 0 && document.DistanceKM <= 0 {
		distanceSource = models.DistanceSourceAuto
	}
	return map[string]interface{}{
		"document_id":     document.ID,
		"document_type":   document.DocumentType,
		"serial_number":   document.SerialNumber,
		"issue_date":      document.IssueDate.Format("2006-01-02"),
		"supply_type":     strings.Title(coalesceString(document.Direction, models.DocumentDirectionOutward)),
		"dispatch_from":   dispatchFrom,
		"dispatch_to":     dispatchTo,
		"distance_km":     distanceKM,
		"distance_source": distanceSource,
		"transporter":     transporter,
		"vehicle":         vehicle,
		"reverse_charge":  boolYN(document.ReverseCharge),
	}, nil
}

func (s *TaxComplianceService) persistQRCode(ctx context.Context, documentID, payload string) (string, error) {
	if strings.TrimSpace(payload) == "" {
		return "", nil
	}
	if s.s3 == nil || s.cfg == nil {
		return "", fmt.Errorf("s3 storage is not configured for qr persistence")
	}
	pngBytes, err := qrcode.Encode(payload, qrcode.Medium, 256)
	if err != nil {
		return "", err
	}
	key := path.Join("gst", documentID, "qr.png")
	if err := s.s3.Upload(ctx, s.cfg.S3.BucketInvoices, key, pngBytes, "image/png"); err != nil {
		return "", err
	}
	return s.s3.GetObjectURL(s.cfg.S3.BucketInvoices, key), nil
}

func (s *TaxComplianceService) resolveIntegrationAccount(ctx context.Context, businessID, operation string) (*models.GSTIntegrationAccount, GSTIntegrationAccountCredentials, error) {
	serviceType := "einvoice"
	if operation == models.GSTOperationGenerateEWayBill || operation == models.GSTOperationUpdateEWayPartB || operation == models.GSTOperationMultiVehicle || operation == models.GSTOperationFetchEWayPDF {
		serviceType = "ewaybill"
	}
	var account models.GSTIntegrationAccount
	err := s.db.WithContext(ctx).
		Where("business_id = ? AND service_type = ? AND deleted_at IS NULL", businessID, serviceType).
		Order("updated_at DESC").
		First(&account).Error
	if err == nil {
		credentials, decErr := s.decryptIntegrationCredentials(account.EncryptedCredentials)
		if decErr != nil {
			return nil, GSTIntegrationAccountCredentials{}, decErr
		}
		credentials.PortalUsername = coalesceString(account.PortalUsername, credentials.PortalUsername)
		return &account, credentials, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, GSTIntegrationAccountCredentials{}, err
	}
	fallback := GSTIntegrationAccountCredentials{
		PortalUsername: gstConfigValue(s.cfg, func(cfg *config.Config) string { return cfg.GST.Username }),
		PortalPassword: gstConfigValue(s.cfg, func(cfg *config.Config) string { return cfg.GST.Password }),
		APIKey:         gstConfigValue(s.cfg, func(cfg *config.Config) string { return cfg.GST.ClientID }),
		APISecret:      gstConfigValue(s.cfg, func(cfg *config.Config) string { return cfg.GST.ClientSecret }),
	}
	return nil, fallback, nil
}

func (s *TaxComplianceService) encryptIntegrationCredentials(credentials GSTIntegrationAccountCredentials) (string, string, error) {
	key, err := s.encryptionKey()
	if err != nil {
		return "", "", err
	}
	raw, err := json.Marshal(credentials)
	if err != nil {
		return "", "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, raw, nil)
	hint := firstNonEmpty(credentials.PortalUsername, credentials.APIUsername, credentials.APIKey)
	return base64.StdEncoding.EncodeToString(ciphertext), hint, nil
}

func (s *TaxComplianceService) decryptIntegrationCredentials(value string) (GSTIntegrationAccountCredentials, error) {
	if strings.TrimSpace(value) == "" {
		return GSTIntegrationAccountCredentials{}, nil
	}
	key, err := s.encryptionKey()
	if err != nil {
		return GSTIntegrationAccountCredentials{}, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return GSTIntegrationAccountCredentials{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return GSTIntegrationAccountCredentials{}, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return GSTIntegrationAccountCredentials{}, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return GSTIntegrationAccountCredentials{}, fmt.Errorf("invalid encrypted credentials payload")
	}
	nonce := ciphertext[:gcm.NonceSize()]
	payload := ciphertext[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return GSTIntegrationAccountCredentials{}, err
	}
	var credentials GSTIntegrationAccountCredentials
	if err := json.Unmarshal(plaintext, &credentials); err != nil {
		return GSTIntegrationAccountCredentials{}, err
	}
	return credentials, nil
}

func (s *TaxComplianceService) encryptionKey() ([]byte, error) {
	if s.cfg == nil {
		return nil, fmt.Errorf("config is required for credential encryption")
	}
	key, err := base64.StdEncoding.DecodeString(s.cfg.Credentials.EncryptionKey)
	if err != nil {
		return nil, err
	}
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, fmt.Errorf("invalid credentials encryption key length")
	}
	return key, nil
}

func (s *TaxComplianceService) emitComplianceEvent(ctx context.Context, businessID, event string, payload map[string]interface{}) error {
	if s.webhooks == nil {
		return nil
	}
	return s.webhooks.EmitEvent(ctx, businessID, event, payload)
}

func classifyGSTError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "tempor"), strings.Contains(msg, "portal"), strings.Contains(msg, "throttle"), strings.Contains(msg, "unavailable"):
		return models.GSTErrorClassRetriable
	case strings.Contains(msg, "credential"), strings.Contains(msg, "unauthorized"), strings.Contains(msg, "forbidden"), strings.Contains(msg, "authentication"):
		return models.GSTErrorClassCredentials
	case strings.Contains(msg, "duplicate"):
		return models.GSTErrorClassDuplicate
	case strings.Contains(msg, "invalid"), strings.Contains(msg, "missing"), strings.Contains(msg, "gstin"), strings.Contains(msg, "hsn"), strings.Contains(msg, "pincode"):
		return models.GSTErrorClassValidation
	case strings.Contains(msg, "window"), strings.Contains(msg, "not enabled"), strings.Contains(msg, "only supported"), strings.Contains(msg, "outside"):
		return models.GSTErrorClassRule
	default:
		return models.GSTErrorClassUnknown
	}
}

func calculateRetryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return time.Minute
	}
	delay := time.Minute * time.Duration(1<<(attempt-1))
	if delay > 30*time.Minute {
		delay = 30 * time.Minute
	}
	return delay
}

func defaultIdempotencyKey(documentID, operation, source string) string {
	hash := sha256.Sum256([]byte(documentID + "|" + operation + "|" + source))
	return hex.EncodeToString(hash[:16])
}

func fallbackProviderName(cfg *config.Config) string {
	if cfg == nil || strings.TrimSpace(cfg.GST.Provider) == "" {
		return "simulated"
	}
	return strings.TrimSpace(cfg.GST.Provider)
}

func gstConfigValue(cfg *config.Config, getter func(*config.Config) string) string {
	if cfg == nil {
		return ""
	}
	return getter(cfg)
}

func gstQueueURL(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return cfg.SQS.GSTQueue
}

func failureEventForOperation(operation string) string {
	switch operation {
	case models.GSTOperationGenerateEWayBill, models.GSTOperationUpdateEWayPartB, models.GSTOperationMultiVehicle, models.GSTOperationFetchEWayPDF:
		return "ewaybill.failed"
	default:
		return "einvoice.failed"
	}
}

func boolYN(value bool) string {
	if value {
		return "Y"
	}
	return "N"
}

func firstNonNilMap(preferred map[string]interface{}, fallback map[string]interface{}) map[string]interface{} {
	if len(preferred) > 0 {
		return preferred
	}
	if len(fallback) > 0 {
		return fallback
	}
	return map[string]interface{}{}
}

func documentPartyName(document *models.Document) string {
	source := unmarshalJSONMap(document.SourceLinkage)
	return coalesceString(readStringCandidate(source, "party_name"), readStringCandidate(source, "counterparty_name"), document.PartyGSTIN)
}
