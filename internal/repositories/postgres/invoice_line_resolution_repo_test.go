package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"invoice-backend/internal/invoiceresolution"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	resolveProductsSQL   = `SELECT .* FROM "products".*business_id = \$1.*id IN \([^)]*\).*ORDER BY id ASC`
	resolveVariantsSQL   = `SELECT .* FROM "product_variants".*business_id = \$1.*id IN \([^)]*\).*ORDER BY id ASC`
	resolveWarehousesSQL = `SELECT .* FROM "warehouses".*business_id = \$1.*id IN \([^)]*\).*ORDER BY id ASC`
	resolveCatalogsSQL   = `SELECT .* FROM "product_warehouse_catalogs".*business_id = \$1.*product_id IN \([^)]*\).*warehouse_id IN \([^)]*\).*is_active = TRUE.*is_visible = TRUE.*ORDER BY id ASC`
	resolvePriceListsSQL = `SELECT .* FROM "price_lists".*business_id = \$1.*id IN \([^)]*\).*is_active = TRUE.*ORDER BY id ASC`
	resolvePriceItemsSQL = `SELECT .* FROM "price_list_items".*price_list_id IN \([^)]*\).*\(product_id IN \([^)]*\) OR variant_id IN \([^)]*\)\).*ORDER BY updated_at DESC, id ASC`
)

func TestInvoiceRepositoryResolveInvoiceLinesUsesSameBoundedQueriesForOneAndLargeSets(t *testing.T) {
	for _, lineCount := range []int{1, 80} {
		t.Run(fmt.Sprintf("%d lines", lineCount), func(t *testing.T) {
			repository, mock, closeDatabase := newStrictInvoiceResolverRepository(t)
			defer closeDatabase()
			businessID := uuid.NewString()
			productID := uuid.NewString()
			variantID := uuid.NewString()
			warehouseID := uuid.NewString()
			catalogID := uuid.NewString()
			priceListID := uuid.NewString()
			price := 91.25

			expectCompleteResolutionQueries(
				mock,
				businessID,
				productID,
				variantID,
				warehouseID,
				catalogID,
				priceListID,
				&price,
			)
			lines := make([]invoiceresolution.LineReference, lineCount)
			for index := range lines {
				lines[index] = invoiceresolution.LineReference{
					ProductID: productID, VariantID: variantID, WarehouseID: warehouseID,
				}
			}

			snapshots, err := repository.ResolveInvoiceLines(context.Background(), invoiceresolution.Request{
				BusinessID: businessID,
				Lines:      lines,
			})

			if err != nil {
				t.Fatalf("resolve %d lines: %v", lineCount, err)
			}
			if len(snapshots) != lineCount {
				t.Fatalf("snapshot count = %d, want %d", len(snapshots), lineCount)
			}
			for index, snapshot := range snapshots {
				if snapshot.ProductID != productID || snapshot.VariantID != variantID ||
					snapshot.WarehouseID != warehouseID || snapshot.CatalogueID != catalogID ||
					snapshot.PriceListID != priceListID || snapshot.UnitPrice != 125 {
					t.Fatalf("snapshot %d = %#v", index, snapshot)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("bounded/deduplicated SQL expectations: %v", err)
			}
		})
	}
}

func TestInvoiceRepositoryResolveInvoiceLinesPreservesMixedInputOrderAndPricingPrecedence(t *testing.T) {
	repository, mock, closeDatabase := newStrictInvoiceResolverRepository(t)
	defer closeDatabase()
	businessID := uuid.NewString()
	productOne := "00000000-0000-0000-0000-000000000001"
	productTwo := "00000000-0000-0000-0000-000000000002"
	variantOne := "00000000-0000-0000-0000-000000000011"
	warehouseOne := "00000000-0000-0000-0000-000000000021"
	warehouseTwo := "00000000-0000-0000-0000-000000000022"
	catalogOne := "00000000-0000-0000-0000-000000000031"
	catalogTwo := "00000000-0000-0000-0000-000000000032"
	priceListOne := "00000000-0000-0000-0000-000000000041"
	priceListTwo := "00000000-0000-0000-0000-000000000042"

	mock.ExpectQuery(resolveProductsSQL).
		WithArgs(businessID, productOne, productTwo).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "name", "sku", "price", "mrp", "hsn_sac_code", "uqc_code", "unit", "default_cess_rate"}).
			AddRow(productOne, businessID, "One", "P-1", 10.0, 12.0, "1001", "PCS", "PCS", 1.0).
			AddRow(productTwo, businessID, "Two", "P-2", 30.0, 32.0, "1002", "KGS", "KGS", 2.0))
	mock.ExpectQuery(resolveVariantsSQL).
		WithArgs(businessID, variantOne).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "product_id", "name", "sku", "price", "mrp", "default_cess_rate"}).
			AddRow(variantOne, businessID, productOne, "One large", "V-1", 15.0, 17.0, 1.5))
	mock.ExpectQuery(resolveWarehousesSQL).
		WithArgs(businessID, warehouseOne, warehouseTwo).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "name"}).
			AddRow(warehouseOne, businessID, "W1").
			AddRow(warehouseTwo, businessID, "W2"))
	mock.ExpectQuery(resolveCatalogsSQL).
		WithArgs(businessID, productOne, productTwo, warehouseOne, warehouseTwo).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "product_id", "warehouse_id", "is_visible", "is_active", "price_override", "price_list_id"}).
			AddRow(catalogOne, businessID, productOne, warehouseOne, true, true, 20.0, priceListOne).
			AddRow(catalogTwo, businessID, productTwo, warehouseTwo, true, true, 35.0, priceListTwo))
	mock.ExpectQuery(resolvePriceListsSQL).
		WithArgs(businessID, priceListOne, priceListTwo).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "is_active"}).
			AddRow(priceListOne, businessID, true).
			AddRow(priceListTwo, businessID, true))
	mock.ExpectQuery(resolvePriceItemsSQL).
		WithArgs(priceListOne, priceListTwo, productOne, productTwo, variantOne).
		WillReturnRows(sqlmock.NewRows([]string{"id", "price_list_id", "product_id", "variant_id", "price", "mrp", "cess_rate", "updated_at"}).
			AddRow(uuid.NewString(), priceListOne, nil, variantOne, 25.0, 27.0, 2.5, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)).
			AddRow(uuid.NewString(), priceListTwo, productTwo, nil, 40.0, 42.0, 3.5, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))

	snapshots, err := repository.ResolveInvoiceLines(context.Background(), invoiceresolution.Request{
		BusinessID: businessID,
		Lines: []invoiceresolution.LineReference{
			{ProductID: productTwo, WarehouseID: warehouseTwo},
			{ProductID: productOne, VariantID: variantOne, WarehouseID: warehouseOne},
			{ProductID: productTwo, WarehouseID: warehouseTwo},
		},
	})

	if err != nil {
		t.Fatalf("resolve mixed lines: %v", err)
	}
	if len(snapshots) != 3 ||
		snapshots[0].ProductID != productTwo || snapshots[0].UnitPrice != 40 ||
		snapshots[1].ProductID != productOne || snapshots[1].VariantID != variantOne || snapshots[1].UnitPrice != 25 ||
		snapshots[2].ProductID != productTwo || snapshots[2].UnitPrice != 40 {
		t.Fatalf("ordered precedence snapshots = %#v", snapshots)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mixed resolution SQL expectations: %v", err)
	}
}

