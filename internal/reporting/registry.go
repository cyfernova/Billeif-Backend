package reporting

var catalog = []Definition{
	{Key: "daily_sales", Name: "Daily Sales", Category: "sales", Description: "Day-wise sales summary.", Family: "daily_documents", DocumentTypes: []string{"sales_invoice", "bill_of_supply"}, DefaultColumns: dailyColumns()},
	{Key: "sales_register", Name: "Sales Register", Category: "sales", Description: "Sales invoices and bills of supply.", Family: "document_register", DocumentTypes: []string{"sales_invoice", "bill_of_supply"}, DefaultColumns: documentRegisterColumns()},
	{Key: "sales_returns_register", Name: "Sales Returns Register", Category: "sales", Description: "Credit notes and sales returns.", Family: "document_register", DocumentTypes: []string{"credit_note"}, DefaultColumns: documentRegisterColumns()},
	{Key: "purchase_register", Name: "Purchase Register", Category: "purchase", Description: "Purchase invoice summary.", Family: "document_register", DocumentTypes: []string{"purchase_invoice"}, DefaultColumns: documentRegisterColumns()},
	{Key: "purchase_returns_register", Name: "Purchase Returns Register", Category: "purchase", Description: "Debit notes and purchase returns.", Family: "document_register", DocumentTypes: []string{"debit_note"}, DefaultColumns: documentRegisterColumns()},
	{Key: "expense_register", Name: "Expense Register", Category: "expense", Description: "Expense document summary.", Family: "document_register", DocumentTypes: []string{"expense"}, DefaultColumns: documentRegisterColumns()},
	{Key: "bill_wise_profit", Name: "Bill-wise Profit", Category: "profitability", Description: "Profit by document.", Family: "line_profit", GroupBy: "document", DefaultColumns: profitColumns("document_key", "Document")},
	{Key: "item_wise_sales", Name: "Item-wise Sales", Category: "sales", Description: "Sales grouped by item.", Family: "line_summary", GroupBy: "product", DocumentTypes: []string{"sales_invoice", "bill_of_supply", "credit_note"}, DefaultColumns: summaryColumns("entity_key", "Item")},
	{Key: "item_wise_profit_loss", Name: "Item-wise Profit/Loss", Category: "profitability", Description: "Profitability by item.", Family: "line_profit", GroupBy: "product", DefaultColumns: profitColumns("entity_key", "Item")},
	{Key: "category_wise_sales", Name: "Category-wise Sales", Category: "sales", Description: "Sales grouped by category.", Family: "line_summary", GroupBy: "category", DocumentTypes: []string{"sales_invoice", "bill_of_supply", "credit_note"}, DefaultColumns: summaryColumns("entity_key", "Category")},
	{Key: "category_wise_profit_loss", Name: "Category-wise Profit/Loss", Category: "profitability", Description: "Profitability by category.", Family: "line_profit", GroupBy: "category", DefaultColumns: profitColumns("entity_key", "Category")},
	{Key: "party_wise_sales", Name: "Party-wise Sales", Category: "sales", Description: "Sales grouped by party.", Family: "line_summary", GroupBy: "party", DocumentTypes: []string{"sales_invoice", "bill_of_supply", "credit_note"}, DefaultColumns: summaryColumns("entity_key", "Party")},
	{Key: "gstr1_hsn_summary", Name: "GSTR-1 HSN Summary", Category: "sales", Description: "GSTR-1 Table 12 HSN-wise outward supplies.", Family: "gst_hsn_summary", DocumentTypes: []string{"sales_invoice", "bill_of_supply", "credit_note"}, DefaultColumns: gstr1HSNColumns()},
	{Key: "customer_wise_profit_loss", Name: "Customer-wise Profit/Loss", Category: "profitability", Description: "Profitability by customer.", Family: "line_profit", GroupBy: "party", DefaultColumns: profitColumns("entity_key", "Party")},
	{Key: "item_wise_purchase", Name: "Item-wise Purchase", Category: "purchase", Description: "Purchases grouped by item.", Family: "line_summary", GroupBy: "product", DocumentTypes: []string{"purchase_invoice", "debit_note"}, DefaultColumns: summaryColumns("entity_key", "Item")},
	{Key: "vendor_wise_purchases", Name: "Vendor-wise Purchases", Category: "purchase", Description: "Purchases grouped by vendor.", Family: "line_summary", GroupBy: "party", DocumentTypes: []string{"purchase_invoice", "debit_note"}, DefaultColumns: summaryColumns("entity_key", "Vendor")},
	{Key: "party_wise_expenses", Name: "Party-wise Expenses", Category: "expense", Description: "Expenses grouped by party.", Family: "line_summary", GroupBy: "party", DocumentTypes: []string{"expense"}, DefaultColumns: summaryColumns("entity_key", "Party")},
	{Key: "profit_and_loss", Name: "Profit & Loss", Category: "accounting", Description: "Overall P&L summary.", Family: "profit_and_loss", DefaultColumns: pnlColumns()},
	{Key: "customer_ledger", Name: "Customer Ledger", Category: "accounting", Description: "Customer-facing receivable ledger.", Family: "customer_ledger", DefaultColumns: ledgerColumns("party_name", "Customer")},
	{Key: "vendor_ledger", Name: "Vendor Ledger", Category: "accounting", Description: "Vendor-facing payable ledger.", Family: "vendor_ledger", DefaultColumns: ledgerColumns("party_name", "Vendor")},
	{Key: "general_ledger", Name: "General Ledger", Category: "accounting", Description: "Posted ledger entries.", Family: "general_ledger", DefaultColumns: generalLedgerColumns()},
	{Key: "journal_register", Name: "Journal Register", Category: "accounting", Description: "Journals and balancing totals.", Family: "journal_register", DefaultColumns: journalColumns()},
	{Key: "payment_register", Name: "Payment Register", Category: "accounting", Description: "Recorded payments and receipts.", Family: "payment_register", DefaultColumns: paymentColumns()},
	{Key: "receipts_register", Name: "Receipts Register", Category: "accounting", Description: "Incoming receipts.", Family: "payment_register", DefaultColumns: paymentColumns()},
	{Key: "receivables", Name: "Receivables", Category: "accounting", Description: "Outstanding customer receivables.", Family: "receivables", DefaultColumns: balanceColumns("party_name", "Customer")},
	{Key: "receivables_aging", Name: "Receivables Aging", Category: "accounting", Description: "Receivables bucketed by age.", Family: "aging_receivables", DefaultColumns: agingColumns("party_name", "Customer")},
	{Key: "payables", Name: "Payables", Category: "accounting", Description: "Outstanding vendor payables.", Family: "payables", DefaultColumns: balanceColumns("party_name", "Vendor")},
	{Key: "payables_aging", Name: "Payables Aging", Category: "accounting", Description: "Payables bucketed by age.", Family: "aging_payables", DefaultColumns: agingColumns("party_name", "Vendor")},
	{Key: "cashflow_summary", Name: "Cashflow Summary", Category: "accounting", Description: "Receipt and payable overview.", Family: "profit_and_loss", DefaultColumns: pnlColumns()},
	{Key: "stock_movement", Name: "Stock Movement", Category: "inventory", Description: "Inventory movement history.", Family: "stock_movement", DefaultColumns: stockMovementColumns()},
	{Key: "inventory_timeline", Name: "Inventory Timeline", Category: "inventory", Description: "Chronological stock movement.", Family: "stock_movement", DefaultColumns: stockMovementColumns()},
	{Key: "stock_valuation", Name: "Stock Valuation", Category: "inventory", Description: "Inventory value by item.", Family: "inventory_balances", GroupBy: "product", DefaultColumns: inventoryColumns("entity_key", "Item")},
	{Key: "current_stock", Name: "Current Stock", Category: "inventory", Description: "Current stock by item.", Family: "inventory_balances", GroupBy: "product", DefaultColumns: inventoryColumns("entity_key", "Item")},
	{Key: "low_stock", Name: "Low Stock", Category: "inventory", Description: "Low stock items.", Family: "low_stock", GroupBy: "product", DefaultColumns: inventoryColumns("entity_key", "Item")},
	{Key: "inventory_alerts", Name: "Inventory Alerts", Category: "inventory", Description: "Low stock and expiry alerts.", Family: "low_stock", GroupBy: "product", DefaultColumns: inventoryColumns("entity_key", "Item")},
	{Key: "batch_expiry", Name: "Batch Expiry", Category: "inventory", Description: "Batch expiry tracking.", Family: "batch_expiry", DefaultColumns: batchColumns()},
	{Key: "serial_tracking", Name: "Serial Tracking", Category: "inventory", Description: "Serial and IMEI tracking.", Family: "serial_tracking", DefaultColumns: serialColumns()},
	{Key: "warehouse_stock", Name: "Warehouse Stock", Category: "inventory", Description: "Stock by warehouse.", Family: "inventory_balances", GroupBy: "warehouse", DefaultColumns: inventoryColumns("entity_key", "Warehouse")},
	{Key: "warehouse_transfer_register", Name: "Warehouse Transfer Register", Category: "inventory", Description: "Warehouse transfer movement register.", Family: "warehouse_transfers", DefaultColumns: warehouseTransferColumns()},
	{Key: "project_sales", Name: "Project Sales", Category: "projects", Description: "Sales grouped by project.", Family: "line_summary", GroupBy: "project", DocumentTypes: []string{"sales_invoice", "bill_of_supply", "credit_note"}, DefaultColumns: summaryColumns("entity_key", "Project")},
	{Key: "project_purchases", Name: "Project Purchases", Category: "projects", Description: "Purchases grouped by project.", Family: "line_summary", GroupBy: "project", DocumentTypes: []string{"purchase_invoice", "debit_note"}, DefaultColumns: summaryColumns("entity_key", "Project")},
	{Key: "project_expenses", Name: "Project Expenses", Category: "projects", Description: "Expenses grouped by project.", Family: "line_summary", GroupBy: "project", DocumentTypes: []string{"expense"}, DefaultColumns: summaryColumns("entity_key", "Project")},
	{Key: "project_ledger", Name: "Project Ledger", Category: "projects", Description: "Project-wise ledger view.", Family: "project_ledger", DefaultColumns: ledgerColumns("project_name", "Project")},
	{Key: "project_receivables", Name: "Project Receivables", Category: "projects", Description: "Receivables grouped by project.", Family: "receivables", DefaultColumns: balanceColumns("project_name", "Project")},
	{Key: "project_payables", Name: "Project Payables", Category: "projects", Description: "Payables grouped by project.", Family: "payables", DefaultColumns: balanceColumns("project_name", "Project")},
	{Key: "project_profitability", Name: "Project Profitability", Category: "projects", Description: "Profitability by project.", Family: "line_profit", GroupBy: "project", DefaultColumns: profitColumns("entity_key", "Project")},
}

