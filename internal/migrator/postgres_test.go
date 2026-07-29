package migrator

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"invoice-backend/internal/config"
	migrationbundle "invoice-backend/migrations"
)

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
