package services

import (
	"context"
	"strings"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/invoiceprojection"
	"invoice-backend/internal/models"
)

type canonicalInvoiceCreator interface {
	CreateByBusiness(ctx context.Context, businessID string, input CreateInvoiceInput) (*models.Invoice, error)
}

type canonicalInvoiceIssuerService interface {
	IssueByBusiness(ctx context.Context, businessID, invoiceID string, input IssueInvoiceInput) (*IssueInvoiceResult, error)
}

type salesInvoiceDocumentIssuer interface {
	IssueSalesInvoiceDocument(ctx context.Context, businessID, invoiceID string, input IssueInvoiceInput) (*IssueInvoiceResult, error)
}

type salesInvoiceDocumentCreator interface {
	CreateSalesInvoiceDocument(ctx context.Context, businessID string, input CreateDocumentInput) (*models.Document, error)
	createPOSSalesInvoiceDocument(ctx context.Context, businessID string, input CreateInvoiceInput) (*models.Document, error)
	createStorefrontSalesInvoiceDocument(ctx context.Context, businessID string, input CreateInvoiceInput) (*models.Document, error)
}

type invoiceSalesDocumentIssuer struct {
	invoices canonicalInvoiceIssuerService
}

func newInvoiceSalesDocumentIssuer(invoices canonicalInvoiceIssuerService) salesInvoiceDocumentIssuer {
	return &invoiceSalesDocumentIssuer{invoices: invoices}
}

func (i *invoiceSalesDocumentIssuer) IssueSalesInvoiceDocument(
	ctx context.Context,
	businessID, invoiceID string,
	input IssueInvoiceInput,
) (*IssueInvoiceResult, error) {
	return i.invoices.IssueByBusiness(ctx, businessID, invoiceID, input)
}

type invoiceSalesDocumentCreator struct {
	invoices canonicalInvoiceCreator
}

func newInvoiceSalesDocumentCreator(invoices canonicalInvoiceCreator) salesInvoiceDocumentCreator {
	return &invoiceSalesDocumentCreator{invoices: invoices}
}

func (c *invoiceSalesDocumentCreator) CreateSalesInvoiceDocument(ctx context.Context, businessID string, input CreateDocumentInput) (*models.Document, error) {
	if err := validateSalesInvoiceDelegationInput(businessID, input); err != nil {
		return nil, err
	}
	invoiceInput := CreateInvoiceInput{
		BusinessID:           businessID,
		IdempotencyKey:       input.IdempotencyKey,
		CustomerID:           input.PartyID,
		ProjectID:            input.ProjectID,
		BranchID:             input.BranchID,
		PriceListID:          input.PriceListID,
		RenderProfileID:      input.RenderProfileID,
		InvoiceDate:          input.IssueDate,
		Notes:                input.Notes,
		TermsAndConditions:   input.Terms,
		CustomFields:         input.CustomFields,
		AdditionalCharges:    input.AdditionalCharges,
		OriginSubscriptionID: input.OriginSubscriptionID,
		OriginRunID:          input.OriginRunID,
		TaxProfile: TaxProfileInput{
			GSTTreatment:          input.GSTTreatment,
			PlaceOfSupply:         input.PlaceOfSupply,
			BillOfSupply:          input.BillOfSupply,
			ExportType:            input.ExportType,
			SupplyType:            input.SupplyType,
			CounterpartyGSTIN:     input.PartyGSTIN,
			CounterpartyPAN:       input.PartyPAN,
			CounterpartyStateCode: input.PartyStateCode,
			GenerateEInvoice:      input.GenerateEInvoice,
			GenerateEWayBill:      input.GenerateEWayBill,
			ReverseCharge:         input.ReverseCharge,
			ReverseChargeReason:   input.ReverseChargeReason,
			DispatchFrom:          input.DispatchFrom,
			DispatchTo:            input.DispatchTo,
			DistanceKM:            input.DistanceKM,
			Transporter:           input.Transporter,
			Vehicle:               input.Vehicle,
			MultiVehiclePlan:      input.MultiVehiclePlan,
			TCS:                   input.Withholdings,
			SourceLinkage:         input.SourceLinkage,
			ReportTags:            input.ReportTags,
		},
		Items: make([]CreateInvoiceItemInput, 0, len(input.Lines)),
	}
	if input.DueDate != nil {
		invoiceInput.DueDate = *input.DueDate
	}
	for _, line := range input.Lines {
		invoiceInput.Items = append(invoiceInput.Items, CreateInvoiceItemInput{
			ProductID:        line.ProductID,
			VariantID:        line.VariantID,
			WarehouseID:      line.WarehouseID,
			Description:      line.Description,
			HSNSACCode:       line.HSNSACCode,
			Unit:             firstNonEmpty(line.Unit, line.UQCCode),
			Quantity:         line.Quantity,
			FreeQuantity:     line.FreeQuantity,
			UnitPrice:        line.UnitPrice,
			MRP:              line.MRP,
			Discount:         line.DiscountAmount,
			TaxRate:          line.TaxRate,
			CessRate:         line.CessRate,
			CustomFields:     line.CustomFields,
			ChargeSnapshot:   line.ChargeLinkage,
			BatchAllocations: line.BatchAllocations,
			SerialIDs:        line.SerialIDs,
		})
	}
	return c.createInvoiceDocument(ctx, businessID, invoiceInput)
}

