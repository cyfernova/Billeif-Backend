package services

import (
	"context"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"

	"gorm.io/gorm"
)

var (
	dashboardA2ATerminalStates = []string{
		string(a2a.TaskStateCompleted),
		string(a2a.TaskStateFailed),
		string(a2a.TaskStateCancelled),
		string(a2a.TaskStateRejected),
		"completed",
		"failed",
		"cancelled",
		"canceled",
		"rejected",
	}

	dashboardRunningProcurementStatuses = []string{
		"pending",
		"matching",
		"negotiating",
		"winner_selected",
		"running",
		"in_progress",
	}
)

type DashboardInvoiceRecord struct {
	ID           string    `json:"id"`
	InvoiceNo    string    `json:"invoice_no"`
	CustomerID   string    `json:"customer_id"`
	CustomerName string    `json:"customer_name"`
	Status       string    `json:"status"`
	Total        float64   `json:"total"`
	AmountPaid   float64   `json:"amount_paid"`
	BalanceDue   float64   `json:"balance_due"`
	InvoiceDate  time.Time `json:"issue_date"`
	DueDate      time.Time `json:"due_date"`
	CreatedAt    time.Time `json:"created_at"`
}

type DashboardFinanceSummary struct {
	CustomerCount           int64                    `json:"customer_count"`
	VendorCount             int64                    `json:"vendor_count"`
	InvoiceCount            int64                    `json:"invoice_count"`
	PaymentCount            int64                    `json:"payment_count"`
	JournalCount            int64                    `json:"journal_count"`
	LedgerEntryCount        int64                    `json:"ledger_entry_count"`
	PriceListCount          int64                    `json:"price_list_count"`
	SubscriptionCount       int64                    `json:"subscription_count"`
	ActiveSubscriptionCount int64                    `json:"active_subscription_count"`
	PartyGroupCount         int64                    `json:"party_group_count"`
	ProjectCount            int64                    `json:"project_count"`
	ActiveProjectCount      int64                    `json:"active_project_count"`
	TeamMemberCount         int64                    `json:"team_member_count"`
	EInvoiceCount           int64                    `json:"einvoice_count"`
	InventoryAlertCount     int64                    `json:"inventory_alert_count"`
	UnreadInventoryAlerts   int64                    `json:"unread_inventory_alerts"`
	TotalReceivable         float64                  `json:"total_receivable"`
	TotalPayable            float64                  `json:"total_payable"`
	TotalCollected          float64                  `json:"total_collected"`
	TodaysCollections       float64                  `json:"todays_collections"`
	OverdueInvoices         int64                    `json:"overdue_invoices"`
	RecentInvoices          []DashboardInvoiceRecord `json:"recent_invoices"`
	UpcomingDue             []DashboardInvoiceRecord `json:"upcoming_due"`
}

type DashboardInventorySummary struct {
	ProductCount     int64    `json:"product_count"`
	ActiveProducts   int64    `json:"active_products"`
	LowStockProducts int64    `json:"low_stock_products"`
	Categories       []string `json:"categories"`
}

type DashboardCommerceSummary struct {
	StorefrontCount int64   `json:"storefront_count"`
	OrderCount      int64   `json:"order_count"`
	PendingOrders   int64   `json:"pending_orders"`
	TotalRevenue    float64 `json:"total_revenue"`
}

type DashboardAISummary struct {
	AgentCount         int64 `json:"agent_count"`
	ActiveAgents       int64 `json:"active_agents"`
	WorkflowCount      int64 `json:"workflow_count"`
	ActiveWorkflows    int64 `json:"active_workflows"`
	WorkflowRunCount   int64 `json:"workflow_run_count"`
	NegotiationCount   int64 `json:"negotiation_count"`
	ActiveNegotiations int64 `json:"active_negotiations"`
	A2ARunning         int64 `json:"a2a_running"`
	ProcurementRuns    int64 `json:"procurement_runs"`
	RunningProcurement int64 `json:"running_procurement"`
}

type DashboardSummary struct {
	Finance   DashboardFinanceSummary   `json:"finance"`
	Inventory DashboardInventorySummary `json:"inventory"`
	Commerce  DashboardCommerceSummary  `json:"commerce"`
	AI        DashboardAISummary        `json:"ai"`
}

type DashboardService struct {
	db         *gorm.DB
	log        *logger.Logger
	now        func() time.Time
	businesses businessTimezoneProvider
}

func NewDashboardService(db *gorm.DB, log *logger.Logger) *DashboardService {
	return &DashboardService{db: db, log: log.Named("dashboard"), now: time.Now}
}

