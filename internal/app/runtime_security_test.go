package app

import (
	"os"
	"strings"
	"testing"
)

func TestPaymentRoutesRequirePaymentPermissions(t *testing.T) {
	source, err := os.ReadFile("runtime.go")
	if err != nil {
		t.Fatalf("read runtime routes: %v", err)
	}
	routes := string(source)

	requiredFragments := []string{
		`payments.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsView), h.Payment.List)`,
		`payments.GET("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsView), h.Payment.Get)`,
		`payments.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsManage), wafUserWriteRL, h.Payment.Create)`,
		`payments.PUT("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsManage), wafUserWriteRL, h.Payment.Update)`,
		`payments.DELETE("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsManage), wafUserWriteRL, h.Payment.Delete)`,
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(routes, fragment) {
			t.Fatalf("payment route is missing required permission gate: %s", fragment)
		}
	}
}

func TestTeamRoleMutationRoutesRequireRoleManagement(t *testing.T) {
	source, err := os.ReadFile("runtime.go")
	if err != nil {
		t.Fatalf("read runtime routes: %v", err)
	}
	routes := string(source)

	requiredFragments := []string{
		`teams.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionTeamsManage), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionRolesManage), h.Team.Create)`,
		`teams.PUT("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionTeamsManage), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionRolesManage), h.Team.Update)`,
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(routes, fragment) {
			t.Fatalf("team mutation route is missing role-management gate: %s", fragment)
		}
	}
}

func TestBranchMutationRoutesExposeBranchScopeParam(t *testing.T) {
	source, err := os.ReadFile("runtime.go")
	if err != nil {
		t.Fatalf("read runtime routes: %v", err)
	}
	routes := string(source)

	requiredFragments := []string{
		`branches.PUT("/:branch_id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionBranchesManage), h.Commerce.UpdateBranch)`,
		`branches.DELETE("/:branch_id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionBranchesManage), h.Commerce.DeleteBranch)`,
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(routes, fragment) {
			t.Fatalf("branch mutation route must expose branch_id to BusinessAuth: %s", fragment)
		}
	}
}

func TestInvoiceAndDocumentRoutesRequireDocumentPermissions(t *testing.T) {
	source, err := os.ReadFile("runtime.go")
	if err != nil {
		t.Fatalf("read runtime routes: %v", err)
	}
	routes := string(source)

	requiredFragments := []string{
		`group.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), handler.List)`,
		`group.GET("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), handler.Get)`,
		`group.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), handler.Create)`,
		`group.PUT("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), handler.Update)`,
		`group.DELETE("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), handler.Delete)`,
		`group.POST("/:id/cancel", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), handler.Cancel)`,
		`group.GET("/:id/pdf", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), handler.GetPDF)`,
		`invoices.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.Invoice.List)`,
		`invoices.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserWriteRL, h.Invoice.Create)`,
		`invoices.POST("/:id/previews", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), wafUserHeavyRL, h.Invoice.Preview)`,
		`invoices.PATCH("/:id/draft", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserWriteRL, h.Invoice.UpdateDraft)`,
		`invoices.GET("/:id/renders/:render_job_id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.Invoice.GetRenderStatus)`,
		`invoices.GET("/:id/pdf", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.Invoice.GetPDF)`,
		`documents.POST("/merge", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserHeavyRL, h.DocumentUtility.Merge)`,
		`documents.GET("/:id/history", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.DocumentUtility.History)`,
		`documents.POST("/:id/einvoice", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserHeavyRL, h.DocumentUtility.GenerateEInvoice)`,
		`documents.GET("/:id/pdf", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsExport), h.DocumentUtility.GetPDF)`,
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(routes, fragment) {
			t.Fatalf("document route is missing required permission gate: %s", fragment)
		}
	}
}

func TestEveryInvoiceReadRouteRequiresDocumentExportPermission(t *testing.T) {
	source, err := os.ReadFile("runtime.go")
	if err != nil {
		t.Fatalf("read runtime routes: %v", err)
	}
	expected := map[string]bool{
		`invoices.GET("",`:                            false,
		`invoices.GET("/:id",`:                        false,
		`invoices.GET("/:id/renders/:render_job_id",`: false,
		`invoices.GET("/:id/pdf",`:                    false,
	}
	for _, line := range strings.Split(string(source), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "invoices.GET(") {
			continue
		}
		if !strings.Contains(line, "services.PermissionDocumentsExport") {
			t.Fatalf("invoice read route lacks PermissionDocumentsExport: %s", line)
		}
		matched := false
		for prefix := range expected {
			if strings.HasPrefix(line, prefix) {
				expected[prefix] = true
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("unclassified invoice read route: %s", line)
		}
	}
	for prefix, found := range expected {
		if !found {
			t.Fatalf("expected classified invoice read route is missing: %s", prefix)
		}
	}
}

