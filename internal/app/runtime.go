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
	WAF    *middleware.WAFRateLimiter
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

	// Initialize WAF rate limiter if enabled
	if cfg.AWS.WAF.Enabled && awsClients.WAF != nil {
		rt.WAF = middleware.NewWAFRateLimiter(awsClients.WAF, cfg.AWS.WAF)
		log.Info("AWS WAF rate limiting enabled", "web_acl_arn", cfg.AWS.WAF.WebACLArn)
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
	Reporting    interfaces.ReportingRepository
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
		Reporting:    postgresrepo.NewReportingRepository(db),
		Team:         postgresrepo.NewTeamMemberRepository(db),
		Webhook:      postgresrepo.NewWebhookRepository(db),
		Subscription: postgresrepo.NewSubscriptionRepository(db),
		AP2:          postgresrepo.NewAP2Repository(db),
	}
}

func initServices(cfg *config.Config, db *gorm.DB, repos *Repositories, aws *awsclients.Config, log *logger.Logger) *services.Container {
	return services.NewContainer(cfg, db, repos.User, repos.Business, repos.Customer, repos.Vendor,
		repos.Product, repos.Document, repos.Journal, repos.Inventory, repos.Shipping, repos.Invoice, repos.Payment, repos.Ledger, repos.Reporting, repos.Team,
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

		// WAF rate limiting (falls back to in-memory if WAF disabled)
		wafLoginRL := middleware.WAFRateLimit(svcs.AWS, cfg.AWS.WAF, time.Minute, 5)
		wafSensitiveRL := middleware.WAFRateLimit(svcs.AWS, cfg.AWS.WAF, time.Hour, 3)
		wafCommonRL := middleware.WAFCommonRateLimit(svcs.AWS, cfg.AWS.WAF)
		wafBulkRL := middleware.WAFBulkRateLimit(svcs.AWS, cfg.AWS.WAF)
		wafLLMRL := middleware.WAFLLMRateLimit(svcs.AWS, cfg.AWS.WAF)
		wafWSRL := middleware.WAFWebSocketRateLimit(svcs.AWS, cfg.AWS.WAF)
		wafUserWriteRL := middleware.WAFUserWriteRateLimit(svcs.AWS, cfg.AWS.WAF)
		wafUserHeavyRL := middleware.WAFUserHeavyRateLimit(svcs.AWS, cfg.AWS.WAF)
		wafUserReportRL := middleware.WAFUserReportRateLimit(svcs.AWS, cfg.AWS.WAF)

		auth := api.Group("/auth")
		{
			auth.POST("/register", sensitiveRL, wafSensitiveRL, h.Auth.Register)
			auth.POST("/login", loginRL, wafLoginRL, h.Auth.Login)
			auth.POST("/logout", h.Auth.Logout)
			auth.POST("/refresh", h.Auth.Refresh)
			auth.POST("/phone/register", sensitiveRL, wafSensitiveRL, h.Auth.PhoneRegister)
			auth.POST("/phone/confirm", sensitiveRL, wafSensitiveRL, h.Auth.PhoneConfirm)
			auth.POST("/phone/resend-confirmation", sensitiveRL, wafSensitiveRL, h.Auth.PhoneResendConfirmation)
			auth.POST("/phone/login", loginRL, wafLoginRL, h.Auth.PhoneLogin)
			auth.POST("/phone/verify-login", sensitiveRL, wafSensitiveRL, h.Auth.PhoneVerifyLogin)
			auth.POST("/phone/refresh", h.Auth.PhoneRefresh)
			auth.POST("/forgot-password", sensitiveRL, wafSensitiveRL, h.Auth.ForgotPassword)
			auth.POST("/reset-password", sensitiveRL, wafSensitiveRL, h.Auth.ResetPassword)
			auth.POST("/verify-email", h.Auth.VerifyEmail)
			auth.POST("/resend-verification", sensitiveRL, wafSensitiveRL, h.Auth.ResendVerification)
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
		googleAuth.Use(middleware.AuthWithTokenUse(cfg.Cognito, log, middleware.TokenUseID))
		{
			googleAuth.POST("/google", h.Auth.GoogleLogin)
		}

		public := api.Group("/public")
		{
			public.GET("/report-shares/:token/metadata", middleware.ReportShareRateLimit(), h.Report.PublicMetadata)
			public.POST("/report-shares/:token/access", middleware.ReportShareRateLimit(), h.Report.PublicAccess)

			store := public.Group("/store/:slug")
			store.Use(middleware.StorefrontCatalogRateLimit())
			{
				store.GET("/catalog", h.Commerce.PublicCatalog)
				store.GET("/categories", h.Commerce.PublicCategories)
				store.POST("/coupons/validate", middleware.StorefrontCouponRateLimit(), h.Commerce.PublicValidateCoupon)
				store.POST("/checkout", middleware.StorefrontCheckoutRateLimit(), h.Commerce.PublicCheckout)
				store.GET("/orders/:token", h.Commerce.PublicOrder)
				store.POST("/webhooks/payment/razorpay", h.Commerce.PublicRazorpayWebhook)
			}
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
				businesses.POST("", wafUserWriteRL, h.Business.Create)
				businesses.PUT("/:id", wafUserWriteRL, h.Business.Update)
				businesses.DELETE("/:id", wafUserWriteRL, h.Business.Delete)
				businesses.POST("/:id/logo", h.Business.UploadLogo)
			}

			customers := protected.Group("/customers")
			{
				customers.GET("", h.Customer.List)
				customers.GET("/:id", h.Customer.Get)
				customers.POST("", wafUserWriteRL, h.Customer.Create)
				customers.PUT("/:id", wafUserWriteRL, h.Customer.Update)
				customers.DELETE("/:id", wafUserWriteRL, h.Customer.Delete)
				customers.POST("/import", wafBulkRL, h.Customer.Import)
				customers.GET("/export", h.Customer.Export)
			}

			vendors := protected.Group("/vendors")
			{
				vendors.GET("", h.Vendor.List)
				vendors.GET("/:id", h.Vendor.Get)
				vendors.POST("", wafUserWriteRL, h.Vendor.Create)
				vendors.PUT("/:id", wafUserWriteRL, h.Vendor.Update)
				vendors.DELETE("/:id", wafUserWriteRL, h.Vendor.Delete)
			}

			products := protected.Group("/products")
			{
				products.GET("", h.Product.List)
				products.GET("/:id", h.Product.Get)
				products.POST("", wafUserWriteRL, h.Product.Create)
				products.PUT("/:id", wafUserWriteRL, h.Product.Update)
				products.DELETE("/:id", wafUserWriteRL, h.Product.Delete)
				products.POST("/:id/clone", wafUserWriteRL, h.Product.Clone)
				products.POST("/:id/image", h.Product.UploadImage)
				products.POST("/:id/stock", wafUserWriteRL, h.Product.AdjustStock)
			}

			projects := protected.Group("/projects")
			{
				projects.GET("", h.Project.List)
				projects.POST("", middleware.RequireRole("admin", "accountant"), h.Project.Create)
				projects.PUT("/:id", middleware.RequireRole("admin", "accountant"), h.Project.Update)
				projects.DELETE("/:id", middleware.RequireRole("admin", "accountant"), h.Project.Delete)
			}

			reports := protected.Group("/reports")
			{
				reports.GET("/catalog", h.Report.Catalog)
				reports.GET("/dashboard", h.Report.Dashboard)
				reports.GET("/shares/history", h.Report.ShareHistory)
				reports.GET("/preferences/:key", h.Report.GetPreference)
				reports.PUT("/preferences/:key", h.Report.SavePreference)
				reports.POST("/:key/query", wafUserReportRL, h.Report.Query)
				reports.POST("/:key/export", wafUserReportRL, h.Report.Export)
				reports.POST("/:key/share", wafUserHeavyRL, h.Report.CreateShare)
			}

			warehouses := protected.Group("/warehouses")
			{
				warehouses.GET("", h.Inventory.ListWarehouses)
				warehouses.POST("", h.Inventory.CreateWarehouse)
				warehouses.PUT("/:id", h.Inventory.UpdateWarehouse)
				warehouses.DELETE("/:id", h.Inventory.DeleteWarehouse)
				warehouses.POST("/:id/catalog", h.Inventory.UpsertCatalog)
				warehouses.GET("/:id/permissions", h.Inventory.ListPermissions)
				warehouses.POST("/:id/permissions", h.Inventory.UpsertPermissions)
			}

			inventory := protected.Group("/inventory")
			{
				inventory.POST("/adjustments", h.Inventory.CreateAdjustment)
				inventory.POST("/transfers", h.Inventory.CreateTransfer)
				inventory.POST("/resets", h.Inventory.ResetStock)
				inventory.GET("/timeline", h.Inventory.Timeline)
				inventory.GET("/valuation", h.Inventory.Valuation)
				inventory.GET("/alerts", h.Inventory.Alerts)
				inventory.GET("/batches", h.Inventory.ListBatches)
				inventory.GET("/serials", h.Inventory.ListSerials)
			}

			assemblies := protected.Group("/assemblies")
			{
				assemblies.GET("", h.Inventory.ListAssemblyRecipes)
				assemblies.POST("", h.Inventory.CreateAssemblyRecipe)
				assemblies.POST("/:id/build", h.Inventory.BuildAssembly)
				assemblies.POST("/:id/disassemble", h.Inventory.DisassembleAssembly)
			}

			barcodes := protected.Group("/barcodes")
			{
				barcodes.POST("/generate", h.Barcode.Generate)
				barcodes.POST("/assign", h.Barcode.Assign)
				barcodes.GET("/lookup", h.Barcode.Lookup)
				barcodes.POST("/render/png", h.Barcode.RenderPNG)
				barcodes.POST("/render/svg", h.Barcode.RenderSVG)
				barcodes.POST("/render/pdf", h.Barcode.RenderPDF)
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
			registerDocumentResource("/expenses", h.Expense)
			registerDocumentResource("/packing-lists", h.PackingList)
			registerDocumentResource("/shipping-labels", h.ShippingLabel)

			invoices := protected.Group("/invoices")
			{
				invoices.GET("", h.Invoice.List)
				invoices.GET("/:id", h.Invoice.Get)
				invoices.POST("", wafUserWriteRL, h.Invoice.Create)
				invoices.PUT("/:id", wafUserWriteRL, h.Invoice.Update)
				invoices.DELETE("", wafUserWriteRL, h.Invoice.Delete)
				invoices.POST("/:id/send", wafUserWriteRL, h.Invoice.Send)
				invoices.POST("/bulk-actions", wafUserHeavyRL, h.BillingOps.CreateInvoiceBulkAction)
				invoices.GET("/:id/pdf", h.Invoice.GetPDF)
				invoices.POST("/:id/einvoice", wafUserHeavyRL, h.Invoice.GenerateEInvoice)
				invoices.GET("/next-number", h.Invoice.NextNumber)
			}

			payments := protected.Group("/payments")
			{
				payments.GET("", h.Payment.List)
				payments.GET("/:id", h.Payment.Get)
				payments.POST("", wafUserWriteRL, h.Payment.Create)
				payments.PUT("/:id", wafUserWriteRL, h.Payment.Update)
				payments.DELETE("", wafUserWriteRL, h.Payment.Delete)
			}

			documents := protected.Group("/documents")
			{
				documents.POST("/merge", wafUserHeavyRL, h.DocumentUtility.Merge)
				documents.POST("/bulk-actions", wafUserHeavyRL, h.BillingOps.CreateDocumentBulkAction)
				documents.POST("/:id/convert", wafUserHeavyRL, h.DocumentUtility.Convert)
				documents.POST("/:id/duplicate", wafUserHeavyRL, h.DocumentUtility.Duplicate)
				documents.GET("/:id/history", h.DocumentUtility.History)
				documents.GET("/:id/compliance", h.DocumentUtility.GetComplianceStatus)
				documents.POST("/:id/einvoice", wafUserHeavyRL, h.DocumentUtility.GenerateEInvoice)
				documents.GET("/:id/einvoice", h.DocumentUtility.GetEInvoice)
				documents.POST("/:id/einvoice/cancel", wafUserHeavyRL, h.DocumentUtility.CancelEInvoice)
				documents.POST("/:id/ewaybill", wafUserHeavyRL, h.DocumentUtility.GenerateEWayBill)
				documents.GET("/:id/ewaybill", h.DocumentUtility.GetEWayBill)
				documents.GET("/:id/ewaybill/pdf", h.DocumentUtility.GetEWayBillPDF)
				documents.PATCH("/:id/ewaybill/part-b", wafUserHeavyRL, h.DocumentUtility.UpdateEWayPartB)
				documents.POST("/:id/ewaybill/multi-vehicle", wafUserHeavyRL, h.DocumentUtility.InitiateMultiVehicle)
				documents.POST("/:id/render", h.DocumentUtility.Render)
				documents.GET("/:id/pdf", h.DocumentUtility.GetPDF)
			}

			priceLists := protected.Group("/price-lists")
			{
				priceLists.GET("", h.BillingOps.ListPriceLists)
				priceLists.GET("/:id", h.BillingOps.GetPriceList)
				priceLists.POST("", wafUserWriteRL, h.BillingOps.CreatePriceList)
				priceLists.PUT("/:id", wafUserWriteRL, h.BillingOps.UpdatePriceList)
				priceLists.DELETE("", wafUserWriteRL, h.BillingOps.DeletePriceList)
			}

			partyGroups := protected.Group("/party-groups")
			{
				partyGroups.GET("", h.BillingOps.ListPartyGroups)
				partyGroups.GET("/:id", h.BillingOps.GetPartyGroup)
				partyGroups.GET("/:id/ledger", h.BillingOps.GetPartyGroupLedger)
				partyGroups.POST("", wafUserWriteRL, h.BillingOps.CreatePartyGroup)
				partyGroups.PUT("/:id", wafUserWriteRL, h.BillingOps.UpdatePartyGroup)
				partyGroups.DELETE("", wafUserWriteRL, h.BillingOps.DeletePartyGroup)
			}

			activityLogs := protected.Group("/activity-logs")
			{
				activityLogs.GET("", h.BillingOps.ListActivityLogs)
			}

			signatures := protected.Group("/signatures")
			{
				signatures.GET("/profiles", h.BillingOps.ListSignatureProfiles)
				signatures.GET("/profiles/:id", h.BillingOps.GetSignatureProfile)
				signatures.POST("/profiles", h.BillingOps.CreateSignatureProfile)
				signatures.POST("/invoices/:id", h.BillingOps.SignInvoice)
				signatures.POST("/documents/:id", h.BillingOps.SignDocument)
			}

			bulkJobs := protected.Group("/bulk-jobs")
			{
				bulkJobs.GET("", h.BillingOps.ListBulkJobs)
				bulkJobs.GET("/:id", h.BillingOps.GetBulkJob)
			}

			imports := protected.Group("/imports")
			{
				imports.POST("/customers", wafBulkRL, h.BillingOps.CreateCustomerImportJob)
				imports.POST("/vendors", wafBulkRL, h.BillingOps.CreateVendorImportJob)
				imports.POST("/products", wafBulkRL, h.BillingOps.CreateProductImportJob)
				imports.POST("/invoices", wafBulkRL, h.BillingOps.CreateInvoiceImportJob)
				imports.POST("/documents", wafBulkRL, h.BillingOps.CreateDocumentImportJob)
			}

			invoiceSubscriptions := protected.Group("/invoice-subscriptions")
			{
				invoiceSubscriptions.GET("", h.BillingOps.ListInvoiceSubscriptions)
				invoiceSubscriptions.GET("/runs", h.BillingOps.ListAggregatedInvoiceSubscriptionRuns)
				invoiceSubscriptions.GET("/:id", h.BillingOps.GetInvoiceSubscription)
				invoiceSubscriptions.GET("/:id/runs", h.BillingOps.ListInvoiceSubscriptionRuns)
				invoiceSubscriptions.POST("", h.BillingOps.CreateInvoiceSubscription)
				invoiceSubscriptions.PUT("/:id", h.BillingOps.UpdateInvoiceSubscription)
				invoiceSubscriptions.POST("/:id/pause", h.BillingOps.PauseInvoiceSubscription)
				invoiceSubscriptions.POST("/:id/resume", h.BillingOps.ResumeInvoiceSubscription)
				invoiceSubscriptions.POST("/:id/generate-now", h.BillingOps.GenerateInvoiceSubscriptionNow)
			}

			journals := protected.Group("/journals")
			{
				journals.GET("", h.Journal.List)
				journals.GET("/:id", h.Journal.Get)
				journals.POST("", wafUserWriteRL, h.Journal.Create)
				journals.PUT("/:id", wafUserWriteRL, h.Journal.Update)
				journals.DELETE("/:id", wafUserWriteRL, h.Journal.Delete)
				journals.POST("/:id/post", wafUserWriteRL, h.Journal.Post)
				journals.POST("/:id/reverse", wafUserWriteRL, h.Journal.Reverse)
			}

			renderProfiles := protected.Group("/render-profiles")
			{
				renderProfiles.GET("", h.RenderProfile.List)
				renderProfiles.GET("/default", h.RenderProfile.GetDefault)
				renderProfiles.GET("/:id", h.RenderProfile.Get)
				renderProfiles.POST("", wafUserWriteRL, h.RenderProfile.Create)
				renderProfiles.POST("/:id/default", wafUserWriteRL, h.RenderProfile.SetDefault)
				renderProfiles.PUT("/:id", wafUserWriteRL, h.RenderProfile.Update)
				renderProfiles.DELETE("/:id", wafUserWriteRL, h.RenderProfile.Delete)
			}

			utils := protected.Group("/utils")
			{
				utils.POST("/gstin/:gstin/fetch", h.Tax.FetchGSTIN)
			}

			tax := protected.Group("/tax")
			{
				tax.GET("/integrations", h.Tax.ListIntegrationAccounts)
				tax.POST("/integrations", h.Tax.UpsertIntegrationAccount)
				tax.PUT("/integrations/:id", h.Tax.UpsertIntegrationAccount)
				tax.POST("/integrations/:id/validate", h.Tax.ValidateIntegrationAccount)
				tax.POST("/gstr-2b/import", h.Tax.ImportGSTR2B)
				tax.GET("/reports/:type", h.Tax.GetReport)
				tax.POST("/reports/:type/export", h.Tax.ExportReport)
				tax.GET("/report-runs/:id", h.Tax.GetReportRun)
			}

			pos := protected.Group("/pos")
			{
				pos.POST("/sessions", h.POS.CreateSession)
				pos.GET("/catalog/search", h.POS.SearchCatalog)
				pos.POST("/carts/:id/items/scan", h.POS.ScanItem)
				pos.POST("/carts/:id/checkout", h.POS.Checkout)
				pos.GET("/receipts/:documentID", h.POS.GetReceipt)
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
				subscriptions.GET("/entitlements", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionSubscriptionsView), h.Commerce.ListEntitlements)
				subscriptions.POST("/entitlements/sync", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionSubscriptionsManage), h.Commerce.SyncEntitlements)
			}

			branches := protected.Group("/branches")
			{
				branches.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionBranchesView), h.Commerce.ListBranches)
				branches.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionBranchesManage), h.Commerce.CreateBranch)
				branches.PUT("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionBranchesManage), h.Commerce.UpdateBranch)
				branches.DELETE("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionBranchesManage), h.Commerce.DeleteBranch)
			}

			roles := protected.Group("/roles")
			{
				roles.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionTeamsView), h.Commerce.ListRoles)
				roles.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionRolesManage), h.Commerce.CreateRole)
				roles.PUT("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionRolesManage), h.Commerce.UpdateRole)
				roles.DELETE("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionRolesManage), h.Commerce.DeleteRole)
			}

			storefronts := protected.Group("/storefronts")
			{
				storefronts.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionStorefrontView), h.Commerce.ListStorefronts)
				storefronts.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionStorefrontManage), h.Commerce.CreateStorefront)
				storefronts.GET("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionStorefrontView), h.Commerce.GetStorefront)
				storefronts.PUT("/:id/settings", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionStorefrontManage), h.Commerce.UpdateStorefrontSettings)
				storefronts.GET("/:id/products", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionStorefrontView), h.Commerce.ListStorefrontProducts)
				storefronts.PUT("/:id/products", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionStorefrontManage), h.Commerce.ReplaceStorefrontProducts)
				storefronts.GET("/:id/coupons", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionStorefrontView), h.Commerce.ListStorefrontCoupons)
				storefronts.POST("/:id/coupons", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionStorefrontManage), h.Commerce.CreateStorefrontCoupon)
				storefronts.PUT("/:id/coupons/:coupon_id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionStorefrontManage), h.Commerce.UpdateStorefrontCoupon)
				storefronts.GET("/:id/orders", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionOrdersView), h.Commerce.ListStorefrontOrders)
				storefronts.POST("/:id/orders/:order_id/approve", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionOrdersManage), h.Commerce.ApproveStorefrontOrder)
				storefronts.POST("/:id/orders/:order_id/cancel", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionOrdersManage), h.Commerce.CancelStorefrontOrder)
			}

			drive := protected.Group("/drive")
			{
				drive.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDriveView), h.Commerce.ListDriveAssets)
				drive.POST("/presign", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDriveManage), h.Commerce.CreateDriveUpload)
				drive.PATCH("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDriveManage), h.Commerce.UpdateDriveAsset)
				drive.DELETE("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDriveManage), h.Commerce.DeleteDriveAsset)
			}

			whatsapp := protected.Group("/notifications/whatsapp")
			{
				whatsapp.GET("/config", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.Commerce.GetWhatsAppConfig)
				whatsapp.PUT("/config", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.Commerce.UpsertWhatsAppConfig)
				whatsapp.GET("/deliveries", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.Commerce.ListNotificationDeliveries)
			}

			email := protected.Group("/email")
			{
				email.GET("/accounts", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.EmailConfig.ListAccounts)
				email.POST("/accounts", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.EmailConfig.CreateAccount)
				email.PUT("/accounts/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.EmailConfig.UpdateAccount)
				email.GET("/deliveries", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.EmailConfig.ListDeliveries)
				email.POST("/accounts/:id/test", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.EmailConfig.SendTest)
			}

			agents := protected.Group("/agents")
			{
				agents.POST("", middleware.AgentCreationRateLimit(), wafBulkRL, h.Agent.CreateAgent)
				agents.GET("", h.Agent.ListAgents)
				agents.GET("/:id", h.Agent.GetAgent)
				agents.PUT("/:id", h.Agent.UpdateAgent)
				agents.DELETE("/:id", h.Agent.DeleteAgent)
				agents.POST("/:id/start", wafBulkRL, h.Procurement.StartAgent)
				agents.POST("/:id/procurement-runs", wafBulkRL, h.Procurement.CreateProcurementRun)
				agents.GET("/:id/procurement-runs/:run_id", h.Procurement.GetProcurementRun)
				agents.POST("/:id/procurement-runs/:run_id/cancel", h.Procurement.CancelProcurementRun)
				agents.GET("/:id/capabilities", h.Agent.GetAgentCapabilities)
				agents.POST("/:id/capabilities", wafBulkRL, h.Agent.AddCapability)
				agents.DELETE("/:id/capabilities/:capability_id", h.Agent.RemoveCapability)
				agents.GET("/active", h.Agent.GetActiveAgents)
				agents.GET("/type/:type", h.Agent.GetAgentByType)
				agents.POST("/validate-permissions/:id", h.Agent.ValidateAgentPermissions)
				agents.POST("/ideate", wafLLMRL, h.ShoppingAgent.GenerateIdeas)

				shopping := agents.Group("/shopping")
				{
					shopping.GET("/search", wafCommonRL, h.ShoppingAgent.SearchProducts)
					shopping.POST("/cart", middleware.ShoppingIntentRateLimit(), h.ShoppingAgent.CreateCart)
					shopping.POST("/cart/add", middleware.ShoppingIntentRateLimit(), h.ShoppingAgent.AddToCart)
					shopping.POST("/checkout", middleware.PaymentRateLimit(), wafBulkRL, h.ShoppingAgent.Checkout)
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
				marketplace.GET("/products", wafCommonRL, h.Marketplace.ListProducts)
				marketplace.GET("/products/search", wafCommonRL, h.Marketplace.SearchProducts)
				marketplace.GET("/products/:id", h.Marketplace.GetProduct)
				marketplace.GET("/products/available", h.Marketplace.GetAvailableProducts)
				marketplace.GET("/merchant/products", h.Marketplace.GetMerchantProducts)
				marketplace.POST("/merchant/products", wafBulkRL, h.Marketplace.AddProduct)
				marketplace.PUT("/merchant/products/:id", h.Marketplace.UpdateProduct)
				marketplace.GET("/orders", h.Marketplace.GetUserOrders)
				marketplace.GET("/orders/status/:status", h.Marketplace.GetOrdersByStatus)
				marketplace.GET("/stats", wafCommonRL, h.Marketplace.GetMarketplaceStats)
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
			llm.POST("/chat", wafLLMRL, h.LLM.Chat)
			llm.POST("/agent-assist", wafLLMRL, h.LLM.AgentAssist)
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
			ws.GET("", wafWSRL, h.WebSocket.HandleConnection)

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
			a2aBargaining.POST("/autonomous/start", h.A2ABargaining.StartAutonomousNegotiation)
			a2aBargaining.GET("/progress/:sessionId", h.A2ABargaining.GetSessionProgress)
			a2aBargaining.POST("/stop/:sessionId", h.A2ABargaining.StopNegotiation)
		}

		admin := api.Group("/admin")
		admin.Use(middleware.Auth(cfg.Cognito, log))
		admin.Use(middleware.RequireRole("admin"))
		{
			if shouldExposeAdminLocalEmails(cfg.Environment) {
				admin.GET("/local-emails", h.Admin.ListEmails)
			}
		}
	}

	return router
}

func shouldExposeAdminLocalEmails(environment string) bool {
	return !logger.IsProductionEnvironment(environment)
}
