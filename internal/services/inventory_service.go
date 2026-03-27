package services

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type InventoryService struct {
	repo        interfaces.InventoryRepository
	productRepo interfaces.ProductRepository
	log         *logger.Logger
}

func NewInventoryService(repo interfaces.InventoryRepository, productRepo interfaces.ProductRepository, log *logger.Logger) *InventoryService {
	return &InventoryService{repo: repo, productRepo: productRepo, log: log}
}

func (s *InventoryService) EnsureDefaultWarehouse(ctx context.Context, businessID string) (*models.Warehouse, error) {
	warehouse, err := s.repo.GetDefaultWarehouse(ctx, businessID)
	if err == nil {
		return warehouse, nil
	}

	warehouse = &models.Warehouse{
		BusinessID: businessID,
		Name:       "Main Warehouse",
		Code:       "MAIN",
		IsDefault:  true,
	}
	if createErr := s.repo.CreateWarehouse(ctx, warehouse); createErr != nil {
		// Handle a concurrent create by retrying the lookup.
		warehouse, err = s.repo.GetDefaultWarehouse(ctx, businessID)
		if err == nil {
			return warehouse, nil
		}
		return nil, createErr
	}
	return warehouse, nil
}

func (s *InventoryService) ApplyDocument(ctx context.Context, document *models.Document) error {
	if document == nil {
		return fmt.Errorf("document is required")
	}
	direction, shouldPost, shouldReserve := stockBehaviorForDocument(document)
	if !shouldPost && !shouldReserve {
		return nil
	}

	defaultWarehouse, err := s.EnsureDefaultWarehouse(ctx, document.BusinessID)
	if err != nil {
		return err
	}

	for _, line := range document.Lines {
		if line.ProductID == nil || line.Quantity <= 0 || strings.EqualFold(line.StockEffect, "none") {
			continue
		}
		qty := line.Quantity + line.FreeQuantity
		if qty <= 0 {
			continue
		}

		warehouseID := line.WarehouseID
		if warehouseID == nil {
			warehouseID = &defaultWarehouse.ID
		}

		if shouldReserve {
			if err := s.repo.CreateReservation(ctx, &models.InventoryReservation{
				BusinessID:     document.BusinessID,
				ProductID:      *line.ProductID,
				WarehouseID:    warehouseID,
				DocumentID:     document.ID,
				DocumentLineID: &line.ID,
				Quantity:       qty,
				Status:         "active",
			}); err != nil {
				return err
			}
			continue
		}

		delta := int64(math.Round(qty))
		if direction == models.StockMoveDirectionOut {
			delta = -delta
		}
		if delta != 0 {
			if err := s.productRepo.AdjustStock(ctx, *line.ProductID, delta); err != nil {
				return err
			}
		}
		if err := s.repo.CreateStockMove(ctx, &models.StockMove{
			BusinessID:     document.BusinessID,
			ProductID:      *line.ProductID,
			WarehouseID:    warehouseID,
			DocumentID:     &document.ID,
			DocumentLineID: &line.ID,
			Direction:      direction,
			Quantity:       qty,
			Reason:         fmt.Sprintf("%s:%s", document.DocumentType, document.SerialNumber),
			RecordedAt:     time.Now(),
		}); err != nil {
			return err
		}
	}

	return nil
}

func (s *InventoryService) ReleaseReservations(ctx context.Context, documentID string) error {
	return s.repo.ReleaseReservationsByDocument(ctx, documentID)
}

func stockBehaviorForDocument(document *models.Document) (direction string, shouldPost bool, shouldReserve bool) {
	switch document.DocumentType {
	case models.DocumentTypePurchaseInvoice:
		return models.StockMoveDirectionIn, true, false
	case models.DocumentTypeDebitNote:
		return models.StockMoveDirectionOut, true, false
	case models.DocumentTypeSalesInvoice, models.DocumentTypeBillOfSupply:
		return models.StockMoveDirectionOut, true, false
	case models.DocumentTypeCreditNote:
		return models.StockMoveDirectionIn, true, false
	case models.DocumentTypeDeliveryChallan:
		if document.Direction == models.DocumentDirectionInward {
			return models.StockMoveDirectionIn, true, false
		}
		return models.StockMoveDirectionOut, true, false
	case models.DocumentTypeSalesOrder:
		return models.StockMoveDirectionReserve, false, true
	default:
		return "", false, false
	}
}
