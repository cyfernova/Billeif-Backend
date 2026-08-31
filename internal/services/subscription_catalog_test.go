package services

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionCatalogPreservesAdvertisedPlansAndAmounts(t *testing.T) {
	catalog := SubscriptionCatalog()
	require.Equal(t, CurrentSubscriptionCatalogVersion, catalog.Version)
	require.Len(t, catalog.Plans, 4)

	tests := []struct {
		id           string
		displayName  string
		legacyPlan   string
		planCode     string
		amountPaise  int64
		maxInvoices  int64
		maxCustomers int64
		maxUsers     int64
		maxStorageMB int64
	}{
		{id: "free", displayName: "Free", legacyPlan: "free", planCode: "free", amountPaise: 0, maxInvoices: 10, maxCustomers: 10, maxUsers: 3, maxStorageMB: 100},
		{id: "pro_monthly", displayName: "Pro", legacyPlan: "starter", planCode: "pro", amountPaise: 29900, maxInvoices: 100, maxCustomers: 100, maxUsers: 3, maxStorageMB: 512},
		{id: "rise_monthly", displayName: "Rise", legacyPlan: "professional", planCode: "rise", amountPaise: 99900, maxInvoices: 1000, maxCustomers: 1000, maxUsers: 10, maxStorageMB: 2048},
		{id: "biz_monthly", displayName: "Biz", legacyPlan: "enterprise", planCode: "biz", amountPaise: 299900, maxInvoices: 100000, maxCustomers: 100000, maxUsers: 50, maxStorageMB: 10240},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			plan, ok := FindSubscriptionPlan(tt.id)
			require.True(t, ok)
			require.Equal(t, tt.displayName, plan.DisplayName)
			require.Equal(t, tt.legacyPlan, plan.LegacyPlan)
			require.Equal(t, tt.planCode, plan.PlanCode)
			require.Equal(t, "INR", plan.Currency)
			require.Equal(t, tt.amountPaise, plan.Amount)
			require.Equal(t, "month", plan.Interval)
			require.Equal(t, tt.maxInvoices, plan.Quotas[QuotaInvoices])
			require.Equal(t, tt.maxCustomers, plan.Quotas[QuotaCustomers])
			require.Equal(t, tt.maxUsers, plan.Quotas[QuotaUsers])
			require.Equal(t, tt.maxStorageMB, plan.Quotas[QuotaStorageMB])
		})
	}
}

func TestRazorpayPlanLookupUsesSubscriptionCatalog(t *testing.T) {
	for _, plan := range SubscriptionCatalog().Plans {
		if plan.Amount == 0 {
			continue
		}
		paymentPlan, ok := paidSubscriptionPlan(plan.ID)
		require.True(t, ok)
		require.Equal(t, plan, paymentPlan)
	}

	_, ok := paidSubscriptionPlan("free")
	require.False(t, ok)
}

func TestCatalogEntitlementSeedsMatchPlanQuotas(t *testing.T) {
	for _, planID := range []string{"free", "pro_monthly", "rise_monthly", "biz_monthly"} {
		plan, ok := FindSubscriptionPlan(planID)
		require.True(t, ok)
		seeds := defaultEntitlementSeedsForPlan(plan)

		require.Equal(t, plan.Features[FeatureMultiUser], seedEnabled(seeds, FeatureMultiUser))
		require.Equal(t, plan.Features[FeatureDriveStorageMB], seedEnabled(seeds, FeatureDriveStorageMB))
		if plan.Features[FeatureMultiUser] {
			require.Equal(t, plan.Quotas[QuotaUsers], *seedLimit(seeds, FeatureMultiUser))
		}
		if plan.Features[FeatureDriveStorageMB] {
			require.Equal(t, plan.Quotas[QuotaStorageMB], *seedLimit(seeds, FeatureDriveStorageMB))
		}
	}
}
