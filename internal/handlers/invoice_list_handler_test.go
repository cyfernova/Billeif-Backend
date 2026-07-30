package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/invoicecursor"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type invoiceListRepository struct {
	interfaces.CanonicalInvoiceRepository
	listCalled bool
	invoices   []*models.Invoice
	hasMore    bool
	cursor     *invoicecursor.Position
	limit      int
}

func (r *invoiceListRepository) GetByBusinessID(
	_ context.Context,
	_ string,
	_, _ int,
) ([]*models.Invoice, int64, error) {
	r.listCalled = true
	return nil, 0, nil
}

func (r *invoiceListRepository) ListByCursor(
	_ context.Context,
	_ string,
	cursor *invoicecursor.Position,
	limit int,
) ([]*models.Invoice, bool, error) {
	r.listCalled = true
	r.cursor = cursor
	r.limit = limit
	return r.invoices, r.hasMore, nil
}

func TestInvoiceListRejectsLegacyPageParameter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &invoiceListRepository{}
	service := services.NewInvoiceService(
		nil,
		&config.Config{},
		repository,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		logger.New(),
	)
	handler := NewInvoiceHandler(service, nil, logger.New())
	router := gin.New()
	router.GET("/api/v1/invoices", func(c *gin.Context) {
		c.Set("business_id", uuid.NewString())
		handler.List(c)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/invoices?page=1", nil))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	if repository.listCalled {
		t.Fatal("repository was called for rejected page parameter")
	}
}

func TestInvoiceListReturnsCursorEnvelopeAndUsesDefaultLimit(t *testing.T) {
	businessID := uuid.NewString()
	createdAt := time.Date(2026, time.July, 30, 5, 30, 0, 123, time.UTC)
	repository := &invoiceListRepository{
		invoices: []*models.Invoice{
			{ID: uuid.NewString(), BusinessID: businessID, CreatedAt: createdAt},
			{ID: uuid.NewString(), BusinessID: businessID, CreatedAt: createdAt},
		},
		hasMore: true,
	}
	codec, err := invoicecursor.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	handler := newInvoiceListHandler(repository, codec)
	response := serveInvoiceList(t, handler, businessID, "/api/v1/invoices")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Items      []*models.Invoice `json:"items"`
		NextCursor *string           `json:"next_cursor"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Items) != 2 || body.NextCursor == nil || *body.NextCursor == "" {
		t.Fatalf("response = %#v, want items and next_cursor", body)
	}
	if repository.limit != 20 || repository.cursor != nil {
		t.Fatalf("repository call limit=%d cursor=%#v, want 20/nil", repository.limit, repository.cursor)
	}
	position, err := codec.Decode(*body.NextCursor, businessID)
	if err != nil {
		t.Fatalf("decode next cursor: %v", err)
	}
	last := repository.invoices[len(repository.invoices)-1]
	if position.ID != last.ID || !position.CreatedAt.Equal(last.CreatedAt) {
		t.Fatalf("next cursor position = %#v, want last returned invoice", position)
	}
	var exact map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &exact); err != nil {
		t.Fatalf("decode exact response: %v", err)
	}
	if len(exact) != 2 || exact["items"] == nil || exact["next_cursor"] == nil {
		t.Fatalf("response keys = %v, want exactly items/next_cursor", exact)
	}
}

func TestInvoiceListLimitValidationAndCap(t *testing.T) {
	codec, err := invoicecursor.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantLimit  int
	}{
		{name: "invalid", query: "?limit=abc", wantStatus: http.StatusBadRequest},
		{name: "zero", query: "?limit=0", wantStatus: http.StatusBadRequest},
		{name: "negative", query: "?limit=-2", wantStatus: http.StatusBadRequest},
		{name: "over cap", query: "?limit=101", wantStatus: http.StatusOK, wantLimit: 100},
		{name: "positive", query: "?limit=7", wantStatus: http.StatusOK, wantLimit: 7},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &invoiceListRepository{}
			response := serveInvoiceList(
				t,
				newInvoiceListHandler(repository, codec),
				uuid.NewString(),
				"/api/v1/invoices"+test.query,
			)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantStatus == http.StatusBadRequest && repository.listCalled {
				t.Fatal("repository called for invalid limit")
			}
			if test.wantStatus == http.StatusOK && repository.limit != test.wantLimit {
				t.Fatalf("repository limit = %d, want %d", repository.limit, test.wantLimit)
			}
		})
	}
}

func TestInvoiceListRejectsInvalidCursorWithStableGenericError(t *testing.T) {
	codec, err := invoicecursor.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	repository := &invoiceListRepository{}
	response := serveInvoiceList(
		t,
		newInvoiceListHandler(repository, codec),
		uuid.NewString(),
		"/api/v1/invoices?cursor=do-not-echo-this-token",
	)
	if response.Code != http.StatusBadRequest || response.Body.String() != `{"error":"invalid cursor"}` {
		t.Fatalf("response = %d %s, want stable generic 400", response.Code, response.Body.String())
	}
	if repository.listCalled {
		t.Fatal("repository called for invalid cursor")
	}
}

func TestInvoiceListFinalPageReturnsNullCursor(t *testing.T) {
	businessID := uuid.NewString()
	codec, err := invoicecursor.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	repository := &invoiceListRepository{
		invoices: []*models.Invoice{{
			ID: uuid.NewString(), BusinessID: businessID, CreatedAt: time.Now().UTC(),
		}},
	}
	response := serveInvoiceList(t, newInvoiceListHandler(repository, codec), businessID, "/api/v1/invoices")
	var body map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if string(body["next_cursor"]) != "null" {
		t.Fatalf("next_cursor = %s, want null", body["next_cursor"])
	}
}

func newInvoiceListHandler(repository interfaces.CanonicalInvoiceRepository, codec *invoicecursor.Codec) *InvoiceHandler {
	service := services.NewInvoiceService(
		nil,
		&config.Config{},
		repository,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		logger.New(),
	)
	return NewInvoiceHandler(service, nil, logger.New(), codec)
}

func serveInvoiceList(
	t *testing.T,
	handler *InvoiceHandler,
	businessID string,
	target string,
) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/invoices", func(c *gin.Context) {
		c.Set("business_id", businessID)
		handler.List(c)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	return response
}
