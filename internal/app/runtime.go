package app

import (
	"context"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"invoice-backend/internal/config"
	"invoice-backend/internal/handlers"
	"invoice-backend/internal/middleware"
	"invoice-backend/internal/repositories/interfaces"
	postgresrepo "invoice-backend/internal/repositories/postgres"
	"invoice-backend/internal/services"
	"invoice-backend/internal/workers"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"
	pkgsentry "invoice-backend/pkg/sentry"

	_ "invoice-backend/docs"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// @title Invoice Backend API
// @version 1.0
// @description Production-grade monolithic Golang backend for an invoice/billing platform.
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.email support@invoiceapp.com

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @BasePath /api/v1

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

type InitializeOptions struct {
	EnableWorker bool
}

type Runtime struct {
	Config *config.Config

	Log    *logger.Logger
	DB     *gorm.DB
	AWS    *awsclients.Config
	Repos  *Repositories
	Svcs   *services.Container
	H      *handlers.Handler
	Router *gin.Engine
	Worker *workers.Worker
}

func Initialize(ctx context.Context, opts InitializeOptions) (*Runtime, error) {
	bootstrapLog := logger.New().Named("bootstrap")

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	log := logger.NewWithConfig(logger.Config{
		Environment:        cfg.Environment,
		Level:              cfg.Logging.Level,
		Format:             cfg.Logging.Format,
		SamplingInitial:    cfg.Logging.SamplingInitial,
		SamplingThereafter: cfg.Logging.SamplingThereafter,
		StacktraceLevel:    cfg.Logging.StacktraceLevel,
	}).Named("api")
	logger.SetGlobal(log)

	log.Info("application startup",
		"service", "invoice-backend",
		"environment", cfg.Environment,
		"log_level", cfg.Logging.Level,
		"log_format", cfg.Logging.Format,
		"sentry_enabled", cfg.Sentry.DSN != "",
	)

	if err := initSentry(cfg, log); err != nil {
		log.Error("failed to initialize Sentry", "error", err)
	}

	if logger.IsProductionEnvironment(cfg.Environment) {
		gin.SetMode(gin.ReleaseMode)
	}

	db, err := initDatabase(cfg, log)
	if err != nil {
		log.Sync()
		return nil, fmt.Errorf("connect database: %w", err)
	}

	awsClients, err := awsclients.New(ctx, cfg.AWS, log.Named("awsclients"))
	if err != nil {
		log.Sync()
		return nil, fmt.Errorf("initialize AWS clients: %w", err)
	}

	repos := initRepositories(db)
	svcs := initServices(cfg, db, repos, awsClients, log)
	h := handlers.New(svcs, &handlers.Repositories{AP2: repos.AP2}, cfg, log)
	router := setupRouter(cfg, svcs, h, log)

	rt := &Runtime{
		Config: cfg,
		Log:    log,
		DB:     db,
		AWS:    awsClients,
		Repos:  repos,
		Svcs:   svcs,
		H:      h,
		Router: router,
	}

	if opts.EnableWorker {
		rt.Worker = workers.New(cfg, svcs, awsClients, log)
	}

	bootstrapLog.Info("runtime initialized")
	return rt, nil
}

func (r *Runtime) Close() {
	if r.Worker != nil {
		r.Worker.Stop()
	}
	pkgsentry.Flush(2 * time.Second)
	if r.Log != nil {
		r.Log.Sync()
	}
}

func initDatabase(cfg *config.Config, log *logger.Logger) (*gorm.DB, error) {
	gormLevel := "warn"
	if strings.EqualFold(cfg.Logging.Level, "debug") || strings.EqualFold(cfg.Logging.Level, "info") {
		gormLevel = cfg.Logging.Level
	}

	return gorm.Open(postgres.New(postgres.Config{
		DSN: fmt.Sprintf(
			"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			cfg.Database.Host,
			cfg.Database.Port,
			cfg.Database.User,
			cfg.Database.Password,
			cfg.Database.Name,
			cfg.Database.SSLMode,
		),
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		Logger: logger.NewGORMLogger(log, logger.GORMOptions{
			Environment:               cfg.Environment,
			SlowThreshold:             200 * time.Millisecond,
			Level:                     gormLevel,
			IgnoreRecordNotFoundError: true,
			IncludeQuery:              !logger.IsProductionEnvironment(cfg.Environment),
		}),
	})
}

type Repositories struct {
	User         interfaces.UserRepository
	Business     interfaces.BusinessRepository
	Customer     interfaces.CustomerRepository
	Vendor       interfaces.VendorRepository
	Product      interfaces.ProductRepository
	Document     interfaces.DocumentRepository
	Journal      interfaces.JournalRepository
	Inventory    interfaces.InventoryRepository
	Shipping     interfaces.ShippingRepository
	Invoice      interfaces.InvoiceRepository
	Payment      interfaces.PaymentRepository
	Ledger       interfaces.LedgerRepository
	Team         interfaces.TeamMemberRepository
	Webhook      interfaces.WebhookRepository
	Subscription interfaces.SubscriptionRepository
	AP2          interfaces.AP2Repository
}

func initRepositories(db *gorm.DB) *Repositories {
	return &Repositories{
		User:         postgresrepo.NewUserRepository(db),
		Business:     postgresrepo.NewBusinessRepository(db),
		Customer:     postgresrepo.NewCustomerRepository(db),
		Vendor:       postgresrepo.NewVendorRepository(db),
		Product:      postgresrepo.NewProductRepository(db),
		Document:     postgresrepo.NewDocumentRepository(db),
		Journal:      postgresrepo.NewJournalRepository(db),
		Inventory:    postgresrepo.NewInventoryRepository(db),
		Shipping:     postgresrepo.NewShippingRepository(db),
		Invoice:      postgresrepo.NewInvoiceRepository(db),
		Payment:      postgresrepo.NewPaymentRepository(db),
		Ledger:       postgresrepo.NewLedgerRepository(db),
		Team:         postgresrepo.NewTeamMemberRepository(db),
		Webhook:      postgresrepo.NewWebhookRepository(db),
		Subscription: postgresrepo.NewSubscriptionRepository(db),
		AP2:          postgresrepo.NewAP2Repository(db),
	}
}

func initServices(cfg *config.Config, db *gorm.DB, repos *Repositories, aws *awsclients.Config, log *logger.Logger) *services.Container {
	return services.NewContainer(cfg, db, repos.User, repos.Business, repos.Customer, repos.Vendor,
		repos.Product, repos.Document, repos.Journal, repos.Inventory, repos.Shipping, repos.Invoice, repos.Payment, repos.Ledger, repos.Team,
		repos.Webhook, repos.Subscription, repos.AP2, aws, log)
}

// initSentry initializes the Sentry SDK with production configuration
func initSentry(cfg *config.Config, log *logger.Logger) error {
	if cfg.Sentry.DSN == "" {
		log.Info("Sentry DSN not configured, error tracking disabled")
		return nil
	}

	// Set default sample rates for production
	sampleRate := cfg.Sentry.SampleRate
	if sampleRate == 0 {
		sampleRate = 1.0 // Capture 100% of errors in production
	}

	tracesSampleRate := cfg.Sentry.TracesSampleRate
	if tracesSampleRate == 0 {
		tracesSampleRate = 0.2 // Sample 20% of transactions for performance monitoring
	}

	err := pkgsentry.Init(pkgsentry.Config{
		DSN:              cfg.Sentry.DSN,
		Environment:      cfg.Environment,
		Release:          "invoice-backend@1.0.0", // TODO: Get from build info
		Debug:            cfg.Sentry.Debug,
		SampleRate:       sampleRate,
		TracesSampleRate: tracesSampleRate,
		EnableTracing:    cfg.Sentry.EnableTracing,
	})

	if err != nil {
		return err
	}

	log.Info("Sentry initialized",
		"environment", cfg.Environment,
		"sample_rate", sampleRate,
		"traces_sample_rate", tracesSampleRate,
	)

	return nil
}

func setupRouter(cfg *config.Config, svcs *services.Container, h *handlers.Handler, log *logger.Logger) *gin.Engine {
	router := gin.New()

	// Add Sentry middleware first for request context
	if cfg.Sentry.DSN != "" {
		router.Use(middleware.SentryMiddleware())
	}

	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.RequestID())
	router.Use(middleware.Logger(log))

	isProd := logger.IsProductionEnvironment(cfg.Environment)

	// Use Sentry-aware recovery if Sentry is enabled
	if cfg.Sentry.DSN != "" {
		router.Use(middleware.SentryRecovery(log))
	} else {
		router.Use(middleware.RecoveryWithOptions(log, isProd))
	}
	router.Use(middleware.CORS(cfg))

	router.GET("/health", h.Health.Check)

	// Only expose Swagger docs in non-production environments
	if !isProd {
		router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}

	// Well-known endpoints
	router.GET("/.well-known/agent-card.json", h.WellKnown.GetAgentCard)
	router.GET("/.well-known/agents.json", func(c *gin.Context) {
		c.File(".well-known/agents.json")
	})

	api := router.Group("/api/v1")
	{
		loginRL := middleware.AuthRateLimit(5, time.Minute)
		sensitiveRL := middleware.AuthRateLimit(3, time.Hour)

		auth := api.Group("/auth")
		{
			auth.POST("/register", sensitiveRL, h.Auth.Register)
			auth.POST("/login", loginRL, h.Auth.Login)
			auth.POST("/logout", h.Auth.Logout)
			auth.POST("/refresh", h.Auth.Refresh)
			auth.POST("/phone/register", sensitiveRL, h.Auth.PhoneRegister)
			auth.POST("/phone/confirm", sensitiveRL, h.Auth.PhoneConfirm)
			auth.POST("/phone/resend-confirmation", sensitiveRL, h.Auth.PhoneResendConfirmation)
			auth.POST("/phone/login", loginRL, h.Auth.PhoneLogin)
			auth.POST("/phone/verify-login", sensitiveRL, h.Auth.PhoneVerifyLogin)
			auth.POST("/phone/refresh", h.Auth.PhoneRefresh)
			auth.POST("/forgot-password", sensitiveRL, h.Auth.ForgotPassword)
			auth.POST("/reset-password", sensitiveRL, h.Auth.ResetPassword)
			auth.POST("/verify-email", h.Auth.VerifyEmail)
			auth.POST("/resend-verification", sensitiveRL, h.Auth.ResendVerification)
			// POST /auth/google needs to be protected because we need to validate the ID token
			// However, typically "login" endpoints are public.
			// But here, the flow is: Frontend gets token -> Backend validates token -> Backend syncs user.
			// The validation happens via middleware.
			// So we should put it in a protected group or apply middleware manually.
			// The existing "protected" group applies Auth middleware.
			// So let's add it to the protected group, or a new group with Auth middleware.
		}

		// We need a specific group for this because the existing "auth" group is public (no middleware).
		// And "protected" group has all the other protected routes.
		// We can add it to "protected" group or create a small subgroup here.
		googleAuth := api.Group("/auth")
		googleAuth.Use(middleware.Auth(cfg.Cognito, log))
		{
			googleAuth.POST("/google", h.Auth.GoogleLogin)
		}

		protected := api.Group("")
		protected.Use(middleware.Auth(cfg.Cognito, log))
		protected.Use(middleware.BusinessAuth(svcs.BusinessAuth))
		{
			protected.GET("/auth/me", h.Auth.Me)
			protected.PUT("/auth/profile", h.Auth.UpdateProfile)
			protected.POST("/auth/change-password", h.Auth.ChangePassword)
			protected.POST("/auth/phone/logout", h.Auth.PhoneLogout)
			protected.POST("/auth/profile-picture", h.Auth.UploadProfilePicture)
			protected.PUT("/auth/profile-picture", h.Auth.UpdateProfilePicture)

			businesses := protected.Group("/business-profiles")
			{
				businesses.GET("", h.Business.List)
				businesses.GET("/:id", h.Business.Get)
				businesses.POST("", h.Business.Create)
				businesses.PUT("/:id", h.Business.Update)
				businesses.DELETE("/:id", h.Business.Delete)
				businesses.POST("/:id/logo", h.Business.UploadLogo)
			}

			customers := protected.Group("/customers")
			{
				customers.GET("", h.Customer.List)
				customers.GET("/:id", h.Customer.Get)
				customers.POST("", h.Customer.Create)
				customers.PUT("/:id", h.Customer.Update)
				customers.DELETE("/:id", h.Customer.Delete)
				customers.POST("/import", h.Customer.Import)
				customers.GET("/export", h.Customer.Export)
			}

			vendors := protected.Group("/vendors")
			{
				vendors.GET("", h.Vendor.List)
				vendors.GET("/:id", h.Vendor.Get)
				vendors.POST("", h.Vendor.Create)
				vendors.PUT("/:id", h.Vendor.Update)
				vendors.DELETE("/:id", h.Vendor.Delete)
			}

			products := protected.Group("/products")
			{
				products.GET("", h.Product.List)
				products.GET("/:id", h.Product.Get)
				products.POST("", h.Product.Create)
				products.PUT("/:id", h.Product.Update)
				products.DELETE("/:id", h.Product.Delete)
				products.POST("/:id/image", h.Product.UploadImage)
				products.POST("/:id/stock", h.Product.AdjustStock)
			}

			registerDocumentResource := func(path string, handler *handlers.DocumentHandler) {
				group := protected.Group(path)
				group.GET("", handler.List)
				group.GET("/:id", handler.Get)
				group.POST("", handler.Create)
				group.PUT("/:id", handler.Update)
				group.DELETE("/:id", handler.Delete)
				group.POST("/:id/cancel", handler.Cancel)
				group.GET("/:id/pdf", handler.GetPDF)
			}

			registerDocumentResource("/purchases", h.Purchase)
			registerDocumentResource("/purchase-orders", h.PurchaseOrder)
			registerDocumentResource("/sales-orders", h.SalesOrder)
			registerDocumentResource("/quotations", h.Quotation)
			registerDocumentResource("/proforma-invoices", h.ProformaInvoice)
			registerDocumentResource("/delivery-challans", h.DeliveryChallan)
			registerDocumentResource("/credit-notes", h.CreditNote)
			registerDocumentResource("/debit-notes", h.DebitNote)
			registerDocumentResource("/bills-of-supply", h.BillOfSupply)
			registerDocumentResource("/packing-lists", h.PackingList)
			registerDocumentResource("/shipping-labels", h.ShippingLabel)

			invoices := protected.Group("/invoices")
			{
				invoices.GET("", h.Invoice.List)
				invoices.GET("/:id", h.Invoice.Get)
				invoices.POST("", h.Invoice.Create)
				invoices.PUT("/:id", h.Invoice.Update)
				invoices.DELETE("/:id", h.Invoice.Delete)
				invoices.POST("/:id/send", h.Invoice.Send)
				invoices.GET("/:id/pdf", h.Invoice.GetPDF)
				invoices.GET("/next-number", h.Invoice.NextNumber)
			}

			payments := protected.Group("/payments")
			{
				payments.GET("", h.Payment.List)
				payments.GET("/:id", h.Payment.Get)
				payments.POST("", h.Payment.Create)
				payments.PUT("/:id", h.Payment.Update)
				payments.DELETE("/:id", h.Payment.Delete)
			}

			documents := protected.Group("/documents")
			{
				documents.POST("/merge", h.DocumentUtility.Merge)
				documents.POST("/:id/convert", h.DocumentUtility.Convert)
				documents.POST("/:id/duplicate", h.DocumentUtility.Duplicate)
				documents.GET("/:id/history", h.DocumentUtility.History)
				documents.POST("/:id/render", h.DocumentUtility.Render)
				documents.GET("/:id/pdf", h.DocumentUtility.GetPDF)
			}

			journals := protected.Group("/journals")
			{
				journals.GET("", h.Journal.List)
				journals.GET("/:id", h.Journal.Get)
				journals.POST("", h.Journal.Create)
				journals.PUT("/:id", h.Journal.Update)
				journals.DELETE("/:id", h.Journal.Delete)
				journals.POST("/:id/post", h.Journal.Post)
				journals.POST("/:id/reverse", h.Journal.Reverse)
			}

			renderProfiles := protected.Group("/render-profiles")
			{
				renderProfiles.GET("", h.RenderProfile.List)
				renderProfiles.GET("/default", h.RenderProfile.GetDefault)
				renderProfiles.GET("/:id", h.RenderProfile.Get)
				renderProfiles.POST("", h.RenderProfile.Create)
				renderProfiles.PUT("/:id", h.RenderProfile.Update)
				renderProfiles.DELETE("/:id", h.RenderProfile.Delete)
			}

			shipments := protected.Group("/shipments")
			{
				shipments.GET("/documents/:id", h.Shipment.GetByDocument)
				shipments.POST("/documents/:id", h.Shipment.UpsertByDocument)
				shipments.GET("/documents/:id/label", h.Shipment.GetLabelByDocument)
				shipments.POST("/documents/:id/label", h.Shipment.GenerateLabelByDocument)
			}

			ledger := protected.Group("/ledger")
			{
				ledger.GET("", h.Ledger.List)
				ledger.GET("/balance", h.Ledger.Balance)
			}

			teams := protected.Group("/teams")
			{
				teams.GET("", h.Team.List)
				teams.GET("/:id", h.Team.Get)
				teams.POST("", h.Team.Create)
				teams.PUT("/:id", h.Team.Update)
				teams.DELETE("/:id", h.Team.Delete)
			}

			webhooks := protected.Group("/webhooks")
			{
				webhooks.GET("", h.Webhook.List)
				webhooks.GET("/:id", h.Webhook.Get)
				webhooks.POST("", h.Webhook.Create)
				webhooks.PUT("/:id", h.Webhook.Update)
				webhooks.DELETE("/:id", h.Webhook.Delete)
			}

			subscriptions := protected.Group("/subscriptions")
			{
				subscriptions.GET("", h.Subscription.Get)
				subscriptions.POST("", h.Subscription.Create)
				subscriptions.PUT("", h.Subscription.Update)
			}

			agents := protected.Group("/agents")
			{
				agents.POST("", middleware.AgentCreationRateLimit(), h.Agent.CreateAgent)
				agents.GET("", h.Agent.ListAgents)
				agents.GET("/:id", h.Agent.GetAgent)
				agents.PUT("/:id", h.Agent.UpdateAgent)
				agents.DELETE("/:id", h.Agent.DeleteAgent)
				agents.POST("/:id/start", h.Procurement.StartAgent)
				agents.POST("/:id/procurement-runs", h.Procurement.CreateProcurementRun)
				agents.GET("/:id/procurement-runs/:run_id", h.Procurement.GetProcurementRun)
				agents.POST("/:id/procurement-runs/:run_id/cancel", h.Procurement.CancelProcurementRun)
				agents.GET("/:id/capabilities", h.Agent.GetAgentCapabilities)
				agents.POST("/:id/capabilities", h.Agent.AddCapability)
				agents.DELETE("/:id/capabilities/:capability_id", h.Agent.RemoveCapability)
				agents.GET("/active", h.Agent.GetActiveAgents)
				agents.GET("/type/:type", h.Agent.GetAgentByType)
				agents.POST("/validate-permissions/:id", h.Agent.ValidateAgentPermissions)
				agents.POST("/ideate", h.ShoppingAgent.GenerateIdeas)

				shopping := agents.Group("/shopping")
				{
					shopping.GET("/search", h.ShoppingAgent.SearchProducts)
					shopping.POST("/cart", middleware.ShoppingIntentRateLimit(), h.ShoppingAgent.CreateCart)
					shopping.POST("/cart/add", middleware.ShoppingIntentRateLimit(), h.ShoppingAgent.AddToCart)
					shopping.POST("/checkout", middleware.PaymentRateLimit(), h.ShoppingAgent.Checkout)
					shopping.GET("/cart/:id", h.ShoppingAgent.GetCart)
					shopping.GET("/carts", h.ShoppingAgent.ListCarts)
					shopping.GET("/orders", h.ShoppingAgent.ListOrders)
					shopping.GET("/orders/:id", h.ShoppingAgent.TrackOrder)
					shopping.GET("/products/available", h.ShoppingAgent.GetAvailableProducts)
					shopping.GET("/products/:id", h.ShoppingAgent.GetProductDetails)
					shopping.GET("/:id/capabilities", h.ShoppingAgent.GetAgentCapabilities)
				}

				credentials := agents.Group("/credentials")
				{
					credentials.GET("/payment-methods", h.Credential.ListPaymentMethods)
					credentials.GET("/payment-methods/default", h.Credential.GetDefaultPaymentMethod)
					credentials.POST("/payment-methods", h.Credential.AddPaymentMethod)
					credentials.GET("/payment-methods/:id", h.Credential.GetPaymentMethod)
					credentials.PUT("/payment-methods/:id", h.Credential.SetDefaultPaymentMethod)
					credentials.DELETE("/payment-methods/:id", h.Credential.DeletePaymentMethod)
					credentials.POST("/tokens", h.Credential.GenerateToken)
					credentials.GET("/tokens/validate", h.Credential.ValidateToken)
				}

				config := agents.Group("/config")
				{
					config.POST("", h.AgentConfig.CreateAgentConfig)
					config.GET("", h.AgentConfig.GetAllAgentConfigs)
					config.GET("/:agent_id", h.AgentConfig.GetAgentConfig)
					config.PUT("/:agent_id", h.AgentConfig.UpdateAgentConfig)
					config.DELETE("/:agent_id", h.AgentConfig.DeleteAgentConfig)
					config.POST("/default", h.AgentConfig.CreateDefaultConfig)

					mentee := config.Group("/mentee")
					{
						mentee.GET("/recommendation/:negotiation_id", h.AgentConfig.GetMenteeRecommendation)
						mentee.GET("/learning/:agent_id", h.AgentConfig.GetMenteeLearningData)
						mentee.DELETE("/learning/:agent_id", h.AgentConfig.ResetMenteeLearning)
						mentee.GET("/export", h.AgentConfig.ExportMenteeData)
						mentee.POST("/import", h.AgentConfig.ImportMenteeData)
					}
				}
			}

			marketplace := protected.Group("/marketplace")
			{
				marketplace.GET("/products", h.Marketplace.ListProducts)
				marketplace.GET("/products/search", h.Marketplace.SearchProducts)
				marketplace.GET("/products/:id", h.Marketplace.GetProduct)
				marketplace.GET("/products/available", h.Marketplace.GetAvailableProducts)
				marketplace.GET("/merchant/products", h.Marketplace.GetMerchantProducts)
				marketplace.POST("/merchant/products", h.Marketplace.AddProduct)
				marketplace.PUT("/merchant/products/:id", h.Marketplace.UpdateProduct)
				marketplace.GET("/orders", h.Marketplace.GetUserOrders)
				marketplace.GET("/orders/status/:status", h.Marketplace.GetOrdersByStatus)
				marketplace.GET("/stats", h.Marketplace.GetMarketplaceStats)
			}
		}

		// Agent Discovery endpoints
		discovery := protected.Group("/discovery")
		{
			// Discover agents
			discovery.GET("/agents", h.AgentDiscovery.DiscoverAgents)
			discovery.GET("/agents/public", h.AgentDiscovery.GetPublicAgents)
			discovery.GET("/agents/verified", h.AgentDiscovery.GetVerifiedAgents)
			discovery.GET("/agents/by-capability", h.AgentDiscovery.GetAgentsByCapability)
			discovery.GET("/agents/:agentID", h.AgentDiscovery.GetAgentRegistry)

			// Register agent
			discovery.POST("/agents/register", h.AgentDiscovery.RegisterAgent)

			// Admin operations
			discovery.POST("/agents/:registryID/verify", middleware.RequireRole("admin"), h.AgentDiscovery.VerifyAgent)
			discovery.POST("/agents/:registryID/unverify", middleware.RequireRole("admin"), h.AgentDiscovery.UnverifyAgent)
			discovery.POST("/agents/:registryID/deactivate", middleware.RequireRole("admin"), h.AgentDiscovery.DeactivateAgent)
			discovery.POST("/agents/:registryID/activate", middleware.RequireRole("admin"), h.AgentDiscovery.ActivateAgent)
			discovery.POST("/agents/:registryID/health-check", middleware.RequireRole("admin"), h.AgentDiscovery.HealthCheck)

			// User operations
			discovery.POST("/agents/:registryID/rate", h.AgentDiscovery.RateAgent)
			discovery.POST("/agents/:registryID/inquiry", h.AgentDiscovery.RecordInquiry)
			discovery.POST("/agents/:registryID/integration", h.AgentDiscovery.RecordIntegration)
		}

		// LLM endpoints
		llm := protected.Group("/llm")
		{
			llm.POST("/chat", h.LLM.Chat)
			llm.POST("/agent-assist", h.LLM.AgentAssist)
		}

		// Workflow automation endpoints
		workflows := protected.Group("/workflows")
		{
			workflows.POST("", h.Workflow.CreateWorkflow)
			workflows.GET("", h.Workflow.ListWorkflows)
			workflows.GET("/:id", h.Workflow.GetWorkflow)
			workflows.PUT("/:id", h.Workflow.UpdateWorkflow)
			workflows.DELETE("/:id", h.Workflow.DeleteWorkflow)
			workflows.POST("/:id/pause", h.Workflow.PauseWorkflow)
			workflows.POST("/:id/resume", h.Workflow.ResumeWorkflow)
			workflows.POST("/:id/run", h.Workflow.RunWorkflow)
		}

		// Intent Processing endpoints
		intent := protected.Group("/intent")
		{
			// Intent processing and parsing
			intent.POST("/process", h.Intent.ProcessIntent)
			intent.POST("/validate", h.Intent.ValidateIntent)
			intent.POST("/parse", h.Intent.ParseIntent)
		}

		// Latest A2A HTTP+JSON endpoints
		protected.Any("/a2a/*a2aPath", h.A2ATask.Handle)

		// WebSocket endpoints
		ws := protected.Group("/ws")
		{
			// WebSocket connection
			ws.GET("", h.WebSocket.HandleConnection)

			// WebSocket management endpoints
			ws.GET("/stats", h.WebSocket.GetStats)
			ws.GET("/status/:userID", h.WebSocket.GetConnectionStatus)
			ws.GET("/users", h.WebSocket.GetConnectedUsers)
			ws.GET("/health", h.WebSocket.HealthCheck)

			// Notification endpoints (for sending notifications via REST)
			ws.POST("/notify/:userID", h.WebSocket.SendNotification)
			ws.POST("/notify-all", h.WebSocket.SendNotificationToAll)
		}

		// Bargaining endpoints
		bargaining := protected.Group("/bargaining")
		{
			bargaining.POST("/negotiations", h.Bargaining.CreateNegotiation)
			bargaining.GET("/negotiations", h.Bargaining.ListNegotiations)
			bargaining.GET("/negotiations/:id", h.Bargaining.GetNegotiation)
			bargaining.POST("/negotiations/:id/counteroffer", h.Bargaining.SubmitCounterOffer)
			bargaining.GET("/negotiations/:id/rounds", h.Bargaining.GetNegotiationRounds)
			bargaining.GET("/negotiations/:id/suggest", h.Bargaining.GetSuggestedCounterOffer)
			bargaining.GET("/negotiations/llm-decision", h.Bargaining.GetLLMBargainingDecision)
			bargaining.GET("/negotiations/llm-summary", h.Bargaining.GetLLMNegotiationSummary)
		}

		// A2A Bargaining endpoints
		a2aBargaining := protected.Group("/a2a-bargaining")
		{
			a2aBargaining.POST("/start", h.A2ABargaining.StartNegotiation)
			a2aBargaining.GET("/progress/:sessionId", h.A2ABargaining.GetSessionProgress)
			a2aBargaining.POST("/stop/:sessionId", h.A2ABargaining.StopNegotiation)
		}

		admin := api.Group("/admin")
		admin.Use(middleware.Auth(cfg.Cognito, log))
		// admin.Use(middleware.RequireRole("admin"))
		{
			admin.GET("/local-emails", h.Admin.ListEmails)
		}
	}

	return router
}
