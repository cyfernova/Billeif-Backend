package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/outbox"
	"invoice-backend/internal/repositories/interfaces"
	voicesession "invoice-backend/internal/voice/session"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/ap2"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"gorm.io/gorm"
)

type invoiceIssueStockEffectConfigurer interface {
	ConfigureInvoiceIssueStockEffect(func(context.Context, *gorm.DB, *models.Document) error)
}

func applyCanonicalInvoiceIssueEffects(
	ctx context.Context,
	tx *gorm.DB,
	document *models.Document,
	inventory *InventoryService,
	journals *JournalService,
) error {
	if inventory == nil || journals == nil {
		return fmt.Errorf("canonical invoice issue effects are not configured")
	}
	overrideID := postingLockOverrideFromContext(ctx)
	if err := inventory.ApplyDocumentTxAuthorized(ctx, tx, document, overrideID); err != nil {
		return err
	}
	journal, err := journals.CreateAutoJournalForDocumentTxAuthorized(ctx, tx, document, overrideID)
	if err != nil || journal != nil || overrideID == nil {
		return err
	}
	return inventory.enforceInventoryLockTx(tx, document.BusinessID, document.IssueDate, overrideID, true)
}

type Container struct {
	Auth                   *AuthService
	BusinessAuth           *BusinessAuthService
	Business               *BusinessService
	Customer               *CustomerService
	Vendor                 *VendorService
	Product                *ProductService
	Project                *ProjectService
	Inventory              *InventoryService
	Document               *DocumentService
	Journal                *JournalService
	Shipping               *ShippingService
	Invoice                *InvoiceService
	BillingOps             *BillingOpsService
	BulkImport             *BulkImportService
	Payment                *PaymentService
	RazorpayPayment        *RazorpayPaymentService
	Ledger                 *LedgerService
	Dashboard              *DashboardService
	Report                 *ReportService
	TaxCompliance          *TaxComplianceService
	Team                   *TeamService
	Webhook                *WebhookService
	Subscription           *SubscriptionService
	SubscriptionLifecycle  *SubscriptionLifecycleService
	Commerce               *CommerceService
	Barcode                *BarcodeService
	POS                    *POSService
	S3                     *S3Service
	Email                  *EmailService
	Agent                  *AgentService
	ShoppingAgent          *ShoppingAgentService
	MerchantAgent          *MerchantAgentService
	CredentialProvider     *CredentialProviderService
	PaymentProcessor       *PaymentProcessorService
	Marketplace            *MarketplaceService
	ProductMatching        *ProductMatchingService
	IntentProcessing       *IntentProcessingService
	AgentDiscovery         *AgentDiscoveryService
	LLM                    *LLMService
	LLMChatHistory         *LLMChatHistoryService
	SarvamTTS              *SarvamTTSService
	VoiceSession           *voicesession.Service
	A2ATask                *A2ATaskService
	A2APush                *A2APushService
	Workflow               *WorkflowService
	Mentee                 *MenteeService
	Bargaining             *BargainingService
	Procurement            *ProcurementService
	AgentConfig            *AgentConfigService
	A2ABargaining          *A2ABargainingService
	WebSocketConnection    *WebSocketConnectionService
	WebSocketTicket        *WebSocketTicketService
	Notification           *NotificationService
	Capability             *CapabilityService
	Operation              *OperationService
	Security               *SecurityService
	AgentGovernance        *AgentGovernanceService
	GovernanceManagement   *GovernanceManagementService
	PendingUpload          *PendingUploadService
	Privacy                *PrivacyService
	Accounting             *AccountingService
	CapabilityGlobalHealth *CapabilityGlobalHealthCache
	AWS                    *awsclients.Config
	capabilityObserver     *capabilityObserverState
}

type capabilityObserverRunner interface {
	Run(context.Context, time.Duration) error
}

type capabilityObserverState struct {
	runner  capabilityObserverRunner
	mu      sync.Mutex
	wait    sync.WaitGroup
	running bool
	cancel  context.CancelFunc
	log     *logger.Logger
}

