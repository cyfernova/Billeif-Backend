package services

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/reporting"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type reportIdentityRepo struct {
	*reportingRepoStub
	expectedUser string
}

func (r *reportIdentityRepo) GetReportPreference(ctx context.Context, business, user, key string) (*models.ReportPreference, error) {
	if user != r.expectedUser {
		return nil, fmt.Errorf("wrong preference identity")
	}
	return r.reportingRepoStub.GetReportPreference(ctx, business, user, key)
}

func (r *reportIdentityRepo) UpsertReportPreference(ctx context.Context, pref *models.ReportPreference) error {
	if pref.UserID != r.expectedUser {
		return fmt.Errorf("report_preferences_user_id_fkey")
	}
	return r.reportingRepoStub.UpsertReportPreference(ctx, pref)
}

func TestReportUserMapsPersistenceWithoutChangingQueryIdentity(t *testing.T) {
	subject, databaseID := uuid.NewString(), uuid.NewString()
	ctx := ContextWithActor(context.Background(), ActorContext{UserID: subject, Role: "viewer"})
	repo := &reportIdentityRepo{
		reportingRepoStub: &reportingRepoStub{queryResult: &reporting.Result{
			Columns: reporting.LookupOrPanic("sales_register").DefaultColumns,
			Rows:    []map[string]interface{}{},
		}},
		expectedUser: databaseID,
	}
	guard := &recordingCapabilityGuard{}
	svc := NewReportService(&config.Config{}, repo, logger.New()).WithCapabilityGuard(guard).WithUserRepository(invoiceActorRepositoryStub{
		bySubject: func(got string) (*models.User, error) {
			require.Equal(t, subject, got)
			return &models.User{ID: databaseID}, nil
		},
		byID: func(string) (*models.User, error) { t.Fatal("must prefer subject mapping"); return nil, nil },
	})
	defaults, err := svc.GetPreference(ctx, "business", subject, "sales_register")
	require.NoError(t, err)
	require.Equal(t, defaultColumnKeys(reporting.LookupOrPanic("sales_register").DefaultColumns), defaults.Columns)
	_, err = svc.SavePreference(ctx, "business", subject, "sales_register", ReportPreferenceInput{Columns: []string{"serial_number", "total"}, DefaultExportFormat: "csv"})
	require.NoError(t, err)
	require.Equal(t, databaseID, repo.preference.UserID)
	pref, err := svc.GetPreference(ctx, "business", subject, "sales_register")
	require.NoError(t, err)
	require.Equal(t, "csv", pref.DefaultExportFormat)
	result, err := svc.Query(ctx, "business", subject, "sales_register", ReportQueryInput{})
	require.NoError(t, err)
	require.Len(t, result.Columns, 2)
	require.Equal(t, subject, repo.lastQueriedQuery.UserID)
	_, err = svc.Export(ctx, "business", subject, "sales_register", ReportExportInput{Format: "csv"})
	require.NoError(t, err)
	require.Equal(t, databaseID, repo.createdRuns[0].GeneratedBy)
	require.Equal(t, subject, guard.request.UserID)
	_, err = svc.CreateShare(ctx, "business", subject, "sales_register", ReportShareInput{Mode: models.ReportShareModeSnapshot})
	require.NoError(t, err)
	require.Equal(t, databaseID, repo.createdShares[0].CreatedBy)
	require.Equal(t, databaseID, repo.createdRuns[1].GeneratedBy)
	require.Equal(t, subject, ActorFromContext(ctx).UserID)
}

func TestReportUserLookupFailureDoesNotFallbackToAnotherIdentity(t *testing.T) {
	outage := errors.New("database unavailable")
	svc := (&ReportService{}).WithUserRepository(invoiceActorRepositoryStub{
		bySubject: func(string) (*models.User, error) { return nil, outage },
		byID:      func(string) (*models.User, error) { t.Fatal("must not fall back on outage"); return nil, nil },
	})
	_, err := svc.reportUserID(context.Background(), uuid.NewString())
	require.ErrorIs(t, err, outage)
}

func TestReportUserAcceptsCanonicalSubjectAndInternalIdentity(t *testing.T) {
	id, subject := uuid.NewString(), uuid.NewString()
	svc := (&ReportService{}).WithUserRepository(invoiceActorRepositoryStub{
		bySubject: func(got string) (*models.User, error) {
			if got == subject {
				return &models.User{ID: id}, nil
			}
			return nil, gorm.ErrRecordNotFound
		},
		byID: func(got string) (*models.User, error) {
			if got == id {
				return &models.User{ID: id}, nil
			}
			return nil, gorm.ErrRecordNotFound
		},
	})
	for _, input := range []string{"pool:" + subject, id} {
		got, err := svc.reportUserID(context.Background(), input)
		require.NoError(t, err)
		require.Equal(t, id, got)
	}
	_, err := svc.reportUserID(context.Background(), uuid.NewString())
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}
