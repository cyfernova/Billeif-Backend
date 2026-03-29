package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/reporting"
	interfaces "invoice-backend/internal/repositories/interfaces"

	"gorm.io/gorm"
)

type reportingRepository struct {
	db *gorm.DB
}

type reportBundle struct {
	rowSQL    string
	rowArgs   []interface{}
	totalSQL  string
	totalArgs []interface{}
	columns   []reporting.Column
	orderBy   string
}

func NewReportingRepository(db *gorm.DB) interfaces.ReportingRepository {
	return &reportingRepository{db: db}
}

func (r *reportingRepository) QueryReport(ctx context.Context, def reporting.Definition, query reporting.Query) (*reporting.Result, error) {
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.Limit <= 0 {
		query.Limit = 50
	}
	if query.Limit > 500 {
		query.Limit = 500
	}

	var (
		bundle reportBundle
		err    error
	)

	switch def.Family {
	case "document_register":
		bundle, err = r.buildDocumentRegisterBundle(def, query)
	case "daily_documents":
		bundle, err = r.buildDailyDocumentBundle(def, query)
	case "line_summary":
		bundle, err = r.buildLineSummaryBundle(def, query)
	case "line_profit":
		bundle, err = r.buildLineProfitBundle(def, query)
	case "profit_and_loss":
		bundle, err = r.buildProfitAndLossBundle(query)
	case "customer_ledger":
		bundle, err = r.buildCustomerLedgerBundle(query)
	case "vendor_ledger":
		bundle, err = r.buildVendorLedgerBundle(query)
	case "project_ledger":
		bundle, err = r.buildProjectLedgerBundle(query)
	case "general_ledger":
		bundle, err = r.buildGeneralLedgerBundle(query)
	case "journal_register":
		bundle, err = r.buildJournalRegisterBundle(query)
	case "payment_register":
		bundle, err = r.buildPaymentRegisterBundle(query)
	case "receivables":
		bundle, err = r.buildOutstandingBalanceBundle(query, "receivable")
	case "payables":
		bundle, err = r.buildOutstandingBalanceBundle(query, "payable")
	case "aging_receivables":
		bundle, err = r.buildAgingBundle(query, "receivable")
	case "aging_payables":
		bundle, err = r.buildAgingBundle(query, "payable")
	case "inventory_balances":
		bundle, err = r.buildInventoryBalanceBundle(def, query)
	case "low_stock":
		bundle, err = r.buildLowStockBundle(query)
	case "stock_movement":
		bundle, err = r.buildStockMovementBundle(query)
	case "batch_expiry":
		bundle, err = r.buildBatchExpiryBundle(query)
	case "serial_tracking":
		bundle, err = r.buildSerialTrackingBundle(query)
	case "warehouse_transfers":
		bundle, err = r.buildWarehouseTransferBundle(query)
	default:
		return nil, fmt.Errorf("unsupported report family: %s", def.Family)
	}
	if err != nil {
		return nil, err
	}

	return r.executeBundle(ctx, bundle, query.Page, query.Limit)
}

func (r *reportingRepository) GetDashboard(ctx context.Context, query reporting.Query) (map[string]interface{}, error) {
	baseArgs := []interface{}{query.BusinessID}
	docWhere := []string{"d.business_id = ?", "d.deleted_at IS NULL"}
	paymentWhere := []string{"p.business_id = ?", "p.deleted_at IS NULL"}
	stockWhere := []string{"ib.business_id = ?", "ib.deleted_at IS NULL"}
	stockArgs := []interface{}{query.BusinessID}

	if query.Filters.DateFrom != nil {
		docWhere = append(docWhere, "d.issue_date >= ?")
		paymentWhere = append(paymentWhere, "p.payment_date >= ?")
		baseArgs = append(baseArgs, *query.Filters.DateFrom)
	}
	if query.Filters.DateTo != nil {
		docWhere = append(docWhere, "d.issue_date <= ?")
		paymentWhere = append(paymentWhere, "p.payment_date <= ?")
		baseArgs = append(baseArgs, *query.Filters.DateTo)
	}
	if query.Filters.ProjectID != "" {
		docWhere = append(docWhere, "d.project_id = ?")
		paymentWhere = append(paymentWhere, "p.project_id = ?")
		stockWhere = append(stockWhere, "sm.project_id = ?")
		baseArgs = append(baseArgs, query.Filters.ProjectID)
		stockArgs = append(stockArgs, query.Filters.ProjectID)
	}
	if query.Filters.WarehouseID != "" {
		stockWhere = append(stockWhere, "ib.warehouse_id = ?")
		stockArgs = append(stockArgs, query.Filters.WarehouseID)
	}

	docArgs := make([]interface{}, len(baseArgs))
	copy(docArgs, baseArgs)
	paymentArgs := make([]interface{}, len(baseArgs))
	copy(paymentArgs, baseArgs)

	cardsSQL := fmt.Sprintf(`
		WITH sales AS (
			SELECT COALESCE(SUM(d.total), 0) AS total_sales
			FROM documents d
			WHERE %s
			  AND d.document_type IN ('sales_invoice', 'bill_of_supply')
		),
		purchases AS (
			SELECT COALESCE(SUM(d.total), 0) AS total_purchases
			FROM documents d
			WHERE %s
			  AND d.document_type = 'purchase_invoice'
		),
		expenses AS (
			SELECT COALESCE(SUM(d.total), 0) AS total_expenses
			FROM documents d
			WHERE %s
			  AND d.document_type = 'expense'
		),
		receivables AS (
			SELECT COALESCE(SUM(d.balance_due), 0) AS pending_receivables
			FROM documents d
			WHERE %s
			  AND d.document_type IN ('sales_invoice', 'bill_of_supply')
		),
		payables AS (
			SELECT COALESCE(SUM(d.balance_due), 0) AS pending_payables
			FROM documents d
			WHERE %s
			  AND d.document_type IN ('purchase_invoice', 'expense')
		),
		receipts AS (
			SELECT COALESCE(SUM(p.amount), 0) AS payments_in
			FROM payments p
			WHERE %s
		)
		SELECT s.total_sales, pu.total_purchases, e.total_expenses,
		       r.pending_receivables, pa.pending_payables, rc.payments_in
		FROM sales s
		CROSS JOIN purchases pu
		CROSS JOIN expenses e
		CROSS JOIN receivables r
		CROSS JOIN payables pa
		CROSS JOIN receipts rc
	`, strings.Join(docWhere, " AND "), strings.Join(docWhere, " AND "), strings.Join(docWhere, " AND "),
		strings.Join(docWhere, " AND "), strings.Join(docWhere, " AND "), strings.Join(paymentWhere, " AND "))

	cardsArgs := append([]interface{}{}, docArgs...)
	cardsArgs = append(cardsArgs, docArgs...)
	cardsArgs = append(cardsArgs, docArgs...)
	cardsArgs = append(cardsArgs, docArgs...)
	cardsArgs = append(cardsArgs, docArgs...)
	cardsArgs = append(cardsArgs, paymentArgs...)
	cards, err := r.scanSingleMap(ctx, cardsSQL, cardsArgs...)
	if err != nil {
		return nil, err
	}

	topProductsSQL := fmt.Sprintf(`
		SELECT p.name AS product_name,
		       COALESCE(SUM(dl.quantity), 0) AS quantity,
		       COALESCE(SUM(dl.line_total), 0) AS total
		FROM documents d
		JOIN document_lines dl ON dl.document_id = d.id
		LEFT JOIN products p ON p.id = dl.product_id
		WHERE %s
		  AND d.document_type IN ('sales_invoice', 'bill_of_supply')
		GROUP BY p.name
		ORDER BY total DESC
		LIMIT 5
	`, strings.Join(docWhere, " AND "))
	topProducts, err := r.scanRows(ctx, topProductsSQL, docArgs...)
	if err != nil {
		return nil, err
	}

	topCustomersSQL := fmt.Sprintf(`
		SELECT COALESCE(c.name, 'Manual') AS customer_name,
		       COALESCE(SUM(d.total), 0) AS total
		FROM documents d
		LEFT JOIN customers c ON c.id = d.party_id AND d.party_type = 'customer' AND c.deleted_at IS NULL
		WHERE %s
		  AND d.document_type IN ('sales_invoice', 'bill_of_supply')
		GROUP BY COALESCE(c.name, 'Manual')
		ORDER BY total DESC
		LIMIT 5
	`, strings.Join(docWhere, " AND "))
	topCustomers, err := r.scanRows(ctx, topCustomersSQL, docArgs...)
	if err != nil {
		return nil, err
	}

	lowStockSQL := `
		SELECT p.name AS product_name,
		       COALESCE(pv.name, 'Default') AS variant_name,
		       COALESCE(SUM(ib.on_hand), 0) AS on_hand,
		       COALESCE(NULLIF(pv.low_stock_threshold, 0), NULLIF(p.low_stock_threshold, 0), p.min_stock, 0) AS low_stock_threshold
		FROM inventory_balances ib
		JOIN products p ON p.id = ib.product_id AND p.deleted_at IS NULL
		LEFT JOIN product_variants pv ON pv.id = ib.variant_id AND pv.deleted_at IS NULL
		WHERE ib.business_id = ?
		  AND ib.deleted_at IS NULL
		GROUP BY p.name, pv.name, pv.low_stock_threshold, p.low_stock_threshold, p.min_stock
		HAVING COALESCE(SUM(ib.on_hand), 0) <= COALESCE(NULLIF(pv.low_stock_threshold, 0), NULLIF(p.low_stock_threshold, 0), p.min_stock, 0)
		ORDER BY on_hand ASC
		LIMIT 5
	`
	lowStock, err := r.scanRows(ctx, lowStockSQL, query.BusinessID)
	if err != nil {
		return nil, err
	}

	expiringBatchesSQL := `
		SELECT pb.batch_number,
		       p.name AS product_name,
		       COALESCE(pv.name, 'Default') AS variant_name,
		       pb.expires_at,
		       COALESCE(SUM(ib.on_hand), 0) AS on_hand
		FROM product_batches pb
		JOIN products p ON p.id = pb.product_id AND p.deleted_at IS NULL
		LEFT JOIN product_variants pv ON pv.id = pb.variant_id AND pv.deleted_at IS NULL
		LEFT JOIN inventory_balances ib ON ib.batch_id = pb.id AND ib.deleted_at IS NULL
		WHERE pb.business_id = ?
		  AND pb.deleted_at IS NULL
		  AND pb.expires_at IS NOT NULL
		  AND pb.expires_at <= NOW() + INTERVAL '30 days'
		GROUP BY pb.batch_number, p.name, pv.name, pb.expires_at
		ORDER BY pb.expires_at ASC
		LIMIT 5
	`
	expiringBatches, err := r.scanRows(ctx, expiringBatchesSQL, query.BusinessID)
	if err != nil {
		return nil, err
	}

	warehouseActivitySQL := fmt.Sprintf(`
		SELECT COALESCE(w.name, 'Unassigned') AS warehouse_name,
		       COUNT(*) AS movement_count,
		       COALESCE(SUM(sm.quantity), 0) AS moved_quantity
		FROM stock_moves sm
		LEFT JOIN warehouses w ON w.id = sm.warehouse_id AND w.deleted_at IS NULL
		WHERE %s
		  AND sm.deleted_at IS NULL
		GROUP BY COALESCE(w.name, 'Unassigned')
		ORDER BY movement_count DESC
		LIMIT 5
	`, strings.Join(stockWhere, " AND "))
	warehouseActivity, err := r.scanRows(ctx, warehouseActivitySQL, stockArgs...)
	if err != nil {
		return nil, err
	}

	projectActivitySQL := `
		SELECT COALESCE(pr.name, 'Unassigned') AS project_name,
		       COUNT(*) AS document_count,
		       COALESCE(SUM(d.total), 0) AS total
		FROM documents d
		LEFT JOIN projects pr ON pr.id = d.project_id AND pr.deleted_at IS NULL
		WHERE d.business_id = ?
		  AND d.deleted_at IS NULL
		GROUP BY COALESCE(pr.name, 'Unassigned')
		ORDER BY total DESC
		LIMIT 5
	`
	projectActivity, err := r.scanRows(ctx, projectActivitySQL, query.BusinessID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"cards": cards,
		"sections": map[string]interface{}{
			"top_products":       topProducts,
			"top_customers":      topCustomers,
			"low_stock_items":    lowStock,
			"expiring_batches":   expiringBatches,
			"warehouse_activity": warehouseActivity,
			"project_activity":   projectActivity,
		},
	}, nil
}

