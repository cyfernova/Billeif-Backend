package models

import (
	"errors"
	"testing"
	"time"
)

func TestInvoiceValidateStateAcceptsAllSupportedOrigins(t *testing.T) {
	customerID := "customer-1"
	anonymousBuyer := PartySnapshot{Name: "Counter sale"}

	tests := []struct {
		name       string
		origin     InvoiceOrigin
		customerID *string
		buyer      PartySnapshot
	}{
		{name: "manual customer", origin: InvoiceOriginManual, customerID: &customerID},
		{name: "manual anonymous", origin: InvoiceOriginManual, buyer: anonymousBuyer},
		{name: "pos customer", origin: InvoiceOriginPOS, customerID: &customerID},
		{name: "pos anonymous", origin: InvoiceOriginPOS, buyer: anonymousBuyer},
		{name: "storefront", origin: InvoiceOriginStorefront, customerID: &customerID},
		{name: "subscription", origin: InvoiceOriginSubscription, customerID: &customerID},
		{name: "conversion", origin: InvoiceOriginConversion, customerID: &customerID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invoice := Invoice{
				Origin:        tt.origin,
				CustomerID:    tt.customerID,
				BuyerSnapshot: tt.buyer,
				Status:        InvoiceStatusDraft,
				Version:       1,
			}
			if err := invoice.ValidateState(); err != nil {
				t.Fatalf("ValidateState() error = %v", err)
			}
			if !invoice.IsEditableDraft() {
				t.Fatal("valid unnumbered draft must be editable")
			}
		})
	}
}

func TestInvoiceValidateStateRejectsInvalidOriginAndCustomerCombinations(t *testing.T) {
	blankCustomerID := " "
	tests := []struct {
		name    string
		invoice Invoice
		want    error
	}{
		{
			name:    "unknown origin",
			invoice: Invoice{Origin: "import", Status: InvoiceStatusDraft, Version: 1},
			want:    ErrInvalidInvoiceOrigin,
		},
		{
			name:    "anonymous manual without snapshot",
			invoice: Invoice{Origin: InvoiceOriginManual, Status: InvoiceStatusDraft, Version: 1},
			want:    ErrInvoicePartySnapshotRequired,
		},
		{
			name:    "anonymous pos without snapshot",
			invoice: Invoice{Origin: InvoiceOriginPOS, Status: InvoiceStatusDraft, Version: 1},
			want:    ErrInvoicePartySnapshotRequired,
		},
		{
			name:    "blank manual customer without snapshot",
			invoice: Invoice{Origin: InvoiceOriginManual, CustomerID: &blankCustomerID, Status: InvoiceStatusDraft, Version: 1},
			want:    ErrInvoicePartySnapshotRequired,
		},
		{
			name:    "storefront without customer",
			invoice: Invoice{Origin: InvoiceOriginStorefront, Status: InvoiceStatusDraft, Version: 1},
			want:    ErrInvoiceCustomerRequired,
		},
		{
			name:    "subscription without customer",
			invoice: Invoice{Origin: InvoiceOriginSubscription, Status: InvoiceStatusDraft, Version: 1},
			want:    ErrInvoiceCustomerRequired,
		},
		{
			name:    "conversion without customer",
			invoice: Invoice{Origin: InvoiceOriginConversion, Status: InvoiceStatusDraft, Version: 1},
			want:    ErrInvoiceCustomerRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.invoice.ValidateState()
			if !errors.Is(err, tt.want) {
				t.Fatalf("ValidateState() error = %v, want errors.Is(_, %v)", err, tt.want)
			}
			var stateErr *InvalidInvoiceStateError
			if !errors.As(err, &stateErr) {
				t.Fatalf("ValidateState() error type = %T, want *InvalidInvoiceStateError", err)
			}
		})
	}
}

