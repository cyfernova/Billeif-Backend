package migrator

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/config"
	migrationbundle "invoice-backend/migrations"

	"github.com/DATA-DOG/go-sqlmock"
)

type recordingMigrationPool struct {
	maxOpen     int
	maxIdle     int
	maxIdleTime time.Duration
	maxLifetime time.Duration
}

func (p *recordingMigrationPool) SetMaxOpenConns(value int)              { p.maxOpen = value }
func (p *recordingMigrationPool) SetMaxIdleConns(value int)              { p.maxIdle = value }
func (p *recordingMigrationPool) SetConnMaxIdleTime(value time.Duration) { p.maxIdleTime = value }
func (p *recordingMigrationPool) SetConnMaxLifetime(value time.Duration) { p.maxLifetime = value }

func TestConfigureMigrationDatabasePoolIsSerialAndBounded(t *testing.T) {
	pool := &recordingMigrationPool{}
	configureMigrationDatabasePool(pool)

	if pool.maxOpen != 1 || pool.maxIdle != 0 {
		t.Fatalf("migration pool open/idle = %d/%d, want 1/0", pool.maxOpen, pool.maxIdle)
	}
	if pool.maxIdleTime != 2*time.Minute || pool.maxLifetime != 10*time.Minute {
		t.Fatalf("migration pool idle/lifetime = %v/%v, want 2m/10m", pool.maxIdleTime, pool.maxLifetime)
	}
}

func TestOpenGORMPostgresUsesTheExistingConnection(t *testing.T) {
	sqlDatabase, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	mock.ExpectPing()

	gormDatabase, err := openGORMPostgres(sqlDatabase)
	if err != nil {
		t.Fatalf("openGORMPostgres() error = %v", err)
	}
	underlying, err := gormDatabase.DB()
	if err != nil {
		t.Fatalf("gorm DB() error = %v", err)
	}
	if underlying != sqlDatabase {
		t.Fatal("GORM migration connection did not preserve the existing bounded SQL pool")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("GORM connection expectations: %v", err)
	}
}

func TestPostgresURLPreservesCredentialAndIdentifierBoundaries(t *testing.T) {
	rawURL, err := postgresURL(config.DatabaseCredentials{
		Host: "database.internal", Port: 5432, User: "migration@example.com",
		Password: "p@ss/word ?", Name: "invoice_db", SSLMode: "require",
	})
	if err != nil {
		t.Fatalf("postgresURL() error = %v", err)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse postgres URL: %v", err)
	}
	if parsed.Scheme != "postgres" || parsed.Hostname() != "database.internal" || parsed.Port() != "5432" {
		t.Fatalf("postgres URL endpoint = %s://%s, want postgres://database.internal:5432", parsed.Scheme, parsed.Host)
	}
	if parsed.User.Username() != "migration@example.com" {
		t.Errorf("username = %q, want exact credential username", parsed.User.Username())
	}
	password, ok := parsed.User.Password()
	if !ok || password != "p@ss/word ?" {
		t.Errorf("password = %q (present %t), want exact credential password", password, ok)
	}
	if parsed.EscapedPath() != "/invoice_db" {
		t.Errorf("database path = %q, want /invoice_db", parsed.EscapedPath())
	}
	if parsed.Query().Get("sslmode") != "require" || parsed.Query().Get("connect_timeout") != "10" {
		t.Errorf("postgres query = %v, want sslmode=require and connect_timeout=10", parsed.Query())
	}
}

func TestOpenPostgresRejectsInvalidConfigurationWithoutLeakingCredentials(t *testing.T) {
	_, err := OpenPostgres(migrationbundle.Embedded, ".", config.DatabaseCredentials{
		Port: 5432, User: "migration-user", Password: sentinelCredential,
		Name: "invoice_db", SSLMode: "require",
	})
	if !errors.Is(err, ErrInvalidDatabaseConfig) {
		t.Fatalf("OpenPostgres() error = %v, want invalid database config", err)
	}
	if strings.Contains(err.Error(), sentinelCredential) || strings.Contains(err.Error(), "migration-user") {
		t.Fatalf("OpenPostgres() leaked credential material: %v", err)
	}
}