func (r *reportingRepository) CreateReportRun(ctx context.Context, run *models.ReportRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}

func (r *reportingRepository) GetReportRun(ctx context.Context, businessID, id string) (*models.ReportRun, error) {
	var run models.ReportRun
	err := r.db.WithContext(ctx).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("report run not found")
	}
	return &run, err
}

func (r *reportingRepository) UpsertReportPreference(ctx context.Context, pref *models.ReportPreference) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.ReportPreference
		err := tx.Where("business_id = ? AND user_id = ? AND report_key = ? AND deleted_at IS NULL",
			pref.BusinessID, pref.UserID, pref.ReportKey).
			First(&existing).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return tx.Create(pref).Error
		case err != nil:
			return err
		default:
			existing.Config = pref.Config
			existing.DeletedAt = gorm.DeletedAt{}
			return tx.Model(&existing).Updates(map[string]interface{}{
				"config":     existing.Config,
				"updated_at": time.Now(),
				"deleted_at": nil,
			}).Error
		}
	})
}

func (r *reportingRepository) GetReportPreference(ctx context.Context, businessID, userID, reportKey string) (*models.ReportPreference, error) {
	var pref models.ReportPreference
	err := r.db.WithContext(ctx).
		Where("business_id = ? AND user_id = ? AND report_key = ? AND deleted_at IS NULL", businessID, userID, reportKey).
		First(&pref).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("report preference not found")
	}
	return &pref, err
}

func (r *reportingRepository) CreateReportShare(ctx context.Context, share *models.ReportShare) error {
	return r.db.WithContext(ctx).Create(share).Error
}

func (r *reportingRepository) ListReportShares(ctx context.Context, businessID string, page, limit int) ([]*models.ReportShare, int64, error) {
	var shares []models.ReportShare
	var total int64
	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.ReportShare{}).
		Where("business_id = ? AND deleted_at IS NULL", businessID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&shares).Error; err != nil {
		return nil, 0, err
	}
	result := make([]*models.ReportShare, len(shares))
	for i := range shares {
		result[i] = &shares[i]
	}
	return result, total, nil
}

func (r *reportingRepository) GetReportShareByTokenHash(ctx context.Context, tokenHash string) (*models.ReportShare, error) {
	var share models.ReportShare
	err := r.db.WithContext(ctx).
		Where("token_hash = ? AND deleted_at IS NULL", tokenHash).
		First(&share).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("report share not found")
	}
	return &share, err
}

func (r *reportingRepository) TouchReportShareAccess(ctx context.Context, shareID string, accessedAt time.Time) error {
	return r.db.WithContext(ctx).Model(&models.ReportShare{}).
		Where("id = ?", shareID).
		Updates(map[string]interface{}{
			"last_accessed_at": accessedAt,
			"access_count":     gorm.Expr("access_count + 1"),
		}).Error
}

func (r *reportingRepository) CreateReportShareAccessLog(ctx context.Context, accessLog *models.ReportShareAccessLog) error {
	return r.db.WithContext(ctx).Create(accessLog).Error
}

