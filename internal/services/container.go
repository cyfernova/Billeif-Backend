package services

import (
	"invoice-backend/internal/config"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/ap2"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"gorm.io/gorm"
)

type Container struct {
	Auth                *AuthService
	BusinessAuth        *BusinessAuthService
	Business            *BusinessService
	Customer            *CustomerService
	Vendor              *VendorService
	Product             *ProductService
	Project             *ProjectService
	Inventory           *InventoryService
	Document            *DocumentService
	Journal             *JournalService
	Shipping            *ShippingService
	Invoice             *InvoiceService
	BillingOps          *BillingOpsService
	Payment             *PaymentService
	RazorpayPayment     *RazorpayPaymentService
	Ledger              *LedgerService
	Dashboard           *DashboardService
	Report              *ReportService
	TaxCompliance       *TaxComplianceService
	Team                *TeamService
	Webhook             *WebhookService
	Subscription        *SubscriptionService
	Commerce            *CommerceService
	Barcode             *BarcodeService
	POS                 *POSService
	S3                  *S3Service
	Email               *EmailService
	Agent               *AgentService
	ShoppingAgent       *ShoppingAgentService
	MerchantAgent       *MerchantAgentService
	CredentialProvider  *CredentialProviderService
	PaymentProcessor    *PaymentProcessorService
	Marketplace         *MarketplaceService
	ProductMatching     *ProductMatchingService
	IntentProcessing    *IntentProcessingService
	AgentDiscovery      *AgentDiscoveryService
	LLM                 *LLMService
	RealtimeVoice       *RealtimeVoiceService
	A2ATask             *A2ATaskService
	A2APush             *A2APushService
	Workflow            *WorkflowService
	Mentee              *MenteeService
	Bargaining          *BargainingService
	Procurement         *ProcurementService
	AgentConfig         *AgentConfigService
	A2ABargaining       *A2ABargainingService
	WebSocketConnection *WebSocketConnectionService
	AWS                 *awsclients.Config
}

