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
	return s.repo.GetByBusinessID(ctx, businessID, page, limit)
}

func (s *LedgerService) GetBalance(ctx context.Context, businessID string) (float64, error) {
	return s.repo.GetBalance(ctx, businessID)
}

func (s *LedgerService) CreateEntry(ctx context.Context, entry *models.LedgerEntry) error {
	return s.repo.Create(ctx, entry)
}