func (r *reportingRepository) executeBundle(ctx context.Context, bundle reportBundle, page, limit int) (*reporting.Result, error) {
	offset := (page - 1) * limit
	rowSQL := bundle.rowSQL
	if strings.TrimSpace(bundle.orderBy) != "" {
		rowSQL += "\nORDER BY " + bundle.orderBy
	}
	rowSQL += "\nLIMIT ? OFFSET ?"
	rowArgs := append([]interface{}{}, bundle.rowArgs...)
	rowArgs = append(rowArgs, limit, offset)

	rows, err := r.scanRows(ctx, rowSQL, rowArgs...)
	if err != nil {
		return nil, err
	}

	totals, err := r.scanSingleMap(ctx, bundle.totalSQL, bundle.totalArgs...)
	if err != nil {
		return nil, err
	}
	total := int64(toFloat64(totals["row_count"]))
	delete(totals, "row_count")

	return &reporting.Result{
		Columns: bundle.columns,
		Rows:    rows,
		Totals:  totals,
		Pagination: reporting.Pagination{
			Page:  page,
			Limit: limit,
			Total: total,
		},
	}, nil
}

func (r *reportingRepository) buildDocumentRegisterBundle(def reporting.Definition, query reporting.Query) (reportBundle, error) {
	whereClause, args := buildDocumentFilters("d", def.DocumentTypes, query, "COALESCE(c.name, v.name, 'Manual')")
	base := fmt.Sprintf(`
		SELECT d.issue_date,
		       d.serial_number,
		       d.document_type,
		       d.status,
		       COALESCE(c.name, v.name, 'Manual') AS party_name,
		       COALESCE(pr.name, 'Unassigned') AS project_name,
		       d.subtotal,
		       d.tax_total,
		       d.total,
		       d.paid_amount,
		       d.balance_due
		FROM documents d
		LEFT JOIN customers c ON c.id = d.party_id AND d.party_type = 'customer' AND c.deleted_at IS NULL
		LEFT JOIN vendors v ON v.id = d.party_id AND d.party_type = 'vendor' AND v.deleted_at IS NULL
		LEFT JOIN projects pr ON pr.id = d.project_id AND pr.deleted_at IS NULL
		WHERE %s
	`, whereClause)
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count,
		       COALESCE(SUM(subtotal), 0) AS subtotal,
		       COALESCE(SUM(tax_total), 0) AS tax_total,
		       COALESCE(SUM(total), 0) AS total,
		       COALESCE(SUM(paid_amount), 0) AS paid_amount,
		       COALESCE(SUM(balance_due), 0) AS balance_due
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   args,
		totalSQL:  totalSQL,
		totalArgs: args,
		columns:   def.DefaultColumns,
		orderBy:   "issue_date DESC, serial_number DESC",
	}, nil
}

func (r *reportingRepository) buildDailyDocumentBundle(def reporting.Definition, query reporting.Query) (reportBundle, error) {
	whereClause, args := buildDocumentFilters("d", def.DocumentTypes, query, "")
	base := fmt.Sprintf(`
		SELECT DATE(d.issue_date) AS report_date,
		       COUNT(*) AS document_count,
		       COALESCE(SUM(d.subtotal), 0) AS subtotal,
		       COALESCE(SUM(d.tax_total), 0) AS tax_total,
		       COALESCE(SUM(d.total), 0) AS total
		FROM documents d
		WHERE %s
		GROUP BY DATE(d.issue_date)
	`, whereClause)
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count,
		       COALESCE(SUM(document_count), 0) AS document_count,
		       COALESCE(SUM(subtotal), 0) AS subtotal,
		       COALESCE(SUM(tax_total), 0) AS tax_total,
		       COALESCE(SUM(total), 0) AS total
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   args,
		totalSQL:  totalSQL,
		totalArgs: args,
		columns:   def.DefaultColumns,
		orderBy:   "report_date DESC",
	}, nil
}

func (r *reportingRepository) buildLineSummaryBundle(def reporting.Definition, query reporting.Query) (reportBundle, error) {
	groupSelect, groupBy, orderBy := groupedEntityColumns(def.GroupBy)
	whereClause, args := buildDocumentLineFilters(def.DocumentTypes, query)
	base := fmt.Sprintf(`
		SELECT %s,
		       COALESCE(SUM(CASE WHEN d.document_type IN ('credit_note', 'debit_note') THEN -dl.quantity ELSE dl.quantity END), 0) AS quantity,
		       COALESCE(SUM(CASE WHEN d.document_type IN ('credit_note', 'debit_note') THEN -dl.line_subtotal ELSE dl.line_subtotal END), 0) AS subtotal,
		       COALESCE(SUM(CASE WHEN d.document_type IN ('credit_note', 'debit_note') THEN -dl.tax_amount ELSE dl.tax_amount END), 0) AS tax_total,
		       COALESCE(SUM(CASE WHEN d.document_type IN ('credit_note', 'debit_note') THEN -dl.line_total ELSE dl.line_total END), 0) AS total
		FROM documents d
		JOIN document_lines dl ON dl.document_id = d.id
		LEFT JOIN products p ON p.id = dl.product_id AND p.deleted_at IS NULL
		LEFT JOIN product_categories pc ON pc.id = p.category_id AND pc.deleted_at IS NULL
		LEFT JOIN projects pr ON pr.id = d.project_id AND pr.deleted_at IS NULL
		LEFT JOIN customers c ON c.id = d.party_id AND d.party_type = 'customer' AND c.deleted_at IS NULL
		LEFT JOIN vendors v ON v.id = d.party_id AND d.party_type = 'vendor' AND v.deleted_at IS NULL
		LEFT JOIN warehouses w ON w.id = dl.warehouse_id AND w.deleted_at IS NULL
		WHERE %s
		GROUP BY %s
	`, groupSelect, whereClause, groupBy)
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count,
		       COALESCE(SUM(quantity), 0) AS quantity,
		       COALESCE(SUM(subtotal), 0) AS subtotal,
		       COALESCE(SUM(tax_total), 0) AS tax_total,
		       COALESCE(SUM(total), 0) AS total
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   args,
		totalSQL:  totalSQL,
		totalArgs: args,
		columns:   def.DefaultColumns,
		orderBy:   orderBy,
	}, nil
}

func (r *reportingRepository) buildLineProfitBundle(def reporting.Definition, query reporting.Query) (reportBundle, error) {
	groupSelect, groupBy, orderBy := groupedEntityColumns(def.GroupBy)
	docTypes := []string{"sales_invoice", "bill_of_supply", "credit_note"}
	whereClause, args := buildDocumentLineFilters(docTypes, query)
	base := fmt.Sprintf(`
		SELECT %s,
		       COALESCE(SUM(CASE WHEN d.document_type = 'credit_note' THEN -dl.quantity ELSE dl.quantity END), 0) AS quantity,
		       COALESCE(SUM(CASE WHEN d.document_type = 'credit_note' THEN -dl.line_subtotal ELSE dl.line_subtotal END), 0) AS sale_value,
		       COALESCE(SUM(CASE WHEN d.document_type = 'credit_note' THEN -(dl.cost_snapshot * dl.quantity) ELSE (dl.cost_snapshot * dl.quantity) END), 0) AS cost_value,
		       COALESCE(SUM(CASE WHEN d.document_type = 'credit_note' THEN -dl.margin_snapshot ELSE dl.margin_snapshot END), 0) AS profit
		FROM documents d
		JOIN document_lines dl ON dl.document_id = d.id
		LEFT JOIN products p ON p.id = dl.product_id AND p.deleted_at IS NULL
		LEFT JOIN product_categories pc ON pc.id = p.category_id AND pc.deleted_at IS NULL
		LEFT JOIN projects pr ON pr.id = d.project_id AND pr.deleted_at IS NULL
		LEFT JOIN customers c ON c.id = d.party_id AND d.party_type = 'customer' AND c.deleted_at IS NULL
		LEFT JOIN vendors v ON v.id = d.party_id AND d.party_type = 'vendor' AND v.deleted_at IS NULL
		WHERE %s
		GROUP BY %s
	`, groupSelect, whereClause, groupBy)
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count,
		       COALESCE(SUM(quantity), 0) AS quantity,
		       COALESCE(SUM(sale_value), 0) AS sale_value,
		       COALESCE(SUM(cost_value), 0) AS cost_value,
		       COALESCE(SUM(profit), 0) AS profit
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   args,
		totalSQL:  totalSQL,
		totalArgs: args,
		columns:   def.DefaultColumns,
		orderBy:   orderBy,
	}, nil
}

