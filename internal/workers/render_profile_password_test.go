package workers

import (
	"context"
	"encoding/base64"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/postgres"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestInvoiceRendererLoadsResolvedRenderProfilePassword(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	if err := db.Exec(`CREATE TABLE render_profiles (
		id TEXT PRIMARY KEY,
		business_id TEXT NOT NULL,
		name TEXT NOT NULL,
		header_html TEXT,
		footer_html TEXT,
		watermark_text TEXT,
		banner_text TEXT,
		font_family TEXT,
		page_size TEXT,
		layout_config TEXT,
		password_protected BOOLEAN NOT NULL DEFAULT FALSE,
		password TEXT,
		password_ciphertext TEXT,
		copy_allowed BOOLEAN NOT NULL DEFAULT TRUE,
		print_allowed BOOLEAN NOT NULL DEFAULT TRUE,
		custom_labels TEXT,
		visibility_config TEXT,
		is_default BOOLEAN NOT NULL DEFAULT FALSE,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create render profiles: %v", err)
	}
	businessID := uuid.NewString()
	profileID := uuid.NewString()
	password := "synthetic-worker-password"
	if err := db.Create(&models.RenderProfile{
		ID:                profileID,
		BusinessID:        businessID,
		Name:              "Worker protected profile",
		PasswordProtected: true,
		LegacyPassword:    &password,
	}).Error; err != nil {
		t.Fatalf("seed render profile: %v", err)
	}
	cfg := &config.Config{}
	cfg.Credentials.EncryptionKey = base64.StdEncoding.EncodeToString(
		[]byte("0123456789abcdef0123456789abcdef"),
	)
	documentService := services.NewDocumentService(
		db,
		cfg,
		nil,
		postgres.NewDocumentRepository(db),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		&awsclients.Config{},
		nil,
		logger.NewWithEnv("test"),
	)
	operations := &servicePreviewRenderOperations{
		svc: &services.Container{Document: documentService},
	}

	profile, err := operations.loadProfile(context.Background(), businessID, profileID)
	if err != nil {
		t.Fatalf("load renderer profile: %v", err)
	}
	if profile.Password != password {
		t.Fatal("invoice renderer did not receive the configured password in memory")
	}
	if profile.LegacyPassword != nil || profile.PasswordCiphertext != nil {
		t.Fatal("invoice renderer received persistence-only password fields")
	}
}
