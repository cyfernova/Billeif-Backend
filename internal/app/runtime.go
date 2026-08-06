package app

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	_ "time/tzdata"

	"invoice-backend/docs"
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

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"github.com/swaggo/swag"
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

// @securityDefinitions.oauth2.accessCode BearerAuth
// @authorizationurl https://example.com/oauth2/authorize
// @tokenUrl https://example.com/oauth2/token

type InitializeOptions struct {
	EnableWorker bool
	Profile      config.Profile
}

type Runtime struct {
	Config *config.Config

	Log     *logger.Logger
	DB      *gorm.DB
	AWS     *awsclients.Config
	Repos   *Repositories
	Svcs    *services.Container
	H       *handlers.Handler
	Router  *gin.Engine
	Worker  *workers.Worker
	WAF     *middleware.WAFRateLimiter
	Secrets *config.RuntimeResolver
}

func Initialize(ctx context.Context, opts InitializeOptions) (*Runtime, error) {
	bootstrapLog := logger.New().Named("bootstrap")

	cfg, err := config.LoadForProfile(opts.Profile)
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

	awsClients, err := awsclients.New(ctx, cfg.AWS, log.Named("awsclients"))
	if err != nil {
		log.Sync()
		return nil, fmt.Errorf("initialize AWS clients: %w", err)
	}

	resolver, err := config.NewRuntimeResolver(config.RuntimeResolverOptions{
		Clients: config.RuntimeResolvers{
			Secrets: secretsmanager.NewFromConfig(awsClients.SDKConfig),
			SSM:     awsClients.SSM,
		},
		SecretIdentifiers: cfg.SecretIdentifierValues(),
		ParameterNames:    []string{cfg.SSM.DatabaseHostParam},
	})
	if err != nil {
		log.Sync()
		return nil, fmt.Errorf("initialize runtime resolver: %w", err)
	}
	var invoiceCursor handlers.InvoiceCursorCodec
	if opts.Profile == config.ProfileHTTP && strings.TrimSpace(cfg.Secrets.InvoiceCursorHMAC) != "" {
		invoiceCursor, err = resolver.InvoiceCursorCodec(ctx, cfg.Secrets.InvoiceCursorHMAC)
		if err != nil {
			log.Sync()
			return nil, fmt.Errorf("initialize invoice cursor codec: %w", err)
		}
	}
	db, err := initDatabase(cfg, resolver, log, opts.Profile)
	if err != nil {
		log.Sync()
		return nil, fmt.Errorf("connect database: %w", err)
	}

	repos := initRepositories(db)
	svcs := initServices(cfg, db, repos, awsClients, resolver, log)
	h := handlers.New(svcs, &handlers.Repositories{AP2: repos.AP2}, cfg, log, invoiceCursor)
	router := setupRouter(cfg, svcs, h, log)

	rt := &Runtime{
		Config:  cfg,
		Log:     log,
		DB:      db,
		AWS:     awsClients,
		Repos:   repos,
		Svcs:    svcs,
		H:       h,
		Router:  router,
		Secrets: resolver,
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
	if r.Svcs != nil && r.Svcs.Workflow != nil {
		r.Svcs.Workflow.Stop()
	}
	if r.DB != nil {
		if sqlDB, err := r.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
	pkgsentry.Flush(2 * time.Second)
	if r.Log != nil {
		r.Log.Sync()
	}
}

type postgresConnectorFactory func(string) (driver.Connector, error)

type refreshingPostgresConnector struct {
	resolver *config.RuntimeResolver
	cfg      *config.Config
	factory  postgresConnectorFactory
}

func newRefreshingPostgresConnector(resolver *config.RuntimeResolver, cfg *config.Config, factory postgresConnectorFactory) driver.Connector {
	return &refreshingPostgresConnector{resolver: resolver, cfg: cfg, factory: factory}
}

func (c *refreshingPostgresConnector) Connect(ctx context.Context) (driver.Conn, error) {
	credentials, err := c.resolver.Database(ctx, c.cfg)
	if err != nil {
		return nil, err
	}
	connector, err := c.factory(postgresDataSourceName(credentials))
	if err != nil {
		return nil, &config.ConfigurationError{Resource: "database connection configuration", Reason: "invalid"}
	}
	return connector.Connect(ctx)
}

func (c *refreshingPostgresConnector) Driver() driver.Driver {
	return &refreshingPostgresDriver{connector: c}
}

type refreshingPostgresDriver struct{ connector driver.Connector }

func (d *refreshingPostgresDriver) Open(string) (driver.Conn, error) {
	return d.connector.Connect(context.Background())
}

func postgresDataSourceName(credentials config.DatabaseCredentials) string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		quotePostgresValue(credentials.Host),
		credentials.Port,
		quotePostgresValue(credentials.User),
		quotePostgresValue(credentials.Password),
		quotePostgresValue(credentials.Name),
		quotePostgresValue(credentials.SSLMode),
	)
}

func quotePostgresValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `\'`)
	return "'" + value + "'"
}

type databasePool interface {
	SetMaxOpenConns(int)
	SetMaxIdleConns(int)
	SetConnMaxIdleTime(time.Duration)
	SetConnMaxLifetime(time.Duration)
}

func configureDatabasePool(pool databasePool, profile config.Profile) {
	maxOpenConnections := 1
	maxIdleConnections := 0
	if profile == config.ProfileHTTP || profile == config.ProfileA2A {
		maxOpenConnections = 2
		maxIdleConnections = 1
	}
	pool.SetMaxOpenConns(maxOpenConnections)
	pool.SetMaxIdleConns(maxIdleConnections)
	pool.SetConnMaxIdleTime(2 * time.Minute)
	pool.SetConnMaxLifetime(10 * time.Minute)
}

func initDatabase(cfg *config.Config, resolver *config.RuntimeResolver, log *logger.Logger, profile config.Profile) (*gorm.DB, error) {
	gormLevel := "warn"
	if strings.EqualFold(cfg.Logging.Level, "debug") || strings.EqualFold(cfg.Logging.Level, "info") {
		gormLevel = cfg.Logging.Level
	}

	connector := newRefreshingPostgresConnector(resolver, cfg, func(dataSourceName string) (driver.Connector, error) {
		return pq.NewConnector(dataSourceName)
	})
	sqlDB := sql.OpenDB(connector)
	configureDatabasePool(sqlDB, profile)

	return gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
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

func OpenDatabase(cfg *config.Config, resolver *config.RuntimeResolver, log *logger.Logger) (*gorm.DB, error) {
	return initDatabase(cfg, resolver, log, "")
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
	Invoice      interfaces.CanonicalInvoiceRepository
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

func initServices(cfg *config.Config, db *gorm.DB, repos *Repositories, aws *awsclients.Config, resolver services.ProviderConfigResolver, log *logger.Logger) *services.Container {
	return services.NewContainer(cfg, resolver, db, repos.User, repos.Business, repos.Customer, repos.Vendor,
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
	router.Use(middleware.ThreatDetection())

	isProd := logger.IsProductionEnvironment(cfg.Environment)

	// Use Sentry-aware recovery if Sentry is enabled
	if cfg.Sentry.DSN != "" {
		router.Use(middleware.SentryRecovery(log))
	} else {
		router.Use(middleware.RecoveryWithOptions(log, isProd))
	}
	router.Use(middleware.CORS(cfg))

	router.GET("/health", h.Health.Check)

	registerSwaggerRoutes(router, cfg)

	// Well-known endpoints
	router.GET("/.well-known/agent-card.json", h.WellKnown.GetAgentCard)
	router.GET("/.well-known/agents.json", func(c *gin.Context) {
		c.File(".well-known/agents.json")
	})

	api := router.Group("/api/v1")
	{
		// MCP tool routing endpoints
		log.Info("MCP handler status", "mcp_handler_nil", h.MCP == nil)
		if h.MCP != nil {
			mcpGroup := api.Group("/mcp")
			mcpGroup.Use(middleware.Auth(cfg.Cognito, log))
			{
				mcpGroup.GET("/tools/list", h.MCP.ListTools)
				mcpGroup.POST("/tools/call", middleware.RequireRole("admin"), h.MCP.CallTool)
				mcpGroup.GET("/health", h.MCP.HealthCheck)
			}
		}

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
			}
		}

		api.POST("/webhooks/razorpay", middleware.RazorpayWebhookRateLimit(), wafCommonRL, h.RazorpayPayment.Webhook)

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

			dashboard := protected.Group("/dashboard")
			{
				dashboard.GET("/summary", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Dashboard.Summary)
			}

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
				products.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsView), h.Product.List)
				products.GET("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsView), h.Product.Get)
				products.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), wafUserWriteRL, h.Product.Create)
				products.PUT("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), wafUserWriteRL, h.Product.Update)
				products.DELETE("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), wafUserWriteRL, h.Product.Delete)
				products.POST("/:id/clone", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), wafUserWriteRL, h.Product.Clone)
				products.POST("/:id/image", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), h.Product.UploadImage)
				products.POST("/:id/stock", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), wafUserWriteRL, h.Product.AdjustStock)
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
				reports.GET("/catalog", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Report.Catalog)
				reports.GET("/dashboard", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Report.Dashboard)
				reports.GET("/shares/history", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsShare), h.Report.ShareHistory)
				reports.GET("/preferences/:key", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Report.GetPreference)
				reports.PUT("/preferences/:key", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Report.SavePreference)
				reports.POST("/:key/query", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), wafUserReportRL, h.Report.Query)
				reports.POST("/:key/export", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsExport), wafUserReportRL, h.Report.Export)
				reports.POST("/:key/share", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsShare), wafUserHeavyRL, h.Report.CreateShare)
			}

			warehouses := protected.Group("/warehouses")
			{
				warehouses.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsView), h.Inventory.ListWarehouses)
				warehouses.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), h.Inventory.CreateWarehouse)
				warehouses.PUT("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), h.Inventory.UpdateWarehouse)
				warehouses.DELETE("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), h.Inventory.DeleteWarehouse)
				warehouses.POST("/:id/catalog", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), h.Inventory.UpsertCatalog)
				warehouses.GET("/:id/permissions", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), h.Inventory.ListPermissions)
				warehouses.POST("/:id/permissions", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), h.Inventory.UpsertPermissions)
			}

			inventory := protected.Group("/inventory")
			{
				inventory.POST("/adjustments", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), h.Inventory.CreateAdjustment)
				inventory.GET("/transfers", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Inventory.ListTransfers)
				inventory.POST("/transfers", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), h.Inventory.CreateTransfer)
				inventory.POST("/transfers/:id/complete", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), h.Inventory.CompleteTransfer)
				inventory.POST("/resets", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), h.Inventory.ResetStock)
				inventory.GET("/timeline", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Inventory.Timeline)
				inventory.GET("/valuation", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Inventory.Valuation)
				inventory.GET("/alerts", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Inventory.Alerts)
				inventory.GET("/batches", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Inventory.ListBatches)
				inventory.GET("/serials", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Inventory.ListSerials)
			}

			assemblies := protected.Group("/assemblies")
			{
				assemblies.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsView), h.Inventory.ListAssemblyRecipes)
				assemblies.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), h.Inventory.CreateAssemblyRecipe)
				assemblies.POST("/:id/build", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), h.Inventory.BuildAssembly)
				assemblies.POST("/:id/disassemble", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), h.Inventory.DisassembleAssembly)
			}

			barcodes := protected.Group("/barcodes")
			{
				barcodes.GET("", h.Barcode.List)
				barcodes.POST("/generate", h.Barcode.Generate)
				barcodes.POST("/assign", h.Barcode.Assign)
				barcodes.GET("/lookup", h.Barcode.Lookup)
				barcodes.POST("/render/png", h.Barcode.RenderPNG)
				barcodes.POST("/render/svg", h.Barcode.RenderSVG)
				barcodes.POST("/render/pdf", h.Barcode.RenderPDF)
			}

			registerDocumentResource := func(path string, handler *handlers.DocumentHandler) {
				group := protected.Group(path)
				group.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), handler.List)
				group.GET("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), handler.Get)
				group.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), handler.Create)
				group.PUT("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), handler.Update)
				group.DELETE("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), handler.Delete)
				group.POST("/:id/cancel", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), handler.Cancel)
				group.GET("/:id/pdf", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), handler.GetPDF)
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
				invoices.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.Invoice.List)
				invoices.GET("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.Invoice.Get)
				invoices.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserWriteRL, h.Invoice.Create)
				invoices.POST("/:id/issue", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserWriteRL, h.Invoice.Issue)
				invoices.POST("/:id/previews", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), wafUserHeavyRL, h.Invoice.Preview)
				invoices.PATCH("/:id/draft", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserWriteRL, h.Invoice.UpdateDraft)
				invoices.PUT("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserWriteRL, h.Invoice.Update)
				invoices.DELETE("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserWriteRL, h.Invoice.Delete)
				invoices.POST("/bulk-actions", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserHeavyRL, h.BillingOps.CreateInvoiceBulkAction)
				invoices.GET("/:id/renders/:render_job_id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.Invoice.GetRenderStatus)
				invoices.GET("/:id/pdf", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.Invoice.GetPDF)
				invoices.POST("/:id/deliveries", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserWriteRL, h.Invoice.Deliver)
				invoices.GET("/:id/deliveries/:delivery_id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.Invoice.GetDeliveryStatus)
				invoices.POST("/:id/einvoice", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserHeavyRL, h.Invoice.GenerateEInvoice)
			}

			payments := protected.Group("/payments")
			{
				payments.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsView), h.Payment.List)
				payments.POST("/razorpay/order", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsManage), middleware.RazorpayCreateOrderRateLimit(), wafUserHeavyRL, h.RazorpayPayment.CreateOrder)
				payments.POST("/razorpay/verify", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsManage), middleware.RazorpayVerifyRateLimit(), wafUserHeavyRL, h.RazorpayPayment.VerifyPayment)
				payments.GET("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsView), h.Payment.Get)
				payments.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsManage), wafUserWriteRL, h.Payment.Create)
				payments.PUT("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsManage), wafUserWriteRL, h.Payment.Update)
				payments.DELETE("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsManage), wafUserWriteRL, h.Payment.Delete)
			}

			documents := protected.Group("/documents")
			{
				documents.POST("/merge", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserHeavyRL, h.DocumentUtility.Merge)
				documents.POST("/bulk-actions", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserHeavyRL, h.BillingOps.CreateDocumentBulkAction)
				documents.POST("/:id/convert", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserHeavyRL, h.DocumentUtility.Convert)
				documents.POST("/:id/duplicate", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserHeavyRL, h.DocumentUtility.Duplicate)
				documents.GET("/:id/history", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.DocumentUtility.History)
				documents.GET("/:id/compliance", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.DocumentUtility.GetComplianceStatus)
				documents.POST("/:id/einvoice", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserHeavyRL, h.DocumentUtility.GenerateEInvoice)
				documents.GET("/:id/einvoice", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.DocumentUtility.GetEInvoice)
				documents.POST("/:id/einvoice/cancel", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserHeavyRL, h.DocumentUtility.CancelEInvoice)
				documents.POST("/:id/ewaybill", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserHeavyRL, h.DocumentUtility.GenerateEWayBill)
				documents.GET("/:id/ewaybill", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.DocumentUtility.GetEWayBill)
				documents.GET("/:id/ewaybill/pdf", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.DocumentUtility.GetEWayBillPDF)
				documents.PATCH("/:id/ewaybill/part-b", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserHeavyRL, h.DocumentUtility.UpdateEWayPartB)
				documents.POST("/:id/ewaybill/multi-vehicle", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserHeavyRL, h.DocumentUtility.InitiateMultiVehicle)
				documents.POST("/:id/render", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.DocumentUtility.Render)
				documents.GET("/:id/pdf", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.DocumentUtility.GetPDF)
			}

			priceLists := protected.Group("/price-lists")
			{
				priceLists.GET("", h.BillingOps.ListPriceLists)
				priceLists.GET("/:id", h.BillingOps.GetPriceList)
				priceLists.POST("", wafUserWriteRL, h.BillingOps.CreatePriceList)
				priceLists.PUT("/:id", wafUserWriteRL, h.BillingOps.UpdatePriceList)
				priceLists.DELETE("/:id", wafUserWriteRL, h.BillingOps.DeletePriceList)
			}

			partyGroups := protected.Group("/party-groups")
			{
				partyGroups.GET("", h.BillingOps.ListPartyGroups)
				partyGroups.GET("/:id", h.BillingOps.GetPartyGroup)
				partyGroups.GET("/:id/ledger", h.BillingOps.GetPartyGroupLedger)
				partyGroups.POST("", wafUserWriteRL, h.BillingOps.CreatePartyGroup)
				partyGroups.PUT("/:id", wafUserWriteRL, h.BillingOps.UpdatePartyGroup)
				partyGroups.DELETE("/:id", wafUserWriteRL, h.BillingOps.DeletePartyGroup)
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
				journals.GET("", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Journal.List)
				journals.GET("/:id", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Journal.Get)
				journals.POST("", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserWriteRL, h.Journal.Create)
				journals.PUT("/:id", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserWriteRL, h.Journal.Update)
				journals.DELETE("/:id", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserWriteRL, h.Journal.Delete)
				journals.POST("/:id/post", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserWriteRL, h.Journal.Post)
				journals.POST("/:id/reverse", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserWriteRL, h.Journal.Reverse)
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
				utils.POST("/gstin/:gstin/fetch", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Tax.FetchGSTIN)
			}

			tax := protected.Group("/tax")
			{
				tax.GET("/integrations", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionTaxIntegrationsManage), h.Tax.ListIntegrationAccounts)
				tax.POST("/integrations", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionTaxIntegrationsManage), h.Tax.UpsertIntegrationAccount)
				tax.PUT("/integrations/:id", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionTaxIntegrationsManage), h.Tax.UpsertIntegrationAccount)
				tax.POST("/integrations/:id/validate", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionTaxIntegrationsManage), h.Tax.ValidateIntegrationAccount)
				tax.POST("/gstr-2b/import", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), h.Tax.ImportGSTR2B)
				tax.GET("/reports/:type", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Tax.GetReport)
				tax.POST("/reports/:type/export", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsExport), h.Tax.ExportReport)
				tax.GET("/report-runs/:id", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Tax.GetReportRun)
			}

			pos := protected.Group("/pos")
			{
				pos.POST("/sessions", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPOSOperate), h.POS.CreateSession)
				pos.GET("/sessions", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPOSOperate), h.POS.ListSessions)
				pos.POST("/sessions/:id/close", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPOSOperate), h.POS.CloseSession)
				pos.GET("/catalog/search", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPOSOperate), h.POS.SearchCatalog)
				pos.POST("/carts/:id/items/scan", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPOSOperate), h.POS.ScanItem)
				pos.POST("/carts/:id/checkout", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPOSOperate), h.POS.Checkout)
				pos.GET("/receipts/:documentID", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPOSOperate), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.POS.GetReceipt)
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
				ledger.GET("", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Ledger.List)
				ledger.GET("/balance", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Ledger.Balance)
			}

			teams := protected.Group("/teams")
			{
				teams.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionTeamsView), h.Team.List)
				teams.GET("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionTeamsView), h.Team.Get)
				teams.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionTeamsManage), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionRolesManage), h.Team.Create)
				teams.PUT("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionTeamsManage), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionRolesManage), h.Team.Update)
				teams.DELETE("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionTeamsManage), h.Team.Delete)
			}

			webhooks := protected.Group("/webhooks")
			{
				webhooks.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.Webhook.List)
				webhooks.GET("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.Webhook.Get)
				webhooks.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.Webhook.Create)
				webhooks.PUT("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.Webhook.Update)
				webhooks.DELETE("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.Webhook.Delete)
			}

			subscriptions := protected.Group("/subscriptions")
			{
				subscriptions.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionSubscriptionsView), h.Subscription.Get)
				subscriptions.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionSubscriptionsManage), h.Subscription.Create)
				subscriptions.PUT("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionSubscriptionsManage), h.Subscription.Update)
				subscriptions.GET("/entitlements", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionSubscriptionsView), h.Commerce.ListEntitlements)
				subscriptions.POST("/entitlements/sync", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionSubscriptionsManage), h.Commerce.SyncEntitlements)
			}

			branches := protected.Group("/branches")
			{
				branches.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionBranchesView), h.Commerce.ListBranches)
				branches.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionBranchesManage), h.Commerce.CreateBranch)
				branches.PUT("/:branch_id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionBranchesManage), h.Commerce.UpdateBranch)
				branches.DELETE("/:branch_id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionBranchesManage), h.Commerce.DeleteBranch)
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
					config.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionAgentsManage), h.AgentConfig.CreateAgentConfig)
					config.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionAgentsView), h.AgentConfig.GetAllAgentConfigs)
					config.GET("/:agent_id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionAgentsView), h.AgentConfig.GetAgentConfig)
					config.PUT("/:agent_id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionAgentsManage), h.AgentConfig.UpdateAgentConfig)
					config.DELETE("/:agent_id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionAgentsManage), h.AgentConfig.DeleteAgentConfig)
					config.POST("/default", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionAgentsManage), h.AgentConfig.CreateDefaultConfig)

					mentee := config.Group("/mentee")
					{
						mentee.GET("/recommendation/:negotiation_id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionAgentsView), h.AgentConfig.GetMenteeRecommendation)
						mentee.GET("/learning/:agent_id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionAgentsView), h.AgentConfig.GetMenteeLearningData)
						mentee.DELETE("/learning/:agent_id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionAgentsManage), h.AgentConfig.ResetMenteeLearning)
						mentee.GET("/export", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionAgentsManage), h.AgentConfig.ExportMenteeData)
						mentee.POST("/import", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionAgentsManage), h.AgentConfig.ImportMenteeData)
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
			discovery.GET("/agents/find-sellers", h.AgentDiscovery.FindSellersByProduct)
			discovery.GET("/agents/find-sellers-by-category", h.AgentDiscovery.FindSellersByCategory)
			discovery.GET("/agents/by-product-categories", h.AgentDiscovery.DiscoverAgentsByProductCategories)
			discovery.GET("/agents/by-categories", h.AgentDiscovery.DiscoverAgentsByCategoriesAndBudget)
			discovery.GET("/agents/search", h.AgentDiscovery.SearchAgentsWithLLM)
			discovery.GET("/agents/:agentID", h.AgentDiscovery.GetAgentRegistry)

			// Register agent
			discovery.POST("/agents/register", h.AgentDiscovery.RegisterAgent)
			discovery.POST("/agents/register-from-agents", h.AgentDiscovery.RegisterAgentFromAgents)

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
			llm.GET("/chat/conversations", h.LLM.ListChatConversations)
			llm.GET("/chat/conversations/:id/messages", h.LLM.ListChatMessages)
			llm.POST("/chat", wafLLMRL, h.LLM.Chat)
			llm.POST("/agent-assist", wafLLMRL, h.LLM.AgentAssist)
		}

		// Voice endpoints
		voice := protected.Group("/voice")
		{
			voice.POST("/text-to-speech", wafLLMRL, h.SarvamTTS.Synthesize)
			voice.GET("/text-to-speech/languages", h.SarvamTTS.ListLanguages)
		}

		if svcs.VoiceSession != nil && h.VoiceSession != nil {
			voiceSessions := protected.Group("/voice/sessions")
			voiceSessions.Use(middleware.RequirePermission(svcs.BusinessAuth, services.PermissionVoiceUse))
			voiceSessions.Use(wafUserHeavyRL)
			voiceSessions.POST("", h.VoiceSession.Create)
			voiceSessions.GET("/:session_id", h.VoiceSession.Get)
			voiceSessions.POST("/:session_id/resume", h.VoiceSession.Resume)
			voiceSessions.DELETE("/:session_id", h.VoiceSession.Delete)
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
			workflows.POST("/:id/duplicate", h.Workflow.DuplicateWorkflow)
			workflows.GET("/:id/runs", h.Workflow.GetWorkflowRuns)
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
			ws.GET("/stats", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.WebSocket.GetStats)
			ws.GET("/status/:userID", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.WebSocket.GetConnectionStatus)
			ws.GET("/users", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.WebSocket.GetConnectedUsers)
			ws.GET("/health", h.WebSocket.HealthCheck)

			// Notification endpoints (for sending notifications via REST)
			ws.POST("/notify/:userID", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.WebSocket.SendNotification)
			ws.POST("/notify-all", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionNotificationsManage), h.WebSocket.SendNotificationToAll)
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
			a2aBargaining.GET("/negotiation/:negotiationId/progress", h.A2ABargaining.GetNegotiationProgress)
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

func registerSwaggerRoutes(router *gin.Engine, cfg *config.Config) {
	const swaggerDocPath = "/swagger.json"

	router.GET(swaggerDocPath, func(c *gin.Context) {
		doc, err := swag.ReadDoc("swagger")
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		var payload map[string]any
		if err := json.Unmarshal([]byte(doc), &payload); err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		schemes, host, basePath := resolveSwaggerEndpoint(cfg)
		if len(schemes) > 0 {
			payload["schemes"] = schemes
		}
		if host != "" {
			payload["host"] = host
		}
		if basePath != "" {
			payload["basePath"] = basePath
		}

		cognitoDomain := cfg.Cognito.ResolveHostedUIDomain()
		payload["securityDefinitions"] = map[string]any{
			"BearerAuth": map[string]any{
				"type":             "oauth2",
				"flow":             "accessCode",
				"authorizationUrl": cognitoDomain + "/oauth2/authorize",
				"tokenUrl":         cognitoDomain + "/oauth2/token",
				"scopes":           map[string]string{},
			},
		}

		c.JSON(http.StatusOK, payload)
	})

	router.GET(
		"/swagger/*any",
		ginSwagger.WrapHandler(
			swaggerFiles.Handler,
			ginSwagger.URL("../swagger.json"),
			ginSwagger.PersistAuthorization(true),
			ginSwagger.Oauth2DefaultClientID(cfg.Cognito.ClientID),
			ginSwagger.Oauth2UsePkce(cfg.Server.SwaggerUsePkce),
		),
	)
}

func resolveSwaggerEndpoint(cfg *config.Config) ([]string, string, string) {
	baseURL := cfg.Server.ResolveBaseURL()
	if baseURL == "" {
		return nil, "", docs.SwaggerInfo.BasePath
	}

	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, "", docs.SwaggerInfo.BasePath
	}

	basePath := strings.TrimRight(parsed.Path, "/") + docs.SwaggerInfo.BasePath
	if basePath == "" {
		basePath = docs.SwaggerInfo.BasePath
	}

	return []string{parsed.Scheme}, parsed.Host, basePath
}

func shouldExposeAdminLocalEmails(environment string) bool {
	return !logger.IsProductionEnvironment(environment)
}