func TestInvoiceValidateStateEnforcesDraftAndIssuedLifecycle(t *testing.T) {
	customerID := "customer-1"
	invoiceNo := "INV-2026-000001"
	issuedAt := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	party := PartySnapshot{Name: "Legal party"}

	tests := []struct {
		name    string
		invoice Invoice
		valid   bool
	}{
		{
			name:    "unnumbered draft",
			invoice: Invoice{Origin: InvoiceOriginManual, CustomerID: &customerID, Status: InvoiceStatusDraft, Version: 1},
			valid:   true,
		},
		{
			name:    "numbered draft",
			invoice: Invoice{Origin: InvoiceOriginManual, CustomerID: &customerID, InvoiceNo: &invoiceNo, Status: InvoiceStatusDraft, Version: 1},
		},
		{
			name:    "draft with issued timestamp",
			invoice: Invoice{Origin: InvoiceOriginManual, CustomerID: &customerID, Status: InvoiceStatusDraft, IssuedAt: &issuedAt, Version: 1},
		},
		{
			name: "issued complete snapshot",
			invoice: Invoice{
				Origin: InvoiceOriginManual, CustomerID: &customerID, InvoiceNo: &invoiceNo,
				Status: InvoiceStatusIssued, IssuedAt: &issuedAt, Version: 2,
				SellerSnapshot: party, BuyerSnapshot: party,
			},
			valid: true,
		},
		{
			name: "issued without number",
			invoice: Invoice{
				Origin: InvoiceOriginManual, CustomerID: &customerID, Status: InvoiceStatusIssued,
				IssuedAt: &issuedAt, Version: 2, SellerSnapshot: party, BuyerSnapshot: party,
			},
		},
		{
			name: "issued without timestamp",
			invoice: Invoice{
				Origin: InvoiceOriginManual, CustomerID: &customerID, InvoiceNo: &invoiceNo,
				Status: InvoiceStatusIssued, Version: 2, SellerSnapshot: party, BuyerSnapshot: party,
			},
		},
		{
			name: "issued without seller snapshot",
			invoice: Invoice{
				Origin: InvoiceOriginManual, CustomerID: &customerID, InvoiceNo: &invoiceNo,
				Status: InvoiceStatusIssued, IssuedAt: &issuedAt, Version: 2, BuyerSnapshot: party,
			},
		},
		{
			name: "issued without buyer snapshot",
			invoice: Invoice{
				Origin: InvoiceOriginManual, CustomerID: &customerID, InvoiceNo: &invoiceNo,
				Status: InvoiceStatusIssued, IssuedAt: &issuedAt, Version: 2, SellerSnapshot: party,
			},
		},
		{
			name:    "invalid version",
			invoice: Invoice{Origin: InvoiceOriginManual, CustomerID: &customerID, Status: InvoiceStatusDraft},
		},
		{
			name: "unknown lifecycle status",
			invoice: Invoice{
				Origin: InvoiceOriginManual, CustomerID: &customerID, InvoiceNo: &invoiceNo,
				Status: "archived", IssuedAt: &issuedAt, Version: 2,
				SellerSnapshot: party, BuyerSnapshot: party,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.invoice.ValidateState()
			if tt.valid && err != nil {
				t.Fatalf("ValidateState() error = %v", err)
			}
			if !tt.valid && !errors.Is(err, ErrInvalidInvoiceLifecycle) {
				t.Fatalf("ValidateState() error = %v, want lifecycle error", err)
			}
		})
	}
}

func TestDocumentRenderJobValidateKind(t *testing.T) {
	for _, kind := range []RenderKind{RenderKindPreview, RenderKindFinal} {
		job := DocumentRenderJob{Kind: kind}
		if err := job.ValidateKind(); err != nil {
			t.Errorf("ValidateKind(%q) error = %v", kind, err)
		}
	}
	job := DocumentRenderJob{Kind: "public"}
	if err := job.ValidateKind(); !errors.Is(err, ErrInvalidRenderKind) {
		t.Fatalf("ValidateKind() error = %v, want ErrInvalidRenderKind", err)
	}
}

func TestCanonicalPersistenceModelsMapExpectedTables(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "document sequence", got: (DocumentSequence{}).TableName(), want: "document_sequences"},
		{name: "idempotency key", got: (APIIdempotencyKey{}).TableName(), want: "api_idempotency_keys"},
		{name: "outbox event", got: (OutboxEvent{}).TableName(), want: "outbox_events"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("TableName() = %q, want %q", tt.got, tt.want)
			}
		})
	}
}
