package handlers

import (
	"invoice-backend/internal/config"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/ap2"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/websocket"
)

type Handler struct {
	Auth           *AuthHandler
	Business       *BusinessHandler
	Customer       *CustomerHandler
	Vendor         *VendorHandler
	Product        *ProductHandler
	Invoice        *InvoiceHandler
	Payment        *PaymentHandler
	Ledger         *LedgerHandler
	Team           *TeamHandler
	Webhook        *WebhookHandler
	Subscription   *SubscriptionHandler
	Health         *HealthHandler
	Admin          *AdminHandler
	Agent          *AgentHandler
	ShoppingAgent  *ShoppingAgentHandler
	Credential     *CredentialHandler
	Marketplace    *MarketplaceHandler
	AgentDiscovery *AgentDiscoveryHandler
	Intent         *IntentHandler
	A2AMessage     *A2AMessageHandler
	WebSocket      *WebSocketHandler
	LLM            *LLMHandler
	WellKnown      *WellKnownHandler
	A2ATask        *A2ATaskHandler
	A2APush        *A2APushHandler
	Workflow       *WorkflowHandler
	Bargaining     *BargainingHandler
	AgentConfig    *AgentConfigHandler
}

func New(svcs *services.Container, repos *Repositories, cfg *config.Config, log *logger.Logger) *Handler {
	log = log.Named("handlers")
	// Create A2A client (uses SignatureService internally)
	sigSvc, _ := ap2.NewSignatureService()
	a2aClient := a2a.NewA2AClient(sigSvc, log)

	// Create WebSocket hub
	wsHub := websocket.NewHub(log)
	wsHub.Run()
	log.Info("handler container initialized")

	return &Handler{
		Auth:           NewAuthHandler(svcs.Auth, log),
		Business:       NewBusinessHandler(svcs.Business, log),
		Customer:       NewCustomerHandler(svcs.Customer, log),
		Vendor:         NewVendorHandler(svcs.Vendor, log),
		Product:        NewProductHandler(svcs.Product, log),
		Invoice:        NewInvoiceHandler(svcs.Invoice, log),
		Payment:        NewPaymentHandler(svcs.Payment, log),
		Ledger:         NewLedgerHandler(svcs.Ledger, log),
		Team:           NewTeamHandler(svcs.Team, log),
		Webhook:        NewWebhookHandler(svcs.Webhook, log),
		Subscription:   NewSubscriptionHandler(svcs.Subscription, log),
		Health:         NewHealthHandler(log),
		Admin:          NewAdminHandler(svcs.Email, log),
		Agent:          NewAgentHandler(svcs.Agent, log),
		ShoppingAgent:  NewShoppingAgentHandler(svcs.ShoppingAgent, log),
		Credential:     NewCredentialHandler(svcs.CredentialProvider, repos.AP2, cfg, log),
		Marketplace:    NewMarketplaceHandler(svcs.Marketplace, repos.AP2, log),
		AgentDiscovery: NewAgentDiscoveryHandler(svcs.AgentDiscovery, log),
		Intent:         NewIntentHandler(svcs.IntentProcessing, log),
		A2AMessage:     NewA2AMessageHandler(svcs.ShoppingAgent, svcs.MerchantAgent, svcs.CredentialProvider, svcs.PaymentProcessor, svcs.Marketplace, a2aClient, log),
		WebSocket:      NewWebSocketHandler(wsHub, log),
		LLM:            NewLLMHandler(svcs.LLM, log),
		WellKnown:      NewWellKnownHandler(cfg, log),
		A2ATask:        NewA2ATaskHandler(svcs.A2ATask, log),
		A2APush:        NewA2APushHandler(svcs.A2APush, log),
		Workflow:       NewWorkflowHandler(svcs.Workflow, log),
		Bargaining:     NewBargainingHandler(svcs.Bargaining, log),
		AgentConfig:    NewAgentConfigHandler(svcs.AgentConfig, svcs.Agent, svcs.Bargaining, svcs.Mentee, log),
	}
}

type Repositories struct {
	AP2 interfaces.AP2Repository
}
