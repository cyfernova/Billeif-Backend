package migrator

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"invoice-backend/internal/config"
	migrationbundle "invoice-backend/migrations"
)

func TestEmbeddedBundleAgainstEmptyPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not configured; skipping empty PostgreSQL migration integration")
	}
	credentials := credentialsFromTestDSN(t, dsn)
	runner, err := OpenPostgres(migrationbundle.Embedded, ".", credentials)
	if err != nil {
		t.Fatalf("open explicit PostgreSQL migration test database: %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close PostgreSQL migration test database: %v", err)
		}
	})

	if version, dirty, err := runner.Version(); !errors.Is(err, ErrNoVersion) {
		t.Fatalf("MIGRATION_TEST_DATABASE_URL must point to an empty migration database; version=%d dirty=%t error=%v", version, dirty, err)
	}
	if err := runner.Up(); err != nil {
		t.Fatalf("apply embedded migrations to empty PostgreSQL database: %v", err)
	}
	version, dirty, err := runner.Version()
	if err != nil {
		t.Fatalf("read final PostgreSQL migration version: %v", err)
	}
	if version != 43 || dirty {
		t.Fatalf("final PostgreSQL migration state = version %d dirty %t, want version 43 clean", version, dirty)
	}
}

func credentialsFromTestDSN(t *testing.T, dsn string) config.DatabaseCredentials {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.User == nil {
		t.Fatal("MIGRATION_TEST_DATABASE_URL must be a PostgreSQL URL with explicit credentials")
	}
	password, ok := parsed.User.Password()
	if !ok || password == "" {
		t.Fatal("MIGRATION_TEST_DATABASE_URL must include a password")
	}
	port := 5432
	if parsed.Port() != "" {
		port, err = strconv.Atoi(parsed.Port())
		if err != nil {
			t.Fatal("MIGRATION_TEST_DATABASE_URL has an invalid port")
		}
	}
	name := strings.TrimPrefix(parsed.EscapedPath(), "/")
	if name == "" || strings.Contains(name, "/") {
		t.Fatal("MIGRATION_TEST_DATABASE_URL must include one database name")
	}
	sslMode := parsed.Query().Get("sslmode")
	if sslMode == "" {
		sslMode = "require"
	}
	return config.DatabaseCredentials{
		Host: parsed.Hostname(), Port: port, User: parsed.User.Username(),
		Password: password, Name: name, SSLMode: sslMode,
	}
}