func (r *reportingRepository) buildProfitAndLossBundle(query reporting.Query) (reportBundle, error) {
	whereClause, args := buildDocumentFilters("d", nil, query, "")
	base := fmt.Sprintf(`
		WITH metrics AS (
			SELECT 'sales' AS metric, COALESCE(SUM(CASE WHEN d.document_type IN ('sales_invoice', 'bill_of_supply') THEN d.subtotal WHEN d.document_type = 'credit_note' THEN -d.subtotal ELSE 0 END), 0) AS amount
			FROM documents d
			WHERE %s
			UNION ALL
			SELECT 'purchases' AS metric, COALESCE(SUM(CASE WHEN d.document_type = 'purchase_invoice' THEN d.subtotal WHEN d.document_type = 'debit_note' THEN -d.subtotal ELSE 0 END), 0) AS amount
			FROM documents d
			WHERE %s
			UNION ALL
			SELECT 'expenses' AS metric, COALESCE(SUM(CASE WHEN d.document_type = 'expense' THEN d.subtotal ELSE 0 END), 0) AS amount
			FROM documents d
			WHERE %s
			UNION ALL
			SELECT 'gross_profit' AS metric,
			       COALESCE(SUM(CASE WHEN d.document_type IN ('sales_invoice', 'bill_of_supply') THEN dl.margin_snapshot WHEN d.document_type = 'credit_note' THEN -dl.margin_snapshot ELSE 0 END), 0) AS amount
			FROM documents d
			LEFT JOIN document_lines dl ON dl.document_id = d.id
			WHERE %s
		)
		SELECT metric, amount FROM metrics
	`, whereClause, whereClause, whereClause, whereClause)
	allArgs := append([]interface{}{}, args...)
	allArgs = append(allArgs, args...)
	allArgs = append(allArgs, args...)
	allArgs = append(allArgs, args...)
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count, COALESCE(SUM(amount), 0) AS amount
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   allArgs,
		totalSQL:  totalSQL,
		totalArgs: allArgs,
		columns:   reporting.LookupOrPanic("profit_and_loss").DefaultColumns,
		orderBy:   "metric ASC",
	}, nil
}

func (r *reportingRepository) buildCustomerLedgerBundle(query reporting.Query) (reportBundle, error) {
	return r.buildLedgerBundle(query, "customer")
}

func (r *reportingRepository) buildVendorLedgerBundle(query reporting.Query) (reportBundle, error) {
	return r.buildLedgerBundle(query, "vendor")
}

func (r *reportingRepository) buildProjectLedgerBundle(query reporting.Query) (reportBundle, error) {
	whereDoc, args := buildDocumentFilters("d", nil, query, "")
	base := fmt.Sprintf(`
		WITH entries AS (
			SELECT COALESCE(pr.name, 'Unassigned') AS project_name,
			       d.issue_date AS entry_date,
			       d.serial_number AS reference,
			       d.document_type AS source_type,
			       CASE WHEN d.document_type IN ('sales_invoice', 'bill_of_supply', 'credit_note') THEN d.total ELSE 0 END AS debit,
			       CASE WHEN d.document_type IN ('purchase_invoice', 'expense', 'debit_note') THEN d.total ELSE 0 END AS credit
			FROM documents d
			LEFT JOIN projects pr ON pr.id = d.project_id AND pr.deleted_at IS NULL
			WHERE %s
			UNION ALL
			SELECT COALESCE(pr.name, 'Unassigned') AS project_name,
			       p.payment_date AS entry_date,
			       COALESCE(d.serial_number, p.reference, p.id) AS reference,
			       'payment' AS source_type,
			       0 AS debit,
			       p.amount AS credit
			FROM payments p
			LEFT JOIN documents d ON d.id = p.invoice_id AND d.deleted_at IS NULL
			LEFT JOIN projects pr ON pr.id = p.project_id AND pr.deleted_at IS NULL
			WHERE p.business_id = ?
			  AND p.deleted_at IS NULL
		)
		SELECT project_name, entry_date, reference, source_type, debit, credit,
		       SUM(debit - credit) OVER (PARTITION BY project_name ORDER BY entry_date, reference ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS running_balance
		FROM entries
	`, whereDoc)
	rowArgs := append([]interface{}{}, args...)
	rowArgs = append(rowArgs, query.BusinessID)
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count,
		       COALESCE(SUM(debit), 0) AS debit,
		       COALESCE(SUM(credit), 0) AS credit,
		       COALESCE(SUM(debit - credit), 0) AS running_balance
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   rowArgs,
		totalSQL:  totalSQL,
		totalArgs: rowArgs,
		columns:   reporting.LookupOrPanic("project_ledger").DefaultColumns,
		orderBy:   "entry_date DESC, reference DESC",
	}, nil
}

func (r *reportingRepository) buildLedgerBundle(query reporting.Query, partyType string) (reportBundle, error) {
	var docTypes []string
	var columns []reporting.Column
	if partyType == "customer" {
		docTypes = []string{"sales_invoice", "bill_of_supply", "credit_note"}
		columns = reporting.LookupOrPanic("customer_ledger").DefaultColumns
	} else {
		docTypes = []string{"purchase_invoice", "expense", "debit_note"}
		columns = reporting.LookupOrPanic("vendor_ledger").DefaultColumns
	}
	whereClause, args := buildDocumentFilters("d", docTypes, query, "")
	whereClause += " AND d.party_type = ?"
	args = append(args, partyType)
	base := fmt.Sprintf(`
		WITH entries AS (
			SELECT COALESCE(CASE WHEN d.party_type = 'customer' THEN c.name ELSE v.name END, 'Manual') AS party_name,
			       d.issue_date AS entry_date,
			       d.serial_number AS reference,
			       d.document_type AS source_type,
			       CASE
			           WHEN d.party_type = 'customer' AND d.document_type IN ('sales_invoice', 'bill_of_supply') THEN d.total
			           WHEN d.party_type = 'vendor' AND d.document_type = 'debit_note' THEN d.total
			           ELSE 0
			       END AS debit,
			       CASE
			           WHEN d.party_type = 'customer' AND d.document_type = 'credit_note' THEN d.total
			           WHEN d.party_type = 'vendor' AND d.document_type IN ('purchase_invoice', 'expense') THEN d.total
			           ELSE 0
			       END AS credit
			FROM documents d
			LEFT JOIN customers c ON c.id = d.party_id AND d.party_type = 'customer' AND c.deleted_at IS NULL
			LEFT JOIN vendors v ON v.id = d.party_id AND d.party_type = 'vendor' AND v.deleted_at IS NULL
			WHERE %s
			UNION ALL
			SELECT COALESCE(c.name, 'Manual') AS party_name,
			       p.payment_date AS entry_date,
			       COALESCE(d.serial_number, p.reference, p.id) AS reference,
			       'payment' AS source_type,
			       0 AS debit,
			       p.amount AS credit
			FROM payments p
			JOIN documents d ON d.id = p.invoice_id AND d.deleted_at IS NULL
			LEFT JOIN customers c ON c.id = d.party_id AND c.deleted_at IS NULL
			WHERE p.business_id = ?
			  AND p.deleted_at IS NULL
		)
		SELECT party_name, entry_date, reference, source_type, debit, credit,
		       SUM(debit - credit) OVER (PARTITION BY party_name ORDER BY entry_date, reference ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS running_balance
		FROM entries
	`, whereClause)
	rowArgs := append([]interface{}{}, args...)
	rowArgs = append(rowArgs, query.BusinessID)
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count,
		       COALESCE(SUM(debit), 0) AS debit,
		       COALESCE(SUM(credit), 0) AS credit,
		       COALESCE(SUM(debit - credit), 0) AS running_balance
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   rowArgs,
		totalSQL:  totalSQL,
		totalArgs: rowArgs,
		columns:   columns,
		orderBy:   "entry_date DESC, reference DESC",
	}, nil
}

