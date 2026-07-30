package interfaces

import (
	"context"
	"errors"
	"time"

	"invoice-backend/internal/invoicecursor"
	"invoice-backend/internal/invoiceresolution"
	"invoice-backend/internal/models"
)

var (
	ErrInvoiceNotFound         = errors.New("invoice not found")
	ErrInvoiceRenderNotFound   = errors.New("invoice render not found")
	ErrInvoiceDeliveryNotFound = errors.New("invoice delivery not found")
	ErrInvoiceNotDeliverable   = errors.New("invoice is not deliverable")
)

type UserRepository interface {
	Create(ctx context.Context, user *models.User) error
	GetByID(ctx context.Context, id string) (*models.User, error)
	GetByEmail(ctx context.Context, email string) (*models.User, error)
	GetByPhoneNumber(ctx context.Context, phoneNumber string) (*models.User, error)
	GetByCognitoID(ctx context.Context, cognitoID string) (*models.User, error)
	Update(ctx context.Context, user *models.User) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, businessID string, page, limit int) ([]*models.User, int64, error)
}

type BusinessRepository interface {
	Create(ctx context.Context, business *models.BusinessProfile) error
	GetByID(ctx context.Context, id string) (*models.BusinessProfile, error)
	Update(ctx context.Context, business *models.BusinessProfile) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, userID string, page, limit int) ([]*models.BusinessProfile, int64, error)
}

type CustomerRepository interface {
	Create(ctx context.Context, customer *models.Customer) error
	GetByID(ctx context.Context, id, businessID string) (*models.Customer, error)
	GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Customer, int64, error)
	Update(ctx context.Context, customer *models.Customer) error
	Delete(ctx context.Context, id string) error
}

type VendorRepository interface {
	Create(ctx context.Context, vendor *models.Vendor) error
	GetByID(ctx context.Context, id, businessID string) (*models.Vendor, error)
	GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Vendor, int64, error)
	Update(ctx context.Context, vendor *models.Vendor) error
	Delete(ctx context.Context, id string) error
}

type ProductRepository interface {
	Create(ctx context.Context, product *models.Product) error
	GetByID(ctx context.Context, id, businessID string) (*models.Product, error)
	GetByIDWithoutTenant(ctx context.Context, id string) (*models.Product, error)
	GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Product, int64, error)
	GetBySKU(ctx context.Context, businessID, sku string) (*models.Product, error)
	Update(ctx context.Context, product *models.Product) error
	Delete(ctx context.Context, id string) error
	AdjustStock(ctx context.Context, productID string, quantity int64) error
}

type InvoiceRepository interface {
	Create(ctx context.Context, invoice *models.Invoice) error
	GetByID(ctx context.Context, id, businessID string) (*models.Invoice, error)
	// GetByIDInternal fetches by ID without tenant scoping. Only for trusted internal callers (workers).
	GetByIDInternal(ctx context.Context, id string) (*models.Invoice, error)
	GetByInvoiceNo(ctx context.Context, businessID, invoiceNo string) (*models.Invoice, error)
	ListByCursor(ctx context.Context, businessID string, cursor *invoicecursor.Position, limit int) ([]*models.Invoice, bool, error)
	GetItems(ctx context.Context, invoiceID string) ([]*models.InvoiceItem, error)
	Update(ctx context.Context, invoice *models.Invoice) error
	UpdateStatus(ctx context.Context, invoiceID string, status string) error
	UpdatePDFURL(ctx context.Context, invoiceID, pdfURL string) error
	Delete(ctx context.Context, id string) error
}

type AtomicInvoiceDraft struct {
	BusinessID      string
	Command         string
	IdempotencyKey  string
	RequestHash     string
	Invoice         *models.Invoice
	Document        *models.Document
	Activity        *models.ActivityLog
	OutboxEvents    []*models.OutboxEvent
	RenderJobs      []*models.DocumentRenderJob
	EmailDeliveries []*models.EmailDelivery
}

type AtomicInvoiceDraftResult struct {
	Invoice  *models.Invoice
	Replayed bool
}

type AtomicInvoiceIssue struct {
	BusinessID      string
	InvoiceID       string
	Command         string
	IdempotencyKey  string
	RequestHash     string
	ExpectedVersion int
	DocumentType    string
	Series          string
	ActorID         string
	ActorRole       string
	RequestID       string
	IPAddress       string
}

type AtomicInvoiceIssueResult struct {
	Invoice     *models.Invoice
	FinalRender *models.DocumentRenderJob
	OutboxEvent *models.OutboxEvent
	Replayed    bool
}

type AtomicInvoicePreview struct {
	BusinessID     string
	InvoiceID      string
	Command        string
	IdempotencyKey string
	RequestHash    string
	ActorID        string
	ActorRole      string
	RequestID      string
	IPAddress      string
}

