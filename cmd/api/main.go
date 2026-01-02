package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

// @host localhost:8080
// @BasePath /api/v1

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	log := logger.New()
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal("failed to load config", "error", err)
	}

	// Initialize Sentry early for production error tracking
	if err := initSentry(cfg, log); err != nil {
		log.Error("failed to initialize Sentry", "error", err)
		// Don't fail startup if Sentry fails, just log the error
	}
	defer pkgsentry.Flush(2 * time.Second)

	if cfg.Environment == "prod" {
		gin.SetMode(gin.ReleaseMode)
	}

	db, err := initDatabase(cfg)
	if err != nil {
		log.Fatal("failed to connect to database", "error", err)
	}

	awsClients, err := awsclients.New(ctx, cfg.AWS)
	if err != nil {
		log.Fatal("failed to initialize AWS clients", "error", err)
	}

	repos := initRepositories(db)
	svcs := initServices(cfg, repos, awsClients, log)
	h := handlers.New(svcs, log)

	router := setupRouter(cfg, h, log)

	worker := workers.New(cfg, svcs, awsClients, log)
	go worker.Start(ctx)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
	}

	go func() {
		log.Info("starting server", "port", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("failed to start server", "error", err)
		}
	}()

	metricsSrv := &http.Server{
		Addr:    ":9090",
		Handler: promhttp.Handler(),
	}
	go func() {
		log.Info("starting metrics server", "port", 9090)
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("metrics server error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	worker.Stop()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("server forced to shutdown", "error", err)
	}
	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("metrics server forced to shutdown", "error", err)
	}

	log.Info("server exited")
}

func initDatabase(cfg *config.Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.Name,
		cfg.Database.SSLMode,
	)
	return gorm.Open(postgres.Open(dsn), &gorm.Config{})
}

type Repositories struct {
	User         interfaces.UserRepository
	Business     interfaces.BusinessRepository
	Customer     interfaces.CustomerRepository
	Vendor       interfaces.VendorRepository
	Product      interfaces.ProductRepository
	Invoice      interfaces.InvoiceRepository
	Payment      interfaces.PaymentRepository
	Ledger       interfaces.LedgerRepository
	Team         interfaces.TeamMemberRepository
	Webhook      interfaces.WebhookRepository
	Subscription interfaces.SubscriptionRepository
}

func initRepositories(db *gorm.DB) *Repositories {
	return &Repositories{
		User:         postgresrepo.NewUserRepository(db),
		Business:     postgresrepo.NewBusinessRepository(db),
		Customer:     postgresrepo.NewCustomerRepository(db),
		Vendor:       postgresrepo.NewVendorRepository(db),
		Product:      postgresrepo.NewProductRepository(db),
		Invoice:      postgresrepo.NewInvoiceRepository(db),
		Payment:      postgresrepo.NewPaymentRepository(db),
		Ledger:       postgresrepo.NewLedgerRepository(db),
		Team:         postgresrepo.NewTeamMemberRepository(db),
		Webhook:      postgresrepo.NewWebhookRepository(db),
		Subscription: postgresrepo.NewSubscriptionRepository(db),
	}
}

func initServices(cfg *config.Config, repos *Repositories, aws *awsclients.Config, log *logger.Logger) *services.Container {
	return services.NewContainer(cfg, repos.User, repos.Business, repos.Customer, repos.Vendor,
		repos.Product, repos.Invoice, repos.Payment, repos.Ledger, repos.Team,
		repos.Webhook, repos.Subscription, aws, log)
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

func setupRouter(cfg *config.Config, h *handlers.Handler, log *logger.Logger) *gin.Engine {
	router := gin.New()

	// Add Sentry middleware first for request context
	if cfg.Sentry.DSN != "" {
		router.Use(middleware.SentryMiddleware())
	}

	router.Use(middleware.RequestID())
	router.Use(middleware.Logger(log))

	// Use Sentry-aware recovery if Sentry is enabled
	if cfg.Sentry.DSN != "" {
		router.Use(middleware.SentryRecovery(log))
	} else {
		router.Use(middleware.Recovery(log))
	}
	router.Use(middleware.CORS())

	router.GET("/health", h.Health.Check)
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	api := router.Group("/api/v1")
	{
		auth := api.Group("/auth")
		{
			auth.POST("/register", h.Auth.Register)
			auth.POST("/login", h.Auth.Login)
			auth.POST("/logout", h.Auth.Logout)
			auth.POST("/refresh", h.Auth.Refresh)
			auth.POST("/forgot-password", h.Auth.ForgotPassword)
			auth.POST("/reset-password", h.Auth.ResetPassword)
			auth.POST("/verify-email", h.Auth.VerifyEmail)
			auth.POST("/resend-verification", h.Auth.ResendVerification)
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
		{
			protected.GET("/auth/me", h.Auth.Me)
			protected.PUT("/auth/profile", h.Auth.UpdateProfile)
			protected.POST("/auth/change-password", h.Auth.ChangePassword)

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
		}

		admin := api.Group("/admin")
		admin.Use(middleware.Auth(cfg.Cognito, log))
		admin.Use(middleware.RequireRole("admin"))
		{
			admin.GET("/local-emails", h.Admin.ListEmails)
		}
	}

	return router
}