func (r *reportingRepository) buildGeneralLedgerBundle(query reporting.Query) (reportBundle, error) {
	clauses := []string{"le.business_id = ?"}
	args := []interface{}{query.BusinessID}
	if query.Filters.DateFrom != nil {
		clauses = append(clauses, "le.entry_date >= ?")
		args = append(args, *query.Filters.DateFrom)
	}
	if query.Filters.DateTo != nil {
		clauses = append(clauses, "le.entry_date <= ?")
		args = append(args, *query.Filters.DateTo)
	}
	if query.Filters.ProjectID != "" {
		clauses = append(clauses, "le.project_id = ?")
		args = append(args, query.Filters.ProjectID)
	}
	base := fmt.Sprintf(`
		SELECT le.entry_date,
		       le.transaction_id,
		       le.category,
		       le.description,
		       COALESCE(pr.name, 'Unassigned') AS project_name,
		       CASE WHEN le.entry_type = 'debit' THEN le.amount ELSE 0 END AS debit,
		       CASE WHEN le.entry_type = 'credit' THEN le.amount ELSE 0 END AS credit
		FROM ledger_entries le
		LEFT JOIN projects pr ON pr.id = le.project_id AND pr.deleted_at IS NULL
		WHERE %s
	`, strings.Join(clauses, " AND "))
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count,
		       COALESCE(SUM(debit), 0) AS debit,
		       COALESCE(SUM(credit), 0) AS credit
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   args,
		totalSQL:  totalSQL,
		totalArgs: args,
		columns:   reporting.LookupOrPanic("general_ledger").DefaultColumns,
		orderBy:   "entry_date DESC, transaction_id DESC",
	}, nil
}

func (r *reportingRepository) buildJournalRegisterBundle(query reporting.Query) (reportBundle, error) {
	clauses := []string{"j.business_id = ?", "j.deleted_at IS NULL"}
	args := []interface{}{query.BusinessID}
	if query.Filters.DateFrom != nil {
		clauses = append(clauses, "j.posting_date >= ?")
		args = append(args, *query.Filters.DateFrom)
	}
	if query.Filters.DateTo != nil {
		clauses = append(clauses, "j.posting_date <= ?")
		args = append(args, *query.Filters.DateTo)
	}
	if query.Filters.ProjectID != "" {
		clauses = append(clauses, "j.project_id = ?")
		args = append(args, query.Filters.ProjectID)
	}
	base := fmt.Sprintf(`
		SELECT j.posting_date,
		       j.name,
		       j.reference,
		       j.status,
		       COALESCE(pr.name, 'Unassigned') AS project_name,
		       COALESCE(SUM(CASE WHEN jl.entry_type = 'debit' THEN jl.amount ELSE 0 END), 0) AS debit_total,
		       COALESCE(SUM(CASE WHEN jl.entry_type = 'credit' THEN jl.amount ELSE 0 END), 0) AS credit_total
		FROM journals j
		LEFT JOIN journal_lines jl ON jl.journal_id = j.id
		LEFT JOIN projects pr ON pr.id = j.project_id AND pr.deleted_at IS NULL
		WHERE %s
		GROUP BY j.posting_date, j.name, j.reference, j.status, pr.name
	`, strings.Join(clauses, " AND "))
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count,
		       COALESCE(SUM(debit_total), 0) AS debit_total,
		       COALESCE(SUM(credit_total), 0) AS credit_total
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   args,
		totalSQL:  totalSQL,
		totalArgs: args,
		columns:   reporting.LookupOrPanic("journal_register").DefaultColumns,
		orderBy:   "posting_date DESC, name DESC",
	}, nil
}

func (r *reportingRepository) buildPaymentRegisterBundle(query reporting.Query) (reportBundle, error) {
	clauses := []string{"p.business_id = ?", "p.deleted_at IS NULL"}
	args := []interface{}{query.BusinessID}
	if query.Filters.DateFrom != nil {
		clauses = append(clauses, "p.payment_date >= ?")
		args = append(args, *query.Filters.DateFrom)
	}
	if query.Filters.DateTo != nil {
		clauses = append(clauses, "p.payment_date <= ?")
		args = append(args, *query.Filters.DateTo)
	}
	if query.Filters.ProjectID != "" {
		clauses = append(clauses, "p.project_id = ?")
		args = append(args, query.Filters.ProjectID)
	}
	if query.Filters.Search != "" {
		like := "%" + strings.TrimSpace(query.Filters.Search) + "%"
		clauses = append(clauses, "(COALESCE(d.serial_number, '') ILIKE ? OR COALESCE(c.name, '') ILIKE ? OR COALESCE(p.reference, '') ILIKE ?)")
		args = append(args, like, like, like)
	}
	base := fmt.Sprintf(`
		SELECT p.payment_date,
		       COALESCE(d.serial_number, '') AS invoice_number,
		       COALESCE(c.name, 'Manual') AS party_name,
		       COALESCE(pr.name, 'Unassigned') AS project_name,
		       p.payment_method,
		       COALESCE(p.reference, '') AS reference,
		       p.amount
		FROM payments p
		LEFT JOIN documents d ON d.id = p.invoice_id AND d.deleted_at IS NULL
		LEFT JOIN customers c ON c.id = d.party_id AND c.deleted_at IS NULL
		LEFT JOIN projects pr ON pr.id = p.project_id AND pr.deleted_at IS NULL
		WHERE %s
	`, strings.Join(clauses, " AND "))
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count,
		       COALESCE(SUM(amount), 0) AS amount
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   args,
		totalSQL:  totalSQL,
		totalArgs: args,
		columns:   reporting.LookupOrPanic("payment_register").DefaultColumns,
		orderBy:   "payment_date DESC, invoice_number DESC",
	}, nil
}

func (r *reportingRepository) buildOutstandingBalanceBundle(query reporting.Query, kind string) (reportBundle, error) {
	docTypes := []string{"sales_invoice", "bill_of_supply"}
	columns := reporting.LookupOrPanic("receivables").DefaultColumns
	partyType := "customer"
	entityExpr := "COALESCE(c.name, 'Manual') AS party_name"
	if kind == "payable" {
		docTypes = []string{"purchase_invoice", "expense"}
		columns = reporting.LookupOrPanic("payables").DefaultColumns
		partyType = "vendor"
		entityExpr = "COALESCE(v.name, 'Manual') AS party_name"
	}
	whereClause, args := buildDocumentFilters("d", docTypes, query, "")
	whereClause += " AND d.party_type = ? AND d.balance_due > 0"
	args = append(args, partyType)
	base := fmt.Sprintf(`
		SELECT %s,
		       COALESCE(pr.name, 'Unassigned') AS project_name,
		       d.serial_number,
		       d.issue_date,
		       d.due_date,
		       d.total,
		       d.paid_amount,
		       d.balance_due,
		       GREATEST(DATE_PART('day', NOW() - COALESCE(d.due_date, d.issue_date)), 0) AS age_days
		FROM documents d
		LEFT JOIN customers c ON c.id = d.party_id AND d.party_type = 'customer' AND c.deleted_at IS NULL
		LEFT JOIN vendors v ON v.id = d.party_id AND d.party_type = 'vendor' AND v.deleted_at IS NULL
		LEFT JOIN projects pr ON pr.id = d.project_id AND pr.deleted_at IS NULL
		WHERE %s
	`, entityExpr, whereClause)
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count,
		       COALESCE(SUM(total), 0) AS total,
		       COALESCE(SUM(paid_amount), 0) AS paid_amount,
		       COALESCE(SUM(balance_due), 0) AS balance_due
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   args,
		totalSQL:  totalSQL,
		totalArgs: args,
		columns:   columns,
		orderBy:   "balance_due DESC, issue_date DESC",
	}, nil
}