type AtomicInvoicePreviewResult struct {
	RenderJob   *models.DocumentRenderJob
	OutboxEvent *models.OutboxEvent
	Replayed    bool
}

type AtomicInvoiceDelivery struct {
	BusinessID     string
	InvoiceID      string
	Command        string
	IdempotencyKey string
	RequestHash    string
	Recipient      string
	ActorID        string
	ActorRole      string
	RequestID      string
	IPAddress      string
}

type AtomicInvoiceDeliveryResult struct {
	Delivery    *models.EmailDelivery
	OutboxEvent *models.OutboxEvent
	Replayed    bool
}

type InvoiceDeliveryRepository interface {
	CreateDeliveryAtomic(ctx context.Context, command AtomicInvoiceDelivery) (*AtomicInvoiceDeliveryResult, error)
	GetInvoiceDelivery(ctx context.Context, businessID, invoiceID, deliveryID string) (*models.EmailDelivery, error)
}

type CanonicalInvoiceIssuer interface {
	IssueDraftAtomic(ctx context.Context, command AtomicInvoiceIssue) (*AtomicInvoiceIssueResult, error)
}

type CanonicalInvoicePreviewer interface {
	RequestPreviewAtomic(ctx context.Context, command AtomicInvoicePreview) (*AtomicInvoicePreviewResult, error)
}

type VersionedInvoiceDraftMetadataUpdater interface {
	UpdateDraftMetadataVersioned(
		ctx context.Context,
		invoice *models.Invoice,
		expectedVersion int,
	) error
}

type CanonicalInvoiceRepository interface {
	InvoiceRepository
	invoiceresolution.Resolver
	ReplayCompletedDraft(ctx context.Context, businessID, command, idempotencyKey, requestHash string) (*AtomicInvoiceDraftResult, error)
	CreateDraftAtomic(ctx context.Context, command AtomicInvoiceDraft) (*AtomicInvoiceDraftResult, error)
}

type PaymentRepository interface {
	Create(ctx context.Context, payment *models.Payment) error
	GetByID(ctx context.Context, id, businessID string) (*models.Payment, error)
	GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Payment, int64, error)
	GetByInvoiceID(ctx context.Context, invoiceID string, page, limit int) ([]*models.Payment, int64, error)
	Update(ctx context.Context, payment *models.Payment) error
	Delete(ctx context.Context, id string) error
}

type DocumentRepository interface {
	Create(ctx context.Context, document *models.Document) error
	GetByID(ctx context.Context, id, businessID string) (*models.Document, error)
	GetByIDInternal(ctx context.Context, id string) (*models.Document, error)
	ListByType(ctx context.Context, businessID, documentType string, page, limit int) ([]*models.Document, int64, error)
	Update(ctx context.Context, document *models.Document) error
	UpdateDraft(ctx context.Context, document *models.Document) error
	UpdatePDF(ctx context.Context, documentID, pdfURL, filename string) error
	Delete(ctx context.Context, id string) error
	CreateLink(ctx context.Context, link *models.DocumentLink) error
	ListLinks(ctx context.Context, businessID, documentID string) ([]*models.DocumentLink, error)
	CreateRenderProfile(ctx context.Context, profile *models.RenderProfile) error
	ListRenderProfiles(ctx context.Context, businessID string, page, limit int) ([]*models.RenderProfile, int64, error)
	GetRenderProfile(ctx context.Context, businessID, id string) (*models.RenderProfile, error)
	GetDefaultRenderProfile(ctx context.Context, businessID string) (*models.RenderProfile, error)
	UpdateRenderProfile(ctx context.Context, profile *models.RenderProfile) error
	DeleteRenderProfile(ctx context.Context, businessID, id string) error
	CreateRenderJob(ctx context.Context, job *models.DocumentRenderJob) error
	GetRenderJob(ctx context.Context, businessID, jobID string) (*models.DocumentRenderJob, error)
	GetLatestRenderJob(ctx context.Context, documentID string) (*models.DocumentRenderJob, error)
	UpdateRenderJob(ctx context.Context, job *models.DocumentRenderJob) error
	CreateRevision(ctx context.Context, revision *models.DocumentRevision) error
	ListRevisions(ctx context.Context, businessID, documentID string, page, limit int) ([]*models.DocumentRevision, int64, error)
}

type InvoiceRenderReadRepository interface {
	GetInvoiceRenderJob(ctx context.Context, businessID, invoiceID, jobID string) (*models.DocumentRenderJob, error)
	GetCompletedFinalRenderJob(ctx context.Context, businessID, invoiceID string, sourceVersion int) (*models.DocumentRenderJob, error)
}