func (s *DashboardService) WithBusinessTimezoneProvider(provider businessTimezoneProvider) *DashboardService {
	s.businesses = provider
	return s
}

func (s *DashboardService) Summary(ctx context.Context, businessID, userID string) (*DashboardSummary, error) {
	finance, err := s.financeSummary(ctx, businessID)
	if err != nil {
		return nil, err
	}

	inventory, err := s.inventorySummary(ctx, businessID)
	if err != nil {
		return nil, err
	}

	commerce, err := s.commerceSummary(ctx, businessID)
	if err != nil {
		return nil, err
	}

	ai := s.aiSummary(ctx, businessID, userID)
	return &DashboardSummary{
		Finance:   finance,
		Inventory: inventory,
		Commerce:  commerce,
		AI:        ai,
	}, nil
}

func (s *DashboardService) financeSummary(ctx context.Context, businessID string) (DashboardFinanceSummary, error) {
	location, err := businessCalendarLocation(ctx, s.businesses, businessID)
	if err != nil {
		return DashboardFinanceSummary{}, err
	}
	now := s.now().In(location)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	dayEnd := dayStart.AddDate(0, 0, 1)
	// Invoice dates are calendar dates; payment timestamps are instants.
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	nextWeek := today.AddDate(0, 0, 7)

	customerCount, err := s.count(ctx, "customers", "business_id = ? AND deleted_at IS NULL", businessID)
	if err != nil {
		return DashboardFinanceSummary{}, err
	}
	vendorCount, err := s.count(ctx, "vendors", "business_id = ? AND deleted_at IS NULL", businessID)
	if err != nil {
		return DashboardFinanceSummary{}, err
	}
	invoiceCount, err := s.count(ctx, "invoices", "business_id = ? AND deleted_at IS NULL", businessID)
	if err != nil {
		return DashboardFinanceSummary{}, err
	}
	paymentCount, err := s.count(ctx, "payments", "business_id = ? AND deleted_at IS NULL", businessID)
	if err != nil {
		return DashboardFinanceSummary{}, err
	}

	lowStockProducts, err := s.count(ctx, "products", "business_id = ? AND deleted_at IS NULL AND is_active = ? AND stock_level <= low_stock_threshold", businessID, true)
	if err != nil {
		return DashboardFinanceSummary{}, err
	}
	collectibleStatuses := []string{models.InvoiceStatusIssued, models.InvoiceStatusSent, models.InvoiceStatusPartiallyPaid, models.InvoiceStatusOverdue}
	totalReceivable, err := s.sum(ctx, "invoices", "balance_due", "business_id = ? AND deleted_at IS NULL AND status IN ? AND balance_due > 0", businessID, collectibleStatuses)
	if err != nil {
		return DashboardFinanceSummary{}, err
	}
	totalPayable, err := s.sum(
		ctx,
		"documents",
		"balance_due",
		`business_id = ? AND deleted_at IS NULL AND party_type = ? AND document_type IN ? AND status NOT IN ? AND balance_due > 0
		AND (status NOT IN ('partially_converted','fully_converted') OR EXISTS (
			SELECT 1 FROM journals j WHERE j.business_id = documents.business_id AND j.source_id = documents.id
			AND j.source_type = 'document' AND j.status IN ('posted','reversed') AND j.deleted_at IS NULL
		))`,
		businessID,
		models.DocumentPartyTypeVendor,
		[]string{models.DocumentTypePurchaseInvoice, models.DocumentTypeExpense},
		[]string{models.DocumentStatusDraft, models.DocumentStatusCancelled},
	)
	if err != nil {
		return DashboardFinanceSummary{}, err
	}
	totalCollected, err := s.sum(ctx, "payments", "amount", "business_id = ? AND deleted_at IS NULL", businessID)
	if err != nil {
		return DashboardFinanceSummary{}, err
	}
	todaysCollections, err := s.sum(ctx, "payments", "amount", "business_id = ? AND deleted_at IS NULL AND payment_date >= ? AND payment_date < ?", businessID, dayStart.UTC(), dayEnd.UTC())
	if err != nil {
		return DashboardFinanceSummary{}, err
	}
	overdueInvoices, err := s.count(ctx, "invoices", "business_id = ? AND deleted_at IS NULL AND status IN ? AND balance_due > 0 AND (status = ? OR due_date < ?)", businessID, collectibleStatuses, models.InvoiceStatusOverdue, today)
	if err != nil {
		return DashboardFinanceSummary{}, err
	}

	recentInvoices, err := s.invoiceRecords(ctx, businessID, "invoices.business_id = ? AND invoices.deleted_at IS NULL", []interface{}{businessID}, "invoices.created_at DESC", 5)
	if err != nil {
		return DashboardFinanceSummary{}, err
	}
	upcomingDue, err := s.invoiceRecords(ctx, businessID, "invoices.business_id = ? AND invoices.deleted_at IS NULL AND invoices.status IN ? AND invoices.balance_due > 0 AND invoices.due_date >= ? AND invoices.due_date <= ?", []interface{}{businessID, collectibleStatuses, today, nextWeek}, "invoices.due_date ASC", 3)
	if err != nil {
		return DashboardFinanceSummary{}, err
	}

	return DashboardFinanceSummary{
		CustomerCount:           customerCount,
		VendorCount:             vendorCount,
		InvoiceCount:            invoiceCount,
		PaymentCount:            paymentCount,
		JournalCount:            s.countOptional(ctx, "journals", "business_id = ? AND deleted_at IS NULL", businessID),
		LedgerEntryCount:        s.countOptional(ctx, "ledger_entries", "business_id = ?", businessID),
		PriceListCount:          s.countOptional(ctx, "price_lists", "business_id = ? AND deleted_at IS NULL", businessID),
		SubscriptionCount:       s.countOptional(ctx, "invoice_subscriptions", "business_id = ? AND deleted_at IS NULL", businessID),
		ActiveSubscriptionCount: s.countOptional(ctx, "invoice_subscriptions", "business_id = ? AND deleted_at IS NULL AND status = ?", businessID, "active"),
		PartyGroupCount:         s.countOptional(ctx, "party_groups", "business_id = ? AND deleted_at IS NULL", businessID),
		ProjectCount:            s.countOptional(ctx, "projects", "business_id = ? AND deleted_at IS NULL", businessID),
		ActiveProjectCount:      s.countOptional(ctx, "projects", "business_id = ? AND deleted_at IS NULL AND is_active = ?", businessID, true),
		TeamMemberCount:         s.countOptional(ctx, "team_members", "business_id = ? AND deleted_at IS NULL", businessID),
		EInvoiceCount:           s.countOptional(ctx, "einvoice_records", "business_id = ? AND deleted_at IS NULL", businessID),
		InventoryAlertCount:     lowStockProducts,
		UnreadInventoryAlerts:   lowStockProducts,
		TotalReceivable:         totalReceivable,
		TotalPayable:            totalPayable,
		TotalCollected:          totalCollected,
		TodaysCollections:       todaysCollections,
		OverdueInvoices:         overdueInvoices,
		RecentInvoices:          recentInvoices,
		UpcomingDue:             upcomingDue,
	}, nil
}

