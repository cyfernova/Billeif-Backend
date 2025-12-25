package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/swaggo/gin-swagger" // swagger import
	_ "github.com/swaggo/files"

	"github.com/cyfernova/invoice-backend/internal/config"
	"github.com/cyfernova/invoice-backend/internal/database"
	"github.com/cyfernova/invoice-backend/internal/handlers"
	"github.com/cyfernova/invoice-backend/internal/middleware"
	"github.com/cyfernova/invoice-backend/internal/models"
	"github.com/cyfernova/invoice-backend/internal/repositories"
	"github.com/cyfernova/invoice-backend/internal/services"
	"github.com/cyfernova/invoice-backend/pkg/awsclients"
	"github.com/cyfernova/invoice-backend/pkg/logger"
)

// @title           Invoice Backend API
// @version         1.0
// @description     Production-grade invoice management system with AWS Cognito authentication
// @termsOfService  http://swagger.io/terms/

// @contact.name   API Support
// @contact.url    http://www.example.com/support
// @contact.email  support@example.com

// @license.name  Apache 2.0
// @license.url   http://www.apache.org/licenses/LICENSE-2.0.html

// @host      localhost:8080
// @BasePath  /api/v1

// @securityDefinitions.apikey Bearer
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

func main() {
	// Load environment variables
	if err := loadEnv(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	// Initialize logger
	log := logger.New(
		getEnv("APP_LOG_LEVEL", "info"),
		getEnv("APP_ENVIRONMENT", "development"),
	)

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// Initialize AWS clients
	ctx := context.Background()

	cognitoClient, err := awsclients.NewCognitoClient(
		ctx,
		cfg.AWS.Cognito.Region,
		cfg.AWS.AccessKeyID,
		cfg.AWS.SecretAccessKey,
		cfg.AWS.LocalStackEndpoint,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize Cognito client")
	}

	dynamoDBClient, err := awsclients.NewDynamoDBClient(
		ctx,
		cfg.AWS.Region,
		cfg.AWS.AccessKeyID,
		cfg.AWS.SecretAccessKey,
		cfg.AWS.LocalStackEndpoint,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize DynamoDB client")
	}

	s3Client, err := awsclients.NewS3Client(
		ctx,
		cfg.AWS.S3.Region,
		cfg.AWS.AccessKeyID,
		cfg.AWS.SecretAccessKey,
		cfg.AWS.LocalStackEndpoint,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize S3 client")
	}

	log.Info().Msg("AWS clients initialized successfully")

	// Initialize database
	db, err := database.NewDatabase(cfg.Database, log)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer db.Close()

	log.Info().Msg("Database connected successfully")

	// Run auto-migration if enabled
	if getEnv("AUTO_MIGRATE", "false") == "true" {
		if err := db.AutoMigrate(); err != nil {
			log.Warn().Err(err).Msg("Auto-migration failed")
		}
	}

	// Initialize repositories
	repos := repositories.NewRegistry(db, dynamoDBClient, cfg.AWS.DynamoDB.SessionsTable)

	// Initialize services
	cognitoService := services.NewCognitoService(cognitoClient, cfg.AWS.Cognito)
	authService := services.NewAuthService(cognitoService, repos.User, repos.Session, cfg.JWT, log)
	userService := services.NewUserService(repos.User, cognitoService)

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(authService)
	userHandler := handlers.NewUserHandler(userService)
	healthHandler := handlers.NewHealthHandler(db, cognitoClient, dynamoDBClient, s3Client)

	// Set Gin mode
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create router
	router := gin.New()

	// Global middleware
	router.Use(middleware.Recovery(log))
	router.Use(middleware.RequestID())
	router.Use(middleware.Logging(log))
	router.Use(middleware.CORS(cfg.Server.AllowedOrigins))

	// Rate limiting (if enabled)
	if cfg.App.RateLimit.Enabled {
		router.Use(middleware.RateLimit(
			float64(cfg.App.RateLimit.Requests)/float64(cfg.App.RateLimit.Window),
			cfg.App.RateLimit.Requests,
		))
	}

	// Health check endpoints (no auth required)
	router.GET("/health", healthHandler.Liveness)
	router.GET("/readiness", healthHandler.Readiness)

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		// Public auth routes
		auth := v1.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
			auth.POST("/verify-email", authHandler.VerifyEmail)
			auth.POST("/resend-verification", authHandler.ResendVerification)
			auth.POST("/forgot-password", authHandler.ForgotPassword)
			auth.POST("/reset-password", authHandler.ResetPassword)
			auth.POST("/refresh", authHandler.RefreshToken)
		}

		// Protected auth routes
		authProtected := v1.Group("/auth")
		authMiddleware := middleware.NewAuthMiddleware(cfg.JWT)
		authProtected.Use(authMiddleware.RequireAuth())
		{
			authProtected.POST("/logout", authHandler.Logout)
			authProtected.POST("/change-password", authHandler.ChangePassword)
			authProtected.GET("/me", authHandler.GetMe)
		}

		// Protected user routes
		users := v1.Group("/users")
		users.Use(authMiddleware.RequireAuth())
		{
			users.GET("/profile", userHandler.GetMyProfile)
			users.PUT("/profile", userHandler.UpdateProfile)
			users.GET(":id", middleware.RequireRole(models.RoleAdmin, models.RoleAccountant), userHandler.GetProfile)
			users.GET("", middleware.RequireAdmin(), userHandler.ListUsers)
		}
	}

	// Start server
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
	}

	// Graceful shutdown
	go func() {
		log.Info().Str("addr", addr).Msg("Starting HTTP server")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Failed to start server")
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Server.ShutdownTimeout)*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Server forced to shutdown")
	}

	log.Info().Msg("Server exited")
}

func loadEnv() error {
	// Try to load .env file if it exists
	if _, err := os.Stat(".env"); err == nil {
		return nil // .env exists, will be loaded by config package
	}
	return nil // .env is optional
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
