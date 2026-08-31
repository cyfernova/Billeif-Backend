package services

import (
	"context"
	"fmt"
	"strings"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"gorm.io/gorm"
)

type InventoryService struct {
	db           *gorm.DB
	repo         interfaces.InventoryRepository
	productRepo  interfaces.ProductRepository
	businessRepo interfaces.BusinessRepository
	teamRepo     interfaces.TeamMemberRepository
	log          *logger.Logger
}

func NewInventoryService(db *gorm.DB, repo interfaces.InventoryRepository, productRepo interfaces.ProductRepository, businessRepo interfaces.BusinessRepository, teamRepo interfaces.TeamMemberRepository, log *logger.Logger) *InventoryService {
	return &InventoryService{
		db:           db,
		repo:         repo,
		productRepo:  productRepo,
		businessRepo: businessRepo,
		teamRepo:     teamRepo,
		log:          log,
	}
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
	if s.db == nil {
		return nil
	}
	_, shouldPost, shouldReserve := stockBehaviorForDocument(document)
	if !shouldPost && !shouldReserve {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.ApplyDocumentTx(ctx, tx, document)
	})
}

func (s *InventoryService) ApplyDocumentTx(ctx context.Context, tx *gorm.DB, document *models.Document) error {
	if document == nil {
		return fmt.Errorf("document is required")
	}
	if tx == nil {
		return fmt.Errorf("inventory transaction is required")
	}
	tx = tx.WithContext(ctx)
	direction, shouldPost, shouldReserve := stockBehaviorForDocument(document)
	if !shouldPost && !shouldReserve {
		return nil
	}
	for _, line := range document.Lines {
		if line.ProductID == nil || line.Quantity <= 0 || strings.EqualFold(line.StockEffect, "none") {
			continue
		}
		qty := line.Quantity + line.FreeQuantity
		if qty <= 0 {
			continue
		}

		warehouseID, err := s.resolveWarehouseIDTx(tx, document.BusinessID, derefString(line.WarehouseID))
		if err != nil {
			return err
		}
		product, variant, err := s.resolveVariantTx(tx, document.BusinessID, *line.ProductID, derefString(line.VariantID))
		if err != nil {
			return err
		}
		reason := fmt.Sprintf("%s:%s", document.DocumentType, document.SerialNumber)
		if shouldReserve {
			if err := tx.Create(&models.InventoryReservation{
				BusinessID:     document.BusinessID,
				ProductID:      *line.ProductID,
				WarehouseID:    &warehouseID,
				DocumentID:     document.ID,
				DocumentLineID: &line.ID,
				Quantity:       qty,
				Status:         "active",
			}).Error; err != nil {
				return err
			}
			if err := s.reserveInventoryTx(tx, document.BusinessID, product.ID, variant.ID, warehouseID, qty, document.ID, &line.ID, reason); err != nil {
				return err
			}
			continue
		}

		signedQty := qty
		if direction == models.StockMoveDirectionOut {
			signedQty = -qty
		}
		if _, err := s.applyInventoryMutationTx(tx, inventoryMutationInput{
			BusinessID:       document.BusinessID,
			Product:          product,
			Variant:          variant,
			WarehouseID:      warehouseID,
			ProjectID:        derefString(document.ProjectID),
			Quantity:         signedQty,
			Reason:           reason,
			UnitCost:         line.CostSnapshot,
			BatchAllocations: unmarshalBatchAllocations(line.BatchAllocations),
			SerialIDs:        unmarshalStringSlice(line.SerialIDs),
			TransactionType:  documentInventoryTransactionType(document.DocumentType, direction),
			DocumentID:       &document.ID,
			DocumentLineID:   &line.ID,
		}); err != nil {
			return err
		}
		if direction == models.StockMoveDirectionOut {
			if err := s.releaseInventoryTx(tx, document.BusinessID, product.ID, variant.ID, warehouseID, qty, document.ID, &line.ID, reason); err != nil && !strings.Contains(strings.ToLower(err.Error()), "record not found") {
				return err
			}
		}
	}
	return nil
}

func (s *InventoryService) ReleaseReservations(ctx context.Context, documentID string) error {
	if s.db == nil {
		return s.repo.ReleaseReservationsByDocument(ctx, documentID)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var reservations []models.InventoryReservation
		if err := tx.Where("document_id = ? AND status = 'active' AND deleted_at IS NULL", documentID).Find(&reservations).Error; err != nil {
			return err
		}
		for _, reservation := range reservations {
			if reservation.WarehouseID != nil {
				product, variant, err := s.resolveVariantTx(tx, reservation.BusinessID, reservation.ProductID, "")
				if err != nil {
					s.log.Warn("failed to resolve variant for reservation release", "reservation_id", reservation.ID, "error", err)
					continue
				}
				if err := s.releaseInventoryTx(tx, reservation.BusinessID, product.ID, variant.ID, *reservation.WarehouseID, reservation.Quantity, reservation.DocumentID, reservation.DocumentLineID, "reservation release"); err != nil {
					s.log.Warn("failed to release reservation balance", "reservation_id", reservation.ID, "error", err)
				}
			}
		}
		return s.repo.ReleaseReservationsByDocument(ctx, documentID)
	})
}

func documentInventoryTransactionType(documentType, direction string) string {
	switch direction {
	case models.StockMoveDirectionIn:
		return models.InventoryTransactionTypeStockIn
	case models.StockMoveDirectionOut:
		return models.InventoryTransactionTypeStockOut
	default:
		switch documentType {
		case models.DocumentTypeSalesOrder:
			return models.InventoryTransactionTypeReservation
		default:
			return models.InventoryTransactionTypeAdjustment
		}
	}
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
