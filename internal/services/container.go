package services

import (
	"invoice-backend/internal/config"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/ap2"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/razorpay"

	"gorm.io/gorm"
)

type Container struct {
	Auth               *AuthService
	BusinessAuth       *BusinessAuthService
	Business           *BusinessService
	Customer           *CustomerService
	Vendor             *VendorService
	Product            *ProductService
	Invoice            *InvoiceService
	Payment            *PaymentService
	Ledger             *LedgerService
	Team               *TeamService
	Webhook            *WebhookService
	Subscription       *SubscriptionService
	S3                 *S3Service
	Email              *EmailService
	Agent              *AgentService
	ShoppingAgent      *ShoppingAgentService
	MerchantAgent      *MerchantAgentService
	CredentialProvider *CredentialProviderService
	PaymentProcessor   *PaymentProcessorService
	Marketplace        *MarketplaceService
	ProductMatching    *ProductMatchingService
	IntentProcessing   *IntentProcessingService
	Razorpay           *razorpay.RazorpayService
	AgentDiscovery     *AgentDiscoveryService
	LLM                *LLMService
	A2ATask            *A2ATaskService
	A2APush            *A2APushService
	Workflow           *WorkflowService
	Mentee             *MenteeService
	Bargaining         *BargainingService
}

func NewContainer(
	cfg *config.Config,
	db *gorm.DB,
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
	ap2Repo interfaces.AP2Repository,
	aws *awsclients.Config,
	log *logger.Logger,
) *Container {
	s3Svc := NewS3Service(cfg, aws, log)
	emailSvc := NewEmailService(cfg, aws, s3Svc, log)
	ap2Signer, _ := ap2.NewSignatureService()
	ap2MandateSigner := ap2.NewMandateSigner(ap2Signer)
	ap2MandateVerifier := ap2.NewMandateVerifier()
	ap2MandateSvc := ap2.NewMandateService(ap2MandateSigner, ap2MandateVerifier)
	a2aSigner, _ := ap2.NewSignatureService()
	a2aClient := a2a.NewA2AClient(a2aSigner, log)
	razorpaySvc := razorpay.NewRazorpayService(&razorpay.Config{
		Key:           cfg.Razorpay.Key,
		Secret:        cfg.Razorpay.Secret,
		WebhookSecret: cfg.Razorpay.WebhookSecret,
	}, log)

	marketplaceSvc := NewMarketplaceService(ap2Repo, log)
	productMatchingSvc := NewProductMatchingService(marketplaceSvc, log)
	intentProcessingSvc, _ := NewIntentProcessingService(productMatchingSvc, marketplaceSvc, log)
	llmSvc := NewLLMService(cfg.LLM, log)

	agentSvc := NewAgentService(ap2Repo, ap2Signer, log)
	menteeSvc := NewMenteeService(log)

	a2aPushSvc := NewA2APushService(db, log)
	a2aTaskSvc := NewA2ATaskService(db, log, a2aPushSvc)
	workflowSvc := NewWorkflowService(db, log, emailSvc, a2aPushSvc)
	bargainingSvc := NewBargainingService(ap2Repo, a2aClient, agentSvc, menteeSvc, log)

	return &Container{
		Auth:               NewAuthService(cfg, userRepo, aws, emailSvc, s3Svc, log),
		BusinessAuth:       NewBusinessAuthService(businessRepo, teamRepo, log),
		Business:           NewBusinessService(businessRepo, s3Svc, log),
		Customer:           NewCustomerService(customerRepo, log),
		Vendor:             NewVendorService(vendorRepo, log),
		Product:            NewProductService(productRepo, s3Svc, log),
		Invoice:            NewInvoiceService(cfg, invoiceRepo, productRepo, customerRepo, aws, s3Svc, emailSvc, log),
		Payment:            NewPaymentService(db, paymentRepo, invoiceRepo, log),
		Ledger:             NewLedgerService(ledgerRepo, log),
		Team:               NewTeamService(teamRepo, log),
		Webhook:            NewWebhookService(webhookRepo, log),
		Subscription:       NewSubscriptionService(subscriptionRepo, log),
		S3:                 s3Svc,
		Email:              emailSvc,
		Agent:              agentSvc,
		ShoppingAgent:      NewShoppingAgentService(ap2Repo, agentSvc, intentProcessingSvc, ap2Signer, ap2MandateSvc, a2aClient, log),
		MerchantAgent:      NewMerchantAgentService(ap2Repo, log),
		CredentialProvider: NewCredentialProviderService(ap2Repo, log),
		PaymentProcessor:   NewPaymentProcessorService(ap2Repo, log),
		Marketplace:        marketplaceSvc,
		ProductMatching:    productMatchingSvc,
		IntentProcessing:   intentProcessingSvc,
		Razorpay:           razorpaySvc,
		AgentDiscovery:     NewAgentDiscoveryService(ap2Repo, log),
		LLM:                llmSvc,
		A2ATask:            a2aTaskSvc,
		A2APush:            a2aPushSvc,
		Workflow:           workflowSvc,
		Mentee:             menteeSvc,
		Bargaining:         bargainingSvc,
	}
}
