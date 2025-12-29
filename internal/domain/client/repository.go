package client

import (
	"context"

	"github.com/skythrill256/invoice-backend/internal/utils"
)

type Repository interface {
	Create(ctx context.Context, client *Client) error
	GetByID(ctx context.Context, id string) (*Client, *utils.AppError)
	GetByEmail(ctx context.Context, email string) (*Client, *utils.AppError)
	List(ctx context.Context, page, limit int) ([]*Client, int, *utils.AppError)
	Update(ctx context.Context, client *Client) *utils.AppError
	Delete(ctx context.Context, id string) *utils.AppError
}

type Service interface {
	CreateClient(ctx context.Context, req *CreateClientRequest) (*Client, *utils.AppError)
	GetClient(ctx context.Context, id string) (*Client, *utils.AppError)
	ListClients(ctx context.Context, page, limit int) ([]*Client, int, *utils.AppError)
	UpdateClient(ctx context.Context, id string, req *UpdateClientRequest) (*Client, *utils.AppError)
	DeleteClient(ctx context.Context, id string) *utils.AppError
	UploadLogo(ctx context.Context, clientID string, filename string, data []byte) (*Client, *utils.AppError)
	GetClientInvoices(ctx context.Context, clientID string, page, limit int) (interface{}, int, *utils.AppError)
}
