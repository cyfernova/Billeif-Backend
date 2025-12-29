package handlers

import (
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"
)

type Handler struct {
	Auth         *AuthHandler
	Business     *BusinessHandler
	Customer     *CustomerHandler
	Vendor       *VendorHandler
	Product      *ProductHandler
	Invoice      *InvoiceHandler
	Payment      *PaymentHandler
	Ledger       *LedgerHandler
	Team         *TeamHandler
	Webhook      *WebhookHandler
	Subscription *SubscriptionHandler
	Health       *HealthHandler
	Admin        *AdminHandler
}

func New(svcs *services.Container, log *logger.Logger) *Handler {
	return &Handler{
		Auth:         NewAuthHandler(svcs.Auth, log),
		Business:     NewBusinessHandler(svcs.Business, log),
		Customer:     NewCustomerHandler(svcs.Customer, log),
		Vendor:       NewVendorHandler(svcs.Vendor, log),
		Product:      NewProductHandler(svcs.Product, log),
		Invoice:      NewInvoiceHandler(svcs.Invoice, log),
		Payment:      NewPaymentHandler(svcs.Payment, log),
		Ledger:       NewLedgerHandler(svcs.Ledger, log),
		Team:         NewTeamHandler(svcs.Team, log),
		Webhook:      NewWebhookHandler(svcs.Webhook, log),
		Subscription: NewSubscriptionHandler(svcs.Subscription, log),
		Health:       NewHealthHandler(log),
		Admin:        NewAdminHandler(svcs.Email, log),
	}
}
