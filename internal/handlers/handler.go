package handlers

import (
	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/websocket"
)

type Handler struct {
	Auth            *AuthHandler
	Business        *BusinessHandler
	Customer        *CustomerHandler
	Vendor          *VendorHandler
	Product         *ProductHandler
	Project         *ProjectHandler
	Inventory       *InventoryHandler
	Barcode         *BarcodeHandler
	POS             *POSHandler
	Purchase        *DocumentHandler
	PurchaseOrder   *DocumentHandler
	SalesOrder      *DocumentHandler
	Quotation       *DocumentHandler
	ProformaInvoice *DocumentHandler
	DeliveryChallan *DocumentHandler
	CreditNote      *DocumentHandler
	DebitNote       *DocumentHandler
	BillOfSupply    *DocumentHandler
	Expense         *DocumentHandler
	PackingList     *DocumentHandler
	ShippingLabel   *DocumentHandler
	DocumentUtility *DocumentUtilityHandler
	Journal         *JournalHandler
	RenderProfile   *RenderProfileHandler
	Shipment        *ShipmentHandler
	Invoice         *InvoiceHandler
	BillingOps      *BillingOpsHandler
	Payment         *PaymentHandler
	RazorpayPayment *RazorpayPaymentHandler
	Ledger          *LedgerHandler
	Dashboard       *DashboardHandler
	Report          *ReportHandler
	Tax             *TaxHandler
	Team            *TeamHandler
	Webhook         *WebhookHandler
	Subscription    *SubscriptionHandler
	Commerce        *CommerceHandler
	EmailConfig     *EmailConfigHandler
	Health          *HealthHandler
	Admin           *AdminHandler
	Agent           *AgentHandler
	ShoppingAgent   *ShoppingAgentHandler
	Credential      *CredentialHandler
	Marketplace     *MarketplaceHandler
	AgentDiscovery  *AgentDiscoveryHandler
	Intent          *IntentHandler
	WebSocket       *WebSocketHandler
	WebSocketTicket *WebSocketTicketHandler
	Notification    *NotificationHandler
	Capability      *CapabilityHandler
	Operation       *OperationHandler
	Security        *SecurityHandler
	LLM             *LLMHandler
	SarvamTTS       *SarvamTTSHandler
	VoiceSession    *VoiceSessionHandler
	WellKnown       *WellKnownHandler
	A2ATask         *A2ATaskHandler
	Workflow        *WorkflowHandler
	Bargaining      *BargainingHandler
	Procurement     *ProcurementHandler
	AgentConfig     *AgentConfigHandler
	A2ABargaining   *A2ABargainingHandler
	MCP             *MCPHandler
}