func Catalog() []Definition {
	result := make([]Definition, len(catalog))
	copy(result, catalog)
	return result
}

func Lookup(key string) (Definition, bool) {
	for _, item := range catalog {
		if item.Key == key {
			return item, true
		}
	}
	return Definition{}, false
}

func LookupOrPanic(key string) Definition {
	item, ok := Lookup(key)
	if !ok {
		panic("unknown report definition: " + key)
	}
	return item
}

func documentRegisterColumns() []Column {
	return []Column{
		{Key: "issue_date", Label: "Date", Type: "date"},
		{Key: "serial_number", Label: "Number", Type: "string"},
		{Key: "document_type", Label: "Type", Type: "string"},
		{Key: "status", Label: "Status", Type: "string"},
		{Key: "party_name", Label: "Party", Type: "string"},
		{Key: "project_name", Label: "Project", Type: "string"},
		{Key: "subtotal", Label: "Subtotal", Type: "number"},
		{Key: "tax_total", Label: "Tax", Type: "number"},
		{Key: "total", Label: "Total", Type: "number"},
		{Key: "paid_amount", Label: "Paid", Type: "number"},
		{Key: "balance_due", Label: "Balance Due", Type: "number"},
	}
}

func dailyColumns() []Column {
	return []Column{
		{Key: "report_date", Label: "Date", Type: "date"},
		{Key: "document_count", Label: "Documents", Type: "number"},
		{Key: "subtotal", Label: "Subtotal", Type: "number"},
		{Key: "tax_total", Label: "Tax", Type: "number"},
		{Key: "total", Label: "Total", Type: "number"},
	}
}

