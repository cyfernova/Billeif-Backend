package services

import (
	"context"

	"invoice-backend/internal/models"

	"gorm.io/gorm"
)

func (s *InventoryService) CreateAssemblyRecipe(ctx context.Context, input CreateAssemblyRecipeInput) (*models.AssemblyRecipe, error) {
	var recipe *models.AssemblyRecipe
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		product, variant, err := s.resolveVariantTx(tx, input.BusinessID, input.ProductID, input.VariantID)
		if err != nil {
			return err
		}
		record := &models.AssemblyRecipe{
			BusinessID:     input.BusinessID,
			Name:           input.Name,
			ProductID:      product.ID,
			VariantID:      variant.ID,
			OutputQuantity: input.OutputQuantity,
			IsActive:       true,
			Metadata:       mustMarshalMap(input.Metadata),
		}
		if err := tx.Create(record).Error; err != nil {
			return err
		}
		for _, component := range input.Components {
			componentProduct, componentVariant, err := s.resolveVariantTx(tx, input.BusinessID, component.ProductID, component.VariantID)
			if err != nil {
				return err
			}
			row := &models.AssemblyRecipeComponent{
				RecipeID:           record.ID,
				BusinessID:         input.BusinessID,
				ComponentProductID: componentProduct.ID,
				ComponentVariantID: componentVariant.ID,
				Quantity:           component.Quantity,
			}
			if err := tx.Create(row).Error; err != nil {
				return err
			}
		}
		record.Components = []*models.AssemblyRecipeComponent{}
		recipe = record
		return nil
	})
	return recipe, err
}

func (s *InventoryService) ListAssemblyRecipes(ctx context.Context, businessID string) ([]*models.AssemblyRecipe, error) {
	var recipes []models.AssemblyRecipe
	if err := s.db.WithContext(ctx).
		Preload("Components").
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Order("created_at DESC").
		Find(&recipes).Error; err != nil {
		return nil, err
	}
	result := make([]*models.AssemblyRecipe, 0, len(recipes))
	for i := range recipes {
		result = append(result, &recipes[i])
	}
	return result, nil
}

func (s *InventoryService) BuildAssembly(ctx context.Context, businessID, recipeID string, input ExecuteAssemblyInput) error {
	return s.executeAssembly(ctx, businessID, recipeID, input, false)
}

func (s *InventoryService) DisassembleAssembly(ctx context.Context, businessID, recipeID string, input ExecuteAssemblyInput) error {
	return s.executeAssembly(ctx, businessID, recipeID, input, true)
}

func (s *InventoryService) executeAssembly(ctx context.Context, businessID, recipeID string, input ExecuteAssemblyInput, reverse bool) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var recipe models.AssemblyRecipe
		if err := tx.Preload("Components").Where("id = ? AND business_id = ? AND deleted_at IS NULL", recipeID, businessID).First(&recipe).Error; err != nil {
			return err
		}
		product, variant, err := s.resolveVariantTx(tx, businessID, recipe.ProductID, recipe.VariantID)
		if err != nil {
			return err
		}

		for _, component := range recipe.Components {
			componentProduct, componentVariant, err := s.resolveVariantTx(tx, businessID, component.ComponentProductID, component.ComponentVariantID)
			if err != nil {
				return err
			}
			componentMove := findAssemblyComponentMove(input.ComponentMoves, componentProduct.ID, componentVariant.ID, componentVariant.IsDefault)
			componentQty := component.Quantity * input.Quantity
			if reverse {
				componentQty = -componentQty
			}
			if _, err := s.applyInventoryMutationTx(tx, inventoryMutationInput{
				BusinessID:       businessID,
				Product:          componentProduct,
				Variant:          componentVariant,
				WarehouseID:      input.WarehouseID,
				Quantity:         -componentQty,
				Reason:           firstNonEmpty(input.Reason, "assembly component movement"),
				UnitCost:         componentVariant.CostPrice,
				BatchAllocations: componentMove.BatchAllocations,
				SerialIDs:        componentMove.SerialIDs,
				TransactionType:  map[bool]string{true: models.InventoryTransactionTypeDisassembly, false: models.InventoryTransactionTypeAssembly}[reverse],
			}); err != nil {
				return err
			}
		}

		outputQty := recipe.OutputQuantity * input.Quantity
		if reverse {
			outputQty = -outputQty
		}
		if _, err := s.applyInventoryMutationTx(tx, inventoryMutationInput{
			BusinessID:       businessID,
			Product:          product,
			Variant:          variant,
			WarehouseID:      input.WarehouseID,
			Quantity:         outputQty,
			Reason:           firstNonEmpty(input.Reason, "assembly output movement"),
			UnitCost:         variant.CostPrice,
			BatchAllocations: input.OutputBatches,
			SerialIDs:        input.OutputSerialIDs,
			TransactionType:  map[bool]string{true: models.InventoryTransactionTypeDisassembly, false: models.InventoryTransactionTypeAssembly}[reverse],
		}); err != nil {
			return err
		}
		return s.emitInventoryEventTx(tx, businessID, map[bool]string{true: "assembly_disassembled", false: "assembly_built"}[reverse], "assembly_recipe", recipeID, map[string]interface{}{
			"warehouse_id": input.WarehouseID,
			"quantity":     input.Quantity,
		})
	})
}

func findAssemblyComponentMove(moves []ExecuteAssemblyComponentMovementInput, productID, variantID string, isDefaultVariant bool) ExecuteAssemblyComponentMovementInput {
	for _, move := range moves {
		if move.ProductID != productID {
			continue
		}
		if move.VariantID == variantID {
			return move
		}
		if move.VariantID == "" && isDefaultVariant {
			return move
		}
	}
	return ExecuteAssemblyComponentMovementInput{}
}
