package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type handlerOperationRepository struct {
	record *interfaces.OperationRecord
}

func (f *handlerOperationRepository) ListOperations(context.Context, string, interfaces.OperationRecordQuery) (interfaces.OperationRecordPage, error) {
	if f.record == nil {
		return interfaces.OperationRecordPage{}, nil
	}
	return interfaces.OperationRecordPage{Records: []interfaces.OperationRecord{*f.record}}, nil
}
func (f *handlerOperationRepository) GetOperation(context.Context, string, string, string) (*interfaces.OperationRecord, error) {
	if f.record == nil {
		return nil, interfaces.ErrOperationNotFound
	}
	return f.record, nil
}
func (f *handlerOperationRepository) ListOperationTimeline(context.Context, string, string, string, int) ([]interfaces.OperationTimelineRecord, error) {
	return nil, nil
}
func (f *handlerOperationRepository) RetryRender(context.Context, interfaces.RenderRecoveryCommand) (*interfaces.OperationRecoveryResult, error) {
	return nil, interfaces.ErrUnsupportedRecovery
}
func (f *handlerOperationRepository) RecordRecoveryDecision(context.Context, interfaces.OperationRecoveryDecision) (*interfaces.OperationRecoveryResult, error) {
	return &interfaces.OperationRecoveryResult{}, nil
}

func TestOperationHandlerBusinessProjectionOmitsOperatorAndProviderDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC)
	record := &interfaces.OperationRecord{
		ID: uuid.NewString(), Type: services.OperationTypeRazorpayWebhook,
		ResourceType: "subscription", ResourceID: uuid.NewString(), InternalStatus: "provider_internal_state",
		ErrorCode: "provider_secret raw payload", CorrelationID: "queue-message-secret", CreatedAt: now, UpdatedAt: now,
	}
	service := services.NewOperationService(&handlerOperationRepository{record: record}, nil, nil, services.OperationServiceOptions{Now: func() time.Time { return now }})
	handler := NewOperationHandler(service, nil)
	router := gin.New()
	router.GET("/operations/:operation_id", func(c *gin.Context) {
		c.Set("validated_business_id", uuid.NewString())
		handler.GetBusiness(c)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/operations/"+record.Type+":"+record.ID, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, forbidden := range []string{"source_status", "provider_internal_state", "provider_secret", "raw payload", "queue-message-secret"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("business operation leaked %q: %s", forbidden, response.Body.String())
		}
	}
	if payload["status"] != string(services.OperationStatusUnknown) {
		t.Fatalf("status = %#v", payload["status"])
	}
}

func TestOperationHandlerMapsStepUpBoundaryToStablePrecondition(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC)
	record := &interfaces.OperationRecord{
		ID: uuid.NewString(), Type: services.OperationTypeRazorpayWebhook,
		ResourceType: "subscription", ResourceID: uuid.NewString(), InternalStatus: "reconciliation_required",
		CreatedAt: now, UpdatedAt: now,
	}
	service := services.NewOperationService(&handlerOperationRepository{record: record}, nil, nil, services.OperationServiceOptions{Now: func() time.Time { return now }})
	handler := NewOperationHandler(service, nil)
	router := gin.New()
	router.POST("/operator/operations/:operation_id/recovery", func(c *gin.Context) {
		c.Set("user_id", "verified-operator-subject")
		handler.RecoverOperator(c)
	})
	body := `{"action":"reprocess_webhook","reason":"provider callback needs review","idempotency_key":"` + uuid.NewString() + `","correlation_id":"` + uuid.NewString() + `"}`
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/operator/operations/"+record.Type+":"+record.ID+"/recovery?business_id="+uuid.NewString(), strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusPreconditionRequired || !strings.Contains(response.Body.String(), services.OperationCodeStepUpRequired) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestOperationHandlerRejectsUnknownRecoveryFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewOperationHandler(services.NewOperationService(
		&handlerOperationRepository{}, nil, nil, services.OperationServiceOptions{},
	), nil)
	router := gin.New()
	router.POST("/operations/:operation_id/recovery", func(c *gin.Context) {
		c.Set("validated_business_id", uuid.NewString())
		handler.RecoverBusiness(c)
	})
	body := `{"action":"retry","reason":"retry","idempotency_key":"` + uuid.NewString() + `","correlation_id":"` + uuid.NewString() + `","provider_payload":"forbidden"}`
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/operations/invoice_render:"+uuid.NewString()+"/recovery", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_recovery_request") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}