func (s *DashboardService) inventorySummary(ctx context.Context, businessID string) (DashboardInventorySummary, error) {
	productCount, err := s.count(ctx, "products", "business_id = ? AND deleted_at IS NULL", businessID)
	if err != nil {
		return DashboardInventorySummary{}, err
	}
	activeProducts, err := s.count(ctx, "products", "business_id = ? AND deleted_at IS NULL AND is_active = ?", businessID, true)
	if err != nil {
		return DashboardInventorySummary{}, err
	}
	lowStockProducts, err := s.count(ctx, "products", "business_id = ? AND deleted_at IS NULL AND is_active = ? AND stock_level <= low_stock_threshold", businessID, true)
	if err != nil {
		return DashboardInventorySummary{}, err
	}

	categories := make([]string, 0, 8)
	_ = s.db.WithContext(ctx).
		Table("product_categories").
		Select("name").
		Where("business_id = ? AND deleted_at IS NULL AND is_active = ?", businessID, true).
		Order("sort_order ASC, name ASC").
		Limit(8).
		Scan(&categories).Error

	return DashboardInventorySummary{
		ProductCount:     productCount,
		ActiveProducts:   activeProducts,
		LowStockProducts: lowStockProducts,
		Categories:       categories,
	}, nil
}

func (s *DashboardService) commerceSummary(ctx context.Context, businessID string) (DashboardCommerceSummary, error) {
	storefrontCount, err := s.count(ctx, "storefronts", "business_id = ? AND deleted_at IS NULL", businessID)
	if err != nil {
		return DashboardCommerceSummary{}, err
	}
	orderCount, err := s.count(ctx, "store_orders", "business_id = ? AND deleted_at IS NULL", businessID)
	if err != nil {
		return DashboardCommerceSummary{}, err
	}
	pendingOrders, err := s.count(ctx, "store_orders", "business_id = ? AND deleted_at IS NULL AND status IN ?", businessID, []string{"pending", "confirmed", "processing"})
	if err != nil {
		return DashboardCommerceSummary{}, err
	}
	totalRevenue, err := s.sum(ctx, "store_orders", "total", "business_id = ? AND deleted_at IS NULL AND status <> ?", businessID, "cancelled")
	if err != nil {
		return DashboardCommerceSummary{}, err
	}

	return DashboardCommerceSummary{
		StorefrontCount: storefrontCount,
		OrderCount:      orderCount,
		PendingOrders:   pendingOrders,
		TotalRevenue:    totalRevenue,
	}, nil
}

