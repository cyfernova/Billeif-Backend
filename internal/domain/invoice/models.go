package invoice

import (
	"time"

	"github.com/google/uuid"
)

type InvoiceStatus string

const (
	StatusDraft     InvoiceStatus = "draft"
	StatusSent      InvoiceStatus = "sent"
	StatusPaid      InvoiceStatus = "paid"
	StatusOverdue   InvoiceStatus = "overdue"
	StatusCancelled InvoiceStatus = "cancelled"
)

type Invoice struct {
	ID            string        `json:"id"`
	ClientID      string        `json:"client_id"`
	InvoiceNumber string        `json:"invoice_number"`
	Status        InvoiceStatus `json:"status"`
	Subtotal      float64       `json:"subtotal"`
	TaxAmount     float64       `json:"tax_amount"`
	Total         float64       `json:"total"`
	Currency      string        `json:"currency"`
	DueDate       time.Time     `json:"due_date"`
	PaidDate      *time.Time    `json:"paid_date,omitempty"`
	Items         []InvoiceItem `json:"items"`
	Notes         string        `json:"notes,omitempty"`
	PDFS3Key      string        `json:"pdf_s3_key,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type InvoiceItem struct {
	ID          string  `json:"id"`
	Description string  `json:"description"`
	Quantity    int     `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	Total       float64 `json:"total"`
}

type CreateInvoiceRequest struct {
	ClientID      string        `json:"client_id" binding:"required"`
	InvoiceNumber string        `json:"invoice_number"`
	DueDate       time.Time     `json:"due_date" binding:"required"`
	Currency      string        `json:"currency"`
	Items         []InvoiceItem `json:"items" binding:"required,min=1"`
	Notes         string        `json:"notes"`
}

type UpdateInvoiceRequest struct {
	Status   *InvoiceStatus `json:"status"`
	DueDate  *time.Time     `json:"due_date"`
	Notes    *string        `json:"notes"`
	PaidDate *time.Time     `json:"paid_date"`
}

type ListInvoicesFilter struct {
	ClientID string
	Status   InvoiceStatus
	From     *time.Time
	To       *time.Time
}

type SendInvoiceRequest struct {
	ToEmail string `json:"to_email" binding:"required,email"`
	Message string `json:"message"`
}

type AddItemRequest struct {
	Description string  `json:"description" binding:"required"`
	Quantity    int     `json:"quantity" binding:"required,min=1"`
	UnitPrice   float64 `json:"unit_price" binding:"required,gt=0"`
}

type UpdateItemRequest struct {
	Description *string  `json:"description"`
	Quantity    *int     `json:"quantity" binding:"omitempty,min=1"`
	UnitPrice   *float64 `json:"unit_price" binding:"omitempty,gt=0"`
}

func NewInvoice(clientID, invoiceNumber string, dueDate time.Time, items []InvoiceItem, currency, notes string) *Invoice {
	id := uuid.New().String()
	now := time.Now()

	subtotal := calculateSubtotal(items)
	taxAmount := subtotal * 0.0
	total := subtotal + taxAmount

	if currency == "" {
		currency = "USD"
	}

	return &Invoice{
		ID:            id,
		ClientID:      clientID,
		InvoiceNumber: invoiceNumber,
		Status:        StatusDraft,
		Subtotal:      subtotal,
		TaxAmount:     taxAmount,
		Total:         total,
		Currency:      currency,
		DueDate:       dueDate,
		Items:         items,
		Notes:         notes,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func NewInvoiceItem(description string, quantity int, unitPrice float64) InvoiceItem {
	return InvoiceItem{
		ID:          uuid.New().String(),
		Description: description,
		Quantity:    quantity,
		UnitPrice:   unitPrice,
		Total:       float64(quantity) * unitPrice,
	}
}

func calculateSubtotal(items []InvoiceItem) float64 {
	var subtotal float64
	for _, item := range items {
		subtotal += item.Total
	}
	return subtotal
}

func (i *Invoice) RecalculateTotals() {
	i.Subtotal = calculateSubtotal(i.Items)
	i.TaxAmount = i.Subtotal * 0.0
	i.Total = i.Subtotal + i.TaxAmount
	i.UpdatedAt = time.Now()
}

func (i *Invoice) IsOverdue() bool {
	return i.Status != StatusPaid && i.Status != StatusCancelled && time.Now().After(i.DueDate)
}

func (i *Invoice) CanSend() bool {
	return i.Status == StatusDraft && len(i.Items) > 0
}

func (i *Invoice) CanPay() bool {
	return i.Status == StatusSent || i.Status == StatusOverdue
}