func TestJournalTaxAndPOSRoutesRequireFineGrainedPermissions(t *testing.T) {
	source, err := os.ReadFile("runtime.go")
	if err != nil {
		t.Fatalf("read runtime routes: %v", err)
	}
	routes := string(source)

	requiredFragments := []string{
		`journals.POST("", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserWriteRL, h.Journal.Create)`,
		`journals.POST("/:id/post", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserWriteRL, h.Journal.Post)`,
		`journals.POST("/:id/reverse", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), wafUserWriteRL, h.Journal.Reverse)`,
		`tax.GET("/integrations", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionTaxIntegrationsManage), h.Tax.ListIntegrationAccounts)`,
		`tax.POST("/integrations/:id/validate", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionTaxIntegrationsManage), h.Tax.ValidateIntegrationAccount)`,
		`tax.POST("/gstr-2b/import", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionDocumentsManage), h.Tax.ImportGSTR2B)`,
		`tax.GET("/reports/:type", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Tax.GetReport)`,
		`tax.POST("/reports/:type/export", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsExport), h.Tax.ExportReport)`,
		`pos.POST("/sessions", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPOSOperate), h.POS.CreateSession)`,
		`pos.POST("/carts/:id/checkout", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPOSOperate), h.POS.Checkout)`,
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(routes, fragment) {
			t.Fatalf("sensitive route is missing required permission gate: %s", fragment)
		}
	}
}

func TestPOSServiceBindsSessionsToUserAndRechecksWarehouseAccess(t *testing.T) {
	source, err := os.ReadFile("../services/pos_service.go")
	if err != nil {
		t.Fatalf("read POS service: %v", err)
	}
	pos := string(source)

	requiredFragments := []string{
		`inventory *InventoryService`,
		`s.authorizeWarehouse(ctx, userID, businessID, posStringValue(session.WarehouseID), warehousePermissionMoveStock)`,
		`Where("id = ? AND business_id = ? AND user_id = ? AND deleted_at IS NULL", sessionID, businessID, userID)`,
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(pos, fragment) {
			t.Fatalf("POS service is missing session/warehouse authorization: %s", fragment)
		}
	}

	inventorySource, err := os.ReadFile("../services/inventory_domain_service.go")
	if err != nil {
		t.Fatalf("read inventory service: %v", err)
	}
	inventory := string(inventorySource)
	for _, fragment := range []string{`userHasWarehouseBranchAccess`, `filterWarehousesByBranchAccess`} {
		if !strings.Contains(inventory, fragment) {
			t.Fatalf("inventory service is missing branch-aware warehouse authorization: %s", fragment)
		}
	}
}

func TestScopedReportAndInventoryQueriesUseServerDerivedScope(t *testing.T) {
	reportHandler, err := os.ReadFile("../handlers/report_handler.go")
	if err != nil {
		t.Fatalf("read report handler: %v", err)
	}
	inventoryHandler, err := os.ReadFile("../handlers/inventory_handler.go")
	if err != nil {
		t.Fatalf("read inventory handler: %v", err)
	}
	reportingTypes, err := os.ReadFile("../reporting/types.go")
	if err != nil {
		t.Fatalf("read reporting types: %v", err)
	}
	reportingRepo, err := os.ReadFile("../repositories/postgres/reporting_repo.go")
	if err != nil {
		t.Fatalf("read reporting repository: %v", err)
	}

	requiredByFile := map[string]struct {
		source    string
		fragments []string
	}{
		"report handler": {
			source: string(reportHandler),
			fragments: []string{
				`middleware.GetValidatedBranchScope(c)`,
				`h.inventory.ReportWarehouseScope`,
				`applyReportScope`,
			},
		},
		"inventory handler": {
			source: string(inventoryHandler),
			fragments: []string{
				`resolveReportWarehouseScope`,
				`filter.WarehouseIDs`,
				`warehouse_id is required`,
			},
		},
		"reporting types": {
			source: string(reportingTypes),
			fragments: []string{
				`AllowedBranchIDs`,
				`AllowedWarehouseIDs`,
			},
		},
		"reporting repository": {
			source: string(reportingRepo),
			fragments: []string{
				`appendDocumentScope`,
				`appendWarehouseScope`,
			},
		},
	}
	for name, check := range requiredByFile {
		for _, fragment := range check.fragments {
			if !strings.Contains(check.source, fragment) {
				t.Fatalf("%s is missing server-derived scope enforcement: %s", name, fragment)
			}
		}
	}
}

