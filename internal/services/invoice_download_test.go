package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
)

type invoiceDownloadRepositoryFake struct {
	interfaces.CanonicalInvoiceRepository
	invoice      *models.Invoice
	invoiceErr   error
	getByIDCalls int
}

func (r *invoiceDownloadRepositoryFake) GetByID(
	_ context.Context,
	id, businessID string,
) (*models.Invoice, error) {
	r.getByIDCalls++
	if r.invoiceErr != nil {
		return nil, r.invoiceErr
	}
	if r.invoice == nil || r.invoice.ID != id || r.invoice.BusinessID != businessID {
		return nil, interfaces.ErrInvoiceNotFound
	}
	return r.invoice, nil
}

type invoiceRenderReadRepositoryFake struct {
	job                  *models.DocumentRenderJob
	err                  error
	statusCalls          int
	completedFinalCalls  int
	businessID           string
	invoiceID            string
	jobID                string
	sourceInvoiceVersion int
}

func (r *invoiceRenderReadRepositoryFake) GetInvoiceRenderJob(
	_ context.Context,
	businessID, invoiceID, jobID string,
) (*models.DocumentRenderJob, error) {
	r.statusCalls++
	r.businessID, r.invoiceID, r.jobID = businessID, invoiceID, jobID
	return r.job, r.err
}

func (r *invoiceRenderReadRepositoryFake) GetCompletedFinalRenderJob(
	_ context.Context,
	businessID, invoiceID string,
	sourceVersion int,
) (*models.DocumentRenderJob, error) {
	r.completedFinalCalls++
	r.businessID, r.invoiceID, r.sourceInvoiceVersion = businessID, invoiceID, sourceVersion
	return r.job, r.err
}

type invoicePDFPresignerFake struct {
	url       string
	err       error
	calls     int
	calledAt  time.Time
	bucket    string
	key       string
	expiresIn int64
}

func (p *invoicePDFPresignerFake) GeneratePresignedDownloadURL(
	_ context.Context,
	bucket, key string,
	expiresIn int64,
) (string, error) {
	p.calls++
	p.calledAt = time.Now().UTC()
	p.bucket, p.key, p.expiresIn = bucket, key, expiresIn
	return p.url, p.err
}

func newInvoiceDownloadService(
	invoices interfaces.CanonicalInvoiceRepository,
	renders interfaces.InvoiceRenderReadRepository,
	presigner InvoicePDFPresigner,
) *InvoiceService {
	return NewInvoiceService(
		nil,
		&config.Config{S3: config.S3Config{BucketInvoices: "private-invoices"}},
		invoices,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		logger.New(),
		WithInvoiceRenderReadRepository(renders),
		WithInvoicePDFPresigner(presigner),
	)
}

func TestInvoiceServiceGetRenderStatusValidatesAllUUIDsBeforeRepositoryAccess(t *testing.T) {
	reader := &invoiceRenderReadRepositoryFake{}
	service := newInvoiceDownloadService(&invoiceDownloadRepositoryFake{}, reader, &invoicePDFPresignerFake{})

	for _, ids := range []struct {
		name       string
		businessID string
		invoiceID  string
		jobID      string
	}{
		{name: "business", businessID: "not-a-uuid", invoiceID: uuid.NewString(), jobID: uuid.NewString()},
		{name: "invoice", businessID: uuid.NewString(), invoiceID: "not-a-uuid", jobID: uuid.NewString()},
		{name: "job", businessID: uuid.NewString(), invoiceID: uuid.NewString(), jobID: "not-a-uuid"},
	} {
		t.Run(ids.name, func(t *testing.T) {
			result, err := service.GetRenderStatusByBusiness(
				context.Background(),
				ids.businessID,
				ids.invoiceID,
				ids.jobID,
			)
			var invalid *idempotency.InvalidPayloadError
			if result != nil || !errors.As(err, &invalid) {
				t.Fatalf("result/error = %#v/%T %v, want invalid payload", result, err, err)
			}
		})
	}
	if reader.statusCalls != 0 {
		t.Fatalf("render repository calls = %d, want 0", reader.statusCalls)
	}
}