func (c *invoiceSalesDocumentCreator) createPOSSalesInvoiceDocument(ctx context.Context, businessID string, input CreateInvoiceInput) (*models.Document, error) {
	input.Origin = models.InvoiceOriginPOS
	if strings.TrimSpace(input.CustomerID) == "" && input.BuyerSnapshot.IsEmpty() {
		return nil, &idempotency.InvalidPayloadError{}
	}
	return c.createInvoiceDocument(ctx, businessID, input)
}

func (c *invoiceSalesDocumentCreator) createStorefrontSalesInvoiceDocument(ctx context.Context, businessID string, input CreateInvoiceInput) (*models.Document, error) {
	input.Origin = models.InvoiceOriginStorefront
	if strings.TrimSpace(input.CustomerID) == "" || !input.BuyerSnapshot.IsEmpty() {
		return nil, &idempotency.InvalidPayloadError{}
	}
	return c.createInvoiceDocument(ctx, businessID, input)
}

func (c *invoiceSalesDocumentCreator) createInvoiceDocument(ctx context.Context, businessID string, input CreateInvoiceInput) (*models.Document, error) {
	invoice, err := c.invoices.CreateByBusiness(ctx, businessID, input)
	if err != nil {
		return nil, err
	}
	return invoiceDocumentProjection(invoice), nil
}

func validateSalesInvoiceDelegationInput(businessID string, input CreateDocumentInput) error {
	unsupported := (input.BusinessID != "" && input.BusinessID != businessID) ||
		(input.PartyType != "" && input.PartyType != models.DocumentPartyTypeCustomer) ||
		(input.Status != "" && input.Status != models.DocumentStatusDraft) ||
		(input.DraftState != "" && input.DraftState != models.DocumentDraftStateDraft) ||
		input.TaxMode != "" ||
		input.DispatchDate != nil ||
		input.Currency != "" ||
		input.ExchangeRate != 0 ||
		(input.Locale != "" && input.Locale != "en-IN") ||
		input.Declaration != "" ||
		(input.Direction != "" && input.Direction != models.DocumentDirectionOutward) ||
		len(input.ExtraFields) != 0
	if !unsupported {
		for _, line := range input.Lines {
			if len(line.PackingMetadata) != 0 {
				unsupported = true
				break
			}
		}
	}
	if unsupported {
		return &idempotency.InvalidPayloadError{}
	}
	return nil
}

func businessPartySnapshot(business *models.BusinessProfile) models.PartySnapshot {
	if business == nil {
		return models.PartySnapshot{}
	}
	return models.PartySnapshot{
		Name:       business.Name,
		Email:      business.Email,
		Phone:      business.Phone,
		Address:    business.Address,
		City:       business.City,
		State:      business.State,
		Country:    business.Country,
		PostalCode: business.PostalCode,
		TaxID:      business.TaxID,
		GSTIN:      business.GSTIN,
	}
}

func customerPartySnapshot(customer *models.Customer) models.PartySnapshot {
	if customer == nil {
		return models.PartySnapshot{}
	}
	return models.PartySnapshot{
		Name:       customer.Name,
		Email:      customer.Email,
		Phone:      customer.Phone,
		Address:    customer.Address,
		City:       customer.City,
		State:      customer.State,
		Country:    customer.Country,
		PostalCode: customer.PostalCode,
		TaxID:      customer.TaxID,
		GSTIN:      customer.GSTIN,
	}
}

func invoiceDocumentProjection(invoice *models.Invoice) *models.Document {
	document := invoiceprojection.Build(invoice)
	if invoice == nil || invoice.Status == models.InvoiceStatusDraft {
		return document
	}
	document.Status = legacyInvoiceStatusToDocument(invoice.Status)
	document.DraftState = models.DocumentDraftStateFinal
	document.SerialNumber = models.StringValue(invoice.InvoiceNo)
	if invoice.IssuedAt != nil {
		document.IssueDate = *invoice.IssuedAt
	}
	return document
}