func (r *reportingRepository) buildAgingBundle(query reporting.Query, kind string) (reportBundle, error) {
	docTypes := []string{"sales_invoice", "bill_of_supply"}
	columns := reporting.LookupOrPanic("receivables_aging").DefaultColumns
	partyType := "customer"
	entityExpr := "COALESCE(c.name, 'Manual')"
	if kind == "payable" {
		docTypes = []string{"purchase_invoice", "expense"}
		columns = reporting.LookupOrPanic("payables_aging").DefaultColumns
		partyType = "vendor"
		entityExpr = "COALESCE(v.name, 'Manual')"
	}
	whereClause, args := buildDocumentFilters("d", docTypes, query, "")
	whereClause += " AND d.party_type = ? AND d.balance_due > 0"
	args = append(args, partyType)
	base := fmt.Sprintf(`
		SELECT %s AS party_name,
		       COALESCE(SUM(CASE WHEN GREATEST(DATE_PART('day', NOW() - COALESCE(d.due_date, d.issue_date)), 0) = 0 THEN d.balance_due ELSE 0 END), 0) AS current_bucket,
		       COALESCE(SUM(CASE WHEN GREATEST(DATE_PART('day', NOW() - COALESCE(d.due_date, d.issue_date)), 0) BETWEEN 1 AND 30 THEN d.balance_due ELSE 0 END), 0) AS bucket_1_30,
		       COALESCE(SUM(CASE WHEN GREATEST(DATE_PART('day', NOW() - COALESCE(d.due_date, d.issue_date)), 0) BETWEEN 31 AND 60 THEN d.balance_due ELSE 0 END), 0) AS bucket_31_60,
		       COALESCE(SUM(CASE WHEN GREATEST(DATE_PART('day', NOW() - COALESCE(d.due_date, d.issue_date)), 0) BETWEEN 61 AND 90 THEN d.balance_due ELSE 0 END), 0) AS bucket_61_90,
		       COALESCE(SUM(CASE WHEN GREATEST(DATE_PART('day', NOW() - COALESCE(d.due_date, d.issue_date)), 0) > 90 THEN d.balance_due ELSE 0 END), 0) AS bucket_90_plus,
		       COALESCE(SUM(d.balance_due), 0) AS total_due
		FROM documents d
		LEFT JOIN customers c ON c.id = d.party_id AND d.party_type = 'customer' AND c.deleted_at IS NULL
		LEFT JOIN vendors v ON v.id = d.party_id AND d.party_type = 'vendor' AND v.deleted_at IS NULL
		WHERE %s
		GROUP BY %s
	`, entityExpr, whereClause, entityExpr)
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count,
		       COALESCE(SUM(current_bucket), 0) AS current_bucket,
		       COALESCE(SUM(bucket_1_30), 0) AS bucket_1_30,
		       COALESCE(SUM(bucket_31_60), 0) AS bucket_31_60,
		       COALESCE(SUM(bucket_61_90), 0) AS bucket_61_90,
		       COALESCE(SUM(bucket_90_plus), 0) AS bucket_90_plus,
		       COALESCE(SUM(total_due), 0) AS total_due
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   args,
		totalSQL:  totalSQL,
		totalArgs: args,
		columns:   columns,
		orderBy:   "total_due DESC, party_name ASC",
	}, nil
}

func (r *reportingRepository) buildInventoryBalanceBundle(def reporting.Definition, query reporting.Query) (reportBundle, error) {
	groupSelect := `
		p.id AS entity_key,
		p.name AS entity_name,
		COALESCE(w.name, 'Unassigned') AS warehouse_name
	`
	groupBy := "p.id, p.name, w.name"
	orderBy := "stock_value DESC, entity_name ASC"
	if def.GroupBy == "warehouse" {
		groupSelect = `
			COALESCE(w.id::text, '') AS entity_key,
			COALESCE(w.name, 'Unassigned') AS entity_name,
			COALESCE(w.name, 'Unassigned') AS warehouse_name
		`
		groupBy = "w.id, w.name"
		orderBy = "stock_value DESC, entity_name ASC"
	}
	clauses := []string{"ib.business_id = ?", "ib.deleted_at IS NULL"}
	args := []interface{}{query.BusinessID}
	if query.Filters.WarehouseID != "" {
		clauses = append(clauses, "ib.warehouse_id = ?")
		args = append(args, query.Filters.WarehouseID)
	}
	if query.Filters.ProductID != "" {
		clauses = append(clauses, "ib.product_id = ?")
		args = append(args, query.Filters.ProductID)
	}
	if query.Filters.VariantID != "" {
		clauses = append(clauses, "ib.variant_id = ?")
		args = append(args, query.Filters.VariantID)
	}
	if query.Filters.CategoryID != "" {
		clauses = append(clauses, "p.category_id = ?")
		args = append(args, query.Filters.CategoryID)
	}
	base := fmt.Sprintf(`
		SELECT %s,
		       COALESCE(SUM(ib.on_hand), 0) AS on_hand,
		       COALESCE(SUM(ib.reserved), 0) AS reserved,
		       COALESCE(SUM(ib.on_hand - ib.reserved), 0) AS available,
		       COALESCE(SUM(ib.stock_value), 0) AS stock_value
		FROM inventory_balances ib
		JOIN products p ON p.id = ib.product_id AND p.deleted_at IS NULL
		LEFT JOIN warehouses w ON w.id = ib.warehouse_id AND w.deleted_at IS NULL
		WHERE %s
		GROUP BY %s
	`, groupSelect, strings.Join(clauses, " AND "), groupBy)
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count,
		       COALESCE(SUM(on_hand), 0) AS on_hand,
		       COALESCE(SUM(reserved), 0) AS reserved,
		       COALESCE(SUM(available), 0) AS available,
		       COALESCE(SUM(stock_value), 0) AS stock_value
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   args,
		totalSQL:  totalSQL,
		totalArgs: args,
		columns:   def.DefaultColumns,
		orderBy:   orderBy,
	}, nil
}

func (r *reportingRepository) buildLowStockBundle(query reporting.Query) (reportBundle, error) {
	clauses := []string{"ib.business_id = ?", "ib.deleted_at IS NULL"}
	args := []interface{}{query.BusinessID}
	if query.Filters.WarehouseID != "" {
		clauses = append(clauses, "ib.warehouse_id = ?")
		args = append(args, query.Filters.WarehouseID)
	}
	base := fmt.Sprintf(`
		SELECT p.id::text AS entity_key,
		       p.name AS entity_name,
		       COALESCE(w.name, 'Unassigned') AS warehouse_name,
		       COALESCE(SUM(ib.on_hand), 0) AS on_hand,
		       COALESCE(SUM(ib.reserved), 0) AS reserved,
		       COALESCE(SUM(ib.on_hand - ib.reserved), 0) AS available,
		       COALESCE(SUM(ib.stock_value), 0) AS stock_value
		FROM inventory_balances ib
		JOIN products p ON p.id = ib.product_id AND p.deleted_at IS NULL
		LEFT JOIN product_variants pv ON pv.id = ib.variant_id AND pv.deleted_at IS NULL
		LEFT JOIN warehouses w ON w.id = ib.warehouse_id AND w.deleted_at IS NULL
		WHERE %s
		GROUP BY p.id, p.name, w.name, pv.low_stock_threshold, p.low_stock_threshold, p.min_stock
		HAVING COALESCE(SUM(ib.on_hand), 0) <= COALESCE(NULLIF(pv.low_stock_threshold, 0), NULLIF(p.low_stock_threshold, 0), p.min_stock, 0)
	`, strings.Join(clauses, " AND "))
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count,
		       COALESCE(SUM(on_hand), 0) AS on_hand,
		       COALESCE(SUM(reserved), 0) AS reserved,
		       COALESCE(SUM(available), 0) AS available,
		       COALESCE(SUM(stock_value), 0) AS stock_value
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   args,
		totalSQL:  totalSQL,
		totalArgs: args,
		columns:   reporting.LookupOrPanic("low_stock").DefaultColumns,
		orderBy:   "available ASC, entity_name ASC",
	}, nil
}