func (s *DashboardService) aiSummary(ctx context.Context, businessID, userID string) DashboardAISummary {
	workflowWhere := "user_id = ? AND deleted_at IS NULL AND agent_id IN (SELECT id FROM agents WHERE business_id = ? AND deleted_at IS NULL)"
	negotiationWhere := "user_id = ? AND (buyer_agent_id IN (SELECT id FROM agents WHERE business_id = ?) OR seller_agent_id IN (SELECT id FROM agents WHERE business_id = ?))"
	procurementWhere := "user_id = ? AND deleted_at IS NULL AND shopping_agent_id IN (SELECT id FROM agents WHERE business_id = ?)"

	return DashboardAISummary{
		AgentCount:         s.countOptional(ctx, "agents", "business_id = ? AND deleted_at IS NULL", businessID),
		ActiveAgents:       s.countOptional(ctx, "agents", "business_id = ? AND deleted_at IS NULL AND is_active = ?", businessID, true),
		WorkflowCount:      s.countOptional(ctx, "workflows", workflowWhere, userID, businessID),
		ActiveWorkflows:    s.countOptional(ctx, "workflows", workflowWhere+" AND is_enabled = ? AND status = ?", userID, businessID, true, WorkflowStatusActive),
		WorkflowRunCount:   s.countOptional(ctx, "workflow_runs", "workflow_id IN (SELECT id FROM workflows WHERE user_id = ? AND deleted_at IS NULL AND agent_id IN (SELECT id FROM agents WHERE business_id = ? AND deleted_at IS NULL))", userID, businessID),
		NegotiationCount:   s.countOptional(ctx, "bargaining_negotiations", negotiationWhere, userID, businessID, businessID),
		ActiveNegotiations: s.countOptional(ctx, "bargaining_negotiations", negotiationWhere+" AND status IN ?", userID, businessID, businessID, []string{"initiated", "in_progress", "countered", "active"}),
		A2ARunning:         s.countOptional(ctx, "a2a_tasks", "business_id = ? AND state NOT IN ?", businessID, dashboardA2ATerminalStates),
		ProcurementRuns:    s.countOptional(ctx, "procurement_runs", procurementWhere, userID, businessID),
		RunningProcurement: s.countOptional(ctx, "procurement_runs", procurementWhere+" AND status IN ?", userID, businessID, dashboardRunningProcurementStatuses),
	}
}

func (s *DashboardService) invoiceRecords(ctx context.Context, businessID, where string, args []interface{}, order string, limit int) ([]DashboardInvoiceRecord, error) {
	records := make([]DashboardInvoiceRecord, 0, limit)
	err := s.db.WithContext(ctx).
		Table("invoices").
		Select("invoices.id, invoices.invoice_no, invoices.customer_id, COALESCE(customers.name, '') AS customer_name, invoices.status, invoices.total, invoices.paid_amount AS amount_paid, invoices.balance_due, invoices.invoice_date, invoices.due_date, invoices.created_at").
		Joins("LEFT JOIN customers ON customers.id = invoices.customer_id AND customers.business_id = ?", businessID).
		Where(where, args...).
		Order(order).
		Limit(limit).
		Scan(&records).Error
	return records, err
}

func (s *DashboardService) count(ctx context.Context, table, where string, args ...interface{}) (int64, error) {
	var total int64
	err := s.db.WithContext(ctx).Table(table).Where(where, args...).Count(&total).Error
	return total, err
}

func (s *DashboardService) countOptional(ctx context.Context, table, where string, args ...interface{}) int64 {
	total, err := s.count(ctx, table, where, args...)
	if err != nil {
		s.log.Debug("optional dashboard count unavailable", "table", table, "error", err)
		return 0
	}
	return total
}

func (s *DashboardService) sum(ctx context.Context, table, column, where string, args ...interface{}) (float64, error) {
	var total float64
	err := s.db.WithContext(ctx).
		Table(table).
		Select("COALESCE(SUM("+column+"), 0)").
		Where(where, args...).
		Scan(&total).Error
	return total, err
}
