package postgres

import (
	"context"
	"fmt"
	"sort"

	"invoice-backend/internal/invoiceresolution"
	"invoice-backend/internal/models"
)

func (r *invoiceRepository) ResolveInvoiceLines(
	ctx context.Context,
	request invoiceresolution.Request,
) ([]invoiceresolution.LineSnapshot, error) {
	productIDs := uniqueSortedLineIDs(request.Lines, func(line invoiceresolution.LineReference) string {
		return line.ProductID
	})
	variantIDs := uniqueSortedLineIDs(request.Lines, func(line invoiceresolution.LineReference) string {
		return line.VariantID
	})
	warehouseIDs := uniqueSortedLineIDs(request.Lines, func(line invoiceresolution.LineReference) string {
		return line.WarehouseID
	})

	products := make(map[string]models.Product, len(productIDs))
	if len(productIDs) > 0 {
		var loaded []models.Product
		if err := r.db.WithContext(ctx).
			Where("business_id = ? AND id IN ?", request.BusinessID, productIDs).
			Order("id ASC").
			Find(&loaded).Error; err != nil {
			return nil, fmt.Errorf("resolve invoice products: %w", err)
		}
		for _, product := range loaded {
			products[product.ID] = product
		}
		if missing := firstMissingID(productIDs, products); missing != "" {
			return nil, &invoiceresolution.MissingReferenceError{
				Kind: invoiceresolution.ReferenceProduct,
				ID:   missing,
			}
		}
	}

	variants := make(map[string]models.ProductVariant, len(variantIDs))
	if len(variantIDs) > 0 {
		var loaded []models.ProductVariant
		if err := r.db.WithContext(ctx).
			Where("business_id = ? AND id IN ?", request.BusinessID, variantIDs).
			Order("id ASC").
			Find(&loaded).Error; err != nil {
			return nil, fmt.Errorf("resolve invoice variants: %w", err)
		}
		for _, variant := range loaded {
			variants[variant.ID] = variant
		}
		if missing := firstMissingID(variantIDs, variants); missing != "" {
			return nil, &invoiceresolution.MissingReferenceError{
				Kind: invoiceresolution.ReferenceVariant,
				ID:   missing,
			}
		}
	}

	warehouses := make(map[string]models.Warehouse, len(warehouseIDs))
	if len(warehouseIDs) > 0 {
		var loaded []models.Warehouse
		if err := r.db.WithContext(ctx).
			Where("business_id = ? AND id IN ?", request.BusinessID, warehouseIDs).
			Order("id ASC").
			Find(&loaded).Error; err != nil {
			return nil, fmt.Errorf("resolve invoice warehouses: %w", err)
		}
		for _, warehouse := range loaded {
			warehouses[warehouse.ID] = warehouse
		}
		if missing := firstMissingID(warehouseIDs, warehouses); missing != "" {
			return nil, &invoiceresolution.MissingReferenceError{
				Kind: invoiceresolution.ReferenceWarehouse,
				ID:   missing,
			}
		}
	}

	catalogues := make(map[string]models.ProductWarehouseCatalog)
	if len(productIDs) > 0 && len(warehouseIDs) > 0 {
		var loaded []models.ProductWarehouseCatalog
		if err := r.db.WithContext(ctx).
			Where(
				"business_id = ? AND product_id IN ? AND warehouse_id IN ? AND is_active = TRUE AND is_visible = TRUE",
				request.BusinessID,
				productIDs,
				warehouseIDs,
			).
			Order("id ASC").
			Find(&loaded).Error; err != nil {
			return nil, fmt.Errorf("resolve invoice catalogues: %w", err)
		}
		for _, catalogue := range loaded {
			catalogues[catalogueKey(catalogue.ProductID, catalogue.WarehouseID)] = catalogue
		}
	}

	priceListSet := make(map[string]struct{})
	if request.PriceListID != "" {
		priceListSet[request.PriceListID] = struct{}{}
	}
	for _, line := range request.Lines {
		if line.VariantID != "" {
			if line.ProductID == "" {
				return nil, &invoiceresolution.InvalidReferenceError{
					Kind: invoiceresolution.ReferenceVariant, ID: line.VariantID, Reason: "product is required",
				}
			}
			if variants[line.VariantID].ProductID != line.ProductID {
				return nil, &invoiceresolution.InvalidReferenceError{
					Kind: invoiceresolution.ReferenceVariant, ID: line.VariantID, Reason: "does not belong to product",
				}
			}
		}
		if line.ProductID != "" && line.WarehouseID != "" {
			key := catalogueKey(line.ProductID, line.WarehouseID)
			catalogue, ok := catalogues[key]
			if !ok {
				return nil, &invoiceresolution.MissingReferenceError{
					Kind: invoiceresolution.ReferenceCatalogue,
					ID:   key,
				}
			}
			if catalogue.PriceListID != nil && *catalogue.PriceListID != "" {
				priceListSet[*catalogue.PriceListID] = struct{}{}
			}
		}
	}

	priceListIDs := sortedSetKeys(priceListSet)
	priceLists := make(map[string]models.PriceList, len(priceListIDs))
	if len(priceListIDs) > 0 {
		var loaded []models.PriceList
		if err := r.db.WithContext(ctx).
			Where("business_id = ? AND id IN ? AND is_active = TRUE", request.BusinessID, priceListIDs).
			Order("id ASC").
			Find(&loaded).Error; err != nil {
			return nil, fmt.Errorf("resolve invoice price lists: %w", err)
		}
		for _, priceList := range loaded {
			priceLists[priceList.ID] = priceList
		}
		if missing := firstMissingID(priceListIDs, priceLists); missing != "" {
			return nil, &invoiceresolution.MissingReferenceError{
				Kind: invoiceresolution.ReferencePriceList,
				ID:   missing,
			}
		}
	}

	priceItems := make(map[string]models.PriceListItem)
	if len(priceListIDs) > 0 && (len(productIDs) > 0 || len(variantIDs) > 0) {
		var loaded []models.PriceListItem
		query := r.db.WithContext(ctx).Where("price_list_id IN ?", priceListIDs)
		switch {
		case len(productIDs) > 0 && len(variantIDs) > 0:
			query = query.Where("(product_id IN ? OR variant_id IN ?)", productIDs, variantIDs)
		case len(productIDs) > 0:
			query = query.Where("product_id IN ?", productIDs)
		default:
			query = query.Where("variant_id IN ?", variantIDs)
		}
		if err := query.Order("updated_at DESC, id ASC").Find(&loaded).Error; err != nil {
			return nil, fmt.Errorf("resolve invoice price-list items: %w", err)
		}
		for _, item := range loaded {
			productID := pointerValue(item.ProductID)
			variantID := pointerValue(item.VariantID)
			if variantID != "" {
				productID = ""
			}
			key := priceItemKey(item.PriceListID, productID, variantID)
			if _, exists := priceItems[key]; !exists {
				priceItems[key] = item
			}
		}
	}

	snapshots := make([]invoiceresolution.LineSnapshot, len(request.Lines))
	for index, line := range request.Lines {
		snapshot := invoiceresolution.LineSnapshot{
			ProductID: line.ProductID, VariantID: line.VariantID, WarehouseID: line.WarehouseID,
		}
		if product, ok := products[line.ProductID]; ok {
			snapshot.ProductName = product.Name
			snapshot.SKU = product.SKU
			snapshot.HSNSACCode = product.HSNSACCode
			snapshot.UQCCode = product.UQCCode
			snapshot.Unit = product.Unit
			snapshot.UnitPrice = product.Price
			snapshot.MRP = product.MRP
			snapshot.CessRate = product.DefaultCessRate
		}
		if variant, ok := variants[line.VariantID]; ok {
			if variant.SKU != "" {
				snapshot.SKU = variant.SKU
			}
			applyResolvedPrice(&snapshot, variant.Price, variant.MRP, variant.DefaultCessRate)
		}
		if catalogue, ok := catalogues[catalogueKey(line.ProductID, line.WarehouseID)]; ok {
			snapshot.CatalogueID = catalogue.ID
			if catalogue.PriceOverride != nil && *catalogue.PriceOverride > 0 {
				snapshot.UnitPrice = *catalogue.PriceOverride
			}
			if request.PriceListID == "" && catalogue.PriceListID != nil {
				snapshot.PriceListID = *catalogue.PriceListID
			}
		}
		if request.PriceListID != "" {
			snapshot.PriceListID = request.PriceListID
		}
		if snapshot.PriceListID != "" {
			var item models.PriceListItem
			var ok bool
			if line.VariantID != "" {
				item, ok = priceItems[priceItemKey(snapshot.PriceListID, "", line.VariantID)]
			}
			if !ok {
				item, ok = priceItems[priceItemKey(snapshot.PriceListID, line.ProductID, "")]
			}
			if ok {
				applyResolvedPrice(&snapshot, item.Price, item.MRP, item.CessRate)
			}
		}
		snapshots[index] = snapshot
	}
	return snapshots, nil
}

func uniqueSortedLineIDs(
	lines []invoiceresolution.LineReference,
	value func(invoiceresolution.LineReference) string,
) []string {
	set := make(map[string]struct{})
	for _, line := range lines {
		if id := value(line); id != "" {
			set[id] = struct{}{}
		}
	}
	return sortedSetKeys(set)
}

func sortedSetKeys(set map[string]struct{}) []string {
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func firstMissingID[T any](ids []string, values map[string]T) string {
	for _, id := range ids {
		if _, ok := values[id]; !ok {
			return id
		}
	}
	return ""
}

func catalogueKey(productID, warehouseID string) string {
	return fmt.Sprintf("%s/%s", productID, warehouseID)
}

func priceItemKey(priceListID, productID, variantID string) string {
	return fmt.Sprintf("%s/%s/%s", priceListID, productID, variantID)
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func applyResolvedPrice(snapshot *invoiceresolution.LineSnapshot, unitPrice, mrp, cessRate float64) {
	if unitPrice > 0 {
		snapshot.UnitPrice = unitPrice
	}
	if mrp > 0 {
		snapshot.MRP = mrp
	}
	if cessRate > 0 {
		snapshot.CessRate = cessRate
	}
}
