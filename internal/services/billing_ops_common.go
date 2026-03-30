package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"gorm.io/gorm"
)

type actorContextKey struct{}

type ActorContext struct {
	UserID    string
	Role      string
	RequestID string
	IPAddress string
}

type resolvedLinePricing struct {
	PriceListID *string
	UnitPrice   float64
	MRP         float64
	CessRate    float64
}

func ContextWithActor(ctx context.Context, actor ActorContext) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actor)
}

func actorFromContext(ctx context.Context) ActorContext {
	actor, _ := ctx.Value(actorContextKey{}).(ActorContext)
	return actor
}

func mustMarshalAny(value interface{}, fallback string) string {
	if value == nil {
		return fallback
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fallback
	}
	return string(data)
}

func recordActivityLog(
	ctx context.Context,
	db *gorm.DB,
	businessID, entityType, entityID, action, reason string,
	snapshot interface{},
	diff map[string]interface{},
	metadata map[string]interface{},
) error {
	if db == nil || businessID == "" || entityType == "" || entityID == "" || action == "" {
		return nil
	}
	actor := actorFromContext(ctx)
	if actor.UserID == "" {
		return nil
	}
	entry := &models.ActivityLog{
		BusinessID: businessID,
		ActorID:    actor.UserID,
		ActorRole:  actor.Role,
		RequestID:  actor.RequestID,
		IPAddress:  actor.IPAddress,
		EntityType: entityType,
		EntityID:   entityID,
		Action:     action,
		Reason:     reason,
		Snapshot:   mustMarshalAny(snapshot, "{}"),
		Diff:       mustMarshalMap(diff),
		Metadata:   mustMarshalMap(metadata),
	}
	return db.WithContext(ctx).Create(entry).Error
}

func resolvePriceListID(
	ctx context.Context,
	db *gorm.DB,
	customerRepo interfaces.CustomerRepository,
	vendorRepo interfaces.VendorRepository,
	businessID, partyType, partyID string,
	explicitPriceListID *string,
	warehouseID *string,
) (*string, error) {
	if explicitPriceListID != nil && *explicitPriceListID != "" {
		return explicitPriceListID, nil
	}
	if db == nil {
		return nil, nil
	}

	findAssignment := func(scopeType, scopeID string) (*string, error) {
		if scopeType == "" || scopeID == "" {
			return nil, nil
		}
		var assignment models.PriceListAssignment
		err := db.WithContext(ctx).
			Where("business_id = ? AND scope_type = ? AND scope_id = ? AND deleted_at IS NULL", businessID, scopeType, scopeID).
			Order("priority DESC, created_at DESC").
			First(&assignment).Error
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, nil
			}
			return nil, err
		}
		return &assignment.PriceListID, nil
	}

	if partyID != "" {
		scopeType := ""
		switch partyType {
		case models.DocumentPartyTypeCustomer:
			scopeType = models.PriceListScopeCustomer
		case models.DocumentPartyTypeVendor:
			scopeType = models.PriceListScopeVendor
		}
		if assignmentID, err := findAssignment(scopeType, partyID); err != nil {
			return nil, err
		} else if assignmentID != nil {
			return assignmentID, nil
		}

		switch partyType {
		case models.DocumentPartyTypeCustomer:
			if customerRepo != nil {
				customer, err := customerRepo.GetByID(ctx, partyID, businessID)
				if err == nil && customer != nil && customer.DefaultPriceListID != nil {
					return customer.DefaultPriceListID, nil
				}
			}
		case models.DocumentPartyTypeVendor:
			if vendorRepo != nil {
				vendor, err := vendorRepo.GetByID(ctx, partyID, businessID)
				if err == nil && vendor != nil && vendor.DefaultPriceListID != nil {
					return vendor.DefaultPriceListID, nil
				}
			}
		}

		var groupIDs []string
		if err := db.WithContext(ctx).
			Model(&models.PartyGroupMember{}).
			Where("business_id = ? AND party_type = ? AND party_id = ? AND deleted_at IS NULL", businessID, partyType, partyID).
			Pluck("party_group_id", &groupIDs).Error; err != nil {
			return nil, err
		}
		if len(groupIDs) > 0 {
			var assignment models.PriceListAssignment
			err := db.WithContext(ctx).
				Where("business_id = ? AND scope_type = ? AND scope_id IN ? AND deleted_at IS NULL", businessID, models.PriceListScopeParty, groupIDs).
				Order("priority DESC, created_at DESC").
				First(&assignment).Error
			if err != nil && err != gorm.ErrRecordNotFound {
				return nil, err
			}
			if err == nil {
				return &assignment.PriceListID, nil
			}
		}
	}

	if warehouseID != nil && *warehouseID != "" {
		if assignmentID, err := findAssignment(models.PriceListScopeWarehouse, *warehouseID); err != nil {
			return nil, err
		} else if assignmentID != nil {
			return assignmentID, nil
		}
	}

	if assignmentID, err := findAssignment(models.PriceListScopeBusiness, businessID); err != nil {
		return nil, err
	} else if assignmentID != nil {
		return assignmentID, nil
	}

	var priceList models.PriceList
	err := db.WithContext(ctx).
		Where("business_id = ? AND is_default = TRUE AND is_active = TRUE AND deleted_at IS NULL", businessID).
		Order("updated_at DESC").
		First(&priceList).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &priceList.ID, nil
}