func New(
	svcs *services.Container,
	repos *Repositories,
	cfg *config.Config,
	log *logger.Logger,
	cursorCodecs ...InvoiceCursorCodec,
) *Handler {
	log = log.Named("handlers")
	var cursor InvoiceCursorCodec
	if len(cursorCodecs) != 0 {
		cursor = cursorCodecs[0]
	}

	// Create WebSocket hub
	wsHub := websocket.NewHub(log)
	wsHub.Run()
	log.Info("handler container initialized")

	return &Handler{
		Auth:            NewAuthHandler(svcs.Auth, log),
		Business:        NewBusinessHandler(svcs.Business, log),
		Customer:        NewCustomerHandler(svcs.Customer, log),
		Vendor:          NewVendorHandler(svcs.Vendor, log),
		Product:         NewProductHandler(svcs.Product, log),
		Project:         NewProjectHandler(svcs.Project, log),
		Inventory:       NewInventoryHandler(svcs.Inventory, log),
		Barcode:         NewBarcodeHandler(svcs.Barcode, log),
		Purchase:        NewDocumentHandler(svcs.Document, models.DocumentTypePurchaseInvoice, log),
		PurchaseOrder:   NewDocumentHandler(svcs.Document, models.DocumentTypePurchaseOrder, log),
		SalesOrder:      NewDocumentHandler(svcs.Document, models.DocumentTypeSalesOrder, log),
		Quotation:       NewDocumentHandler(svcs.Document, models.DocumentTypeQuotation, log),
		ProformaInvoice: NewDocumentHandler(svcs.Document, models.DocumentTypeProformaInvoice, log),
		DeliveryChallan: NewDocumentHandler(svcs.Document, models.DocumentTypeDeliveryChallan, log),
		CreditNote:      NewDocumentHandler(svcs.Document, models.DocumentTypeCreditNote, log),
		DebitNote:       NewDocumentHandler(svcs.Document, models.DocumentTypeDebitNote, log),
		BillOfSupply:    NewDocumentHandler(svcs.Document, models.DocumentTypeBillOfSupply, log),
		Expense:         NewDocumentHandler(svcs.Document, models.DocumentTypeExpense, log),
		PackingList:     NewDocumentHandler(svcs.Document, models.DocumentTypePackingList, log),
		ShippingLabel:   NewDocumentHandler(svcs.Document, models.DocumentTypeShippingLabel, log),
		DocumentUtility: NewDocumentUtilityHandler(svcs.Document, svcs.TaxCompliance, log),
		Journal:         NewJournalHandler(svcs.Journal, log),
		RenderProfile:   NewRenderProfileHandler(svcs.Document, log),
		Shipment:        NewShipmentHandler(svcs.Shipping, svcs.Document, log),
		Invoice:         NewInvoiceHandler(svcs.Invoice, svcs.TaxCompliance, log, cursor),
		BillingOps:      NewBillingOpsHandler(svcs.BillingOps, log),
		Payment:         NewPaymentHandler(svcs.Payment, log),
		RazorpayPayment: NewRazorpayPaymentHandler(svcs.RazorpayPayment, log, svcs.SubscriptionLifecycle),
		Ledger:          NewLedgerHandler(svcs.Ledger, log),
		Dashboard:       NewDashboardHandler(svcs.Dashboard, log),
		Report:          NewReportHandler(svcs.Report, svcs.Inventory, log),
		Tax:             NewTaxHandler(svcs.TaxCompliance, log),
		Team:            NewTeamHandler(svcs.Team, log),
		Webhook:         NewWebhookHandler(svcs.Webhook, log),
		Subscription:    NewSubscriptionHandler(svcs.Subscription, log, svcs.SubscriptionLifecycle),
		Commerce:        NewCommerceHandler(svcs.Commerce, log),
		EmailConfig:     NewEmailConfigHandler(svcs.Email, log),
		POS:             NewPOSHandler(svcs.POS, log),
		Health:          NewHealthHandler(log),
		Admin:           NewAdminHandler(svcs.Email, log),
		Agent:           NewAgentHandler(svcs.Agent, log),
		ShoppingAgent:   NewShoppingAgentHandler(svcs.ShoppingAgent, repos.AP2, log),
		Credential:      NewCredentialHandler(svcs.CredentialProvider, repos.AP2, cfg, log),
		Marketplace:     NewMarketplaceHandler(svcs.Marketplace, repos.AP2, log),
		AgentDiscovery:  NewAgentDiscoveryHandler(svcs.AgentDiscovery, log),
		Intent:          NewIntentHandler(svcs.IntentProcessing, log),
		WebSocket:       NewWebSocketHandler(wsHub, svcs.WebSocketConnection, svcs.WebSocketTicket, log),
		WebSocketTicket: NewWebSocketTicketHandler(svcs.WebSocketTicket, log),
		Notification:    NewNotificationHandler(svcs.Notification, log),
		Capability:      NewCapabilityHandler(svcs.Capability, log),
		Operation:       NewOperationHandler(svcs.Operation, log),
		Security:        NewSecurityHandler(svcs.Security, svcs.Auth, svcs.PendingUpload, svcs.Privacy),
		LLM:             NewLLMHandler(svcs.LLM, svcs.LLMChatHistory, log),
		SarvamTTS:       NewSarvamTTSHandler(svcs.SarvamTTS, log),
		VoiceSession:    NewVoiceSessionHandler(svcs.VoiceSession, log),
		WellKnown:       NewWellKnownHandler(cfg, log),
		A2ATask:         NewA2ATaskHandler(svcs.A2ATask, svcs.A2APush, log),
		Workflow:        NewWorkflowHandler(svcs.Workflow, log),
		Bargaining:      NewBargainingHandler(svcs.Bargaining, repos.AP2, log),
		Procurement:     NewProcurementHandler(svcs.Procurement, repos.AP2, log),
		AgentConfig:     NewAgentConfigHandler(svcs.AgentConfig, repos.AP2, svcs.Agent, svcs.Bargaining, svcs.Mentee, log),
		A2ABargaining:   NewA2ABargainingHandler(svcs.A2ABargaining, svcs.Agent, cfg, log),
		MCP:             NewMCPHandlerFromConfig(cfg, log),
	}
}

type Repositories struct {
	AP2 interfaces.AP2Repository
}