func summaryColumns(key, label string) []Column {
	return []Column{
		{Key: key, Label: label + " ID", Type: "string"},
		{Key: "entity_name", Label: label, Type: "string"},
		{Key: "quantity", Label: "Quantity", Type: "number"},
		{Key: "subtotal", Label: "Subtotal", Type: "number"},
		{Key: "tax_total", Label: "Tax", Type: "number"},
		{Key: "total", Label: "Total", Type: "number"},
	}
}

func gstr1HSNColumns() []Column {
	return []Column{
		{Key: "hsn_sac_code", Label: "HSN/SAC", Type: "string"},
		{Key: "description", Label: "Description", Type: "string"},
		{Key: "unit", Label: "UQC", Type: "string"},
		{Key: "tax_rate", Label: "Tax Rate", Type: "number"},
		{Key: "quantity", Label: "Quantity", Type: "number"},
		{Key: "taxable_value", Label: "Taxable Value", Type: "number"},
		{Key: "igst_amount", Label: "IGST", Type: "number"},
		{Key: "cgst_amount", Label: "CGST", Type: "number"},
		{Key: "sgst_amount", Label: "SGST", Type: "number"},
		{Key: "total_value", Label: "Total Value", Type: "number"},
	}
}

func profitColumns(key, label string) []Column {
	return []Column{
		{Key: key, Label: label + " ID", Type: "string"},
		{Key: "entity_name", Label: label, Type: "string"},
		{Key: "quantity", Label: "Quantity", Type: "number"},
		{Key: "sale_value", Label: "Sale Value", Type: "number"},
		{Key: "cost_value", Label: "Cost Value", Type: "number"},
		{Key: "profit", Label: "Profit", Type: "number"},
	}
}