func TestInvoiceServiceGetRenderStatusReturnsOnlySafeDTOFields(t *testing.T) {
	businessID, invoiceID, jobID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	version := 4
	completedAt := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	createdAt := completedAt.Add(-time.Minute)
	reader := &invoiceRenderReadRepositoryFake{job: &models.DocumentRenderJob{
		ID:                   jobID,
		InvoiceID:            &invoiceID,
		BusinessID:           businessID,
		Kind:                 models.RenderKindFinal,
		SourceInvoiceVersion: &version,
		ObjectKey:            "private/object.pdf",
		OutputURL:            "https://must-not-leak.example",
		ErrorMessage:         "must not leak",
		Attempts:             9,
		LeaseOwner:           models.StringPointer("must-not-leak"),
		Status:               models.RenderJobStatusCompleted,
		CreatedAt:            createdAt,
		UpdatedAt:            completedAt,
		CompletedAt:          &completedAt,
	}}
	service := newInvoiceDownloadService(&invoiceDownloadRepositoryFake{}, reader, &invoicePDFPresignerFake{})

	result, err := service.GetRenderStatusByBusiness(context.Background(), businessID, invoiceID, jobID)
	if err != nil {
		t.Fatalf("GetRenderStatusByBusiness() error = %v", err)
	}
	want := InvoiceRenderStatus{
		ID: jobID, InvoiceID: invoiceID, Kind: models.RenderKindFinal,
		SourceInvoiceVersion: version, Status: models.RenderJobStatusCompleted,
		CreatedAt: createdAt, UpdatedAt: completedAt, CompletedAt: &completedAt,
	}
	if *result != want {
		t.Fatalf("GetRenderStatusByBusiness() = %#v, want %#v", result, want)
	}
	if reader.businessID != businessID || reader.invoiceID != invoiceID || reader.jobID != jobID {
		t.Fatalf("render lookup scope = %q/%q/%q", reader.businessID, reader.invoiceID, reader.jobID)
	}
}

func TestInvoiceServiceGetRenderStatusTreatsRepositoryIdentityMismatchAsNotFound(t *testing.T) {
	businessID, invoiceID, jobID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	wrongInvoiceID := uuid.NewString()
	reader := &invoiceRenderReadRepositoryFake{job: &models.DocumentRenderJob{
		ID: jobID, InvoiceID: &wrongInvoiceID, BusinessID: businessID,
	}}
	service := newInvoiceDownloadService(&invoiceDownloadRepositoryFake{}, reader, &invoicePDFPresignerFake{})

	result, err := service.GetRenderStatusByBusiness(context.Background(), businessID, invoiceID, jobID)
	if result != nil || !errors.Is(err, interfaces.ErrInvoiceRenderNotFound) {
		t.Fatalf("result/error = %#v/%v, want ErrInvoiceRenderNotFound", result, err)
	}
}

func TestInvoiceServiceGetPDFDownloadUsesCurrentCompletedFinalPrivateObject(t *testing.T) {
	businessID, invoiceID, jobID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	version := 7
	objectKey := "invoices/" + businessID + "/" + invoiceID + "/v7/final.pdf"
	invoices := &invoiceDownloadRepositoryFake{invoice: &models.Invoice{
		ID: invoiceID, BusinessID: businessID, Version: version, PDFURL: "https://legacy.example/public.pdf",
	}}
	reader := &invoiceRenderReadRepositoryFake{job: &models.DocumentRenderJob{
		ID: jobID, InvoiceID: &invoiceID, BusinessID: businessID, Kind: models.RenderKindFinal,
		SourceInvoiceVersion: &version, Status: models.RenderJobStatusCompleted, ObjectKey: objectKey,
	}}
	presigner := &invoicePDFPresignerFake{url: "https://signed.example/final.pdf"}
	service := newInvoiceDownloadService(invoices, reader, presigner)
	before := time.Now().UTC()

	result, err := service.GetPDFDownloadByBusiness(context.Background(), businessID, invoiceID)
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("GetPDFDownloadByBusiness() error = %v", err)
	}
	if result.DownloadURL != presigner.url {
		t.Fatalf("download URL = %q, want %q", result.DownloadURL, presigner.url)
	}
	if result.ExpiresAt.Before(before.Add(5*time.Minute)) || result.ExpiresAt.After(after.Add(5*time.Minute)) {
		t.Fatalf("expires_at = %s, want call time + 5m", result.ExpiresAt)
	}
	if result.ExpiresAt.After(presigner.calledAt.Add(5 * time.Minute)) {
		t.Fatalf("expires_at = %s, later than actual presign validity from %s", result.ExpiresAt, presigner.calledAt)
	}
	if reader.businessID != businessID || reader.invoiceID != invoiceID || reader.sourceInvoiceVersion != version {
		t.Fatalf("final render scope/version = %q/%q/%d", reader.businessID, reader.invoiceID, reader.sourceInvoiceVersion)
	}
	if presigner.calls != 1 || presigner.bucket != "private-invoices" ||
		presigner.key != objectKey || presigner.expiresIn != 300 {
		t.Fatalf("presign call = %d %q %q %d", presigner.calls, presigner.bucket, presigner.key, presigner.expiresIn)
	}
}

