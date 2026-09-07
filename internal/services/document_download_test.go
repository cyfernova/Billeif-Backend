package services

import (
	"context"
	"errors"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"gorm.io/gorm"
)

type documentDownloadRepositoryFake struct {
	interfaces.DocumentRepository
	document *models.Document
	job      *models.DocumentRenderJob
	jobErr   error
}

func (r *documentDownloadRepositoryFake) GetByID(_ context.Context, id, businessID string) (*models.Document, error) {
	if r.document == nil || r.document.ID != id || r.document.BusinessID != businessID {
		return nil, gorm.ErrRecordNotFound
	}
	return r.document, nil
}

func (r *documentDownloadRepositoryFake) GetLatestRenderJob(context.Context, string) (*models.DocumentRenderJob, error) {
	return r.job, r.jobErr
}

func TestDocumentPDFDownload(t *testing.T) {
	const stored = "https://private-invoices.s3.ap-south-1.amazonaws.com/documents/business/document/render.pdf"
	for _, tc := range []struct {
		name, storedURL, businessID string
		job                         *models.DocumentRenderJob
		jobErr                      error
		wantErr                     bool
	}{
		{name: "private PDF is signed", storedURL: stored, businessID: "business"},
		{name: "render fallback is signed", businessID: "business", job: &models.DocumentRenderJob{OutputURL: stored}},
		{name: "other business denied", storedURL: stored, businessID: "other", wantErr: true},
		{name: "other bucket denied", storedURL: "https://other.s3.ap-south-1.amazonaws.com/documents/business/document/render.pdf", businessID: "business", wantErr: true},
		{name: "other document denied", storedURL: "https://private-invoices.s3.ap-south-1.amazonaws.com/documents/business/other/render.pdf", businessID: "business", wantErr: true},
		{name: "traversal denied", storedURL: "https://private-invoices.s3.ap-south-1.amazonaws.com/documents/business/document/../other/render.pdf", businessID: "business", wantErr: true},
		{name: "pending render", businessID: "business", jobErr: gorm.ErrRecordNotFound, wantErr: true},
		{name: "database outage", businessID: "business", jobErr: errors.New("database unavailable"), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &documentDownloadRepositoryFake{document: &models.Document{ID: "document", BusinessID: "business", PDFURL: tc.storedURL}, job: tc.job, jobErr: tc.jobErr}
			presigner := &invoicePDFPresignerFake{url: "https://signed.example.test/download"}
			service := &DocumentService{repo: repo, cfg: &config.Config{S3: config.S3Config{BucketInvoices: "private-invoices"}, AWS: config.AWSConfig{Region: "ap-south-1"}}, pdfPresigner: presigner}
			got, err := service.GetPDFURLByBusiness(context.Background(), tc.businessID, "document")
			if tc.wantErr {
				if err == nil || presigner.calls != 0 {
					t.Fatalf("expected rejection without signing, calls=%d error=%v", presigner.calls, err)
				}
				if tc.jobErr != nil && !errors.Is(tc.jobErr, gorm.ErrRecordNotFound) && !errors.Is(err, tc.jobErr) {
					t.Fatalf("database error not preserved: %v", err)
				}
				return
			}
			if err != nil || got != presigner.url || presigner.calls != 1 || presigner.bucket != "private-invoices" || presigner.key != "documents/business/document/render.pdf" || presigner.expiresIn != 300 {
				t.Fatalf("invalid signed download: calls=%d key=%s expiry=%d error=%v", presigner.calls, presigner.key, presigner.expiresIn, err)
			}
		})
	}
}
