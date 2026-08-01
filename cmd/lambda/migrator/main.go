package main

import (
	"context"
	"log"
	"strings"

	"invoice-backend/internal/config"
	"invoice-backend/internal/migrator"
	migrationbundle "invoice-backend/migrations"

	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

func newMigrationHandler(ctx context.Context) (*migrator.Handler, error) {
	cfg, err := config.LoadForProfile(config.ProfileMigration)
	if err != nil {
		return nil, err
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.AWS.Region))
	if err != nil {
		return nil, &config.ResolutionError{Resource: "AWS SDK configuration"}
	}

	parameterNames := make([]string, 0, 1)
	if strings.TrimSpace(cfg.SSM.DatabaseHostParam) != "" {
		parameterNames = append(parameterNames, cfg.SSM.DatabaseHostParam)
	}
	resolver, err := config.NewRuntimeResolver(config.RuntimeResolverOptions{
		Clients: config.RuntimeResolvers{
			Secrets: secretsmanager.NewFromConfig(awsCfg),
			SSM:     ssm.NewFromConfig(awsCfg),
		},
		SecretIdentifiers: []string{cfg.Secrets.Database},
		ParameterNames:    parameterNames,
	})
	if err != nil {
		return nil, err
	}
	return migrator.New(migrator.Options{
		Bundle: migrationbundle.Embedded, BundleRoot: ".", Config: cfg,
		Resolver: resolver, Open: migrator.OpenPostgres,
	})
}

func main() {
	handler, err := newMigrationHandler(context.Background())
	if err != nil {
		log.Fatal("migration runtime initialization failed")
	}
	lambda.Start(handler.Handle)
}