func TestInvoiceRepositoryResolveInvoiceLinesReturnsTypedMissingTenantReference(t *testing.T) {
	repository, mock, closeDatabase := newStrictInvoiceResolverRepository(t)
	defer closeDatabase()
	businessID := uuid.NewString()
	foreignProductID := uuid.NewString()
	mock.ExpectQuery(resolveProductsSQL).
		WithArgs(businessID, foreignProductID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id"}))

	snapshots, err := repository.ResolveInvoiceLines(context.Background(), invoiceresolution.Request{
		BusinessID: businessID,
		Lines:      []invoiceresolution.LineReference{{ProductID: foreignProductID}},
	})

	if snapshots != nil {
		t.Fatalf("snapshots = %#v, want nil", snapshots)
	}
	var missing *invoiceresolution.MissingReferenceError
	if !errors.As(err, &missing) || missing.Kind != invoiceresolution.ReferenceProduct || missing.ID != foreignProductID {
		t.Fatalf("error = %T %v, want typed missing product %s", err, err, foreignProductID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("tenant query expectations: %v", err)
	}
}

func TestInvoiceRepositoryResolveInvoiceLinesReturnsTypedMissingCatalogue(t *testing.T) {
	repository, mock, closeDatabase := newStrictInvoiceResolverRepository(t)
	defer closeDatabase()
	businessID := uuid.NewString()
	productID := uuid.NewString()
	warehouseID := uuid.NewString()
	mock.ExpectQuery(resolveProductsSQL).
		WithArgs(businessID, productID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id"}).AddRow(productID, businessID))
	mock.ExpectQuery(resolveWarehousesSQL).
		WithArgs(businessID, warehouseID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id"}).AddRow(warehouseID, businessID))
	mock.ExpectQuery(resolveCatalogsSQL).
		WithArgs(businessID, productID, warehouseID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "product_id", "warehouse_id"}))

	snapshots, err := repository.ResolveInvoiceLines(context.Background(), invoiceresolution.Request{
		BusinessID: businessID,
		Lines:      []invoiceresolution.LineReference{{ProductID: productID, WarehouseID: warehouseID}},
	})

	if snapshots != nil {
		t.Fatalf("snapshots = %#v, want nil", snapshots)
	}
	var missing *invoiceresolution.MissingReferenceError
	if !errors.As(err, &missing) || missing.Kind != invoiceresolution.ReferenceCatalogue {
		t.Fatalf("error = %T %v, want typed missing catalogue", err, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("catalogue query expectations: %v", err)
	}
}

func TestInvoiceRepositoryResolveInvoiceLinesValidatesExplicitPriceListTenant(t *testing.T) {
	repository, mock, closeDatabase := newStrictInvoiceResolverRepository(t)
	defer closeDatabase()
	businessID := uuid.NewString()
	foreignPriceListID := uuid.NewString()
	mock.ExpectQuery(resolvePriceListsSQL).
		WithArgs(businessID, foreignPriceListID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id"}))

	snapshots, err := repository.ResolveInvoiceLines(context.Background(), invoiceresolution.Request{
		BusinessID:  businessID,
		PriceListID: foreignPriceListID,
		Lines:       []invoiceresolution.LineReference{{}},
	})

	if snapshots != nil {
		t.Fatalf("snapshots = %#v, want nil", snapshots)
	}
	var missing *invoiceresolution.MissingReferenceError
	if !errors.As(err, &missing) ||
		missing.Kind != invoiceresolution.ReferencePriceList ||
		missing.ID != foreignPriceListID {
		t.Fatalf("error = %T %v, want typed tenant-hidden price list", err, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("price-list tenant query expectations: %v", err)
	}
}

func TestInvoiceRepositoryResolveInvoiceLinesFreeTextUsesNoQueries(t *testing.T) {
	repository, mock, closeDatabase := newStrictInvoiceResolverRepository(t)
	defer closeDatabase()

	snapshots, err := repository.ResolveInvoiceLines(context.Background(), invoiceresolution.Request{
		BusinessID: uuid.NewString(),
		Lines:      []invoiceresolution.LineReference{{}},
	})

	if err != nil || len(snapshots) != 1 {
		t.Fatalf("free-text snapshots/error = %#v/%v", snapshots, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("free-text unexpectedly queried catalogue: %v", err)
	}
}

func newStrictInvoiceResolverRepository(t *testing.T) (*invoiceRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDatabase, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	gormDatabase, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDatabase}), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		_ = sqlDatabase.Close()
		t.Fatalf("open gorm postgres adapter: %v", err)
	}
	return &invoiceRepository{db: gormDatabase}, mock, func() {
		mock.ExpectClose()
		if err := sqlDatabase.Close(); err != nil {
			t.Errorf("close sqlmock: %v", err)
		}
	}
}

func expectCompleteResolutionQueries(
	mock sqlmock.Sqlmock,
	businessID, productID, variantID, warehouseID, catalogID, priceListID string,
	priceOverride *float64,
) {
	mock.ExpectQuery(resolveProductsSQL).
		WithArgs(businessID, productID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "name", "sku", "price", "mrp", "hsn_sac_code", "uqc_code", "unit", "default_cess_rate"}).
			AddRow(productID, businessID, "Product", "P-1", 100.0, 110.0, "1001", "PCS", "PCS", 1.0))
	mock.ExpectQuery(resolveVariantsSQL).
		WithArgs(businessID, variantID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "product_id", "name", "sku", "price", "mrp", "default_cess_rate"}).
			AddRow(variantID, businessID, productID, "Variant", "V-1", 105.0, 115.0, 1.5))
	mock.ExpectQuery(resolveWarehousesSQL).
		WithArgs(businessID, warehouseID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "name"}).
			AddRow(warehouseID, businessID, "Warehouse"))
	mock.ExpectQuery(resolveCatalogsSQL).
		WithArgs(businessID, productID, warehouseID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "product_id", "warehouse_id", "is_visible", "is_active", "price_override", "price_list_id"}).
			AddRow(catalogID, businessID, productID, warehouseID, true, true, priceOverride, priceListID))
	mock.ExpectQuery(resolvePriceListsSQL).
		WithArgs(businessID, priceListID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "is_active"}).
			AddRow(priceListID, businessID, true))
	mock.ExpectQuery(resolvePriceItemsSQL).
		WithArgs(priceListID, productID, variantID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "price_list_id", "product_id", "variant_id", "price", "mrp", "cess_rate", "updated_at"}).
			AddRow(uuid.NewString(), priceListID, productID, variantID, 125.0, 130.0, 2.0, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
}
