package app

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type rotatingSecretsClient struct {
	values []string
}

func (f *rotatingSecretsClient) GetSecretValue(_ context.Context, _ *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	value := f.values[0]
	if len(f.values) > 1 {
		f.values = f.values[1:]
	}
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(value)}, nil
}

type noopConnector struct{}

func (noopConnector) Connect(context.Context) (driver.Conn, error) { return noopConn{}, nil }
func (noopConnector) Driver() driver.Driver                        { return noopDriver{} }

type noopDriver struct{}

func (noopDriver) Open(string) (driver.Conn, error) { return noopConn{}, nil }

type noopConn struct{}

func (noopConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (noopConn) Close() error                        { return nil }
func (noopConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }

func TestDatabaseConnectorResolvesRotatedCredentialsAtEachPhysicalConnect(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	resolver, err := config.NewRuntimeResolver(config.RuntimeResolverOptions{
		Clients: config.RuntimeResolvers{Secrets: &rotatingSecretsClient{values: []string{
			`{"username":"invoice_v1","password":"password_v1"}`,
			`{"username":"invoice_v2","password":"password_v2"}`,
		}}},
		SecretIdentifiers: []string{"rds-secret"},
		TTL:               time.Minute,
		Now:               func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new runtime resolver: %v", err)
	}
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Host: "db.internal", Port: 5432, Name: "invoice", SSLMode: "require",
		},
		Secrets: config.SecretIdentifiers{Database: "rds-secret"},
	}

	var dataSources []string
	connector := newRefreshingPostgresConnector(resolver, cfg, func(dataSourceName string) (driver.Connector, error) {
		dataSources = append(dataSources, dataSourceName)
		return noopConnector{}, nil
	})
	if _, err := connector.Connect(context.Background()); err != nil {
		t.Fatalf("first physical connect: %v", err)
	}
	now = now.Add(61 * time.Second)
	if _, err := connector.Connect(context.Background()); err != nil {
		t.Fatalf("physical reconnect after TTL: %v", err)
	}

	if len(dataSources) != 2 {
		t.Fatalf("expected two physical connection configurations, got %d", len(dataSources))
	}
	if !strings.Contains(dataSources[0], "user='invoice_v1'") || !strings.Contains(dataSources[0], "password='password_v1'") {
		t.Fatalf("initial DSN did not use initial credentials")
	}
	if !strings.Contains(dataSources[1], "user='invoice_v2'") || !strings.Contains(dataSources[1], "password='password_v2'") {
		t.Fatalf("reconnect DSN did not use rotated credentials")
	}
}
