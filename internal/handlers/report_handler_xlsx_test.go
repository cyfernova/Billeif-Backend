package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"

	"github.com/gin-gonic/gin"
)

func TestWriteReportExportResponseDeliversNativeXLSXForBrowserAndNativeClients(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	writeReportExportResponse(context, &services.ReportExportResponse{
		Run:         &models.ReportRun{ID: "run-1"},
		Filename:    "sales_register-20260902-120000.xlsx",
		ContentType: services.ReportXLSXContentType,
		Binary:      []byte("PK\x03\x04workbook"),
	})

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != services.ReportXLSXContentType {
		t.Fatalf("Content-Type=%q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); got != `attachment; filename="sales_register-20260902-120000.xlsx"` {
		t.Fatalf("Content-Disposition=%q", got)
	}
	if got := recorder.Header().Get("X-Report-Run-ID"); got != "run-1" {
		t.Fatalf("X-Report-Run-ID=%q", got)
	}
	if got := recorder.Header().Get("Access-Control-Expose-Headers"); got != "Content-Disposition, X-Report-Run-ID" {
		t.Fatalf("Access-Control-Expose-Headers=%q", got)
	}
	if recorder.Body.String() != "PK\x03\x04workbook" {
		t.Fatalf("binary body=%q", recorder.Body.String())
	}
}
