package services

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/reporting"
	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

type reportingRepoStub struct {
	queryResult      *reporting.Result
	dashboardResult  map[string]interface{}
	preference       *models.ReportPreference
	createdRuns      []*models.ReportRun
	createdShares    []*models.ReportShare
	sharesByToken    map[string]*models.ReportShare
	runsByID         map[string]*models.ReportRun
	accessLogs       []*models.ReportShareAccessLog
	touchedShareID   string
	queryCalls       int
	lastQueriedDef   reporting.Definition
	lastQueriedQuery reporting.Query
}

func (s *reportingRepoStub) QueryReport(_ context.Context, def reporting.Definition, query reporting.Query) (*reporting.Result, error) {
	s.queryCalls++
	s.lastQueriedDef = def
	s.lastQueriedQuery = query
	if s.queryResult == nil {
		return nil, fmt.Errorf("query result not configured")
	}
	return cloneReportResult(s.queryResult), nil
}

func (s *reportingRepoStub) GetDashboard(_ context.Context, query reporting.Query) (map[string]interface{}, error) {
	s.lastQueriedQuery = query
	return s.dashboardResult, nil
}

func (s *reportingRepoStub) CreateReportRun(_ context.Context, run *models.ReportRun) error {
	if run.ID == "" {
		run.ID = fmt.Sprintf("run-%d", len(s.createdRuns)+1)
	}
	s.createdRuns = append(s.createdRuns, run)
	if s.runsByID == nil {
		s.runsByID = map[string]*models.ReportRun{}
	}
	s.runsByID[run.ID] = run
	return nil
}

func (s *reportingRepoStub) GetReportRun(_ context.Context, _, id string) (*models.ReportRun, error) {
	if run, ok := s.runsByID[id]; ok {
		return run, nil
	}
	return nil, fmt.Errorf("report run not found")
}

func (s *reportingRepoStub) UpsertReportPreference(_ context.Context, pref *models.ReportPreference) error {
	s.preference = pref
	return nil
}

func (s *reportingRepoStub) GetReportPreference(_ context.Context, _, _, _ string) (*models.ReportPreference, error) {
	if s.preference == nil {
		return nil, fmt.Errorf("report preference not found")
	}
	return s.preference, nil
}

func (s *reportingRepoStub) CreateReportShare(_ context.Context, share *models.ReportShare) error {
	if share.ID == "" {
		share.ID = fmt.Sprintf("share-%d", len(s.createdShares)+1)
	}
	s.createdShares = append(s.createdShares, share)
	if s.sharesByToken == nil {
		s.sharesByToken = map[string]*models.ReportShare{}
	}
	s.sharesByToken[share.TokenHash] = share
	return nil
}

func (s *reportingRepoStub) ListReportShares(_ context.Context, _ string, _, _ int) ([]*models.ReportShare, int64, error) {
	return s.createdShares, int64(len(s.createdShares)), nil
}

func (s *reportingRepoStub) GetReportShareByTokenHash(_ context.Context, tokenHash string) (*models.ReportShare, error) {
	if share, ok := s.sharesByToken[tokenHash]; ok {
		return share, nil
	}
	return nil, fmt.Errorf("report share not found")
}

func (s *reportingRepoStub) TouchReportShareAccess(_ context.Context, shareID string, _ time.Time) error {
	s.touchedShareID = shareID
	return nil
}

func (s *reportingRepoStub) CreateReportShareAccessLog(_ context.Context, accessLog *models.ReportShareAccessLog) error {
	s.accessLogs = append(s.accessLogs, accessLog)
	return nil
}

