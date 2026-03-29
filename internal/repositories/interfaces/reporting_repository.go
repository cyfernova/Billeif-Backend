package interfaces

import (
	"context"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/reporting"
)

type ReportingRepository interface {
	QueryReport(ctx context.Context, def reporting.Definition, query reporting.Query) (*reporting.Result, error)
	GetDashboard(ctx context.Context, query reporting.Query) (map[string]interface{}, error)
	CreateReportRun(ctx context.Context, run *models.ReportRun) error
	GetReportRun(ctx context.Context, businessID, id string) (*models.ReportRun, error)
	UpsertReportPreference(ctx context.Context, pref *models.ReportPreference) error
	GetReportPreference(ctx context.Context, businessID, userID, reportKey string) (*models.ReportPreference, error)
	CreateReportShare(ctx context.Context, share *models.ReportShare) error
	ListReportShares(ctx context.Context, businessID string, page, limit int) ([]*models.ReportShare, int64, error)
	GetReportShareByTokenHash(ctx context.Context, tokenHash string) (*models.ReportShare, error)
	TouchReportShareAccess(ctx context.Context, shareID string, accessedAt time.Time) error
	CreateReportShareAccessLog(ctx context.Context, accessLog *models.ReportShareAccessLog) error
}
