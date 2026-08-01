package invoiceresolution

import (
	"context"
	"fmt"
)

type ReferenceKind string

const (
	ReferenceProduct   ReferenceKind = "product"
	ReferenceVariant   ReferenceKind = "variant"
	ReferenceWarehouse ReferenceKind = "warehouse"
	ReferenceCatalogue ReferenceKind = "catalogue"
	ReferencePriceList ReferenceKind = "price_list"
)

type MissingReferenceError struct {
	Kind ReferenceKind
	ID   string
}

func (e *MissingReferenceError) Error() string {
	return fmt.Sprintf("%s reference not found: %s", e.Kind, e.ID)
}

type InvalidReferenceError struct {
	Kind   ReferenceKind
	ID     string
	Reason string
}

func (e *InvalidReferenceError) Error() string {
	return fmt.Sprintf("invalid %s reference %s: %s", e.Kind, e.ID, e.Reason)
}

type UnavailableError struct{}

func (*UnavailableError) Error() string {
	return "invoice line resolution is temporarily unavailable"
}

type LineReference struct {
	ProductID   string
	VariantID   string
	WarehouseID string
}

type Request struct {
	BusinessID  string
	PriceListID string
	Lines       []LineReference
}

type LineSnapshot struct {
	ProductID   string
	VariantID   string
	WarehouseID string
	CatalogueID string
	PriceListID string
	ProductName string
	SKU         string
	HSNSACCode  string
	UQCCode     string
	Unit        string
	UnitPrice   float64
	MRP         float64
	CessRate    float64
}

type Resolver interface {
	ResolveInvoiceLines(ctx context.Context, request Request) ([]LineSnapshot, error)
}