type PreviewRenderRepository interface {
	ClaimPreviewRender(
		ctx context.Context,
		businessID, jobID string,
		sourceVersion int,
		owner string,
		now, leaseUntil time.Time,
	) (PreviewRenderClaimState, error)
	ObsoletePreviewRender(ctx context.Context, businessID, jobID, owner string) error
	FailPreviewRender(ctx context.Context, businessID, jobID, owner, errorMessage string) error
	CompletePreviewRender(
		ctx context.Context,
		businessID, jobID string,
		sourceVersion int,
		owner string,
		claimedObjectKey, selectedObjectKey, filename string,
	) (bool, error)
}

type PreviewRenderClaimState string

const (
	PreviewRenderClaimed           PreviewRenderClaimState = "claimed"
	PreviewRenderAlreadyProcessing PreviewRenderClaimState = "processing"
	PreviewRenderAlreadyCompleted  PreviewRenderClaimState = "completed"
	PreviewRenderAlreadyObsolete   PreviewRenderClaimState = "obsolete"
)

type FinalRenderClaimState string

const (
	FinalRenderClaimed           FinalRenderClaimState = "claimed"
	FinalRenderAlreadyProcessing FinalRenderClaimState = "processing"
	FinalRenderAlreadyCompleted  FinalRenderClaimState = "completed"
	FinalRenderAlreadyObsolete   FinalRenderClaimState = "obsolete"
)

type RenderLeaseRepository interface {
	VerifyRenderLease(ctx context.Context, businessID, jobID string, kind models.RenderKind, owner string, now time.Time) error
}

type FinalRenderRepository interface {
	ClaimFinalRender(
		ctx context.Context,
		businessID, jobID string,
		sourceVersion int,
		owner string,
		now, leaseUntil time.Time,
	) (FinalRenderClaimState, error)
	LoadFinalRenderSnapshot(
		ctx context.Context,
		businessID, invoiceID, jobID string,
		sourceVersion int,
	) (*models.Document, error)
	FailFinalRender(ctx context.Context, businessID, jobID, owner, errorMessage string) error
	CompleteFinalRender(
		ctx context.Context,
		businessID, invoiceID, jobID string,
		sourceVersion int,
		owner string,
		objectKey, filename string,
	) (bool, error)
}

type LedgerRepository interface {
	Create(ctx context.Context, entry *models.LedgerEntry) error
	GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.LedgerEntry, int64, error)
	GetBalance(ctx context.Context, businessID string) (float64, error)
}

type JournalRepository interface {
	Create(ctx context.Context, journal *models.Journal) error
	GetByID(ctx context.Context, id, businessID string) (*models.Journal, error)
	ListByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Journal, int64, error)
	Update(ctx context.Context, journal *models.Journal) error
	Delete(ctx context.Context, id string) error
}

type InventoryRepository interface {
	CreateWarehouse(ctx context.Context, warehouse *models.Warehouse) error
	GetWarehouseByID(ctx context.Context, id, businessID string) (*models.Warehouse, error)
	GetDefaultWarehouse(ctx context.Context, businessID string) (*models.Warehouse, error)
	ListWarehouses(ctx context.Context, businessID string) ([]*models.Warehouse, error)
	CreateStockMove(ctx context.Context, move *models.StockMove) error
	CreateReservation(ctx context.Context, reservation *models.InventoryReservation) error
	ReleaseReservationsByDocument(ctx context.Context, documentID string) error
}

type ShippingRepository interface {
	CreateShipment(ctx context.Context, shipment *models.Shipment) error
	GetShipmentByDocument(ctx context.Context, businessID, documentID string) (*models.Shipment, error)
	UpdateShipment(ctx context.Context, shipment *models.Shipment) error
	CreateShippingLabel(ctx context.Context, label *models.ShippingLabel) error
	GetShippingLabelByDocument(ctx context.Context, businessID, documentID string) (*models.ShippingLabel, error)
	UpdateShippingLabel(ctx context.Context, label *models.ShippingLabel) error
}

type TeamMemberRepository interface {
	Create(ctx context.Context, member *models.TeamMember) error
	GetByID(ctx context.Context, id, businessID string) (*models.TeamMember, error)
	GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.TeamMember, int64, error)
	Update(ctx context.Context, member *models.TeamMember) error
	Delete(ctx context.Context, id string) error
	GetByUserID(ctx context.Context, userID string) ([]*models.TeamMember, error)
}

type WebhookRepository interface {
	Create(ctx context.Context, webhook *models.Webhook) error
	GetByID(ctx context.Context, id, businessID string) (*models.Webhook, error)
	GetByBusinessID(ctx context.Context, businessID string) ([]*models.Webhook, error)
	Update(ctx context.Context, webhook *models.Webhook) error
	Delete(ctx context.Context, id string) error
}

type SubscriptionRepository interface {
	Create(ctx context.Context, subscription *models.Subscription) error
	GetByID(ctx context.Context, id, businessID string) (*models.Subscription, error)
	GetByBusinessID(ctx context.Context, businessID string) (*models.Subscription, error)
	Update(ctx context.Context, subscription *models.Subscription) error
}
