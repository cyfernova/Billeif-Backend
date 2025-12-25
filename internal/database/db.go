package database

import (
	"context"
	"fmt"
	"time"

	"github.com/cyfernova/invoice-backend/internal/config"
	"github.com/cyfernova/invoice-backend/internal/models"
	applogger "github.com/cyfernova/invoice-backend/pkg/logger"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Database wraps the GORM DB connection
type Database struct {
	DB *gorm.DB
}

// GormLogger adapts our logger to GORM's logger interface
type GormLogger struct {
	Log *applogger.Logger
}

// NewGormLogger creates a new GORM logger
func NewGormLogger(log *applogger.Logger) *GormLogger {
	return &GormLogger{Log: log}
}

func (l *GormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	return l
}

func (l *GormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	l.Log.Info().Msgf(msg, data...)
}

func (l *GormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	l.Log.Warn().Msgf(msg, data...)
}

func (l *GormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	l.Log.Error().Msgf(msg, data...)
}

func (l *GormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	sql, _ := fc()

	if err != nil {
		l.Log.Error().
			Str("duration", elapsed.String()).
			Str("sql", sql).
			Err(err).
			Msg("Database query error")
	} else {
		l.Log.Debug().
			Str("duration", elapsed.String()).
			Str("sql", sql).
			Msg("Database query")
	}
}

// NewDatabase creates a new database connection
func NewDatabase(cfg config.DatabaseConfig, log *applogger.Logger) (*Database, error) {
	gormLogger := NewGormLogger(log)

	db, err := gorm.Open(postgres.Open(cfg.GetDSN()), &gorm.Config{
		Logger: gormLogger,
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database instance: %w", err)
	}

	// Connection pool settings
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	log.Info().Str("host", cfg.Host).Int("port", cfg.Port).Msg("Database connected successfully")

	return &Database{DB: db}, nil
}

// AutoMigrate runs auto migration for all models
func (d *Database) AutoMigrate() error {
	return d.DB.AutoMigrate(
		&models.User{},
		&models.BusinessProfile{},
		&models.Customer{},
		&models.Vendor{},
		&models.Invoice{},
		&models.InvoiceItem{},
	)
}

// HealthCheck checks if the database is accessible
func (d *Database) HealthCheck(ctx context.Context) error {
	sqlDB, err := d.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// Close closes the database connection
func (d *Database) Close() error {
	sqlDB, err := d.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// GetDB returns the underlying GORM DB instance
func (d *Database) GetDB() *gorm.DB {
	return d.DB
}