func TestReportServiceQueryAppliesSavedColumnsAndBuildsClipboard(t *testing.T) {
	repo := &reportingRepoStub{
		queryResult: &reporting.Result{
			Columns: reporting.LookupOrPanic("sales_register").DefaultColumns,
			Rows: []map[string]interface{}{
				{
					"issue_date":    "2026-03-29",
					"serial_number": "INV-001",
					"party_name":    "Acme",
					"total":         125.5,
				},
			},
			Totals: map[string]interface{}{"total": 125.5},
			Pagination: reporting.Pagination{
				Page:  1,
				Limit: 50,
				Total: 1,
			},
		},
		preference: &models.ReportPreference{
			Config: `{"columns":["serial_number","total"]}`,
		},
	}
	svc := NewReportService(&config.Config{}, repo, logger.New())

	result, err := svc.Query(context.Background(), "biz-1", "user-1", "sales_register", ReportQueryInput{})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(result.Columns) != 2 || result.Columns[0].Key != "serial_number" || result.Columns[1].Key != "total" {
		t.Fatalf("unexpected columns: %#v", result.Columns)
	}
	if got := strings.TrimSpace(result.ClipboardTSV); got != "Number\tTotal\nINV-001\t125.5" {
		t.Fatalf("unexpected clipboard TSV: %q", got)
	}
}

