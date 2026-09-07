package services

import (
	"context"
	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"testing"

	"github.com/stretchr/testify/require"
)

type governanceManagementFake struct {
	controls []models.AIGovernanceControl
	business string
	command  interfaces.AgentExecutionGateCommand
}

func (r *governanceManagementFake) GovernanceOverview(_ context.Context, business string) ([]models.AIGovernanceControl, []models.AIAgentRun, error) {
	r.business = business
	return r.controls, nil, nil
}
func (r *governanceManagementFake) SetExecutionGate(_ context.Context, command interfaces.AgentExecutionGateCommand) error {
	r.command = command
	return nil
}

type governanceManagementPermission bool

func (p governanceManagementPermission) UserHasPermission(context.Context, string, string, string) bool {
	return bool(p)
}

func TestGovernanceOverviewRequiresBothPlatformGates(t *testing.T) {
	for _, platform := range []bool{false, true} {
		for _, global := range []bool{false, true} {
			for _, business := range []bool{false, true} {
				repo := &governanceManagementFake{controls: []models.AIGovernanceControl{{ScopeKind: "global", ExecutionEnabled: global}, {ScopeKind: "business", ExecutionEnabled: business}}}
				svc := NewGovernanceManagementService(repo, governanceManagementPermission(true), nil, config.AIGovernanceConfig{ExecutionEnabled: platform})
				result, err := svc.Overview(context.Background(), "actor", "tenant")
				require.NoError(t, err)
				require.Equal(t, platform && global && business, result.ExecutionReady)
				require.Equal(t, business, result.BusinessEnabled)
				require.Equal(t, "tenant", repo.business)
				require.NotNil(t, result.Runs)
			}
		}
	}
}

func TestGovernanceManagementDeniesBeforeRepositoryAccess(t *testing.T) {
	repo := &governanceManagementFake{}
	svc := NewGovernanceManagementService(repo, governanceManagementPermission(false), invoiceActorRepositoryStub{}, config.AIGovernanceConfig{})
	_, err := svc.Overview(context.Background(), "actor", "tenant")
	require.ErrorIs(t, err, ErrAgentToolDenied)
	require.ErrorIs(t, svc.SetBusinessEnabled(context.Background(), "actor", "tenant", true), ErrAgentToolDenied)
	require.Empty(t, repo.business)
	require.Empty(t, repo.command.ScopeKind)
}

func TestGovernanceBusinessGateResolvesActorAndCannotChangeGlobalScope(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		repo := &governanceManagementFake{}
		users := invoiceActorRepositoryStub{bySubject: func(subject string) (*models.User, error) {
			require.Equal(t, "subject", subject)
			return &models.User{ID: "44444444-4444-4444-8444-444444444444"}, nil
		}}
		svc := NewGovernanceManagementService(repo, governanceManagementPermission(true), users, config.AIGovernanceConfig{})
		require.NoError(t, svc.SetBusinessEnabled(context.Background(), "subject", "tenant", enabled))
		require.Equal(t, "business", repo.command.ScopeKind)
		require.Equal(t, "tenant", repo.command.BusinessID)
		require.Empty(t, repo.command.AgentID)
		require.Equal(t, "44444444-4444-4444-8444-444444444444", repo.command.UpdatedByUserID)
		require.Equal(t, enabled, repo.command.ExecutionEnabled)
	}
}