func (r *reportingRepository) buildStockMovementBundle(query reporting.Query) (reportBundle, error) {
	clauses := []string{"sm.business_id = ?", "sm.deleted_at IS NULL"}
	args := []interface{}{query.BusinessID}
	if query.Filters.DateFrom != nil {
		clauses = append(clauses, "sm.recorded_at >= ?")
		args = append(args, *query.Filters.DateFrom)
	}
	if query.Filters.DateTo != nil {
		clauses = append(clauses, "sm.recorded_at <= ?")
		args = append(args, *query.Filters.DateTo)
	}
	if query.Filters.ProjectID != "" {
		clauses = append(clauses, "sm.project_id = ?")
		args = append(args, query.Filters.ProjectID)
	}
	if query.Filters.WarehouseID != "" {
		clauses = append(clauses, "(sm.warehouse_id = ? OR sm.source_warehouse_id = ?)")
		args = append(args, query.Filters.WarehouseID, query.Filters.WarehouseID)
	}
	if query.Filters.ProductID != "" {
		clauses = append(clauses, "sm.product_id = ?")
		args = append(args, query.Filters.ProductID)
	}
	if query.Filters.VariantID != "" {
		clauses = append(clauses, "sm.variant_id = ?")
		args = append(args, query.Filters.VariantID)
	}
	base := fmt.Sprintf(`
		SELECT sm.recorded_at,
		       p.name AS product_name,
		       COALESCE(pv.name, 'Default') AS variant_name,
		       COALESCE(w.name, 'Unassigned') AS warehouse_name,
		       COALESCE(sw.name, '') AS source_warehouse_name,
		       COALESCE(pr.name, 'Unassigned') AS project_name,
		       sm.transaction_type,
		       sm.direction,
		       sm.quantity,
		       sm.unit_cost,
		       COALESCE(sm.reason, '') AS reason
		FROM stock_moves sm
		JOIN products p ON p.id = sm.product_id AND p.deleted_at IS NULL
		LEFT JOIN product_variants pv ON pv.id = sm.variant_id AND pv.deleted_at IS NULL
		LEFT JOIN warehouses w ON w.id = sm.warehouse_id AND w.deleted_at IS NULL
		LEFT JOIN warehouses sw ON sw.id = sm.source_warehouse_id AND sw.deleted_at IS NULL
		LEFT JOIN projects pr ON pr.id = sm.project_id AND pr.deleted_at IS NULL
		WHERE %s
	`, strings.Join(clauses, " AND "))
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count,
		       COALESCE(SUM(quantity), 0) AS quantity,
		       COALESCE(SUM(unit_cost * quantity), 0) AS unit_cost
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   args,
		totalSQL:  totalSQL,
		totalArgs: args,
		columns:   reporting.LookupOrPanic("stock_movement").DefaultColumns,
		orderBy:   "recorded_at DESC, product_name ASC",
	}, nil
}

func (r *reportingRepository) buildBatchExpiryBundle(query reporting.Query) (reportBundle, error) {
	clauses := []string{"pb.business_id = ?", "pb.deleted_at IS NULL", "pb.expires_at IS NOT NULL"}
	args := []interface{}{query.BusinessID}
	if query.Filters.ProductID != "" {
		clauses = append(clauses, "pb.product_id = ?")
		args = append(args, query.Filters.ProductID)
	}
	if query.Filters.VariantID != "" {
		clauses = append(clauses, "pb.variant_id = ?")
		args = append(args, query.Filters.VariantID)
	}
	base := fmt.Sprintf(`
		SELECT pb.batch_number,
		       p.name AS product_name,
		       COALESCE(pv.name, 'Default') AS variant_name,
		       COALESCE(w.name, 'Unassigned') AS warehouse_name,
		       COALESCE(SUM(ib.on_hand), 0) AS on_hand,
		       pb.expires_at,
		       GREATEST(DATE_PART('day', pb.expires_at - NOW()), 0) AS days_to_expiry
		FROM product_batches pb
		JOIN products p ON p.id = pb.product_id AND p.deleted_at IS NULL
		LEFT JOIN product_variants pv ON pv.id = pb.variant_id AND pv.deleted_at IS NULL
		LEFT JOIN inventory_balances ib ON ib.batch_id = pb.id AND ib.deleted_at IS NULL
		LEFT JOIN warehouses w ON w.id = ib.warehouse_id AND w.deleted_at IS NULL
		WHERE %s
		GROUP BY pb.batch_number, p.name, pv.name, w.name, pb.expires_at
	`, strings.Join(clauses, " AND "))
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count,
		       COALESCE(SUM(on_hand), 0) AS on_hand
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   args,
		totalSQL:  totalSQL,
		totalArgs: args,
		columns:   reporting.LookupOrPanic("batch_expiry").DefaultColumns,
		orderBy:   "expires_at ASC, product_name ASC",
	}, nil
}

func (r *reportingRepository) buildSerialTrackingBundle(query reporting.Query) (reportBundle, error) {
	clauses := []string{"psn.business_id = ?", "psn.deleted_at IS NULL"}
	args := []interface{}{query.BusinessID}
	if query.Filters.ProductID != "" {
		clauses = append(clauses, "psn.product_id = ?")
		args = append(args, query.Filters.ProductID)
	}
	if query.Filters.VariantID != "" {
		clauses = append(clauses, "psn.variant_id = ?")
		args = append(args, query.Filters.VariantID)
	}
	if query.Filters.WarehouseID != "" {
		clauses = append(clauses, "psn.warehouse_id = ?")
		args = append(args, query.Filters.WarehouseID)
	}
	base := fmt.Sprintf(`
		SELECT psn.serial_number,
		       COALESCE(psn.imei, '') AS imei,
		       p.name AS product_name,
		       COALESCE(pv.name, 'Default') AS variant_name,
		       COALESCE(w.name, 'Unassigned') AS warehouse_name,
		       psn.status
		FROM product_serial_numbers psn
		JOIN products p ON p.id = psn.product_id AND p.deleted_at IS NULL
		LEFT JOIN product_variants pv ON pv.id = psn.variant_id AND pv.deleted_at IS NULL
		LEFT JOIN warehouses w ON w.id = psn.warehouse_id AND w.deleted_at IS NULL
		WHERE %s
	`, strings.Join(clauses, " AND "))
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   args,
		totalSQL:  totalSQL,
		totalArgs: args,
		columns:   reporting.LookupOrPanic("serial_tracking").DefaultColumns,
		orderBy:   "product_name ASC, serial_number ASC",
	}, nil
}

func (r *reportingRepository) buildWarehouseTransferBundle(query reporting.Query) (reportBundle, error) {
	clauses := []string{"sm.business_id = ?", "sm.deleted_at IS NULL", "sm.transaction_type = 'transfer'"}
	args := []interface{}{query.BusinessID}
	if query.Filters.DateFrom != nil {
		clauses = append(clauses, "sm.recorded_at >= ?")
		args = append(args, *query.Filters.DateFrom)
	}
	if query.Filters.DateTo != nil {
		clauses = append(clauses, "sm.recorded_at <= ?")
		args = append(args, *query.Filters.DateTo)
	}
	if query.Filters.ProjectID != "" {
		clauses = append(clauses, "sm.project_id = ?")
		args = append(args, query.Filters.ProjectID)
	}
	if query.Filters.WarehouseID != "" {
		clauses = append(clauses, "(sm.warehouse_id = ? OR sm.source_warehouse_id = ?)")
		args = append(args, query.Filters.WarehouseID, query.Filters.WarehouseID)
	}
	base := fmt.Sprintf(`
		SELECT sm.recorded_at,
		       p.name AS product_name,
		       COALESCE(pv.name, 'Default') AS variant_name,
		       COALESCE(sw.name, 'Unassigned') AS source_warehouse_name,
		       COALESCE(w.name, 'Unassigned') AS warehouse_name,
		       sm.quantity,
		       sm.unit_cost,
		       COALESCE(sm.reason, '') AS reason
		FROM stock_moves sm
		JOIN products p ON p.id = sm.product_id AND p.deleted_at IS NULL
		LEFT JOIN product_variants pv ON pv.id = sm.variant_id AND pv.deleted_at IS NULL
		LEFT JOIN warehouses w ON w.id = sm.warehouse_id AND w.deleted_at IS NULL
		LEFT JOIN warehouses sw ON sw.id = sm.source_warehouse_id AND sw.deleted_at IS NULL
		WHERE %s
	`, strings.Join(clauses, " AND "))
	totalSQL := fmt.Sprintf(`
		WITH rows AS (%s)
		SELECT COUNT(*) AS row_count,
		       COALESCE(SUM(quantity), 0) AS quantity,
		       COALESCE(SUM(unit_cost * quantity), 0) AS unit_cost
		FROM rows
	`, base)
	return reportBundle{
		rowSQL:    base,
		rowArgs:   args,
		totalSQL:  totalSQL,
		totalArgs: args,
		columns:   reporting.LookupOrPanic("warehouse_transfer_register").DefaultColumns,
		orderBy:   "recorded_at DESC, product_name ASC",
	}, nil
}