func TestEncodeCSVNeutralizesSpreadsheetFormulas(t *testing.T) {
	csv, err := encodeCSV(&reporting.Result{
		Columns: []reporting.Column{{Key: "party_name", Label: "=Label"}},
		Rows: []map[string]interface{}{
			{"party_name": "=HYPERLINK(\"https://attacker.example\")"},
			{"party_name": "+SUM(1,2)"},
			{"party_name": "-10"},
			{"party_name": "@cmd"},
			{"party_name": "\t=1+1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"'=Label", "'=HYPERLINK", "\"'+SUM(1,2)\"", "'-10", "'@cmd", "'\t=1+1"} {
		if !strings.Contains(csv, want) {
			t.Fatalf("expected CSV to contain neutralized cell %q, got %q", want, csv)
		}
	}
}

func TestReportServiceExportRejectsUnavailableCapabilityBeforeQueryOrRun(t *testing.T) {
	repo := &reportingRepoStub{}
	guard := &recordingCapabilityGuard{err: &CapabilityUnavailableError{
		Code: "capability_unavailable", Capability: CapabilityReportExports,
		State: CapabilityStateUpgradeRequired, ReasonCode: ReasonEntitlementRequired,
	}}
	svc := NewReportService(&config.Config{}, repo, logger.New()).WithCapabilityGuard(guard)

	_, err := svc.Export(context.Background(), "biz-1", "user-1", "sales_register", ReportExportInput{})

	var unavailable *CapabilityUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Zero(t, repo.queryCalls)
	require.Empty(t, repo.createdRuns)
	require.Equal(t, CapabilityRequest{
		BusinessID: "biz-1", UserID: "user-1", Platform: CapabilityPlatformWeb, Capability: CapabilityReportExports,
	}, guard.request)
}

type recordingCapabilityGuard struct {
	request CapabilityRequest
	err     error
}

func (g *recordingCapabilityGuard) Require(_ context.Context, request CapabilityRequest) error {
	g.request = request
	return g.err
}

func TestReportServiceCreateSnapshotShareCreatesRunAndHashesSecrets(t *testing.T) {
	repo := &reportingRepoStub{
		queryResult: &reporting.Result{
			Columns: reporting.LookupOrPanic("sales_register").DefaultColumns,
			Rows: []map[string]interface{}{
				{"serial_number": "INV-001", "total": 125.5},
			},
			Totals: map[string]interface{}{"total": 125.5},
			Pagination: reporting.Pagination{
				Page:  1,
				Limit: 50,
				Total: 1,
			},
		},
	}
	cfg := &config.Config{Server: config.ServerConfig{BaseURL: "https://api.example.com"}}
	svc := NewReportService(cfg, repo, logger.New())

	resp, err := svc.CreateShare(context.Background(), "biz-1", "user-1", "sales_register", ReportShareInput{
		Mode:     models.ReportShareModeSnapshot,
		Passcode: "1234",
	})
	if err != nil {
		t.Fatalf("CreateShare returned error: %v", err)
	}
	if len(repo.createdRuns) != 1 {
		t.Fatalf("expected 1 report run, got %d", len(repo.createdRuns))
	}
	if repo.createdRuns[0].RunKind != models.ReportRunKindSnapshot {
		t.Fatalf("unexpected run kind: %s", repo.createdRuns[0].RunKind)
	}
	if len(repo.createdShares) != 1 {
		t.Fatalf("expected 1 report share, got %d", len(repo.createdShares))
	}
	createdShare := repo.createdShares[0]
	if createdShare.ReportRunID == nil || *createdShare.ReportRunID == "" {
		t.Fatalf("expected snapshot share to reference a report run")
	}
	if createdShare.TokenHash == "" || createdShare.TokenHash == resp.Token {
		t.Fatalf("expected token hash to be stored instead of raw token")
	}
	if createdShare.PasscodeHash == "" || !createdShare.RequiresPasscode {
		t.Fatalf("expected passcode protection to be enabled")
	}
	if !strings.Contains(resp.AccessURL, "/api/v1/public/report-shares/") {
		t.Fatalf("unexpected access URL: %s", resp.AccessURL)
	}
}

func TestReportServiceAccessLiveShareRequeriesAndLogsAccess(t *testing.T) {
	passcodeHash, err := bcrypt.GenerateFromPassword([]byte("4321"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword returned error: %v", err)
	}
	share := &models.ReportShare{
		ID:               "share-1",
		BusinessID:       "biz-1",
		ReportKey:        "sales_register",
		Title:            "Shared Sales",
		ShareMode:        models.ReportShareModeLive,
		VisibleColumns:   `["serial_number","total"]`,
		Filters:          `{}`,
		TokenHash:        reportShareTokenHash("token-123"),
		PasscodeHash:     string(passcodeHash),
		RequiresPasscode: true,
		CreatedAt:        time.Now(),
	}
	repo := &reportingRepoStub{
		queryResult: &reporting.Result{
			Columns: reporting.LookupOrPanic("sales_register").DefaultColumns,
			Rows: []map[string]interface{}{
				{"serial_number": "INV-002", "total": 200.0},
			},
			Totals: map[string]interface{}{"total": 200.0},
			Pagination: reporting.Pagination{
				Page:  1,
				Limit: 50,
				Total: 1,
			},
		},
		sharesByToken: map[string]*models.ReportShare{
			share.TokenHash: share,
		},
	}
	svc := NewReportService(&config.Config{}, repo, logger.New())

	resp, err := svc.AccessPublicShare(context.Background(), "token-123", ReportShareAccessInput{
		Passcode: "4321",
	}, "127.0.0.1", "unit-test")
	if err != nil {
		t.Fatalf("AccessPublicShare returned error: %v", err)
	}
	if repo.queryCalls != 1 {
		t.Fatalf("expected 1 live query, got %d", repo.queryCalls)
	}
	if repo.touchedShareID != share.ID {
		t.Fatalf("expected touched share ID %s, got %s", share.ID, repo.touchedShareID)
	}
	if len(repo.accessLogs) != 1 || !repo.accessLogs[0].Successful {
		t.Fatalf("expected one successful access log, got %#v", repo.accessLogs)
	}
	if len(resp.Result.Columns) != 2 || resp.Result.Columns[0].Key != "serial_number" || resp.Result.Columns[1].Key != "total" {
		t.Fatalf("unexpected result columns: %#v", resp.Result.Columns)
	}
}

func cloneReportResult(input *reporting.Result) *reporting.Result {
	if input == nil {
		return nil
	}
	cloned := &reporting.Result{
		Columns:    append([]reporting.Column(nil), input.Columns...),
		Totals:     map[string]interface{}{},
		Pagination: input.Pagination,
	}
	for key, value := range input.Totals {
		cloned.Totals[key] = value
	}
	cloned.Rows = make([]map[string]interface{}, 0, len(input.Rows))
	for _, row := range input.Rows {
		copied := map[string]interface{}{}
		for key, value := range row {
			copied[key] = value
		}
		cloned.Rows = append(cloned.Rows, copied)
	}
	return cloned
}