func ledgerColumns(key, label string) []Column {
	return []Column{
		{Key: key, Label: label, Type: "string"},
		{Key: "entry_date", Label: "Date", Type: "date"},
		{Key: "reference", Label: "Reference", Type: "string"},
		{Key: "source_type", Label: "Source", Type: "string"},
		{Key: "debit", Label: "Debit", Type: "number"},
		{Key: "credit", Label: "Credit", Type: "number"},
		{Key: "running_balance", Label: "Running Balance", Type: "number"},
	}
}

func generalLedgerColumns() []Column {
	return []Column{
		{Key: "entry_date", Label: "Date", Type: "date"},
		{Key: "transaction_id", Label: "Transaction", Type: "string"},
		{Key: "category", Label: "Category", Type: "string"},
		{Key: "description", Label: "Description", Type: "string"},
		{Key: "project_name", Label: "Project", Type: "string"},
		{Key: "debit", Label: "Debit", Type: "number"},
		{Key: "credit", Label: "Credit", Type: "number"},
	}
}

func journalColumns() []Column {
	return []Column{
		{Key: "posting_date", Label: "Posting Date", Type: "date"},
		{Key: "name", Label: "Journal", Type: "string"},
		{Key: "reference", Label: "Reference", Type: "string"},
		{Key: "status", Label: "Status", Type: "string"},
		{Key: "project_name", Label: "Project", Type: "string"},
		{Key: "debit_total", Label: "Debit", Type: "number"},
		{Key: "credit_total", Label: "Credit", Type: "number"},
	}
}

func paymentColumns() []Column {
	return []Column{
		{Key: "payment_date", Label: "Payment Date", Type: "date"},
		{Key: "invoice_number", Label: "Invoice", Type: "string"},
		{Key: "party_name", Label: "Customer", Type: "string"},
		{Key: "project_name", Label: "Project", Type: "string"},
		{Key: "payment_method", Label: "Method", Type: "string"},
		{Key: "reference", Label: "Reference", Type: "string"},
		{Key: "amount", Label: "Amount", Type: "number"},
	}
}

