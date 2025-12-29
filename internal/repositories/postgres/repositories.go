package postgres

import (
	"invoice-backend/internal/repositories/interfaces"
)

type Repositories struct {
	User         interfaces.UserRepository
	Business     interfaces.BusinessRepository
	Customer     interfaces.CustomerRepository
	Vendor       interfaces.VendorRepository
	Product      interfaces.ProductRepository
	Invoice      interfaces.InvoiceRepository
	Payment      interfaces.PaymentRepository
	Ledger       interfaces.LedgerRepository
	TeamMember   interfaces.TeamMemberRepository
	Webhook      interfaces.WebhookRepository
	Subscription interfaces.SubscriptionRepository
}

func NewRepositories(userRepo interfaces.UserRepository, businessRepo interfaces.BusinessRepository, customerRepo interfaces.CustomerRepository, vendorRepo interfaces.VendorRepository, productRepo interfaces.ProductRepository, invoiceRepo interfaces.InvoiceRepository, paymentRepo interfaces.PaymentRepository, ledgerRepo interfaces.LedgerRepository, teamRepo interfaces.TeamMemberRepository, webhookRepo interfaces.WebhookRepository, subscriptionRepo interfaces.SubscriptionRepository) *Repositories {
	return &Repositories{
		User:         userRepo,
		Business:     businessRepo,
		Customer:     customerRepo,
		Vendor:       vendorRepo,
		Product:      productRepo,
		Invoice:      invoiceRepo,
		Payment:      paymentRepo,
		Ledger:       ledgerRepo,
		TeamMember:   teamRepo,
		Webhook:      webhookRepo,
		Subscription: subscriptionRepo,
	}
}
