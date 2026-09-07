package services

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTenantArtifactObjectKeyCannotEscapeTenantPrefix(t *testing.T) {
	key, err := tenantArtifactObjectKey("bulk-jobs", "business-1", "job-1", "invoice.csv")
	require.NoError(t, err)
	require.Equal(t, "bulk-jobs/business-1/job-1/invoice.csv", key)

	key, err = tenantArtifactObjectKey("bulk-jobs", "business-1", "job-1", "../../business-2/attack.csv")
	require.NoError(t, err)
	require.Equal(t, "bulk-jobs/business-1/job-1/attack.csv", key)

	for name, input := range map[string][4]string{
		"missing tenant":    {"bulk-jobs", "", "job-1", "invoice.csv"},
		"tenant separator":  {"bulk-jobs", "business/2", "job-1", "invoice.csv"},
		"missing namespace": {"bulk-jobs", "business-1", "", "invoice.csv"},
		"missing filename":  {"bulk-jobs", "business-1", "job-1", "../.."},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := tenantArtifactObjectKey(input[0], input[1], input[2], input[3])
			require.Error(t, err)
		})
	}
}
