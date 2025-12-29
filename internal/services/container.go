package services

import (
	"invoice-backend/internal/config"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"
)

type Container struct {
	Auth         *AuthService
	Business     *BusinessService
	Customer     *CustomerService
	Vendor       *VendorService
	Product      *ProductService
	Invoice      *InvoiceService
	Payment      *PaymentService
	Ledger       *LedgerService
	Team         *TeamService
	Webhook      *WebhookService
	Subscription *SubscriptionService
	S3           *S3Service
	Email        *EmailService
}

func NewContainer(
	cfg *config.Config,
	userRepo interfaces.UserRepository,
	businessRepo interfaces.BusinessRepository,
	customerRepo interfaces.CustomerRepository,
	vendorRepo interfaces.VendorRepository,
	productRepo interfaces.ProductRepository,
	invoiceRepo interfaces.InvoiceRepository,
	paymentRepo interfaces.PaymentRepository,
	ledgerRepo interfaces.LedgerRepository,
	teamRepo interfaces.TeamMemberRepository,
	webhookRepo interfaces.WebhookRepository,
	subscriptionRepo interfaces.SubscriptionRepository,
	aws *awsclients.Config,
	log *logger.Logger,
) *Container {
	s3Svc := NewS3Service(cfg, aws, log)
	emailSvc := NewEmailService(cfg, aws, s3Svc, log)

	return &Container{
		Auth:         NewAuthService(cfg, userRepo, aws, emailSvc, log),
		Business:     NewBusinessService(businessRepo, s3Svc, log),
		Customer:     NewCustomerService(customerRepo, log),
		Vendor:       NewVendorService(vendorRepo, log),
		Product:      NewProductService(productRepo, s3Svc, log),
		Invoice:      NewInvoiceService(cfg, invoiceRepo, productRepo, customerRepo, aws, s3Svc, emailSvc, log),
		Payment:      NewPaymentService(paymentRepo, invoiceRepo, log),
		Ledger:       NewLedgerService(ledgerRepo, log),
		Team:         NewTeamService(teamRepo, log),
		Webhook:      NewWebhookService(webhookRepo, log),
		Subscription: NewSubscriptionService(subscriptionRepo, log),
		S3:           s3Svc,
		Email:        emailSvc,
	}
}
