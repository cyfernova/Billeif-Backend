package services

import (
	"context"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
)

// GovernanceManagementRepository exposes only tenant-scoped control and audit reads.
type GovernanceManagementRepository interface {
	GovernanceOverview(context.Context, string) ([]models.AIGovernanceControl, []models.AIAgentRun, error)
	SetExecutionGate(context.Context, interfaces.AgentExecutionGateCommand) error
}

type GovernanceRunSummary struct {
	ID                    string    `json:"id"`
	AgentID               string    `json:"agent_id"`
	Status                string    `json:"status"`
	StepsUsed             int       `json:"steps_used"`
	MaxSteps              int       `json:"max_steps"`
	CostMicros            int64     `json:"cost_micros"`
	Currency              string    `json:"currency"`
	CreatedAt             time.Time `json:"created_at"`
	CancellationRequested bool      `json:"cancellation_requested"`
}

type GovernanceOverview struct {
	BusinessEnabled          bool                   `json:"business_enabled"`
	PlatformEnabled          bool                   `json:"platform_enabled"`
	ExecutionReady           bool                   `json:"execution_ready"`
	CanManage                bool                   `json:"can_manage"`
	MaxSteps                 int                    `json:"max_steps"`
	MaxToolCalls             int                    `json:"max_tool_calls"`
	MaxDurationSeconds       int64                  `json:"max_duration_seconds"`
	TokenBudget              int64                  `json:"token_budget"`
	BusinessDailyLimitMicros int64                  `json:"business_daily_limit_micros"`
	AgentDailyLimitMicros    int64                  `json:"agent_daily_limit_micros"`
	SpendCurrency            string                 `json:"spend_currency"`
	Runs                     []GovernanceRunSummary `json:"runs"`
}

type GovernanceManagementService struct {
	repository  GovernanceManagementRepository
	permissions PermissionChecker
	users       ReportUserRepository
	config      config.AIGovernanceConfig
}

func NewGovernanceManagementService(repository GovernanceManagementRepository, permissions PermissionChecker, users ReportUserRepository, cfg config.AIGovernanceConfig) *GovernanceManagementService {
	return &GovernanceManagementService{repository: repository, permissions: permissions, users: users, config: cfg}
}

func (s *GovernanceManagementService) Overview(ctx context.Context, userID, businessID string) (*GovernanceOverview, error) {
	if s == nil || s.repository == nil || s.permissions == nil {
		return nil, ErrAgentGovernanceUnavailable
	}
	if !s.permissions.UserHasPermission(ctx, userID, businessID, PermissionAgentsView) {
		return nil, ErrAgentToolDenied
	}
	controls, runs, err := s.repository.GovernanceOverview(ctx, businessID)
	if err != nil {
		return nil, err
	}
	result := &GovernanceOverview{BusinessEnabled: true, CanManage: s.permissions.UserHasPermission(ctx, userID, businessID, PermissionAgentsManage), MaxSteps: s.config.MaxSteps, MaxToolCalls: s.config.MaxToolCalls, MaxDurationSeconds: int64(s.config.MaxDuration.Seconds()), TokenBudget: s.config.RunTokenBudget, BusinessDailyLimitMicros: s.config.BusinessDailyLimitMicros, AgentDailyLimitMicros: s.config.AgentDailyLimitMicros, SpendCurrency: s.config.SpendCurrency, Runs: make([]GovernanceRunSummary, 0, len(runs))}
	for _, control := range controls {
		if control.ScopeKind == "global" {
			result.PlatformEnabled = control.ExecutionEnabled && s.config.ExecutionEnabled
		}
		if control.ScopeKind == "business" {
			result.BusinessEnabled = control.ExecutionEnabled
		}
	}
	result.ExecutionReady = result.PlatformEnabled && result.BusinessEnabled
	for _, run := range runs {
		result.Runs = append(result.Runs, GovernanceRunSummary{ID: run.ID, AgentID: run.AgentID, Status: run.Status, StepsUsed: run.StepsUsed, MaxSteps: run.MaxSteps, CostMicros: run.CostMicros, Currency: run.CostCurrency, CreatedAt: run.CreatedAt, CancellationRequested: run.CancelRequestedAt != nil})
	}
	return result, nil
}

func (s *GovernanceManagementService) SetBusinessEnabled(ctx context.Context, userID, businessID string, enabled bool) error {
	if s == nil || s.repository == nil || s.permissions == nil || s.users == nil {
		return ErrAgentGovernanceUnavailable
	}
	if !s.permissions.UserHasPermission(ctx, userID, businessID, PermissionAgentsManage) {
		return ErrAgentToolDenied
	}
	actorID, err := resolveDatabaseUserID(ctx, s.users, userID)
	if err != nil {
		return err
	}
	reason := "business_admin_paused"
	if enabled {
		reason = "business_admin_resumed"
	}
	return s.repository.SetExecutionGate(ctx, interfaces.AgentExecutionGateCommand{ScopeKind: "business", BusinessID: businessID, UpdatedByUserID: actorID, ExecutionEnabled: enabled, ReasonCode: reason, Now: time.Now().UTC()})
}
