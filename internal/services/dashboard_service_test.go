package services

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"invoice-backend/pkg/logger"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDashboardServiceSummaryScopesByBusiness(t *testing.T) {
	db := newDashboardTestDB(t)
	svc := NewDashboardService(db, logger.New())
	now := time.Now().UTC()

	execDashboardSQL(t, db, `INSERT INTO customers (id, business_id, name, deleted_at) VALUES ('cust-1', 'biz-1', 'Acme', NULL), ('cust-2', 'biz-2', 'Other', NULL)`)
	execDashboardSQL(t, db, `INSERT INTO vendors (id, business_id, name, deleted_at) VALUES ('vendor-1', 'biz-1', 'Supplier', NULL)`)
	execDashboardSQL(t, db, `INSERT INTO invoices (id, business_id, customer_id, invoice_no, status, total, paid_amount, balance_due, invoice_date, due_date, created_at, deleted_at) VALUES
		('inv-1', 'biz-1', 'cust-1', 'INV-1', 'sent', 300, 50, 250, ?, ?, ?, NULL),
		('inv-2', 'biz-2', 'cust-2', 'INV-2', 'sent', 900, 0, 900, ?, ?, ?, NULL)`, now, now.AddDate(0, 0, 2), now, now, now.AddDate(0, 0, 2), now)
	execDashboardSQL(t, db, `INSERT INTO documents (id, business_id, document_type, party_type, status, balance_due, deleted_at) VALUES
		('doc-payable-1', 'biz-1', 'purchase_invoice', 'vendor', 'issued', 225, NULL),
		('doc-payable-2', 'biz-1', 'expense', 'vendor', 'issued', 100, NULL),
		('doc-receivable-1', 'biz-1', 'sales_invoice', 'customer', 'issued', 800, NULL),
		('doc-draft', 'biz-1', 'purchase_invoice', 'vendor', 'draft', 500, NULL),
		('doc-cancelled', 'biz-1', 'expense', 'vendor', 'cancelled', 600, NULL),
		('doc-other-business', 'biz-2', 'purchase_invoice', 'vendor', 'issued', 900, NULL),
		('doc-deleted', 'biz-1', 'purchase_invoice', 'vendor', 'issued', 50, ?)`, now)
	execDashboardSQL(t, db, `INSERT INTO payments (id, business_id, amount, payment_date, deleted_at) VALUES ('pay-1', 'biz-1', 50, ?, NULL), ('pay-2', 'biz-2', 900, ?, NULL)`, now, now)
	execDashboardSQL(t, db, `INSERT INTO products (id, business_id, is_active, stock_level, low_stock_threshold, deleted_at) VALUES ('prod-1', 'biz-1', 1, 2, 5, NULL), ('prod-2', 'biz-2', 1, 20, 5, NULL)`)
	execDashboardSQL(t, db, `INSERT INTO product_categories (id, business_id, name, is_active, sort_order, deleted_at) VALUES ('cat-1', 'biz-1', 'Hardware', 1, 1, NULL)`)
	execDashboardSQL(t, db, `INSERT INTO projects (id, business_id, is_active, deleted_at) VALUES
		('project-active', 'biz-1', 1, NULL),
		('project-inactive', 'biz-1', 0, NULL),
		('project-other-business', 'biz-2', 1, NULL)`)
	execDashboardSQL(t, db, `INSERT INTO storefronts (id, business_id, deleted_at) VALUES ('store-1', 'biz-1', NULL), ('store-2', 'biz-2', NULL)`)
	execDashboardSQL(t, db, `INSERT INTO store_orders (id, business_id, status, total, deleted_at) VALUES ('order-1', 'biz-1', 'pending', 125, NULL), ('order-2', 'biz-2', 'pending', 500, NULL)`)
	execDashboardSQL(t, db, `INSERT INTO agents (id, business_id, is_active, deleted_at) VALUES ('agent-1', 'biz-1', 1, NULL), ('agent-2', 'biz-2', 1, NULL)`)
	execDashboardSQL(t, db, `INSERT INTO workflows (id, user_id, agent_id, is_enabled, status, deleted_at) VALUES ('workflow-1', 'user-1', 'agent-1', 1, 'active', NULL), ('workflow-2', 'user-1', 'agent-2', 1, 'active', NULL)`)
	execDashboardSQL(t, db, `INSERT INTO workflow_runs (id, workflow_id) VALUES ('run-1', 'workflow-1'), ('run-2', 'workflow-2')`)
	execDashboardSQL(t, db, `INSERT INTO bargaining_negotiations (id, user_id, buyer_agent_id, seller_agent_id, status) VALUES ('neg-1', 'user-1', 'agent-1', 'agent-2', 'in_progress'), ('neg-2', 'user-1', 'agent-2', 'agent-2', 'in_progress')`)
	execDashboardSQL(t, db, `INSERT INTO a2a_tasks (id, business_id, state) VALUES
		('task-1', 'biz-1', 'TASK_STATE_WORKING'),
		('task-2', 'biz-2', 'TASK_STATE_WORKING'),
		('task-3', 'biz-1', 'TASK_STATE_COMPLETED')`)
	execDashboardSQL(t, db, `INSERT INTO procurement_runs (id, user_id, shopping_agent_id, status, deleted_at) VALUES
		('proc-1', 'user-1', 'agent-1', 'negotiating', NULL),
		('proc-2', 'user-1', 'agent-2', 'negotiating', NULL),
		('proc-3', 'user-1', 'agent-1', 'completed', NULL)`)

	summary, err := svc.Summary(context.Background(), "biz-1", "user-1")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}

	if summary.Finance.CustomerCount != 1 || summary.Finance.InvoiceCount != 1 || summary.Finance.TotalReceivable != 250 {
		t.Fatalf("finance summary was not scoped correctly: %+v", summary.Finance)
	}
	if summary.Finance.TotalPayable != 325 {
		t.Fatalf("expected payables from purchase and expense documents, got %+v", summary.Finance)
	}
	if summary.Finance.ProjectCount != 2 || summary.Finance.ActiveProjectCount != 1 {
		t.Fatalf("expected project counts to use the is_active schema, got %+v", summary.Finance)
	}
	if summary.Inventory.ProductCount != 1 || summary.Inventory.LowStockProducts != 1 || len(summary.Inventory.Categories) != 1 {
		t.Fatalf("inventory summary was not scoped correctly: %+v", summary.Inventory)
	}
	if summary.Commerce.OrderCount != 1 || summary.Commerce.PendingOrders != 1 || summary.Commerce.TotalRevenue != 125 {
		t.Fatalf("commerce summary was not scoped correctly: %+v", summary.Commerce)
	}
	if summary.AI.AgentCount != 1 || summary.AI.WorkflowCount != 1 || summary.AI.ActiveWorkflows != 1 || summary.AI.WorkflowRunCount != 1 || summary.AI.NegotiationCount != 1 || summary.AI.A2ARunning != 1 || summary.AI.RunningProcurement != 1 {
		t.Fatalf("ai summary was not scoped correctly: %+v", summary.AI)
	}
}

func newDashboardTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}

	statements := []string{
		`CREATE TABLE customers (id TEXT PRIMARY KEY, business_id TEXT, name TEXT, deleted_at DATETIME)`,
		`CREATE TABLE vendors (id TEXT PRIMARY KEY, business_id TEXT, name TEXT, deleted_at DATETIME)`,
		`CREATE TABLE invoices (id TEXT PRIMARY KEY, business_id TEXT, customer_id TEXT, invoice_no TEXT, status TEXT, total REAL, paid_amount REAL, balance_due REAL, invoice_date DATETIME, due_date DATETIME, created_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE documents (id TEXT PRIMARY KEY, business_id TEXT, document_type TEXT, party_type TEXT, status TEXT, balance_due REAL, deleted_at DATETIME)`,
		`CREATE TABLE payments (id TEXT PRIMARY KEY, business_id TEXT, amount REAL, payment_date DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE products (id TEXT PRIMARY KEY, business_id TEXT, is_active BOOLEAN, stock_level INTEGER, low_stock_threshold INTEGER, deleted_at DATETIME)`,
		`CREATE TABLE product_categories (id TEXT PRIMARY KEY, business_id TEXT, name TEXT, is_active BOOLEAN, sort_order INTEGER, deleted_at DATETIME)`,
		`CREATE TABLE projects (id TEXT PRIMARY KEY, business_id TEXT, is_active BOOLEAN, deleted_at DATETIME)`,
		`CREATE TABLE storefronts (id TEXT PRIMARY KEY, business_id TEXT, deleted_at DATETIME)`,
		`CREATE TABLE store_orders (id TEXT PRIMARY KEY, business_id TEXT, status TEXT, total REAL, deleted_at DATETIME)`,
		`CREATE TABLE agents (id TEXT PRIMARY KEY, business_id TEXT, is_active BOOLEAN, deleted_at DATETIME)`,
		`CREATE TABLE workflows (id TEXT PRIMARY KEY, user_id TEXT, agent_id TEXT, is_enabled BOOLEAN, status TEXT, deleted_at DATETIME)`,
		`CREATE TABLE workflow_runs (id TEXT PRIMARY KEY, workflow_id TEXT)`,
		`CREATE TABLE bargaining_negotiations (id TEXT PRIMARY KEY, user_id TEXT, buyer_agent_id TEXT, seller_agent_id TEXT, status TEXT)`,
		`CREATE TABLE a2a_tasks (id TEXT PRIMARY KEY, business_id TEXT, state TEXT)`,
		`CREATE TABLE procurement_runs (id TEXT PRIMARY KEY, user_id TEXT, shopping_agent_id TEXT, status TEXT, deleted_at DATETIME)`,
	}
	for _, stmt := range statements {
		execDashboardSQL(t, db, stmt)
	}

	return db
}

func execDashboardSQL(t *testing.T, db *gorm.DB, sql string, args ...interface{}) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("exec dashboard sql %q: %v", sql, err)
	}
}
