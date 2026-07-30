package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/invoiceissue"
	"invoice-backend/internal/invoiceprojection"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestEnsureEmptyDisposableDatabaseRejectsNonEmptyDatabase(t *testing.T) {
	repository, mock, closeDatabase := newAtomicSQLMockRepository(t)
	defer closeDatabase()
	mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	err := ensureEmptyDisposableDatabase(repository.db)

	if err == nil || !strings.Contains(err.Error(), "empty disposable PostgreSQL database") {
		t.Fatalf("guard error = %v, want empty disposable database error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestInvoiceRepositoryCreateDraftAtomicPostgresConcurrencyAndRollback(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not configured; skipping atomic invoice PostgreSQL integration")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("MIGRATION_TEST_DATABASE_URL must be a PostgreSQL URL")
	}

	admin, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open disposable PostgreSQL database: %v", err)
	}
	if err := ensureEmptyDisposableDatabase(admin); err != nil {
		t.Fatal(err)
	}
	schema := "invoice_atomic_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schema)).Error; err != nil {
		t.Fatalf("create isolated integration schema: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schema)).Error; err != nil {
			t.Errorf("drop isolated integration schema: %v", err)
		}
	})

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	database, err := gorm.Open(gormpostgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open isolated PostgreSQL schema: %v", err)
	}
	if err := database.Exec(`
		CREATE TABLE business_profiles (
			id UUID PRIMARY KEY,
			timezone VARCHAR(64) NOT NULL DEFAULT 'Asia/Kolkata',
			deleted_at TIMESTAMPTZ
		);
		CREATE TABLE customers (
			id UUID PRIMARY KEY,
			business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE
		)
	`).Error; err != nil {
		t.Fatalf("create isolated tenant parents: %v", err)
	}
	if err := database.AutoMigrate(
		&models.APIIdempotencyKey{},
		&models.DocumentSequence{},
		&models.RenderProfile{},
		&models.Invoice{},
		&models.InvoiceItem{},
		&models.Document{},
		&models.DocumentLine{},
		&models.DocumentRevision{},
		&models.ActivityLog{},
		&models.OutboxEvent{},
		&models.DocumentRenderJob{},
		&models.EmailDelivery{},
	); err != nil {
		t.Fatalf("create isolated atomic repository schema: %v", err)
	}
	for _, statement := range []string{
		`ALTER TABLE api_idempotency_keys ALTER COLUMN business_id TYPE UUID USING business_id::uuid`,
		`ALTER TABLE document_sequences ALTER COLUMN business_id TYPE UUID USING business_id::uuid`,
		`ALTER TABLE render_profiles ALTER COLUMN business_id TYPE UUID USING business_id::uuid`,
		`ALTER TABLE invoices ALTER COLUMN business_id TYPE UUID USING business_id::uuid, ALTER COLUMN customer_id TYPE UUID USING customer_id::uuid`,
		`ALTER TABLE documents ALTER COLUMN business_id TYPE UUID USING business_id::uuid`,
		`ALTER TABLE document_revisions ALTER COLUMN business_id TYPE UUID USING business_id::uuid, ALTER COLUMN document_id TYPE UUID USING document_id::uuid`,
		`ALTER TABLE activity_logs ALTER COLUMN business_id TYPE UUID USING business_id::uuid`,
		`ALTER TABLE outbox_events ALTER COLUMN business_id TYPE UUID USING business_id::uuid`,
		`ALTER TABLE document_render_jobs ALTER COLUMN business_id TYPE UUID USING business_id::uuid, ALTER COLUMN document_id TYPE UUID USING document_id::uuid, ALTER COLUMN invoice_id TYPE UUID USING invoice_id::uuid`,
		`ALTER TABLE email_deliveries ALTER COLUMN business_id TYPE UUID USING business_id::uuid, ALTER COLUMN invoice_id TYPE UUID USING invoice_id::uuid, ALTER COLUMN render_job_id TYPE UUID USING render_job_id::uuid`,
	} {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatalf("align isolated production UUID type: %v", err)
		}
	}
	for _, statement := range []string{
		`ALTER TABLE api_idempotency_keys ADD CONSTRAINT fk_atomic_idempotency_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE document_sequences ADD CONSTRAINT fk_atomic_sequence_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE render_profiles ADD CONSTRAINT fk_atomic_render_profile_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE invoices ADD CONSTRAINT fk_atomic_invoice_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE invoices ADD CONSTRAINT fk_atomic_invoice_customer FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE RESTRICT`,
		`ALTER TABLE documents ADD CONSTRAINT fk_atomic_document_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE document_revisions ADD CONSTRAINT fk_atomic_revision_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE document_revisions ADD CONSTRAINT fk_atomic_revision_document FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE CASCADE`,
		`ALTER TABLE activity_logs ADD CONSTRAINT fk_atomic_activity_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE outbox_events ADD CONSTRAINT fk_atomic_outbox_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE document_render_jobs ADD CONSTRAINT fk_atomic_render_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE document_render_jobs ADD CONSTRAINT fk_atomic_render_document FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE CASCADE`,
		`ALTER TABLE document_render_jobs ADD CONSTRAINT fk_atomic_render_invoice FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE`,
		`ALTER TABLE email_deliveries ADD CONSTRAINT fk_atomic_email_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE email_deliveries ADD CONSTRAINT fk_atomic_email_invoice FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE`,
		`ALTER TABLE email_deliveries ADD CONSTRAINT fk_atomic_email_render FOREIGN KEY (render_job_id) REFERENCES document_render_jobs(id) ON DELETE SET NULL`,
	} {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatalf("create isolated production FK: %v", err)
		}
	}

	repository := &invoiceRepository{db: database}
	firstCommand := atomicRepositoryTestCommand()
	secondCommand := atomicRepositoryTestCommand()
	firstCommand.Invoice.SellerSnapshot = models.PartySnapshot{Name: "Seller"}
	firstCommand.Invoice.BuyerSnapshot = models.PartySnapshot{Name: "Buyer"}
	secondCommand.Invoice.SellerSnapshot = models.PartySnapshot{Name: "Seller"}
	secondCommand.Invoice.BuyerSnapshot = models.PartySnapshot{Name: "Buyer"}
	copyAtomicScope(&secondCommand, firstCommand)
	firstCommand.Document = invoiceprojection.Build(firstCommand.Invoice)
	secondCommand.Document = invoiceprojection.Build(secondCommand.Invoice)
	if err := database.Exec(`INSERT INTO business_profiles (id) VALUES (?)`, firstCommand.BusinessID).Error; err != nil {
		t.Fatalf("seed isolated business: %v", err)
	}
	customerIDs := []string{models.StringValue(firstCommand.Invoice.CustomerID), models.StringValue(secondCommand.Invoice.CustomerID)}
	for _, customerID := range customerIDs {
		if err := database.Exec(`INSERT INTO customers (id, business_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			customerID, firstCommand.BusinessID).Error; err != nil {
			t.Fatalf("seed isolated customer: %v", err)
		}
	}

	var group sync.WaitGroup
	results := make(chan string, 2)
	errs := make(chan error, 2)
	for _, command := range []interfaces.AtomicInvoiceDraft{firstCommand, secondCommand} {
		command := command
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := repository.CreateDraftAtomic(context.Background(), command)
			if result != nil && result.Invoice != nil {
				results <- result.Invoice.ID
			}
			errs <- err
		}()
	}
	group.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent atomic create: %v", err)
		}
	}
	var resultID string
	for id := range results {
		if resultID == "" {
			resultID = id
		}
		if id != resultID {
			t.Fatalf("concurrent result ID = %s, want %s", id, resultID)
		}
	}
	assertAtomicTableCount(t, database, "invoices", 1)
	assertAtomicTableCount(t, database, "invoice_items", 1)
	assertAtomicTableCount(t, database, "documents", 1)
	assertAtomicTableCount(t, database, "document_lines", 1)
	assertAtomicTableCount(t, database, "activity_logs", 1)
	assertAtomicTableCount(t, database, "api_idempotency_keys", 1)
	assertAtomicTableCount(t, database, "outbox_events", 1)
	assertAtomicTableCount(t, database, "document_render_jobs", 1)
	assertAtomicTableCount(t, database, "email_deliveries", 1)

	conflict := atomicRepositoryTestCommand()
	copyAtomicScope(&conflict, firstCommand)
	conflict.RequestHash = strings.Repeat("b", 64)
	if _, err := repository.CreateDraftAtomic(context.Background(), conflict); err == nil {
		t.Fatal("changed request hash unexpectedly succeeded")
	} else {
		var typedConflict *idempotency.ConflictError
		if !errors.As(err, &typedConflict) {
			t.Fatalf("changed request error = %T %v, want conflict", err, err)
		}
	}

	rollbackCommand := atomicRepositoryTestCommand()
	rollbackCommand.Activity = nil
	if _, err := repository.CreateDraftAtomic(context.Background(), rollbackCommand); err == nil {
		t.Fatal("invalid atomic command unexpectedly succeeded")
	}
	var rolledBackClaims int64
	if err := database.Model(&models.APIIdempotencyKey{}).
		Where("business_id = ? AND command = ? AND idempotency_key = ?",
			rollbackCommand.BusinessID, rollbackCommand.Command, rollbackCommand.IdempotencyKey).
		Count(&rolledBackClaims).Error; err != nil {
		t.Fatalf("count rolled back idempotency claim: %v", err)
	}
	if rolledBackClaims != 0 {
		t.Fatalf("rolled back claim count = %d, want 0", rolledBackClaims)
	}
	rollbackCommand.Activity = atomicRepositoryTestCommand().Activity
	rollbackCommand.Activity.BusinessID = rollbackCommand.BusinessID
	rollbackCommand.Activity.EntityID = rollbackCommand.Invoice.ID
	if err := database.Exec(`INSERT INTO business_profiles (id) VALUES (?)`, rollbackCommand.BusinessID).Error; err != nil {
		t.Fatalf("seed rollback business: %v", err)
	}
	if err := database.Exec(`INSERT INTO customers (id, business_id) VALUES (?, ?)`,
		models.StringValue(rollbackCommand.Invoice.CustomerID), rollbackCommand.BusinessID).Error; err != nil {
		t.Fatalf("seed rollback customer: %v", err)
	}
	if _, err := repository.CreateDraftAtomic(context.Background(), rollbackCommand); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}

	issueCommand := issueRepositoryTestCommand()
	issueCommand.BusinessID = firstCommand.BusinessID
	issueCommand.InvoiceID = resultID
	var issueGroup sync.WaitGroup
	issueResults := make(chan *interfaces.AtomicInvoiceIssueResult, 2)
	issueErrors := make(chan error, 2)
	for attempt := 0; attempt < 2; attempt++ {
		issueGroup.Add(1)
		go func() {
			defer issueGroup.Done()
			result, err := repository.IssueDraftAtomic(context.Background(), issueCommand)
			issueResults <- result
			issueErrors <- err
		}()
	}
	issueGroup.Wait()
	close(issueResults)
	close(issueErrors)
	for err := range issueErrors {
		if err != nil {
			t.Fatalf("concurrent atomic issue: %v", err)
		}
	}
	var issuedNumber string
	var finalRenderID string
	for result := range issueResults {
		if result == nil || result.Invoice == nil || result.FinalRender == nil || result.Invoice.InvoiceNo == nil {
			t.Fatalf("concurrent issue result = %#v", result)
		}
		if issuedNumber == "" {
			issuedNumber = *result.Invoice.InvoiceNo
			finalRenderID = result.FinalRender.ID
		}
		if *result.Invoice.InvoiceNo != issuedNumber || result.FinalRender.ID != finalRenderID {
			t.Fatalf("concurrent issue identities differ: %#v", result)
		}
	}
	if issuedNumber != "INV/26-27/000001" {
		t.Fatalf("issued number = %q, want INV/26-27/000001", issuedNumber)
	}
	assertAtomicTableCount(t, database, "document_sequences", 1)
	var finalRenderCount int64
	if err := database.Model(&models.DocumentRenderJob{}).
		Where("invoice_id = ? AND kind = ?", resultID, models.RenderKindFinal).
		Count(&finalRenderCount).Error; err != nil {
		t.Fatalf("count final renders: %v", err)
	}
	if finalRenderCount != 1 {
		t.Fatalf("final render count = %d, want 1", finalRenderCount)
	}
	var issuedEventCount int64
	if err := database.Model(&models.OutboxEvent{}).
		Where("business_id = ? AND aggregate_id = ? AND event_type = ?",
			firstCommand.BusinessID, resultID, "invoice.issued.v1").
		Count(&issuedEventCount).Error; err != nil {
		t.Fatalf("count issue outbox events: %v", err)
	}
	if issuedEventCount != 1 {
		t.Fatalf("issue outbox count = %d, want 1", issuedEventCount)
	}
	var issuedActivityCount int64
	if err := database.Model(&models.ActivityLog{}).
		Where("business_id = ? AND entity_id = ? AND action = ?",
			firstCommand.BusinessID, resultID, "issued").
		Count(&issuedActivityCount).Error; err != nil {
		t.Fatalf("count issue activities: %v", err)
	}
	if issuedActivityCount != 1 {
		t.Fatalf("issue activity count = %d, want 1", issuedActivityCount)
	}

	deliveryCommand := interfaces.AtomicInvoiceDelivery{
		BusinessID: firstCommand.BusinessID, InvoiceID: resultID,
		Command: "invoice.delivery.create.v1", IdempotencyKey: uuid.NewString(),
		RequestHash: strings.Repeat("d", 64), Recipient: "buyer@example.com",
		ActorID: uuid.NewString(), ActorRole: "accountant",
	}
	renderRepository := &documentRepository{db: database}
	claimNow := time.Now().UTC()
	claimState, err := renderRepository.ClaimFinalRender(
		context.Background(), firstCommand.BusinessID, finalRenderID, 2,
		"integration-render-owner", claimNow, claimNow.Add(2*time.Minute),
	)
	if err != nil || claimState != interfaces.FinalRenderClaimed {
		t.Fatalf("claim final render before delivery race = %q/%v", claimState, err)
	}
	deliveryResults := make(chan *interfaces.AtomicInvoiceDeliveryResult, 1)
	deliveryRaceErrors := make(chan error, 2)
	var deliveryRace sync.WaitGroup
	deliveryRace.Add(2)
	go func() {
		defer deliveryRace.Done()
		result, createErr := repository.CreateDeliveryAtomic(context.Background(), deliveryCommand)
		deliveryResults <- result
		deliveryRaceErrors <- createErr
	}()
	go func() {
		defer deliveryRace.Done()
		_, completeErr := renderRepository.CompleteFinalRender(
			context.Background(), firstCommand.BusinessID, resultID, finalRenderID, 2,
			"integration-render-owner",
			"invoices/"+firstCommand.BusinessID+"/"+resultID+"/v2/final.pdf",
			"invoice-final.pdf",
		)
		deliveryRaceErrors <- completeErr
	}()
	deliveryRace.Wait()
	close(deliveryResults)
	close(deliveryRaceErrors)
	for raceErr := range deliveryRaceErrors {
		if raceErr != nil {
			t.Fatalf("delivery versus final completion race: %v", raceErr)
		}
	}
	deliveryResult := <-deliveryResults
	if deliveryResult == nil || deliveryResult.Delivery == nil {
		t.Fatalf("delivery race result = %#v", deliveryResult)
	}
	var persistedDelivery models.EmailDelivery
	if err := database.Where("id = ?", deliveryResult.Delivery.ID).First(&persistedDelivery).Error; err != nil {
		t.Fatalf("load raced delivery: %v", err)
	}
	if persistedDelivery.Status != models.EmailDeliveryStatusQueued {
		t.Fatalf("raced delivery status = %q, want queued", persistedDelivery.Status)
	}
	var deliveryEventCount int64
	if err := database.Model(&models.OutboxEvent{}).
		Where("aggregate_type = ? AND aggregate_id = ? AND event_type = ?",
			"email_delivery", persistedDelivery.ID, invoiceDeliveryRequestedEvent).
		Count(&deliveryEventCount).Error; err != nil {
		t.Fatalf("count raced delivery events: %v", err)
	}
	if deliveryEventCount != 1 {
		t.Fatalf("raced delivery outbox count = %d, want 1", deliveryEventCount)
	}

	var persistedInvoice models.Invoice
	if err := database.Where("id = ? AND business_id = ?", resultID, firstCommand.BusinessID).
		First(&persistedInvoice).Error; err != nil {
		t.Fatalf("load issued invoice: %v", err)
	}
	if persistedInvoice.InvoiceNo == nil || *persistedInvoice.InvoiceNo != issuedNumber ||
		persistedInvoice.Status != models.InvoiceStatusIssued || persistedInvoice.Version != 2 ||
		persistedInvoice.IssuedAt == nil || persistedInvoice.SellerSnapshot.Name != "Seller" ||
		persistedInvoice.BuyerSnapshot.Name != "Buyer" {
		t.Fatalf("persisted legal invoice = %#v", persistedInvoice)
	}
	var persistedDocument models.Document
	if err := database.Where("id = ? AND business_id = ?", resultID, firstCommand.BusinessID).
		First(&persistedDocument).Error; err != nil {
		t.Fatalf("load issued document: %v", err)
	}
	var sourceLinkage map[string]interface{}
	if err := json.Unmarshal([]byte(persistedDocument.SourceLinkage), &sourceLinkage); err != nil {
		t.Fatalf("decode persisted legal source linkage: %v", err)
	}
	if persistedDocument.SerialNumber != issuedNumber ||
		persistedDocument.Status != models.DocumentStatusIssued ||
		persistedDocument.DraftState != models.DocumentDraftStateFinal ||
		sourceLinkage["source_invoice_version"] != float64(2) ||
		sourceLinkage["seller_snapshot"] == nil ||
		sourceLinkage["buyer_snapshot"] == nil {
		t.Fatalf("persisted legal document projection = %#v", persistedDocument)
	}
	differentKey := issueCommand
	differentKey.IdempotencyKey = uuid.NewString()
	if _, err := repository.IssueDraftAtomic(context.Background(), differentKey); err == nil {
		t.Fatal("different-key duplicate issue succeeded")
	} else {
		var alreadyIssued *invoiceissue.AlreadyIssuedError
		if !errors.As(err, &alreadyIssued) {
			t.Fatalf("different-key duplicate error = %T %v, want already-issued", err, err)
		}
	}

	secondBusinessID := uuid.NewString()
	if err := database.Exec(`INSERT INTO business_profiles (id) VALUES (?)`, secondBusinessID).Error; err != nil {
		t.Fatalf("seed second allocation business: %v", err)
	}
	type allocationScope struct {
		name         string
		businessID   string
		documentType string
		series       string
		invoiceDate  time.Time
		billOfSupply bool
	}
	scopes := []allocationScope{
		{name: "tenant-one-tax", businessID: firstCommand.BusinessID, documentType: invoiceissue.DocumentTypeTaxInvoice, series: "TAX", invoiceDate: time.Date(2026, 4, 1, 0, 30, 0, 0, time.UTC)},
		{name: "tenant-one-bill", businessID: firstCommand.BusinessID, documentType: invoiceissue.DocumentTypeBillOfSupply, series: "BOS", invoiceDate: time.Date(2026, 4, 1, 0, 30, 0, 0, time.UTC), billOfSupply: true},
		{name: "before-fy-boundary", businessID: firstCommand.BusinessID, documentType: invoiceissue.DocumentTypeTaxInvoice, series: "FYR", invoiceDate: time.Date(2026, 3, 31, 18, 29, 59, 0, time.UTC)},
		{name: "after-fy-boundary", businessID: firstCommand.BusinessID, documentType: invoiceissue.DocumentTypeTaxInvoice, series: "FYR", invoiceDate: time.Date(2026, 3, 31, 18, 30, 0, 0, time.UTC)},
		{name: "second-tenant", businessID: secondBusinessID, documentType: invoiceissue.DocumentTypeTaxInvoice, series: "TAX", invoiceDate: time.Date(2026, 4, 1, 0, 30, 0, 0, time.UTC)},
	}
	type allocationWork struct {
		scope   allocationScope
		command interfaces.AtomicInvoiceIssue
	}
	work := make([]allocationWork, 0, 100)
	for _, scope := range scopes {
		for index := 0; index < 20; index++ {
			invoiceID := uuid.NewString()
			invoice := &models.Invoice{
				ID: invoiceID, BusinessID: scope.businessID, Version: 1, Status: models.InvoiceStatusDraft,
				Origin: models.InvoiceOriginManual, SellerSnapshot: models.PartySnapshot{Name: "Seller"},
				BuyerSnapshot: models.PartySnapshot{Name: "Buyer"}, InvoiceDate: scope.invoiceDate,
				DueDate: scope.invoiceDate.AddDate(0, 0, 30), Currency: "INR", Total: 100, BalanceDue: 100,
			}
			if scope.billOfSupply {
				invoice.TaxProfile = `{"bill_of_supply":true}`
			}
			if err := database.Omit("Items").Create(invoice).Error; err != nil {
				t.Fatalf("seed allocation invoice: %v", err)
			}
			document := invoiceprojection.Build(invoice)
			if err := database.Omit("Lines").Create(document).Error; err != nil {
				t.Fatalf("seed allocation document: %v", err)
			}
			command := issueRepositoryTestCommand()
			command.BusinessID = scope.businessID
			command.InvoiceID = invoiceID
			command.DocumentType = scope.documentType
			command.Series = scope.series
			work = append(work, allocationWork{scope: scope, command: command})
		}
	}
	type allocationResult struct {
		scope  string
		number int
		err    error
	}
	allocationResults := make(chan allocationResult, len(work))
	var allocationGroup sync.WaitGroup
	for _, item := range work {
		item := item
		allocationGroup.Add(1)
		go func() {
			defer allocationGroup.Done()
			result, err := repository.IssueDraftAtomic(context.Background(), item.command)
			if err != nil {
				allocationResults <- allocationResult{scope: item.scope.name, err: err}
				return
			}
			serial := models.StringValue(result.Invoice.InvoiceNo)
			lastSlash := strings.LastIndex(serial, "/")
			number, parseErr := strconv.Atoi(serial[lastSlash+1:])
			allocationResults <- allocationResult{scope: item.scope.name, number: number, err: parseErr}
		}()
	}
	allocationGroup.Wait()
	close(allocationResults)
	numbersByScope := make(map[string][]int)
	for result := range allocationResults {
		if result.err != nil {
			t.Fatalf("100-way allocation for %s: %v", result.scope, result.err)
		}
		numbersByScope[result.scope] = append(numbersByScope[result.scope], result.number)
	}
	for _, scope := range scopes {
		numbers := numbersByScope[scope.name]
		sort.Ints(numbers)
		if len(numbers) != 20 {
			t.Fatalf("%s allocation count = %d, want 20", scope.name, len(numbers))
		}
		for index, number := range numbers {
			if number != index+1 {
				t.Fatalf("%s allocation[%d] = %d, want %d", scope.name, index, number, index+1)
			}
		}
	}
}

func copyAtomicScope(target *interfaces.AtomicInvoiceDraft, source interfaces.AtomicInvoiceDraft) {
	target.BusinessID = source.BusinessID
	target.Command = source.Command
	target.IdempotencyKey = source.IdempotencyKey
	target.RequestHash = source.RequestHash
	target.Invoice.BusinessID = source.BusinessID
	target.Document.BusinessID = source.BusinessID
	target.Activity.BusinessID = source.BusinessID
	for _, event := range target.OutboxEvents {
		event.BusinessID = source.BusinessID
	}
	for _, job := range target.RenderJobs {
		job.BusinessID = source.BusinessID
	}
	for _, delivery := range target.EmailDeliveries {
		delivery.BusinessID = source.BusinessID
	}
}

func ensureEmptyDisposableDatabase(database *gorm.DB) error {
	var tableCount int64
	if err := database.Raw(`
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
	`).Scan(&tableCount).Error; err != nil {
		return fmt.Errorf("inspect disposable PostgreSQL database: %w", err)
	}
	if tableCount != 0 {
		return fmt.Errorf("MIGRATION_TEST_DATABASE_URL must point to an empty disposable PostgreSQL database")
	}
	return nil
}

func assertAtomicTableCount(t *testing.T, database *gorm.DB, table string, want int64) {
	t.Helper()
	var got int64
	if err := database.Table(table).Count(&got).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}