func TestBusinessWideDashboardAndLedgerRequireAllBranchAccess(t *testing.T) {
	source, err := os.ReadFile("runtime.go")
	if err != nil {
		t.Fatalf("read runtime routes: %v", err)
	}
	routes := string(source)

	requiredFragments := []string{
		`dashboard.GET("/summary", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Dashboard.Summary)`,
		`ledger.GET("", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Ledger.List)`,
		`ledger.GET("/balance", middleware.RequireAllBranches(), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Ledger.Balance)`,
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(routes, fragment) {
			t.Fatalf("business-wide route is missing all-branch report authorization: %s", fragment)
		}
	}
}

func TestInventoryRoutesRequireInventoryAndReportPermissions(t *testing.T) {
	source, err := os.ReadFile("runtime.go")
	if err != nil {
		t.Fatalf("read runtime routes: %v", err)
	}
	routes := string(source)
	requiredFragments := []string{
		`warehouses.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsView), h.Inventory.ListWarehouses)`,
		`warehouses.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), h.Inventory.CreateWarehouse)`,
		`inventory.POST("/adjustments", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionProductsManage), h.Inventory.CreateAdjustment)`,
		`inventory.GET("/timeline", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Inventory.Timeline)`,
		`inventory.GET("/valuation", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Inventory.Valuation)`,
		`inventory.GET("/batches", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionReportsView), h.Inventory.ListBatches)`,
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(routes, fragment) {
			t.Fatalf("inventory route is missing required permission gate: %s", fragment)
		}
	}
}

func TestTerraformAddsGatewayAuthLoggingAndLeastPrivilegeBoundaries(t *testing.T) {
	apiGateway, err := os.ReadFile("../../infrastructure/terraform/api_gateway_rest.tf")
	if err != nil {
		t.Fatalf("read REST API Gateway terraform: %v", err)
	}
	iam, err := os.ReadFile("../../infrastructure/terraform/iam.tf")
	if err != nil {
		t.Fatalf("read IAM terraform: %v", err)
	}
	lambda, err := os.ReadFile("../../infrastructure/terraform/lambda.tf")
	if err != nil {
		t.Fatalf("read Lambda terraform: %v", err)
	}

	requiredByFile := map[string]struct {
		source    string
		fragments []string
	}{
		"api gateway": {
			source: string(apiGateway),
			fragments: []string{
				`resource "aws_api_gateway_authorizer" "cognito"`,
				`authorization = "COGNITO_USER_POOLS"`,
				`public_auth_direct_paths`,
				`resource "aws_api_gateway_method" "api_v1_auth_public_post"`,
				`resource "aws_api_gateway_method" "api_v1_auth_phone_logout_post"`,
				`resource "aws_api_gateway_method" "api_v1_auth_google_options"`,
				`access_log_settings`,
				`throttling_burst_limit`,
				`throttling_rate_limit`,
			},
		},
		"iam": {
			source: string(iam),
			fragments: []string{
				`resources = ["${aws_apigatewayv2_api.websocket.execution_arn}/${var.environment}/POST/@connections/*"]`,
				`resource "aws_iam_role" "lambda_worker_exec"`,
				`resource "aws_iam_role" "lambda_websocket_exec"`,
			},
		},
		"lambda": {
			source: string(lambda),
			fragments: []string{
				`role             = aws_iam_role.lambda_worker_exec["invoice"].arn`,
				`role             = aws_iam_role.lambda_websocket_exec.arn`,
				`DATABASE_SECRET_ARN`,
				`CREDENTIAL_ENCRYPTION_SECRET_ARN`,
				`RAZORPAY_SECRET_ARN`,
				`LLM_SECRET_ARN`,
				`EXA_SECRET_ARN`,
				`GST_LOOKUP_SECRET_ARN`,
				`GST_PROVIDER_SECRET_ARN`,
				`DEEPGRAM_SECRET_ARN`,
				`DEEPSEEK_SECRET_ARN`,
			},
		},
	}
	for name, check := range requiredByFile {
		for _, fragment := range check.fragments {
			if !strings.Contains(check.source, fragment) {
				t.Fatalf("%s is missing infrastructure security control: %s", name, fragment)
			}
		}
	}
	for _, forbidden := range []string{
		`CREDENTIAL_ENCRYPTION_KEY = var.credential_encryption_key`,
		`LLM_API_KEY               = var.llm_api_key`,
		`EXA_API_KEY               = var.exa_api_key`,
		`GST_LOOKUP_API_KEY        = var.gst_lookup_api_key`,
		`DEEPGRAM_API_KEY          = var.deepgram_api_key`,
		`DEEPSEEK_API_KEY          = var.deepseek_api_key`,
	} {
		if strings.Contains(string(lambda), forbidden) {
			t.Fatalf("lambda environment still contains a direct provider secret: %s", forbidden)
		}
	}
}
