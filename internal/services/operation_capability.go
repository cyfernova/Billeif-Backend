package services

import (
	"context"
	"strings"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/awsclients"
)

// ConfiguredOperationRecoveryCapabilityGuard evaluates the live in-process
// configuration and clients on every command. It never caches a previously
// available recovery path across a runtime configuration change.
type ConfiguredOperationRecoveryCapabilityGuard struct {
	cfg *config.Config
	aws *awsclients.Config
}

func NewConfiguredOperationRecoveryCapabilityGuard(
	cfg *config.Config,
	aws *awsclients.Config,
) *ConfiguredOperationRecoveryCapabilityGuard {
	return &ConfiguredOperationRecoveryCapabilityGuard{cfg: cfg, aws: aws}
}

func (g *ConfiguredOperationRecoveryCapabilityGuard) RequireOperationRecovery(
	ctx context.Context,
	request OperationRecoveryCapabilityRequest,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if g == nil || g.cfg == nil || g.aws == nil ||
		request.OperationType != OperationTypeInvoiceRender || request.Action != OperationActionRetry ||
		strings.TrimSpace(request.BusinessID) == "" || strings.TrimSpace(request.UserID) == "" ||
		strings.TrimSpace(g.cfg.SQS.InvoiceQueue) == "" || strings.TrimSpace(g.cfg.S3.BucketInvoices) == "" ||
		g.aws.SQS == nil || g.aws.S3 == nil {
		return ErrUnsupportedOperationRecovery
	}
	return nil
}
