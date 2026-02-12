package services

import (
	"context"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type LedgerService struct {
	repo interfaces.LedgerRepository
	log  *logger.Logger
}

func NewLedgerService(repo interfaces.LedgerRepository, log *logger.Logger) *LedgerService {
	return &LedgerService{repo: repo, log: log}
}

func (s *LedgerService) List(ctx context.Context, businessID string, page, limit int) ([]*models.LedgerEntry, int64, error) {
	log := logger.FromContext(ctx).With("service", "ledger", "operation", "list", "business_id", businessID, "page", page, "limit", limit)
	entries, total, err := s.repo.GetByBusinessID(ctx, businessID, page, limit)
	if err != nil {
		log.Error("failed to list ledger entries", "error", err)
		return nil, 0, err
	}
	log.Debug("listed ledger entries", "count", len(entries), "total", total)
	return entries, total, nil
}

func (s *LedgerService) GetBalance(ctx context.Context, businessID string) (float64, error) {
	log := logger.FromContext(ctx).With("service", "ledger", "operation", "get_balance", "business_id", businessID)
	balance, err := s.repo.GetBalance(ctx, businessID)
	if err != nil {
		log.Error("failed to get ledger balance", "error", err)
		return 0, err
	}
	log.Debug("retrieved ledger balance", "balance", balance)
	return balance, nil
}

func (s *LedgerService) CreateEntry(ctx context.Context, entry *models.LedgerEntry) error {
	log := logger.FromContext(ctx).With("service", "ledger", "operation", "create_entry", "business_id", entry.BusinessID)
	if err := s.repo.Create(ctx, entry); err != nil {
		log.Error("failed to create ledger entry", "error", err)
		return err
	}
	log.Info("ledger entry created", "entry_id", entry.ID)
	return nil
}
