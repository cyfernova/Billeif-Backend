package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type fakeTaxIntegrationAccountService struct {
	listErr     error
	upsertErr   error
	validateErr error
}

func (f fakeTaxIntegrationAccountService) ListIntegrationAccounts(context.Context, string) ([]*models.GSTIntegrationAccount, error) {
	return nil, f.listErr
}

func (f fakeTaxIntegrationAccountService) UpsertIntegrationAccount(
	context.Context,
	string,
	string,
	services.UpsertGSTIntegrationAccountInput,
) (*models.GSTIntegrationAccount, error) {
	return nil, f.upsertErr
}

func (f fakeTaxIntegrationAccountService) ValidateIntegrationAccount(context.Context, string, string) (*models.GSTIntegrationAccount, error) {
	return nil, f.validateErr
}

func TestTaxIntegrationHandlerMapsInternalFailuresToGeneric500WithoutLeak(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.ErrorLevel)
	rawErr := errors.New(`pq: relation "tenant_private_credentials" does not exist`)
	handler := &TaxHandler{
		integrationSvc: fakeTaxIntegrationAccountService{upsertErr: rawErr},
		log:            logger.FromZap(zap.New(core)),
	}
	response := performTaxIntegrationRequest(t, http.MethodPost, "/tax/integrations", []byte(`{"service_type":"einvoice"}`), handler.UpsertIntegrationAccount)

	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Equal(t, TaxIntegrationErrorResponse{Error: TaxIntegrationAPIError{
		Code: "tax_integration_internal_error", Message: "Tax integration request failed.",
	}}, decodeTaxIntegrationError(t, response))
	require.NotContains(t, response.Body.String(), "tenant_private_credentials")
	require.NotContains(t, response.Body.String(), "pq:")
	require.Len(t, logs.All(), 1)
	require.Contains(t, logs.All()[0].ContextMap()["error"], "tenant_private_credentials")
}

func TestTaxIntegrationHandlerMapsStableValidationAndRevisionErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fixtures := []struct {
		name        string
		err         error
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{name: "validation not configured", err: services.ErrGSTCredentialValidationNotConfigured, wantStatus: http.StatusUnprocessableEntity, wantCode: "gst_credential_validation_not_configured", wantMessage: "GST credential validation is not configured."},
		{name: "validation failed", err: services.ErrGSTCredentialValidationFailed, wantStatus: http.StatusUnprocessableEntity, wantCode: "gst_credential_validation_failed", wantMessage: "GST credential validation failed."},
		{name: "account missing", err: interfaces.ErrGSTIntegrationAccountNotFound, wantStatus: http.StatusNotFound, wantCode: "gst_integration_account_not_found", wantMessage: "GST integration account was not found."},
		{name: "revision changed", err: fmt.Errorf("persist observation: %w", interfaces.ErrCapabilityProviderHealthCredentialRevisionStale), wantStatus: http.StatusConflict, wantCode: "gst_credential_revision_conflict", wantMessage: "GST integration credentials changed. Refresh and retry."},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			handler := &TaxHandler{integrationSvc: fakeTaxIntegrationAccountService{validateErr: fixture.err}, log: logger.FromZap(zap.NewNop())}
			response := performTaxIntegrationRequest(t, http.MethodPost, "/tax/integrations/account-a/validate", nil, handler.ValidateIntegrationAccount)

			require.Equal(t, fixture.wantStatus, response.Code)
			require.Equal(t, TaxIntegrationErrorResponse{Error: TaxIntegrationAPIError{
				Code: fixture.wantCode, Message: fixture.wantMessage,
			}}, decodeTaxIntegrationError(t, response))
			require.NotContains(t, response.Body.String(), "account\":")
		})
	}
}

func TestTaxIntegrationHandlerUsesStableBindingError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &TaxHandler{integrationSvc: fakeTaxIntegrationAccountService{}, log: logger.FromZap(zap.NewNop())}
	response := performTaxIntegrationRequest(t, http.MethodPost, "/tax/integrations", []byte(`{"service_type":`), handler.UpsertIntegrationAccount)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Equal(t, TaxIntegrationErrorResponse{Error: TaxIntegrationAPIError{
		Code: "invalid_tax_integration_request", Message: "Tax integration request is invalid.",
	}}, decodeTaxIntegrationError(t, response))
	require.NotContains(t, response.Body.String(), "unexpected EOF")
}

func performTaxIntegrationRequest(
	t *testing.T,
	method, path string,
	body []byte,
	handle func(*gin.Context),
) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(method, path, bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("business_id", "business-a")
	ctx.Params = gin.Params{{Key: "id", Value: "account-a"}}
	handle(ctx)
	return response
}

func decodeTaxIntegrationError(t *testing.T, response *httptest.ResponseRecorder) TaxIntegrationErrorResponse {
	t.Helper()
	var result TaxIntegrationErrorResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	return result
}
