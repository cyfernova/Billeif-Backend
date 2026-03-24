package handlers

import (
	"invoice-backend/internal/config"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
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
	WebSocket      *WebSocketHandler
	LLM            *LLMHandler
	WellKnown      *WellKnownHandler
	A2ATask        *A2ATaskHandler
	Workflow       *WorkflowHandler
	Bargaining     *BargainingHandler
	Procurement    *ProcurementHandler
	AgentConfig    *AgentConfigHandler
	A2ABargaining  *A2ABargainingHandler
}

func New(svcs *services.Container, repos *Repositories, cfg *config.Config, log *logger.Logger) *Handler {
	log = log.Named("handlers")

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
		ShoppingAgent:  NewShoppingAgentHandler(svcs.ShoppingAgent, repos.AP2, log),
		Credential:     NewCredentialHandler(svcs.CredentialProvider, repos.AP2, cfg, log),
		Marketplace:    NewMarketplaceHandler(svcs.Marketplace, repos.AP2, log),
		AgentDiscovery: NewAgentDiscoveryHandler(svcs.AgentDiscovery, log),
		Intent:         NewIntentHandler(svcs.IntentProcessing, log),
		WebSocket:      NewWebSocketHandler(wsHub, svcs.WebSocketConnection, log),
		LLM:            NewLLMHandler(svcs.LLM, log),
		WellKnown:      NewWellKnownHandler(cfg, log),
		A2ATask:        NewA2ATaskHandler(svcs.A2ATask, svcs.A2APush, log),
		Workflow:       NewWorkflowHandler(svcs.Workflow, log),
		Bargaining:     NewBargainingHandler(svcs.Bargaining, repos.AP2, log),
		Procurement:    NewProcurementHandler(svcs.Procurement, repos.AP2, log),
		AgentConfig:    NewAgentConfigHandler(svcs.AgentConfig, svcs.Agent, svcs.Bargaining, svcs.Mentee, log),
		A2ABargaining:  NewA2ABargainingHandler(svcs.A2ABargaining, svcs.Agent, cfg, log),
	}
}

type Repositories struct {
	AP2 interfaces.AP2Repository
}