func resolveLinePricing(
	ctx context.Context,
	db *gorm.DB,
	productRepo interfaces.ProductRepository,
	businessID string,
	priceListID *string,
	productID, variantID string,
	warehouseID *string,
) (resolvedLinePricing, error) {
	resolved := resolvedLinePricing{PriceListID: priceListID}
	if businessID == "" {
		return resolved, nil
	}
	if db != nil && resolved.PriceListID == nil && warehouseID != nil && *warehouseID != "" && productID != "" {
		var catalog models.ProductWarehouseCatalog
		err := db.WithContext(ctx).
			Where("business_id = ? AND product_id = ? AND warehouse_id = ? AND deleted_at IS NULL", businessID, productID, *warehouseID).
			First(&catalog).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return resolved, err
		}
		if err == nil && catalog.PriceListID != nil && *catalog.PriceListID != "" {
			resolved.PriceListID = catalog.PriceListID
		}
		if err == nil && catalog.PriceOverride != nil && *catalog.PriceOverride > 0 {
			resolved.UnitPrice = *catalog.PriceOverride
		}
	}

	var product *models.Product
	if productID != "" && productRepo != nil {
		loaded, err := productRepo.GetByID(ctx, productID, businessID)
		if err != nil {
			return resolved, err
		}
		product = loaded
		resolved.UnitPrice = product.Price
		resolved.MRP = product.MRP
		resolved.CessRate = product.DefaultCessRate
	}

	if variantID != "" && db != nil {
		var variant models.ProductVariant
		err := db.WithContext(ctx).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", variantID, businessID).
			First(&variant).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return resolved, err
		}
		if err == nil {
			if variant.Price > 0 {
				resolved.UnitPrice = variant.Price
			}
			if variant.MRP > 0 {
				resolved.MRP = variant.MRP
			}
			if variant.DefaultCessRate > 0 {
				resolved.CessRate = variant.DefaultCessRate
			}
		}
	}

	if resolved.PriceListID == nil || *resolved.PriceListID == "" || db == nil {
		return resolved, nil
	}

	var item models.PriceListItem
	query := db.WithContext(ctx).Where("price_list_id = ? AND deleted_at IS NULL", *resolved.PriceListID)
	switch {
	case variantID != "":
		query = query.Where("variant_id = ?", variantID)
	case productID != "":
		query = query.Where("product_id = ? AND variant_id IS NULL", productID)
	default:
		return resolved, nil
	}
	err := query.Order("updated_at DESC").First(&item).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return resolved, nil
		}
		return resolved, err
	}
	if item.Price > 0 {
		resolved.UnitPrice = item.Price
	}
	if item.MRP > 0 {
		resolved.MRP = item.MRP
	}
	if item.CessRate > 0 {
		resolved.CessRate = item.CessRate
	}
	return resolved, nil
}

func cadenceNextRun(base time.Time, cadence, timezoneName string) (time.Time, error) {
	location := time.UTC
	if timezoneName != "" {
		if loaded, err := time.LoadLocation(timezoneName); err == nil {
			location = loaded
		}
	}
	current := base.In(location)
	switch cadence {
	case "daily":
		return current.AddDate(0, 0, 1), nil
	case "weekly":
		return current.AddDate(0, 0, 7), nil
	case "monthly":
		return current.AddDate(0, 1, 0), nil
	case "quarterly":
		return current.AddDate(0, 3, 0), nil
	case "yearly":
		return current.AddDate(1, 0, 0), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported cadence: %s", cadence)
	}
}
