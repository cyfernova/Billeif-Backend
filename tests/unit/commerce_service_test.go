package unit

import (
	"context"
	"testing"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/require"
)

// ListFeatureEntitlements and SyncFeatureEntitlements live on CommerceService
// and require a real *gorm.DB connection. Since CommerceService.db is unexported
// and the codebase does not expose a DB interface, these methods can only be
// tested reliably via integration tests with a CGO-enabled SQLite build.
// Unit tests are skipped with guidance to run integration tests instead.

func TestCommerceService_ListFeatureEntitlements_IntegrationOnly(t *testing.T) {
	t.Skip("ListFeatureEntitlements and SyncFeatureEntitlements require a real database connection. CommerceService.db is unexported, preventing mocking. Run integration tests with CGO enabled instead.")
}

func TestCommerceService_SyncFeatureEntitlements_IntegrationOnly(t *testing.T) {
	t.Skip("SyncFeatureEntitlements requires a real database connection. Run integration tests with CGO enabled instead.")
}

// EntitlementService_ResolveByBusiness tests the fallback path when subscription
// repo is nil or returns an error - these DO work in unit test mode without CGO.

func TestEntitlementService_ResolveByBusiness_NilSubscriptionRepo_ReturnsFreeDefaults(t *testing.T) {
	svc := services.NewEntitlementService(nil, nil, nil, logger.New())

	entitlements, err := svc.ResolveByBusiness(context.Background(), "any-business")

	require.NoError(t, err)
	require.NotNil(t, entitlements)
	require.True(t, entitlements.EInvoiceEnabled)
	require.True(t, entitlements.EWayBillEnabled)
	require.True(t, entitlements.BulkGSTEnabled)
	require.True(t, entitlements.GSTAPIEnabled)
	require.True(t, entitlements.POSEnabled)
}
