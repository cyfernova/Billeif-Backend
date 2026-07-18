package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/reporting"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"golang.org/x/crypto/bcrypt"
)

type ReportService struct {
	cfg  *config.Config
	repo interfaces.ReportingRepository
	log  *logger.Logger
}

var ErrReportScopeUnsupported = errors.New("report cannot be safely limited to the caller's branch or warehouse scope")

type ReportQueryInput struct {
	Page    int               `json:"page,omitempty"`
	Limit   int               `json:"limit,omitempty"`
	Columns []string          `json:"columns,omitempty"`
	Filters reporting.Filters `json:"filters,omitempty"`
}

type ReportExportInput struct {
	Page    int               `json:"page,omitempty"`
	Limit   int               `json:"limit,omitempty"`
	Columns []string          `json:"columns,omitempty"`
	Filters reporting.Filters `json:"filters,omitempty"`
	Format  string            `json:"format,omitempty"`
}

type ReportPreferenceInput struct {
	Columns               []string `json:"columns,omitempty"`
	DefaultExportFormat   string   `json:"default_export_format,omitempty"`
	DefaultShareMode      string   `json:"default_share_mode,omitempty"`
	ShareRequiresPasscode *bool    `json:"share_requires_passcode,omitempty"`
}

type ReportShareInput struct {
	Page      int               `json:"page,omitempty"`
	Limit     int               `json:"limit,omitempty"`
	Columns   []string          `json:"columns,omitempty"`
	Filters   reporting.Filters `json:"filters,omitempty"`
	Mode      string            `json:"mode,omitempty"`
	Title     string            `json:"title,omitempty"`
	Passcode  string            `json:"passcode,omitempty"`
	ExpiresAt *time.Time        `json:"expires_at,omitempty"`
}