func (c *Container) StartCapabilityHealthObservation() {
	if c == nil || c.capabilityObserver == nil || c.capabilityObserver.runner == nil {
		return
	}
	state := c.capabilityObserver
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.running {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	state.running = true
	state.wait.Add(1)
	observer := state.runner
	log := state.log
	go func() {
		defer state.wait.Done()
		if err := observer.Run(ctx, 2*time.Minute); err != nil && !errors.Is(err, context.Canceled) && log != nil {
			log.Warn("capability health observer stopped")
		}
	}()
}

func (c *Container) StopCapabilityHealthObservation() {
	if c == nil {
		return
	}
	state := c.capabilityObserver
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.running {
		return
	}
	cancel := state.cancel
	if cancel != nil {
		cancel()
	}
	state.wait.Wait()
	state.cancel = nil
	state.running = false
}

func NewContainer(
	cfg *config.Config,
	resolver ProviderConfigResolver,
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
	invoiceRepo interfaces.CanonicalInvoiceRepository,
	paymentRepo interfaces.PaymentRepository,
	ledgerRepo interfaces.LedgerRepository,
	reportingRepo interfaces.ReportingRepository,
	teamRepo interfaces.TeamMemberRepository,
	webhookRepo interfaces.WebhookRepository,
	subscriptionRepo interfaces.SubscriptionRepository,
	subscriptionLifecycleRepo interfaces.SubscriptionLifecycleRepository,
	websocketTicketRepo interfaces.WebSocketTicketRepository,
	notificationRepo interfaces.NotificationRepository,
	capabilityProviderHealthRepo interfaces.CapabilityProviderHealthRepository,
	operationRepo interfaces.OperationRepository,
	securityRepo interfaces.SecurityPrivacyRepository,
	agentGovernanceRepo interfaces.AgentGovernanceRepository,
	ap2Repo interfaces.AP2Repository,
	aws *awsclients.Config,
	log *logger.Logger,
	agentConfigRepo interfaces.AgentConfigRepository,
) *Container {
	s3Svc := NewS3Service(cfg, aws, log)
	businessAuthSvc := NewBusinessAuthService(db, businessRepo, teamRepo, log)
	agentGovernanceSvc := NewAgentGovernanceService(AgentGovernanceServiceConfig{
		ExecutionEnabled: cfg.AIGovernance.ExecutionEnabled,
		Repository:       agentGovernanceRepo,
		Permissions:      businessAuthSvc,
	})
	governanceManagementRepo, _ := agentGovernanceRepo.(GovernanceManagementRepository)
	governanceManagementSvc := NewGovernanceManagementService(governanceManagementRepo, businessAuthSvc, userRepo, cfg.AIGovernance)
	emailSvc := NewEmailService(cfg, aws, s3Svc, log).WithDB(db)
	ap2Signer, _ := ap2.NewSignatureService()
	ap2MandateSigner := ap2.NewMandateSigner(ap2Signer)
	ap2MandateVerifier := ap2.NewMandateVerifier()
	ap2MandateSvc := ap2.NewMandateService(ap2MandateSigner, ap2MandateVerifier)
	a2aSigner, _ := ap2.NewSignatureService()
	a2aClient := a2a.NewA2AClient(a2aSigner, log)
	inventorySvc := NewInventoryService(db, inventoryRepo, productRepo, businessRepo, teamRepo, log)
	journalSvc := NewJournalService(db, journalRepo, log).WithBusinessTimezoneProvider(businessRepo)
	if configurer, ok := invoiceRepo.(invoiceIssueStockEffectConfigurer); ok {
		configurer.ConfigureInvoiceIssueStockEffect(func(ctx context.Context, tx *gorm.DB, document *models.Document) error {
			return applyCanonicalInvoiceIssueEffects(ctx, tx, document, inventorySvc, journalSvc)
		})
	}
	shippingSvc := NewShippingService(cfg, shippingRepo, customerRepo, vendorRepo, log)
	documentSvc := NewDocumentService(db, cfg, resolver, documentRepo, businessRepo, customerRepo, vendorRepo, productRepo, inventorySvc, journalSvc, shippingSvc, aws, businessAuthSvc, log)
	documentSvc.pdfPresigner = s3Svc
	barcodeSvc := NewBarcodeService(db, log)
	projectSvc := NewProjectService(db, log)
	reportSvc := NewReportService(cfg, reportingRepo, log).WithBusinessTimezoneProvider(businessRepo).WithUserRepository(userRepo)
	marketplaceSvc := NewMarketplaceService(ap2Repo, log)
	productMatchingSvc := NewProductMatchingService(marketplaceSvc, log)
	intentProcessingSvc, _ := NewIntentProcessingService(productMatchingSvc, marketplaceSvc, log)
	llmSvc := NewLLMServiceWithResolver(cfg, resolver, log)
	llmChatHistorySvc := NewLLMChatHistoryService(db, log)
	sarvamTTSSvc := NewSarvamTTSService(cfg, resolver, nil, log)

	agentSvc := NewAgentService(ap2Repo, productRepo, ap2Signer, log)
	menteeSvc := NewMenteeService(log)

	a2aPushSvc := NewA2APushService(db, ap2Repo, log)
	a2aTaskSvc := NewA2ATaskService(db, log, a2aPushSvc)
	workflowSvc := NewWorkflowService(db, log, emailSvc, a2aPushSvc)
	bargainingSvc := NewBargainingService(ap2Repo, a2aClient, agentSvc, menteeSvc, llmSvc, log)
	merchantAgentSvc := NewMerchantAgentService(ap2Repo, ap2Signer, log)
	agentConfigSvc := NewAgentConfigService(".well-known", log).WithRepository(agentConfigRepo)
	sellerNegotiationSvc := NewSellerNegotiationService(ap2Repo, agentConfigSvc, log)
	a2aBargainingSvc := NewA2ABargainingService(a2aClient, bargainingSvc, menteeSvc, ap2Repo, aws.SQS, cfg, log).WithGovernance(NewA2AGovernanceAdapter(agentGovernanceSvc, userRepo, llmSvc, cfg))
	websocketConnectionSvc := NewWebSocketConnectionService(cfg, aws, log)
	websocketTicketSvc := NewWebSocketTicketService(websocketTicketRepo, WebSocketTicketServiceOptions{})
	notificationSvc := NewNotificationService(notificationRepo, NotificationServiceOptions{})
	credentialProviderSvc := NewCredentialProviderServiceWithResolver(ap2Repo, cfg, resolver, log)
	var capabilitySvc *CapabilityService
	var voiceSessionSvc *voicesession.Service
	if cfg.VoiceSession.Enabled() && aws != nil && aws.DynamoDB != nil && aws.AgentCore != nil {
		voiceConfig := voicesession.Config{
			TableName: cfg.VoiceSession.TableName, AgentRuntimeARN: cfg.VoiceSession.AgentRuntimeARN,
			AgentRuntimeQualifier: cfg.VoiceSession.AgentRuntimeQualifier, ProtocolVersion: cfg.VoiceSession.ProtocolVersion,
			AdmissionEnabled: cfg.VoiceSession.AdmissionEnabled, RolloutStage: cfg.VoiceSession.RolloutStage,
			RolloutInternalSubjectHashes: append([]string(nil), cfg.VoiceSession.RolloutInternalSubjectHashes...),
			KVSChannelCount:              cfg.VoiceSession.KVSChannelCount, MaxDuration: cfg.VoiceSession.MaxDuration,
			RotateAfter: cfg.VoiceSession.RotateAfter, LeaseDuration: cfg.VoiceSession.LeaseDuration,
			IdempotencyTTL: cfg.VoiceSession.IdempotencyTTL, LeaseIndexName: cfg.VoiceSession.LeaseIndexName,
			GlobalCapacityLimit: cfg.VoiceSession.GlobalCapacityLimit, PerUserCapacityLimit: cfg.VoiceSession.PerUserCapacityLimit,
		}
		voiceStore := voicesession.NewDynamoDBStore(aws.DynamoDB, voicesession.DynamoDBStoreConfig{
			TableName: voiceConfig.TableName, LeaseIndexName: voiceConfig.LeaseIndexName,
			GlobalCapacityLimit: voiceConfig.GlobalCapacityLimit, PerUserCapacityLimit: voiceConfig.PerUserCapacityLimit,
		})
		voiceSessionSvc = voicesession.NewService(voiceStore, voicesession.NewAgentCoreRuntimeStopper(aws.AgentCore), voiceConfig, voicesession.ServiceOptions{
			CreateGuard: func(ctx context.Context, scope voicesession.Scope, input voicesession.CreateInput) error {
				if err := requireCapability(ctx, capabilitySvc, CapabilityRequest{
					BusinessID: scope.BusinessID, UserID: scope.UserID,
					Platform: CapabilityPlatform(input.Client.Platform), Capability: CapabilityVoice,
				}); err != nil {
					return err
				}
				return requireCapability(ctx, capabilitySvc, CapabilityRequest{
					BusinessID: scope.BusinessID, UserID: scope.UserID,
					Platform: CapabilityPlatform(input.Client.Platform), Capability: CapabilityAI,
				})
			},
		})
	}

	webhookSvc := NewWebhookService(webhookRepo, log)
	taxComplianceSvc := NewTaxComplianceService(cfg, db, businessRepo, customerRepo, vendorRepo, subscriptionRepo, aws, s3Svc, webhookSvc, log, resolver)
	securitySvc := NewSecurityService(securityRepo, SecurityServiceOptions{})
	accountingSvc := NewAccountingService(db, securitySvc, journalSvc).WithPendingStatementFiles(securityRepo, s3Svc)
	journalSvc.WithAccounting(accountingSvc)
	inventorySvc.WithAccounting(accountingSvc)
	invoiceSvc := NewInvoiceService(db, cfg, invoiceRepo, businessRepo, productRepo, customerRepo, documentSvc, aws, s3Svc, emailSvc, log, WithInvoiceAccounting(accountingSvc), WithInvoiceActorRepository(userRepo))
	if marker, ok := invoiceRepo.(outbox.PublishedMarker); ok {
		invoiceSvc.WithImmediateOutboxPublisher(
			outbox.NewRoutedImmediatePublisher(
				cfg.SQS.InvoiceQueue,
				cfg.SQS.EmailDeliveryQueue,
				aws.SQS,
				marker,
			),
		)
	}
	documentSvc.salesInvoices = newInvoiceSalesDocumentCreator(invoiceSvc)
	documentSvc.salesInvoiceIssuer = newInvoiceSalesDocumentIssuer(invoiceSvc)
	billingOpsSvc := NewBillingOpsService(cfg, db, customerRepo, vendorRepo, productRepo, invoiceSvc, documentSvc, s3Svc, businessAuthSvc, log)
	var bulkImportQueue BulkImportQueueSender
	if aws != nil {
		bulkImportQueue = NewSQSBulkImportQueueSender(cfg.SQS.BulkImportQueue, aws.SQS)
	}
	bulkImportSvc := NewBulkImportService(db, securityRepo, s3Svc, bulkImportQueue, BulkImportOptions{
		Permissions: businessAuthSvc, Capability: capabilitySvc,
		ArtifactStore: s3Svc, ArtifactBucket: cfg.S3.BucketInvoices, Notifications: notificationSvc,
	})
	documentSvc.AttachTaxComplianceService(taxComplianceSvc)
	taxComplianceSvc.AttachDocumentService(documentSvc)
	posSvc := NewPOSService(db, documentSvc, barcodeSvc, inventorySvc, taxComplianceSvc.entitlements, log)
	commerceSvc := NewCommerceService(cfg, db, businessRepo, customerRepo, productRepo, subscriptionRepo, inventorySvc, documentSvc, s3Svc, log)
	razorpayPaymentSvc := NewRazorpayPaymentService(cfg, db, log, resolver)
	subscriptionLifecycleSvc := NewSubscriptionLifecycleService(subscriptionLifecycleRepo, razorpayPaymentSvc, SubscriptionLifecycleConfig{
		Resolve: razorpayPaymentSvc.SubscriptionProviderSettings,
	}, log)
	capabilityGlobalHealth := NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{})
	capabilityConfiguration := config.CapabilityConfigurationSnapshot(cfg)
	capabilityBusinessHealth := NewCapabilityBusinessHealthReader(capabilityProviderHealthRepo, nil)
	capabilitySvc = NewCapabilityService(CapabilityServiceOptions{
		Configuration:  capabilityConfiguration,
		Entitlements:   taxComplianceSvc.entitlements,
		Permissions:    businessAuthSvc,
		Setup:          NewDBCapabilityBusinessSetupReader(db),
		GlobalHealth:   capabilityGlobalHealth,
		BusinessHealth: capabilityBusinessHealth,
		AIGovernance:   agentGovernanceSvc,
	})
	pendingUploadSvc := NewPendingUploadService(securityRepo, s3Svc, FailClosedUploadScanner{}, PendingUploadOptions{Bucket: cfg.S3.BucketDrive})
	logoUploadSvc := NewPendingUploadService(securityRepo, s3Svc, nil, PendingUploadOptions{Bucket: cfg.S3.BucketLogos})
	businessSvc := NewBusinessService(businessRepo, s3Svc, log).
		WithLogoUploadWorkflow(securityRepo, logoUploadSvc, s3Svc, cfg.S3.BucketLogos)
	privacySvc := NewPrivacyService(
		securityRepo,
		NewDatabasePrivacyExporter(db, s3Svc, cfg.S3.BucketDrive),
		NewPrivacyCleanupCoordinator(
			NewDatabasePrivacyCleanupTarget(db),
			NewS3PrivacyCleanupTarget(s3Svc, cfg.S3.BucketDrive),
			NewCognitoPrivacyCleanupTarget(aws.Cognito, cfg.Cognito.UserPoolID, cfg.Cognito.Phone.UserPoolID),
		),
		nil,
	)
	operationSvc := NewOperationService(
		operationRepo,
		businessAuthSvc,
		NewConfiguredOperationRecoveryCapabilityGuard(cfg, aws),
		OperationServiceOptions{StepUpVerifier: securitySvc},
	)
	globalProbers := make(map[CapabilityKey]CapabilityGlobalProviderProber, 2)
	if capabilityConfiguration.Razorpay {
		globalProbers[CapabilityRazorpay] = razorpayPaymentSvc
	}
	if capabilityConfiguration.AI {
		globalProbers[CapabilityAI] = llmSvc
	}
	capabilityObserver := NewCapabilityGlobalHealthObserver(
		globalProbers,
		NewCapabilityGlobalHealthRecorder(capabilityGlobalHealth, nil),
		CapabilityGlobalHealthObserverOptions{OnCycleIssue: func(issue CapabilityHealthCycleIssue) {
			log.Warn("capability health observation cycle issue", "code", issue.Code)
		}},
	)
	gstHealthRecorder := NewGSTProviderHealthRecorder(capabilityProviderHealthRepo, GSTProviderHealthRecorderOptions{})
	reportSvc.WithCapabilityGuard(capabilitySvc)
	taxComplianceSvc.WithCapabilityGuard(capabilitySvc).WithGSTProviderHealthRecorder(gstHealthRecorder)
	billingOpsSvc.WithCapabilityGuard(capabilitySvc)
	commerceSvc.WithCapabilityControls(capabilitySvc, taxComplianceSvc.entitlements)
	razorpayPaymentSvc.WithCapabilityGuard(capabilitySvc)
	credentialProviderSvc.WithCapabilityGuard(capabilitySvc)
	llmSvc.WithCapabilityGuard(capabilitySvc)
	customerSvc := NewCustomerService(customerRepo, businessAuthSvc, log).WithCapabilityGuard(capabilitySvc)

	log.Info("service container initialized",
		"components", 34,
		"llm_model", cfg.LLM.Model,
		"workflow_enabled", workflowSvc != nil,
	)

	shoppingAgentSvc := NewShoppingAgentService(ap2Repo, agentSvc, intentProcessingSvc, ap2Signer, ap2MandateSvc, a2aClient, cfg.Server.A2AMessageEndpoint(), log).WithUserRepository(userRepo)
	procurementSvc := NewProcurementService(ap2Repo, agentSvc, intentProcessingSvc, shoppingAgentSvc, merchantAgentSvc, bargainingSvc, agentConfigSvc, ap2Signer, log)
	a2aTaskSvc.ConfigureDomainServices(ap2Repo, merchantAgentSvc, sellerNegotiationSvc, ap2Signer)

	return &Container{
		Auth:                   NewAuthService(cfg, userRepo, aws, emailSvc, s3Svc, log).WithAuthAuditRecorder(securityRepo),
		BusinessAuth:           businessAuthSvc,
		Business:               businessSvc,
		Customer:               customerSvc,
		Vendor:                 NewVendorService(vendorRepo, businessAuthSvc, log),
		Product:                NewProductService(db, productRepo, s3Svc, inventorySvc, log),
		Project:                projectSvc,
		Inventory:              inventorySvc,
		Document:               documentSvc,
		Journal:                journalSvc,
		Shipping:               shippingSvc,
		Invoice:                invoiceSvc,
		BillingOps:             billingOpsSvc,
		BulkImport:             bulkImportSvc,
		Payment:                NewPaymentService(db, paymentRepo, invoiceRepo, documentSvc, journalSvc, log),
		RazorpayPayment:        razorpayPaymentSvc,
		Ledger:                 NewLedgerService(ledgerRepo, log),
		Dashboard:              NewDashboardService(db, log).WithBusinessTimezoneProvider(businessRepo),
		Report:                 reportSvc,
		TaxCompliance:          taxComplianceSvc,
		Team:                   NewTeamService(teamRepo, log).WithDB(db),
		Webhook:                webhookSvc,
		Subscription:           NewSubscriptionService(subscriptionRepo, log),
		SubscriptionLifecycle:  subscriptionLifecycleSvc,
		Commerce:               commerceSvc,
		Barcode:                barcodeSvc,
		POS:                    posSvc,
		S3:                     s3Svc,
		Email:                  emailSvc,
		Agent:                  agentSvc,
		ShoppingAgent:          shoppingAgentSvc,
		MerchantAgent:          merchantAgentSvc,
		CredentialProvider:     credentialProviderSvc,
		PaymentProcessor:       NewPaymentProcessorService(ap2Repo, log),
		Marketplace:            marketplaceSvc,
		ProductMatching:        productMatchingSvc,
		IntentProcessing:       intentProcessingSvc,
		AgentDiscovery:         NewAgentDiscoveryService(ap2Repo, productRepo, llmSvc, log),
		LLM:                    llmSvc,
		LLMChatHistory:         llmChatHistorySvc,
		SarvamTTS:              sarvamTTSSvc,
		VoiceSession:           voiceSessionSvc,
		A2ATask:                a2aTaskSvc,
		A2APush:                a2aPushSvc,
		Workflow:               workflowSvc,
		Mentee:                 menteeSvc,
		Bargaining:             bargainingSvc,
		Procurement:            procurementSvc,
		AgentConfig:            agentConfigSvc,
		A2ABargaining:          a2aBargainingSvc,
		WebSocketConnection:    websocketConnectionSvc,
		WebSocketTicket:        websocketTicketSvc,
		Notification:           notificationSvc,
		Capability:             capabilitySvc,
		Operation:              operationSvc,
		Security:               securitySvc,
		AgentGovernance:        agentGovernanceSvc,
		GovernanceManagement:   governanceManagementSvc,
		PendingUpload:          pendingUploadSvc,
		Privacy:                privacySvc,
		Accounting:             accountingSvc,
		CapabilityGlobalHealth: capabilityGlobalHealth,
		AWS:                    aws,
		capabilityObserver:     &capabilityObserverState{runner: capabilityObserver, log: log},
	}
}