func balanceColumns(key, label string) []Column {
	return []Column{
		{Key: key, Label: label, Type: "string"},
		{Key: "serial_number", Label: "Document", Type: "string"},
		{Key: "issue_date", Label: "Date", Type: "date"},
		{Key: "due_date", Label: "Due Date", Type: "date"},
		{Key: "total", Label: "Total", Type: "number"},
		{Key: "paid_amount", Label: "Paid", Type: "number"},
		{Key: "balance_due", Label: "Balance Due", Type: "number"},
		{Key: "age_days", Label: "Age (Days)", Type: "number"},
	}
}

func agingColumns(key, label string) []Column {
	return []Column{
		{Key: key, Label: label, Type: "string"},
		{Key: "current_bucket", Label: "Current", Type: "number"},
		{Key: "bucket_1_30", Label: "1-30 Days", Type: "number"},
		{Key: "bucket_31_60", Label: "31-60 Days", Type: "number"},
		{Key: "bucket_61_90", Label: "61-90 Days", Type: "number"},
		{Key: "bucket_90_plus", Label: "90+ Days", Type: "number"},
		{Key: "total_due", Label: "Total Due", Type: "number"},
	}
}

func inventoryColumns(key, label string) []Column {
	return []Column{
		{Key: key, Label: label + " ID", Type: "string"},
		{Key: "entity_name", Label: label, Type: "string"},
		{Key: "warehouse_name", Label: "Warehouse", Type: "string"},
		{Key: "on_hand", Label: "On Hand", Type: "number"},
		{Key: "reserved", Label: "Reserved", Type: "number"},
		{Key: "available", Label: "Available", Type: "number"},
		{Key: "stock_value", Label: "Stock Value", Type: "number"},
	}
}

func stockMovementColumns() []Column {
	return []Column{
		{Key: "recorded_at", Label: "Recorded At", Type: "date"},
		{Key: "product_name", Label: "Product", Type: "string"},
		{Key: "variant_name", Label: "Variant", Type: "string"},
		{Key: "warehouse_name", Label: "Warehouse", Type: "string"},
		{Key: "source_warehouse_name", Label: "Source Warehouse", Type: "string"},
		{Key: "project_name", Label: "Project", Type: "string"},
		{Key: "transaction_type", Label: "Transaction", Type: "string"},
		{Key: "direction", Label: "Direction", Type: "string"},
		{Key: "quantity", Label: "Quantity", Type: "number"},
		{Key: "unit_cost", Label: "Unit Cost", Type: "number"},
		{Key: "reason", Label: "Reason", Type: "string"},
	}
}

func batchColumns() []Column {
	return []Column{
		{Key: "batch_number", Label: "Batch Number", Type: "string"},
		{Key: "product_name", Label: "Product", Type: "string"},
		{Key: "variant_name", Label: "Variant", Type: "string"},
		{Key: "warehouse_name", Label: "Warehouse", Type: "string"},
		{Key: "on_hand", Label: "On Hand", Type: "number"},
		{Key: "expires_at", Label: "Expiry Date", Type: "date"},
		{Key: "days_to_expiry", Label: "Days To Expiry", Type: "number"},
	}
}

func serialColumns() []Column {
	return []Column{
		{Key: "serial_number", Label: "Serial Number", Type: "string"},
		{Key: "imei", Label: "IMEI", Type: "string"},
		{Key: "product_name", Label: "Product", Type: "string"},
		{Key: "variant_name", Label: "Variant", Type: "string"},
		{Key: "warehouse_name", Label: "Warehouse", Type: "string"},
		{Key: "status", Label: "Status", Type: "string"},
	}
}

func warehouseTransferColumns() []Column {
	return []Column{
		{Key: "recorded_at", Label: "Transfer Date", Type: "date"},
		{Key: "product_name", Label: "Product", Type: "string"},
		{Key: "variant_name", Label: "Variant", Type: "string"},
		{Key: "source_warehouse_name", Label: "From", Type: "string"},
		{Key: "warehouse_name", Label: "To", Type: "string"},
		{Key: "quantity", Label: "Quantity", Type: "number"},
		{Key: "unit_cost", Label: "Unit Cost", Type: "number"},
		{Key: "reason", Label: "Reason", Type: "string"},
	}
}

func pnlColumns() []Column {
	return []Column{
		{Key: "metric", Label: "Metric", Type: "string"},
		{Key: "amount", Label: "Amount", Type: "number"},
	}
}