type ReportShareAccessInput struct {
	Page     int    `json:"page,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Passcode string `json:"passcode,omitempty"`
}

type ReportPreferenceResponse struct {
	ReportKey              string             `json:"report_key"`
	Columns                []string           `json:"columns"`
	DefaultExportFormat    string             `json:"default_export_format"`
	DefaultShareMode       string             `json:"default_share_mode"`
	ShareRequiresPasscode  bool               `json:"share_requires_passcode"`
	AvailableColumns       []reporting.Column `json:"available_columns"`
	AvailableExportFormats []string           `json:"available_export_formats"`
	AvailableShareModes    []string           `json:"available_share_modes"`
}

type ReportExportResponse struct {
	Run         *models.ReportRun `json:"run"`
	Filename    string            `json:"filename"`
	ContentType string            `json:"content_type"`
	Data        string            `json:"data"`
}

type ReportShareHistoryItem struct {
	ID               string     `json:"id"`
	ReportKey        string     `json:"report_key"`
	ReportName       string     `json:"report_name"`
	Title            string     `json:"title"`
	ShareMode        string     `json:"share_mode"`
	RequiresPasscode bool       `json:"requires_passcode"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	LastAccessedAt   *time.Time `json:"last_accessed_at,omitempty"`
	AccessCount      int64      `json:"access_count"`
	Status           string     `json:"status"`
	CreatedBy        string     `json:"created_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type ReportShareCreateResponse struct {
	Share       ReportShareHistoryItem `json:"share"`
	Token       string                 `json:"token"`
	MetadataURL string                 `json:"metadata_url"`
	AccessURL   string                 `json:"access_url"`
}

type PublicReportShareMetadata struct {
	Title            string     `json:"title"`
	ReportKey        string     `json:"report_key"`
	ReportName       string     `json:"report_name"`
	ShareMode        string     `json:"share_mode"`
	RequiresPasscode bool       `json:"requires_passcode"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"created_at"`
	Columns          []string   `json:"columns,omitempty"`
}

type PublicReportShareAccessResponse struct {
	Metadata PublicReportShareMetadata `json:"metadata"`
	Result   *reporting.Result         `json:"result"`
}

func NewReportService(cfg *config.Config, repo interfaces.ReportingRepository, log *logger.Logger) *ReportService {
	return &ReportService{cfg: cfg, repo: repo, log: log}
}

func (s *ReportService) Catalog() []reporting.Definition {
	return reporting.Catalog()
}

func (s *ReportService) Query(ctx context.Context, businessID, userID, reportKey string, input ReportQueryInput) (*reporting.Result, error) {
	def, ok := reporting.Lookup(reportKey)
	if !ok {
		return nil, fmt.Errorf("report not found")
	}
	if err := validateReportScope(def, input.Filters); err != nil {
		return nil, err
	}
	columns, err := s.resolveColumns(ctx, businessID, userID, def, input.Columns, true)
	if err != nil {
		return nil, err
	}
	return s.runReport(ctx, def, reporting.Query{
		BusinessID: businessID,
		UserID:     userID,
		Page:       input.Page,
		Limit:      input.Limit,
		Filters:    input.Filters,
	}, columns)
}

func (s *ReportService) Export(ctx context.Context, businessID, userID, reportKey string, input ReportExportInput) (*ReportExportResponse, error) {
	def, ok := reporting.Lookup(reportKey)
	if !ok {
		return nil, fmt.Errorf("report not found")
	}
	if err := validateReportScope(def, input.Filters); err != nil {
		return nil, err
	}
	columns, err := s.resolveColumns(ctx, businessID, userID, def, input.Columns, true)
	if err != nil {
		return nil, err
	}
	result, err := s.runReport(ctx, def, reporting.Query{
		BusinessID: businessID,
		UserID:     userID,
		Page:       input.Page,
		Limit:      input.Limit,
		Filters:    input.Filters,
	}, columns)
	if err != nil {
		return nil, err
	}

	format := strings.ToLower(strings.TrimSpace(input.Format))
	if format == "" {
		format = "json"
	}
	filename := fmt.Sprintf("%s-%s.%s", def.Key, time.Now().UTC().Format("20060102-150405"), format)
	contentType := "application/json"
	data := ""

	switch format {
	case "csv":
		contentType = "text/csv"
		data, err = encodeCSV(result)
	case "json":
		data, err = encodeJSON(result)
	default:
		return nil, fmt.Errorf("unsupported export format")
	}
	if err != nil {
		return nil, err
	}

	run := &models.ReportRun{
		BusinessID:     businessID,
		ReportKey:      def.Key,
		RunKind:        models.ReportRunKindExport,
		Filters:        mustMarshalJSON(input.Filters, "{}"),
		VisibleColumns: mustMarshalJSON(columns, "[]"),
		ExportFormat:   format,
		Status:         models.ReportRunStatusCompleted,
		Payload: mustMarshalJSON(map[string]interface{}{
			"filename":     filename,
			"content_type": contentType,
			"data":         data,
		}, "{}"),
		Summary:     mustMarshalJSON(reportSummary(result), "{}"),
		GeneratedBy: userID,
	}
	if err := s.repo.CreateReportRun(ctx, run); err != nil {
		return nil, err
	}

	return &ReportExportResponse{
		Run:         run,
		Filename:    filename,
		ContentType: contentType,
		Data:        data,
	}, nil
}

func (s *ReportService) Dashboard(ctx context.Context, businessID string, input ReportQueryInput) (map[string]interface{}, error) {
	if input.Filters.BranchScopeRestricted || input.Filters.WarehouseScopeRestricted {
		return nil, ErrReportScopeUnsupported
	}
	return s.repo.GetDashboard(ctx, reporting.Query{
		BusinessID: businessID,
		Page:       input.Page,
		Limit:      input.Limit,
		Filters:    input.Filters,
	})
}

func (s *ReportService) GetPreference(ctx context.Context, businessID, userID, reportKey string) (*ReportPreferenceResponse, error) {
	def, ok := reporting.Lookup(reportKey)
	if !ok {
		return nil, fmt.Errorf("report not found")
	}
	config := defaultReportPreferenceConfig(def)
	if pref, err := s.repo.GetReportPreference(ctx, businessID, userID, reportKey); err == nil && pref != nil {
		config = mergeReportPreferenceConfig(config, pref.Config)
	} else if err != nil && !reportPreferenceMissing(err) {
		return nil, err
	}
	columns := normalizeColumns(config.Columns, def.DefaultColumns)
	return &ReportPreferenceResponse{
		ReportKey:              reportKey,
		Columns:                columns,
		DefaultExportFormat:    config.DefaultExportFormat,
		DefaultShareMode:       config.DefaultShareMode,
		ShareRequiresPasscode:  config.ShareRequiresPasscode,
		AvailableColumns:       def.DefaultColumns,
		AvailableExportFormats: []string{"json", "csv"},
		AvailableShareModes:    []string{models.ReportShareModeSnapshot, models.ReportShareModeLive},
	}, nil
}

func (s *ReportService) SavePreference(ctx context.Context, businessID, userID, reportKey string, input ReportPreferenceInput) (*ReportPreferenceResponse, error) {
	def, ok := reporting.Lookup(reportKey)
	if !ok {
		return nil, fmt.Errorf("report not found")
	}
	config := defaultReportPreferenceConfig(def)
	if pref, err := s.repo.GetReportPreference(ctx, businessID, userID, reportKey); err == nil && pref != nil {
		config = mergeReportPreferenceConfig(config, pref.Config)
	} else if err != nil && !reportPreferenceMissing(err) {
		return nil, err
	}
	if input.Columns != nil {
		columns := normalizeColumns(input.Columns, def.DefaultColumns)
		if len(columns) == 0 {
			return nil, fmt.Errorf("at least one valid column is required")
		}
		config.Columns = columns
	}
	if input.DefaultExportFormat != "" {
		format := strings.ToLower(strings.TrimSpace(input.DefaultExportFormat))
		if format != "json" && format != "csv" {
			return nil, fmt.Errorf("unsupported export format")
		}
		config.DefaultExportFormat = format
	}
	if input.DefaultShareMode != "" {
		mode := strings.ToLower(strings.TrimSpace(input.DefaultShareMode))
		if mode != models.ReportShareModeSnapshot && mode != models.ReportShareModeLive {
			return nil, fmt.Errorf("unsupported share mode")
		}
		config.DefaultShareMode = mode
	}
	if input.ShareRequiresPasscode != nil {
		config.ShareRequiresPasscode = *input.ShareRequiresPasscode
	}
	if len(config.Columns) == 0 {
		config.Columns = normalizeColumns(nil, def.DefaultColumns)
	}
	pref := &models.ReportPreference{
		BusinessID: businessID,
		UserID:     userID,
		ReportKey:  reportKey,
		Config:     mustMarshalJSON(config, "{}"),
	}
	if err := s.repo.UpsertReportPreference(ctx, pref); err != nil {
		return nil, err
	}
	return &ReportPreferenceResponse{
		ReportKey:              reportKey,
		Columns:                config.Columns,
		DefaultExportFormat:    config.DefaultExportFormat,
		DefaultShareMode:       config.DefaultShareMode,
		ShareRequiresPasscode:  config.ShareRequiresPasscode,
		AvailableColumns:       def.DefaultColumns,
		AvailableExportFormats: []string{"json", "csv"},
		AvailableShareModes:    []string{models.ReportShareModeSnapshot, models.ReportShareModeLive},
	}, nil
}

type reportPreferenceConfig struct {
	Columns               []string `json:"columns,omitempty"`
	DefaultExportFormat   string   `json:"default_export_format,omitempty"`
	DefaultShareMode      string   `json:"default_share_mode,omitempty"`
	ShareRequiresPasscode bool     `json:"share_requires_passcode,omitempty"`
}

func defaultReportPreferenceConfig(def reporting.Definition) reportPreferenceConfig {
	return reportPreferenceConfig{
		Columns:               normalizeColumns(nil, def.DefaultColumns),
		DefaultExportFormat:   "json",
		DefaultShareMode:      models.ReportShareModeSnapshot,
		ShareRequiresPasscode: false,
	}
}

func mergeReportPreferenceConfig(base reportPreferenceConfig, raw string) reportPreferenceConfig {
	var stored reportPreferenceConfig
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return base
	}
	if len(stored.Columns) > 0 {
		base.Columns = stored.Columns
	}
	if strings.TrimSpace(stored.DefaultExportFormat) != "" {
		base.DefaultExportFormat = strings.ToLower(strings.TrimSpace(stored.DefaultExportFormat))
	}
	if strings.TrimSpace(stored.DefaultShareMode) != "" {
		base.DefaultShareMode = strings.ToLower(strings.TrimSpace(stored.DefaultShareMode))
	}
	base.ShareRequiresPasscode = stored.ShareRequiresPasscode
	return base
}

func reportPreferenceMissing(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "not found")
}

func (s *ReportService) CreateShare(ctx context.Context, businessID, userID, reportKey string, input ReportShareInput) (*ReportShareCreateResponse, error) {
	def, ok := reporting.Lookup(reportKey)
	if !ok {
		return nil, fmt.Errorf("report not found")
	}
	if err := validateReportScope(def, input.Filters); err != nil {
		return nil, err
	}
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode == "" {
		mode = models.ReportShareModeSnapshot
	}
	if mode != models.ReportShareModeSnapshot && mode != models.ReportShareModeLive {
		return nil, fmt.Errorf("unsupported share mode")
	}
	if mode == models.ReportShareModeLive && (input.Filters.BranchScopeRestricted || input.Filters.WarehouseScopeRestricted) {
		return nil, fmt.Errorf("%w: scoped users may create snapshot shares only", ErrReportScopeUnsupported)
	}
	if input.ExpiresAt != nil && input.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("expires_at must be in the future")
	}

	columns, err := s.resolveColumns(ctx, businessID, userID, def, input.Columns, true)
	if err != nil {
		return nil, err
	}

	share := &models.ReportShare{
		BusinessID:       businessID,
		ReportKey:        reportKey,
		Title:            strings.TrimSpace(input.Title),
		ShareMode:        mode,
		Filters:          mustMarshalJSON(input.Filters, "{}"),
		VisibleColumns:   mustMarshalJSON(columns, "[]"),
		RequiresPasscode: strings.TrimSpace(input.Passcode) != "",
		ExpiresAt:        input.ExpiresAt,
		CreatedBy:        userID,
	}
	if share.Title == "" {
		share.Title = def.Name
	}

	if share.RequiresPasscode {
		hash, err := bcrypt.GenerateFromPassword([]byte(strings.TrimSpace(input.Passcode)), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		share.PasscodeHash = string(hash)
	}

	if mode == models.ReportShareModeSnapshot {
		result, err := s.runReport(ctx, def, reporting.Query{
			BusinessID: businessID,
			UserID:     userID,
			Page:       input.Page,
			Limit:      input.Limit,
			Filters:    input.Filters,
		}, columns)
		if err != nil {
			return nil, err
		}
		run := &models.ReportRun{
			BusinessID:     businessID,
			ReportKey:      reportKey,
			RunKind:        models.ReportRunKindSnapshot,
			Filters:        mustMarshalJSON(input.Filters, "{}"),
			VisibleColumns: mustMarshalJSON(columns, "[]"),
			ExportFormat:   "json",
			Status:         models.ReportRunStatusCompleted,
			Payload:        mustMarshalJSON(result, "{}"),
			Summary:        mustMarshalJSON(reportSummary(result), "{}"),
			GeneratedBy:    userID,
		}
		if err := s.repo.CreateReportRun(ctx, run); err != nil {
			return nil, err
		}
		share.ReportRunID = &run.ID
	}

	token, err := generateReportShareToken()
	if err != nil {
		return nil, err
	}
	share.TokenHash = reportShareTokenHash(token)
	if err := s.repo.CreateReportShare(ctx, share); err != nil {
		return nil, err
	}

	item := mapReportShareHistoryItem(*share)
	metadataURL, accessURL := s.reportShareURLs(token)
	return &ReportShareCreateResponse{
		Share:       item,
		Token:       token,
		MetadataURL: metadataURL,
		AccessURL:   accessURL,
	}, nil
}

func validateReportScope(def reporting.Definition, filters reporting.Filters) error {
	if !filters.BranchScopeRestricted && !filters.WarehouseScopeRestricted {
		return nil
	}
	switch def.Family {
	case "document_register", "daily_documents", "line_summary", "line_profit", "gst_hsn_summary",
		"profit_and_loss", "receivables", "payables", "aging_receivables", "aging_payables":
		return nil
	case "inventory_balances", "low_stock", "stock_movement", "batch_expiry", "serial_tracking", "warehouse_transfers":
		return nil
	default:
		return ErrReportScopeUnsupported
	}
}

func (s *ReportService) ListShares(ctx context.Context, businessID string, page, limit int) ([]ReportShareHistoryItem, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	shares, total, err := s.repo.ListReportShares(ctx, businessID, page, limit)
	if err != nil {
		return nil, 0, err
	}
	items := make([]ReportShareHistoryItem, 0, len(shares))
	for _, share := range shares {
		items = append(items, mapReportShareHistoryItem(*share))
	}
	return items, total, nil
}

func (s *ReportService) GetPublicMetadata(ctx context.Context, token string) (*PublicReportShareMetadata, error) {
	share, def, err := s.loadActiveShare(ctx, token)
	if err != nil {
		return nil, err
	}
	return &PublicReportShareMetadata{
		Title:            firstNonEmpty(strings.TrimSpace(share.Title), def.Name),
		ReportKey:        share.ReportKey,
		ReportName:       def.Name,
		ShareMode:        share.ShareMode,
		RequiresPasscode: share.RequiresPasscode,
		ExpiresAt:        share.ExpiresAt,
		Status:           reportShareStatus(share),
		CreatedAt:        share.CreatedAt,
		Columns:          unmarshalStringList(share.VisibleColumns),
	}, nil
}

func (s *ReportService) AccessPublicShare(ctx context.Context, token string, input ReportShareAccessInput, ipAddress, userAgent string) (*PublicReportShareAccessResponse, error) {
	share, def, err := s.loadActiveShare(ctx, token)
	if err != nil {
		return nil, err
	}

	if share.RequiresPasscode {
		if strings.TrimSpace(input.Passcode) == "" {
			_ = s.logShareAccess(ctx, share, false, "passcode_required", ipAddress, userAgent)
			return nil, fmt.Errorf("passcode required")
		}
		if err := bcrypt.CompareHashAndPassword([]byte(share.PasscodeHash), []byte(strings.TrimSpace(input.Passcode))); err != nil {
			_ = s.logShareAccess(ctx, share, false, "invalid_passcode", ipAddress, userAgent)
			return nil, fmt.Errorf("invalid passcode")
		}
	}

	var result *reporting.Result
	switch share.ShareMode {
	case models.ReportShareModeSnapshot:
		if share.ReportRunID == nil || *share.ReportRunID == "" {
			return nil, fmt.Errorf("report run not found")
		}
		run, err := s.repo.GetReportRun(ctx, share.BusinessID, *share.ReportRunID)
		if err != nil {
			return nil, err
		}
		result = &reporting.Result{}
		if err := json.Unmarshal([]byte(run.Payload), result); err != nil {
			return nil, err
		}
	case models.ReportShareModeLive:
		columns := unmarshalStringList(share.VisibleColumns)
		filters := reporting.Filters{}
		if err := json.Unmarshal([]byte(share.Filters), &filters); err != nil {
			return nil, err
		}
		result, err = s.runReport(ctx, def, reporting.Query{
			BusinessID: share.BusinessID,
			Page:       input.Page,
			Limit:      input.Limit,
			Filters:    filters,
		}, columns)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported share mode")
	}

	now := time.Now()
	if err := s.repo.TouchReportShareAccess(ctx, share.ID, now); err != nil {
		s.log.Warn("failed to touch report share access", "share_id", share.ID, "error", err)
	}
	if err := s.logShareAccess(ctx, share, true, "", ipAddress, userAgent); err != nil {
		s.log.Warn("failed to create report share access log", "share_id", share.ID, "error", err)
	}

	return &PublicReportShareAccessResponse{
		Metadata: PublicReportShareMetadata{
			Title:            firstNonEmpty(strings.TrimSpace(share.Title), def.Name),
			ReportKey:        share.ReportKey,
			ReportName:       def.Name,
			ShareMode:        share.ShareMode,
			RequiresPasscode: share.RequiresPasscode,
			ExpiresAt:        share.ExpiresAt,
			Status:           reportShareStatus(share),
			CreatedAt:        share.CreatedAt,
			Columns:          unmarshalStringList(share.VisibleColumns),
		},
		Result: result,
	}, nil
}

func (s *ReportService) runReport(ctx context.Context, def reporting.Definition, query reporting.Query, columns []string) (*reporting.Result, error) {
	result, err := s.repo.QueryReport(ctx, def, query)
	if err != nil {
		return nil, err
	}
	return applyVisibleColumns(result, columns), nil
}

func (s *ReportService) resolveColumns(ctx context.Context, businessID, userID string, def reporting.Definition, requested []string, allowPreference bool) ([]string, error) {
	if len(requested) > 0 {
		columns := normalizeColumns(requested, def.DefaultColumns)
		if len(columns) == 0 {
			return nil, fmt.Errorf("at least one valid column is required")
		}
		return columns, nil
	}
	if allowPreference && businessID != "" && userID != "" {
		pref, err := s.repo.GetReportPreference(ctx, businessID, userID, def.Key)
		if err == nil {
			columns := normalizeColumns(unmarshalColumnsFromPreference(pref.Config), def.DefaultColumns)
			if len(columns) > 0 {
				return columns, nil
			}
		} else if !strings.Contains(strings.ToLower(err.Error()), "not found") {
			return nil, err
		}
	}
	return defaultColumnKeys(def.DefaultColumns), nil
}

func (s *ReportService) loadActiveShare(ctx context.Context, token string) (*models.ReportShare, reporting.Definition, error) {
	share, err := s.repo.GetReportShareByTokenHash(ctx, reportShareTokenHash(strings.TrimSpace(token)))
	if err != nil {
		return nil, reporting.Definition{}, err
	}
	def, ok := reporting.Lookup(share.ReportKey)
	if !ok {
		return nil, reporting.Definition{}, fmt.Errorf("report not found")
	}
	switch status := reportShareStatus(share); status {
	case "expired":
		return nil, reporting.Definition{}, fmt.Errorf("report share expired")
	case "revoked":
		return nil, reporting.Definition{}, fmt.Errorf("report share revoked")
	}
	return share, def, nil
}

func (s *ReportService) logShareAccess(ctx context.Context, share *models.ReportShare, successful bool, failureReason, ipAddress, userAgent string) error {
	return s.repo.CreateReportShareAccessLog(ctx, &models.ReportShareAccessLog{
		ReportShareID: share.ID,
		AccessedAt:    time.Now(),
		Successful:    successful,
		FailureReason: failureReason,
		IPAddress:     truncateString(ipAddress, 64),
		UserAgent:     truncateString(userAgent, 500),
		Metadata:      mustMarshalJSON(map[string]interface{}{"share_mode": share.ShareMode}, "{}"),
	})
}

func (s *ReportService) reportShareURLs(token string) (string, string) {
	basePath := "/api/v1/public/report-shares/" + token
	baseURL := ""
	if s.cfg != nil {
		baseURL = s.cfg.Server.ResolveBaseURL()
	}
	if baseURL == "" {
		return basePath + "/metadata", basePath + "/access"
	}
	return baseURL + basePath + "/metadata", baseURL + basePath + "/access"
}

func applyVisibleColumns(result *reporting.Result, requested []string) *reporting.Result {
	if result == nil {
		return nil
	}
	columnMap := map[string]reporting.Column{}
	for _, column := range result.Columns {
		columnMap[column.Key] = column
	}

	selectedKeys := requested
	if len(selectedKeys) == 0 {
		selectedKeys = defaultColumnKeys(result.Columns)
	}

	filteredColumns := make([]reporting.Column, 0, len(selectedKeys))
	filteredRows := make([]map[string]interface{}, 0, len(result.Rows))
	for _, key := range selectedKeys {
		column, ok := columnMap[key]
		if !ok {
			continue
		}
		filteredColumns = append(filteredColumns, column)
	}
	if len(filteredColumns) == 0 {
		filteredColumns = result.Columns
	}

	for _, row := range result.Rows {
		filtered := map[string]interface{}{}
		for _, column := range filteredColumns {
			filtered[column.Key] = row[column.Key]
		}
		filteredRows = append(filteredRows, filtered)
	}

	final := &reporting.Result{
		Columns:    filteredColumns,
		Rows:       filteredRows,
		Totals:     result.Totals,
		Pagination: result.Pagination,
	}
	final.ClipboardTSV = buildClipboardTSV(final.Columns, final.Rows)
	return final
}

func buildClipboardTSV(columns []reporting.Column, rows []map[string]interface{}) string {
	if len(columns) == 0 {
		return ""
	}
	var builder strings.Builder
	headers := make([]string, 0, len(columns))
	for _, column := range columns {
		headers = append(headers, sanitizeTSVValue(column.Label))
	}
	builder.WriteString(strings.Join(headers, "\t"))
	builder.WriteByte('\n')
	for _, row := range rows {
		values := make([]string, 0, len(columns))
		for _, column := range columns {
			values = append(values, sanitizeTSVValue(fmtValue(row[column.Key])))
		}
		builder.WriteString(strings.Join(values, "\t"))
		builder.WriteByte('\n')
	}
	return builder.String()
}

func encodeCSV(result *reporting.Result) (string, error) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	headers := make([]string, 0, len(result.Columns))
	for _, column := range result.Columns {
		headers = append(headers, sanitizeSpreadsheetCell(column.Label))
	}
	if err := writer.Write(headers); err != nil {
		return "", err
	}
	for _, row := range result.Rows {
		record := make([]string, 0, len(result.Columns))
		for _, column := range result.Columns {
			record = append(record, sanitizeSpreadsheetCell(fmtValue(row[column.Key])))
		}
		if err := writer.Write(record); err != nil {
			return "", err
		}
	}
	writer.Flush()
	return buffer.String(), writer.Error()
}

func sanitizeSpreadsheetCell(value string) string {
	if value == "" {
		return value
	}
	trimmedLeft := strings.TrimLeft(value, " \r\n")
	if trimmedLeft == "" {
		return value
	}
	switch trimmedLeft[0] {
	case '=', '+', '-', '@', '\t':
		return "'" + value
	default:
		return value
	}
}

func encodeJSON(value interface{}) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func reportSummary(result *reporting.Result) map[string]interface{} {
	if result == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"row_count": result.Pagination.Total,
		"totals":    result.Totals,
	}
}

func defaultColumnKeys(columns []reporting.Column) []string {
	keys := make([]string, 0, len(columns))
	for _, column := range columns {
		keys = append(keys, column.Key)
	}
	return keys
}

func normalizeColumns(columns []string, available []reporting.Column) []string {
	allowed := map[string]struct{}{}
	for _, column := range available {
		allowed[column.Key] = struct{}{}
	}
	normalized := make([]string, 0, len(columns))
	seen := map[string]struct{}{}
	for _, key := range columns {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := allowed[key]; !ok {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, key)
	}
	return normalized
}

func unmarshalColumnsFromPreference(raw string) []string {
	payload := map[string][]string{}
	if err := json.Unmarshal([]byte(raw), &payload); err == nil {
		return payload["columns"]
	}
	var columns []string
	if err := json.Unmarshal([]byte(raw), &columns); err == nil {
		return columns
	}
	return nil
}

func unmarshalStringList(raw string) []string {
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil
	}
	return values
}

func mustMarshalJSON(value interface{}, fallback string) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fallback
	}
	return string(data)
}

func mapReportShareHistoryItem(share models.ReportShare) ReportShareHistoryItem {
	def, _ := reporting.Lookup(share.ReportKey)
	return ReportShareHistoryItem{
		ID:               share.ID,
		ReportKey:        share.ReportKey,
		ReportName:       def.Name,
		Title:            firstNonEmpty(strings.TrimSpace(share.Title), def.Name),
		ShareMode:        share.ShareMode,
		RequiresPasscode: share.RequiresPasscode,
		ExpiresAt:        share.ExpiresAt,
		RevokedAt:        share.RevokedAt,
		LastAccessedAt:   share.LastAccessedAt,
		AccessCount:      share.AccessCount,
		Status:           reportShareStatus(&share),
		CreatedBy:        share.CreatedBy,
		CreatedAt:        share.CreatedAt,
	}
}

func reportShareStatus(share *models.ReportShare) string {
	if share == nil {
		return "unknown"
	}
	if share.RevokedAt != nil {
		return "revoked"
	}
	if share.ExpiresAt != nil && share.ExpiresAt.Before(time.Now()) {
		return "expired"
	}
	return "active"
}

func reportShareTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func generateReportShareToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func fmtValue(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case time.Time:
		return typed.Format(time.RFC3339)
	case *time.Time:
		if typed == nil {
			return ""
		}
		return typed.Format(time.RFC3339)
	default:
		return fmt.Sprint(typed)
	}
}

func sanitizeTSVValue(value string) string {
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

func truncateString(value string, maxLen int) string {
	if len(value) <= maxLen {
		return value
	}
	return value[:maxLen]
}
