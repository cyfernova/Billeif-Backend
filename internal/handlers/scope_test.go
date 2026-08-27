package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequireUploadSizeBytesValidatesRequiredPositiveBoundedQuery(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantSize   int64
		wantOK     bool
		wantStatus int
	}{
		{name: "missing", wantOK: false, wantStatus: http.StatusBadRequest},
		{name: "not an integer", query: "?size_bytes=large", wantOK: false, wantStatus: http.StatusBadRequest},
		{name: "zero", query: "?size_bytes=0", wantOK: false, wantStatus: http.StatusBadRequest},
		{name: "above limit", query: "?size_bytes=5242881", wantOK: false, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "at limit", query: "?size_bytes=5242880", wantSize: 5 * 1024 * 1024, wantOK: true, wantStatus: http.StatusOK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodPost, "/upload"+test.query, nil)

			gotSize, gotOK := requireUploadSizeBytes(context, 5*1024*1024)

			if gotSize != test.wantSize || gotOK != test.wantOK {
				t.Fatalf("requireUploadSizeBytes() = %d, %v; want %d, %v", gotSize, gotOK, test.wantSize, test.wantOK)
			}
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
		})
	}
}