func NewContainer(
	cfg *config.Config,
	db *gorm.DB,
	userRepo interfaces.UserRepository,
	businessRepo interfaces.BusinessRepository,
	customerRepo interfaces.CustomerRepository,
	vendorRepo interfaces.VendorRepository,
	productRepo interfaces.ProductRepository,
	documentRepo interfaces.DocumentRepository,
	journalRepo interfaces.JournalRepository,
	inventoryRepo interfaces.InventoryRepository,
	shippingRepo interfaces.ShippingRepository,
	invoiceRepo interfaces.InvoiceRepository,
	paymentRepo interfaces.PaymentRepository,
	ledgerRepo interfaces.LedgerRepository,
	reportingRepo interfaces.ReportingRepository,
	teamRepo interfaces.TeamMemberRepository,
	webhookRepo interfaces.WebhookRepository,
	subscriptionRepo interfaces.SubscriptionRepository,
	ap2Repo interfaces.AP2Repository,
	aws *awsclients.Config,
	log *logger.Logger,
) *Container {
	s3Svc := NewS3Service(cfg, aws, log)
	emailSvc := NewEmailService(cfg, aws, s3Svc, log).WithDB(db)
	ap2Signer, _ := ap2.NewSignatureService()
	ap2MandateSigner := ap2.NewMandateSigner(ap2Signer)
	ap2MandateVerifier := ap2.NewMandateVerifier()
	ap2MandateSvc := ap2.NewMandateService(ap2MandateSigner, ap2MandateVerifier)
	a2aSigner, _ := ap2.NewSignatureService()
	a2aClient := a2a.NewA2AClient(a2aSigner, log)
	inventorySvc := NewInventoryService(db, inventoryRepo, productRepo, businessRepo, teamRepo, log)
	journalSvc := NewJournalService(journalRepo, ledgerRepo, log)
	shippingSvc := NewShippingService(cfg, shippingRepo, customerRepo, vendorRepo, log)
	documentSvc := NewDocumentService(db, cfg, documentRepo, businessRepo, customerRepo, vendorRepo, productRepo, inventorySvc, journalSvc, shippingSvc, aws, log)
	barcodeSvc := NewBarcodeService(db, log)
	projectSvc := NewProjectService(db, log)
	reportSvc := NewReportService(cfg, reportingRepo, log)
	marketplaceSvc := NewMarketplaceService(ap2Repo, log)
	productMatchingSvc := NewProductMatchingService(marketplaceSvc, log)
	intentProcessingSvc, _ := NewIntentProcessingService(productMatchingSvc, marketplaceSvc, log)
	llmSvc := NewLLMService(cfg.LLM, log)
	realtimeVoiceSvc := NewRealtimeVoiceService(cfg.VoiceRealtime, log)

	agentSvc := NewAgentService(ap2Repo, productRepo, ap2Signer, log)
	menteeSvc := NewMenteeService(log)

	a2aPushSvc := NewA2APushService(db, ap2Repo, log)
	a2aTaskSvc := NewA2ATaskService(db, log, a2aPushSvc)
	workflowSvc := NewWorkflowService(db, log, emailSvc, a2aPushSvc)
	bargainingSvc := NewBargainingService(ap2Repo, a2aClient, agentSvc, menteeSvc, llmSvc, log)
	merchantAgentSvc := NewMerchantAgentService(ap2Repo, log)
	agentConfigSvc := NewAgentConfigService(".well-known", log)
	sellerNegotiationSvc := NewSellerNegotiationService(ap2Repo, agentConfigSvc, log)
	a2aBargainingSvc := NewA2ABargainingService(a2aClient, bargainingSvc, menteeSvc, ap2Repo, aws.SQS, cfg, log)
	websocketConnectionSvc := NewWebSocketConnectionService(cfg, aws, log)
	credentialProviderSvc, err := NewCredentialProviderService(ap2Repo, cfg.Credentials.EncryptionKey, log)
	if err != nil {
		log.Fatal("failed to initialize credential provider service", "error", err)
	}

	webhookSvc := NewWebhookService(webhookRepo, log)
	taxComplianceSvc := NewTaxComplianceService(cfg, db, businessRepo, customerRepo, vendorRepo, subscriptionRepo, aws, s3Svc, webhookSvc, log)
	invoiceSvc := NewInvoiceService(db, cfg, invoiceRepo, productRepo, customerRepo, documentSvc, aws, s3Svc, emailSvc, log)
	billingOpsSvc := NewBillingOpsService(cfg, db, customerRepo, vendorRepo, productRepo, invoiceSvc, documentSvc, s3Svc, log)
	documentSvc.AttachTaxComplianceService(taxComplianceSvc)
	taxComplianceSvc.AttachDocumentService(documentSvc)
	posSvc := NewPOSService(db, documentSvc, barcodeSvc, taxComplianceSvc.entitlements, log)
	commerceSvc := NewCommerceService(cfg, db, businessRepo, customerRepo, productRepo, subscriptionRepo, inventorySvc, documentSvc, s3Svc, log)
	razorpayPaymentSvc := NewRazorpayPaymentService(cfg, db, log)

	log.Info("service container initialized",
		"components", 34,
		"llm_model", cfg.LLM.Model,
		"workflow_enabled", workflowSvc != nil,
	)

	shoppingAgentSvc := NewShoppingAgentService(ap2Repo, agentSvc, intentProcessingSvc, ap2Signer, ap2MandateSvc, a2aClient, cfg.Server.A2AMessageEndpoint(), log)
	procurementSvc := NewProcurementService(ap2Repo, agentSvc, intentProcessingSvc, shoppingAgentSvc, merchantAgentSvc, bargainingSvc, agentConfigSvc, ap2Signer, log)
	a2aTaskSvc.ConfigureDomainServices(ap2Repo, merchantAgentSvc, sellerNegotiationSvc, ap2Signer)

	return &Container{
		Auth:                NewAuthService(cfg, userRepo, aws, emailSvc, s3Svc, log),
		BusinessAuth:        NewBusinessAuthService(db, businessRepo, teamRepo, log),
		Business:            NewBusinessService(businessRepo, s3Svc, log),
		Customer:            NewCustomerService(customerRepo, log),
		Vendor:              NewVendorService(vendorRepo, log),
		Product:             NewProductService(db, productRepo, s3Svc, inventorySvc, log),
		Project:             projectSvc,
		Inventory:           inventorySvc,
		Document:            documentSvc,
		Journal:             journalSvc,
		Shipping:            shippingSvc,
		Invoice:             invoiceSvc,
		BillingOps:          billingOpsSvc,
		Payment:             NewPaymentService(db, paymentRepo, invoiceRepo, documentSvc, journalSvc, log),
		RazorpayPayment:     razorpayPaymentSvc,
		Ledger:              NewLedgerService(ledgerRepo, log),
		Dashboard:           NewDashboardService(db, log),
		Report:              reportSvc,
		TaxCompliance:       taxComplianceSvc,
		Team:                NewTeamService(teamRepo, log).WithDB(db),
		Webhook:             webhookSvc,
		Subscription:        NewSubscriptionService(subscriptionRepo, log),
		Commerce:            commerceSvc,
		Barcode:             barcodeSvc,
		POS:                 posSvc,
		S3:                  s3Svc,
		Email:               emailSvc,
		Agent:               agentSvc,
		ShoppingAgent:       shoppingAgentSvc,
		MerchantAgent:       merchantAgentSvc,
		CredentialProvider:  credentialProviderSvc,
		PaymentProcessor:    NewPaymentProcessorService(ap2Repo, log),
		Marketplace:         marketplaceSvc,
		ProductMatching:     productMatchingSvc,
		IntentProcessing:    intentProcessingSvc,
		AgentDiscovery:      NewAgentDiscoveryService(ap2Repo, productRepo, llmSvc, log),
		LLM:                 llmSvc,
		RealtimeVoice:       realtimeVoiceSvc,
		A2ATask:             a2aTaskSvc,
		A2APush:             a2aPushSvc,
		Workflow:            workflowSvc,
		Mentee:              menteeSvc,
		Bargaining:          bargainingSvc,
		Procurement:         procurementSvc,
		AgentConfig:         agentConfigSvc,
		A2ABargaining:       a2aBargainingSvc,
		WebSocketConnection: websocketConnectionSvc,
		AWS:                 aws,
	}
}