func buildDocumentFilters(alias string, docTypes []string, query reporting.Query, searchExpr string) (string, []interface{}) {
	clauses := []string{fmt.Sprintf("%s.business_id = ?", alias), fmt.Sprintf("%s.deleted_at IS NULL", alias)}
	args := []interface{}{query.BusinessID}
	if !query.Filters.IncludeCancelled {
		clauses = append(clauses, fmt.Sprintf("%s.status <> 'cancelled'", alias))
	}
	if len(docTypes) > 0 {
		placeholders := make([]string, len(docTypes))
		for i, docType := range docTypes {
			placeholders[i] = "?"
			args = append(args, docType)
		}
		clauses = append(clauses, fmt.Sprintf("%s.document_type IN (%s)", alias, strings.Join(placeholders, ", ")))
	}
	if query.Filters.DateFrom != nil {
		clauses = append(clauses, fmt.Sprintf("%s.issue_date >= ?", alias))
		args = append(args, *query.Filters.DateFrom)
	}
	if query.Filters.DateTo != nil {
		clauses = append(clauses, fmt.Sprintf("%s.issue_date <= ?", alias))
		args = append(args, *query.Filters.DateTo)
	}
	if query.Filters.ProjectID != "" {
		clauses = append(clauses, fmt.Sprintf("%s.project_id = ?", alias))
		args = append(args, query.Filters.ProjectID)
	}
	if query.Filters.PartyID != "" {
		clauses = append(clauses, fmt.Sprintf("%s.party_id = ?", alias))
		args = append(args, query.Filters.PartyID)
	}
	if query.Filters.Search != "" && searchExpr != "" {
		like := "%" + strings.TrimSpace(query.Filters.Search) + "%"
		clauses = append(clauses, fmt.Sprintf("(%s.serial_number ILIKE ? OR %s ILIKE ?)", alias, searchExpr))
		args = append(args, like, like)
	}
	return strings.Join(clauses, " AND "), args
}

func buildDocumentLineFilters(docTypes []string, query reporting.Query) (string, []interface{}) {
	clauses := []string{"d.business_id = ?", "d.deleted_at IS NULL"}
	args := []interface{}{query.BusinessID}
	if !query.Filters.IncludeCancelled {
		clauses = append(clauses, "d.status <> 'cancelled'")
	}
	if len(docTypes) > 0 {
		placeholders := make([]string, len(docTypes))
		for i, docType := range docTypes {
			placeholders[i] = "?"
			args = append(args, docType)
		}
		clauses = append(clauses, fmt.Sprintf("d.document_type IN (%s)", strings.Join(placeholders, ", ")))
	}
	if query.Filters.DateFrom != nil {
		clauses = append(clauses, "d.issue_date >= ?")
		args = append(args, *query.Filters.DateFrom)
	}
	if query.Filters.DateTo != nil {
		clauses = append(clauses, "d.issue_date <= ?")
		args = append(args, *query.Filters.DateTo)
	}
	if query.Filters.ProjectID != "" {
		clauses = append(clauses, "d.project_id = ?")
		args = append(args, query.Filters.ProjectID)
	}
	if query.Filters.PartyID != "" {
		clauses = append(clauses, "d.party_id = ?")
		args = append(args, query.Filters.PartyID)
	}
	if query.Filters.WarehouseID != "" {
		clauses = append(clauses, "dl.warehouse_id = ?")
		args = append(args, query.Filters.WarehouseID)
	}
	if query.Filters.ProductID != "" {
		clauses = append(clauses, "dl.product_id = ?")
		args = append(args, query.Filters.ProductID)
	}
	if query.Filters.VariantID != "" {
		clauses = append(clauses, "dl.variant_id = ?")
		args = append(args, query.Filters.VariantID)
	}
	if query.Filters.CategoryID != "" {
		clauses = append(clauses, "p.category_id = ?")
		args = append(args, query.Filters.CategoryID)
	}
	if query.Filters.Search != "" {
		like := "%" + strings.TrimSpace(query.Filters.Search) + "%"
		clauses = append(clauses, "(COALESCE(p.name, '') ILIKE ? OR COALESCE(pc.name, '') ILIKE ? OR COALESCE(c.name, v.name, 'Manual') ILIKE ?)")
		args = append(args, like, like, like)
	}
	return strings.Join(clauses, " AND "), args
}

func groupedEntityColumns(groupBy string) (string, string, string) {
	switch groupBy {
	case "document":
		return "d.id::text AS document_key, d.serial_number AS entity_name", "d.id, d.serial_number", "sale_value DESC, entity_name ASC"
	case "category":
		return "COALESCE(pc.id::text, '') AS entity_key, COALESCE(pc.name, 'Uncategorized') AS entity_name", "pc.id, pc.name", "total DESC, entity_name ASC"
	case "party":
		return "COALESCE(d.party_id::text, '') AS entity_key, COALESCE(c.name, v.name, 'Manual') AS entity_name", "d.party_id, c.name, v.name", "total DESC, entity_name ASC"
	case "project":
		return "COALESCE(pr.id::text, '') AS entity_key, COALESCE(pr.name, 'Unassigned') AS entity_name", "pr.id, pr.name", "total DESC, entity_name ASC"
	case "warehouse":
		return "COALESCE(w.id::text, '') AS entity_key, COALESCE(w.name, 'Unassigned') AS entity_name", "w.id, w.name", "stock_value DESC, entity_name ASC"
	default:
		return "COALESCE(p.id::text, '') AS entity_key, COALESCE(p.name, 'Unknown Product') AS entity_name", "p.id, p.name", "total DESC, entity_name ASC"
	}
}

func (r *reportingRepository) scanRows(ctx context.Context, sqlQuery string, args ...interface{}) ([]map[string]interface{}, error) {
	rows, err := r.db.WithContext(ctx).Raw(sqlQuery, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := make([]map[string]interface{}, 0)
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}
		item := make(map[string]interface{}, len(columns))
		for i, col := range columns {
			item[col] = normalizeSQLValue(values[i])
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *reportingRepository) scanSingleMap(ctx context.Context, sqlQuery string, args ...interface{}) (map[string]interface{}, error) {
	rows, err := r.scanRows(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return map[string]interface{}{}, nil
	}
	return rows[0], nil
}

func normalizeSQLValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case nil:
		return nil
	case []byte:
		str := string(typed)
		var parsed interface{}
		if json.Unmarshal(typed, &parsed) == nil && (strings.HasPrefix(str, "{") || strings.HasPrefix(str, "[")) {
			return parsed
		}
		return str
	case time.Time:
		return typed.UTC().Format(time.RFC3339)
	case *time.Time:
		if typed == nil {
			return nil
		}
		return typed.UTC().Format(time.RFC3339)
	case float32:
		return math.Round(float64(typed)*1000) / 1000
	case float64:
		return math.Round(typed*1000) / 1000
	case int64, int32, int16, int8, int:
		return typed
	case bool:
		return typed
	case string:
		return typed
	default:
		if scanner, ok := typed.(sql.NullString); ok {
			if scanner.Valid {
				return scanner.String
			}
			return nil
		}
		return fmt.Sprint(typed)
	}
}

func toFloat64(value interface{}) float64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int64:
		return float64(typed)
	case int32:
		return float64(typed)
	case int:
		return float64(typed)
	case json.Number:
		v, _ := typed.Float64()
		return v
	case string:
		var parsed float64
		fmt.Sscanf(typed, "%f", &parsed)
		return parsed
	default:
		return 0
	}
}