func TestInvoiceServiceGetPDFDownloadRejectsUnusableFinalRenderWithoutPresigning(t *testing.T) {
	businessID, invoiceID := uuid.NewString(), uuid.NewString()
	currentVersion := 7
	invoices := &invoiceDownloadRepositoryFake{invoice: &models.Invoice{
		ID: invoiceID, BusinessID: businessID, Version: currentVersion,
	}}

	for _, fixture := range []struct {
		name string
		job  *models.DocumentRenderJob
		err  error
	}{
		{name: "missing", err: interfaces.ErrInvoiceRenderNotFound},
		{name: "not complete", job: finalRenderFixture(businessID, invoiceID, currentVersion, models.RenderJobStatusProcessing, "private.pdf")},
		{name: "obsolete", job: finalRenderFixture(businessID, invoiceID, currentVersion, models.RenderJobStatusObsolete, "private.pdf")},
		{name: "wrong version", job: finalRenderFixture(businessID, invoiceID, currentVersion-1, models.RenderJobStatusCompleted, "private.pdf")},
		{name: "empty object key", job: finalRenderFixture(businessID, invoiceID, currentVersion, models.RenderJobStatusCompleted, "")},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			reader := &invoiceRenderReadRepositoryFake{job: fixture.job, err: fixture.err}
			presigner := &invoicePDFPresignerFake{}
			service := newInvoiceDownloadService(invoices, reader, presigner)

			result, err := service.GetPDFDownloadByBusiness(context.Background(), businessID, invoiceID)
			if result != nil || !errors.Is(err, ErrInvoicePDFNotReady) {
				t.Fatalf("result/error = %#v/%v, want ErrInvoicePDFNotReady", result, err)
			}
			if presigner.calls != 0 {
				t.Fatalf("presigner calls = %d, want 0", presigner.calls)
			}
		})
	}
}

func TestInvoiceServiceGetPDFDownloadPropagatesRenderStorageFailure(t *testing.T) {
	businessID, invoiceID := uuid.NewString(), uuid.NewString()
	invoices := &invoiceDownloadRepositoryFake{invoice: &models.Invoice{
		ID: invoiceID, BusinessID: businessID, Version: 7,
	}}
	reader := &invoiceRenderReadRepositoryFake{err: errors.New("postgres unavailable")}
	presigner := &invoicePDFPresignerFake{}
	service := newInvoiceDownloadService(invoices, reader, presigner)

	result, err := service.GetPDFDownloadByBusiness(context.Background(), businessID, invoiceID)
	if result != nil || err == nil || errors.Is(err, ErrInvoicePDFNotReady) {
		t.Fatalf("result/error = %#v/%v, want operational repository error", result, err)
	}
	if presigner.calls != 0 {
		t.Fatalf("presigner calls = %d, want 0", presigner.calls)
	}
}

func TestInvoiceServiceGetPDFDownloadRejectsEmptyPresignedURL(t *testing.T) {
	businessID, invoiceID := uuid.NewString(), uuid.NewString()
	version := 7
	invoices := &invoiceDownloadRepositoryFake{invoice: &models.Invoice{
		ID: invoiceID, BusinessID: businessID, Version: version,
	}}
	reader := &invoiceRenderReadRepositoryFake{job: finalRenderFixture(
		businessID,
		invoiceID,
		version,
		models.RenderJobStatusCompleted,
		"private.pdf",
	)}
	service := newInvoiceDownloadService(invoices, reader, &invoicePDFPresignerFake{})

	result, err := service.GetPDFDownloadByBusiness(context.Background(), businessID, invoiceID)
	if result != nil || err == nil {
		t.Fatalf("result/error = %#v/%v, want empty presign URL rejected", result, err)
	}
}

func finalRenderFixture(
	businessID, invoiceID string,
	version int,
	status, objectKey string,
) *models.DocumentRenderJob {
	return &models.DocumentRenderJob{
		ID: uuid.NewString(), InvoiceID: &invoiceID, BusinessID: businessID, Kind: models.RenderKindFinal,
		SourceInvoiceVersion: &version, Status: status, ObjectKey: objectKey,
	}
}

func TestInvoiceServiceGetPDFDownloadReturnsInvoiceNotFoundBeforeRenderLookup(t *testing.T) {
	invoices := &invoiceDownloadRepositoryFake{invoiceErr: interfaces.ErrInvoiceNotFound}
	reader := &invoiceRenderReadRepositoryFake{}
	service := newInvoiceDownloadService(invoices, reader, &invoicePDFPresignerFake{})

	result, err := service.GetPDFDownloadByBusiness(context.Background(), uuid.NewString(), uuid.NewString())
	if result != nil || !errors.Is(err, interfaces.ErrInvoiceNotFound) {
		t.Fatalf("result/error = %#v/%v, want ErrInvoiceNotFound", result, err)
	}
	if reader.completedFinalCalls != 0 {
		t.Fatalf("render lookup calls = %d, want 0", reader.completedFinalCalls)
	}
}
