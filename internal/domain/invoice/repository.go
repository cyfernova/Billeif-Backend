package invoice

import (
	"context"
	"time"

	"github.com/skythrill256/invoice-backend/internal/utils"
)

type Repository interface {
	Create(ctx context.Context, invoice *Invoice) error
	GetByID(ctx context.Context, id string) (*Invoice, *utils.AppError)
	List(ctx context.Context, filter ListInvoicesFilter, page, limit int) ([]*Invoice, int, *utils.AppError)
	Update(ctx context.Context, invoice *Invoice) *utils.AppError
	Delete(ctx context.Context, id string) *utils.AppError
	GetByClientID(ctx context.Context, clientID string, page, limit int) ([]*Invoice, int, *utils.AppError)
	GetByInvoiceNumber(ctx context.Context, invoiceNumber string) (*Invoice, *utils.AppError)
	UpdateStatus(ctx context.Context, id string, status InvoiceStatus) *utils.AppError
}

type Service interface {
	CreateInvoice(ctx context.Context, req *CreateInvoiceRequest) (*Invoice, *utils.AppError)
	GetInvoice(ctx context.Context, id string) (*Invoice, *utils.AppError)
	ListInvoices(ctx context.Context, filter ListInvoicesFilter, page, limit int) ([]*Invoice, int, *utils.AppError)
	UpdateInvoice(ctx context.Context, id string, req *UpdateInvoiceRequest) (*Invoice, *utils.AppError)
	DeleteInvoice(ctx context.Context, id string) *utils.AppError
	SendInvoice(ctx context.Context, id string, req *SendInvoiceRequest) *utils.AppError
	AddItem(ctx context.Context, invoiceID string, req *AddItemRequest) (*Invoice, *utils.AppError)
	UpdateItem(ctx context.Context, invoiceID, itemID string, req *UpdateItemRequest) (*Invoice, *utils.AppError)
	DeleteItem(ctx context.Context, invoiceID, itemID string) (*Invoice, *utils.AppError)
	GeneratePDF(ctx context.Context, invoice *Invoice) ([]byte, *utils.AppError)
}
