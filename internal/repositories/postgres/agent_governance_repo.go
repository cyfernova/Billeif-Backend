package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AgentGovernanceRepository struct{ db *gorm.DB }

func NewAgentGovernanceRepository(db *gorm.DB) *AgentGovernanceRepository {
	return &AgentGovernanceRepository{db: db}
}

func (r *AgentGovernanceRepository) ExecutionReady(ctx context.Context, businessID string) (bool, error) {
	if r == nil || r.db == nil || !validUUID(businessID) {
		return false, interfaces.ErrAgentGovernanceInvalidScope
	}
	var controls []models.AIGovernanceControl
	if err := r.db.WithContext(ctx).Where("scope_kind = 'global' OR (scope_kind = 'business' AND business_id = ?)", businessID).Find(&controls).Error; err != nil {
		return false, fmt.Errorf("read agent governance readiness: %w", err)
	}
	globalEnabled := false
	for _, control := range controls {
		if control.ScopeKind == "global" {
			globalEnabled = control.ExecutionEnabled
		}
		if control.ScopeKind == "business" && !control.ExecutionEnabled {
			return false, nil
		}
	}
	return globalEnabled, nil
}

func (r *AgentGovernanceRepository) IssueApproval(ctx context.Context, c interfaces.AgentApprovalCommand) (*interfaces.AgentApprovalResult, error) {
	if r == nil || r.db == nil || !validGovernanceScope(c.BusinessID, c.AgentID, c.UserID) || !validHash(c.ArgumentsHash) || !validHash(c.RequestHash) || c.ToolKey == "" || c.RiskClass == "" || c.ExecutionIdempotencyKey == "" || c.ID == "" || !c.ExpiresAt.After(c.IssuedAt) || !validJSONDocument(c.SanitizedArguments) || !validImpact(c.MonetaryAmountMinor, c.MonetaryCurrency) || !validImpact(c.TaxAmountMinor, c.TaxCurrency) {
		return nil, interfaces.ErrAgentGovernanceInvalidScope
	}
	row := models.AIToolApproval{ID: c.ID, BusinessID: c.BusinessID, AgentID: c.AgentID, UserID: c.UserID, ToolKey: c.ToolKey, RiskClass: c.RiskClass, ArgumentsHash: c.ArgumentsHash, SanitizedArguments: c.SanitizedArguments, ResourceType: c.ResourceType, ResourceID: c.ResourceID, MonetaryAmountMinor: c.MonetaryAmountMinor, TaxAmountMinor: c.TaxAmountMinor, ExecutionIdempotencyKey: c.ExecutionIdempotencyKey, RequestHash: c.RequestHash, IssuedAt: c.IssuedAt.UTC(), ExpiresAt: c.ExpiresAt.UTC(), CreatedAt: c.IssuedAt.UTC()}
	if c.MonetaryCurrency != "" {
		row.MonetaryCurrency = &c.MonetaryCurrency
	}
	if c.TaxCurrency != "" {
		row.TaxCurrency = &c.TaxCurrency
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err == nil {
		return &interfaces.AgentApprovalResult{ID: row.ID}, nil
	} else if !governanceUniqueViolation(err) {
		return nil, fmt.Errorf("issue agent approval: %w", err)
	}
	var existing models.AIToolApproval
	err := r.db.WithContext(ctx).Where("business_id = ? AND agent_id = ? AND user_id = ? AND tool_key = ? AND execution_idempotency_key = ?", c.BusinessID, c.AgentID, c.UserID, c.ToolKey, c.ExecutionIdempotencyKey).First(&existing).Error
	if err != nil || existing.RequestHash != c.RequestHash || existing.ArgumentsHash != c.ArgumentsHash {
		return nil, interfaces.ErrAgentGovernanceConflict
	}
	return &interfaces.AgentApprovalResult{ID: existing.ID, Replayed: true}, nil
}

func (r *AgentGovernanceRepository) StartRun(ctx context.Context, c interfaces.StartAgentRunCommand) (*interfaces.AgentRunResult, error) {
	if r == nil || r.db == nil || !validGovernanceScope(c.BusinessID, c.AgentID, c.UserID) || c.ID == "" || c.IdempotencyKey == "" || !validHash(c.RequestHash) || c.ProviderKey == "" || c.ModelKey == "" || c.PromptTemplateVersion == "" || !validJSONDocument(c.ModelConfig) || c.TokenBudget <= 0 || c.MaxSteps <= 0 || c.MaxToolCalls < 0 || c.MaxRetries < 0 || !c.DeadlineAt.After(c.Now) || !validCurrency(c.SpendCurrency) {
		return nil, interfaces.ErrAgentGovernanceInvalidScope
	}
	var result *interfaces.AgentRunResult
	var policyErr error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireExecutionGates(tx, c.BusinessID, c.AgentID); err != nil {
			policyErr = err
			return nil
		}
		var existing models.AIAgentRun
		err := tx.Where("business_id = ? AND agent_id = ? AND user_id = ? AND idempotency_key = ?", c.BusinessID, c.AgentID, c.UserID, c.IdempotencyKey).First(&existing).Error
		if err == nil {
			if existing.RequestHash != c.RequestHash || existing.ProviderKey != c.ProviderKey || existing.ModelKey != c.ModelKey || existing.ModelConfig != c.ModelConfig || existing.PromptTemplateVersion != c.PromptTemplateVersion {
				policyErr = interfaces.ErrAgentGovernanceConflict
				return nil
			}
			result = runResult(existing, true)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("inspect agent run: %w", err)
		}
		started := c.Now.UTC()
		run := models.AIAgentRun{ID: c.ID, BusinessID: c.BusinessID, AgentID: c.AgentID, UserID: c.UserID, IdempotencyKey: c.IdempotencyKey, RequestHash: c.RequestHash, ProviderKey: c.ProviderKey, ModelKey: c.ModelKey, ModelConfig: c.ModelConfig, PromptTemplateVersion: c.PromptTemplateVersion, TokenBudget: c.TokenBudget, MaxSteps: c.MaxSteps, MaxToolCalls: c.MaxToolCalls, MaxRetries: c.MaxRetries, DeadlineAt: c.DeadlineAt.UTC(), Status: "running", CostCurrency: strings.ToUpper(c.SpendCurrency), CreatedAt: started, StartedAt: &started, UpdatedAt: started}
		if err := tx.Create(&run).Error; err != nil {
			return fmt.Errorf("create agent run: %w", err)
		}
		result = runResult(run, false)
		return nil
	})
	if err != nil {
		if governanceUniqueViolation(err) {
			return r.replayRun(ctx, c)
		}
		return nil, err
	}
	if policyErr != nil {
		return nil, policyErr
	}
	return result, nil
}

func (r *AgentGovernanceRepository) replayRun(ctx context.Context, c interfaces.StartAgentRunCommand) (*interfaces.AgentRunResult, error) {
	var existing models.AIAgentRun
	err := r.db.WithContext(ctx).Where("business_id = ? AND agent_id = ? AND user_id = ? AND idempotency_key = ?", c.BusinessID, c.AgentID, c.UserID, c.IdempotencyKey).First(&existing).Error
	if err != nil || existing.RequestHash != c.RequestHash || existing.ProviderKey != c.ProviderKey || existing.ModelKey != c.ModelKey || existing.ModelConfig != c.ModelConfig || existing.PromptTemplateVersion != c.PromptTemplateVersion {
		return nil, interfaces.ErrAgentGovernanceConflict
	}
	return runResult(existing, true), nil
}

func (r *AgentGovernanceRepository) ReserveRunCapacity(ctx context.Context, c interfaces.ReserveAgentRunCapacityCommand) (*interfaces.AgentSpendReservationResult, error) {
	if r == nil || r.db == nil || !validGovernanceScope(c.BusinessID, c.AgentID, c.UserID) || c.RunID == "" || c.IdempotencyKey == "" || !validHash(c.RequestHash) || c.TokenAmount < 0 || c.SpendMicros < 0 || !c.PeriodEnd.After(c.PeriodStart) || !c.ExpiresAt.After(c.Now) || (c.SpendMicros > 0 && (!validCurrency(c.SpendCurrency) || c.BusinessLimitMicros <= 0 || c.AgentDailyLimitMicros <= 0)) {
		return nil, interfaces.ErrAgentGovernanceInvalidScope
	}
	var result *interfaces.AgentSpendReservationResult
	var policyErr error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireExecutionGates(tx, c.BusinessID, c.AgentID); err != nil {
			policyErr = err
			return nil
		}
		var run models.AIAgentRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND business_id = ? AND agent_id = ? AND user_id = ?", c.RunID, c.BusinessID, c.AgentID, c.UserID).First(&run).Error; err != nil {
			policyErr = scopedNotFound(err)
			return nil
		}
		if run.RequestHash != c.RequestHash || run.ProviderKey != c.ProviderKey || run.ModelKey != c.ModelKey || run.CostCurrency != strings.ToUpper(c.SpendCurrency) {
			policyErr = interfaces.ErrAgentGovernanceConflict
			return nil
		}
		if err := activeRunError(run, c.Now); err != nil {
			policyErr = err
			return nil
		}
		var existing models.AISpendReservation
		err := tx.Where("run_id = ? AND idempotency_key = ?", c.RunID, c.IdempotencyKey).First(&existing).Error
		if err == nil {
			if existing.RequestHash != c.RequestHash || existing.ProviderKey != c.ProviderKey || existing.ModelKey != c.ModelKey || existing.ReservedMicros != c.SpendMicros {
				policyErr = interfaces.ErrAgentGovernanceConflict
				return nil
			}
			result = reservationResult(existing, true)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if run.TokenReserved+run.TokenUsed+c.TokenAmount > run.TokenBudget {
			policyErr = interfaces.ErrAgentGovernanceTokenLimit
			return nil
		}
		if c.Step && run.StepsUsed >= run.MaxSteps {
			policyErr = interfaces.ErrAgentGovernanceStepLimit
			return nil
		}
		if c.Retry && run.RetriesUsed >= run.MaxRetries {
			policyErr = interfaces.ErrAgentGovernanceRetryLimit
			return nil
		}
		if c.SpendMicros == 0 {
			applyRunCapacity(&run, c)
			if err := tx.Save(&run).Error; err != nil {
				return err
			}
			result = &interfaces.AgentSpendReservationResult{Status: "reserved"}
			return nil
		}
		businessBudget, err := lockBudgetPeriod(tx, "business", c, nil, c.BusinessLimitMicros)
		if err != nil {
			return err
		}
		agentID := c.AgentID
		agentBudget, err := lockBudgetPeriod(tx, "agent", c, &agentID, c.AgentDailyLimitMicros)
		if err != nil {
			return err
		}
		if businessBudget.ReservedMicros+businessBudget.SettledMicros+c.SpendMicros > businessBudget.LimitMicros {
			policyErr = interfaces.ErrAgentGovernanceBusinessSpendLimit
			return nil
		}
		if agentBudget.ReservedMicros+agentBudget.SettledMicros+c.SpendMicros > agentBudget.LimitMicros {
			policyErr = interfaces.ErrAgentGovernanceAgentSpendLimit
			return nil
		}
		businessBudget.ReservedMicros += c.SpendMicros
		agentBudget.ReservedMicros += c.SpendMicros
		businessBudget.UpdatedAt, agentBudget.UpdatedAt = c.Now.UTC(), c.Now.UTC()
		if err := tx.Save(&businessBudget).Error; err != nil {
			return err
		}
		if err := tx.Save(&agentBudget).Error; err != nil {
			return err
		}
		applyRunCapacity(&run, c)
		if err := tx.Save(&run).Error; err != nil {
			return err
		}
		reservation := models.AISpendReservation{ID: uuid.NewString(), RunID: c.RunID, BusinessBudgetPeriodID: businessBudget.ID, AgentBudgetPeriodID: agentBudget.ID, ProviderKey: c.ProviderKey, ModelKey: c.ModelKey, SpendCurrency: strings.ToUpper(c.SpendCurrency), ReservedMicros: c.SpendMicros, Status: "reserved", IdempotencyKey: c.IdempotencyKey, RequestHash: c.RequestHash, ReservedAt: c.Now.UTC(), ExpiresAt: c.ExpiresAt.UTC(), UpdatedAt: c.Now.UTC()}
		if err := tx.Create(&reservation).Error; err != nil {
			return err
		}
		result = reservationResult(reservation, false)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reserve agent run capacity: %w", err)
	}
	if policyErr != nil {
		return nil, policyErr
	}
	return result, nil
}

func applyRunCapacity(run *models.AIAgentRun, c interfaces.ReserveAgentRunCapacityCommand) {
	run.TokenReserved += c.TokenAmount
	if c.Step {
		run.StepsUsed++
	}
	if c.Retry {
		run.RetriesUsed++
	}
	run.UpdatedAt = c.Now.UTC()
}

func (r *AgentGovernanceRepository) AuthorizeToolExecution(ctx context.Context, c interfaces.AuthorizeAgentToolCommand) (*interfaces.AgentToolExecutionResult, error) {
	if r == nil || r.db == nil || !validGovernanceScope(c.BusinessID, c.AgentID, c.UserID) || c.ID == "" || c.RunID == "" || c.Sequence <= 0 || c.ToolKey == "" || c.RiskClass == "" || !validHash(c.ArgumentsHash) || !validHash(c.RequestHash) || !validJSONDocument(c.SanitizedArguments) || c.IdempotencyKey == "" || (c.ApprovalRequired && c.ApprovalID == "") || !validImpact(c.MonetaryAmountMinor, c.MonetaryCurrency) || !validImpact(c.TaxAmountMinor, c.TaxCurrency) {
		return nil, interfaces.ErrAgentGovernanceInvalidScope
	}
	var result *interfaces.AgentToolExecutionResult
	var policyErr error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireExecutionGates(tx, c.BusinessID, c.AgentID); err != nil {
			policyErr = err
			return nil
		}
		var run models.AIAgentRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND business_id = ? AND agent_id = ? AND user_id = ?", c.RunID, c.BusinessID, c.AgentID, c.UserID).First(&run).Error; err != nil {
			policyErr = scopedNotFound(err)
			return nil
		}
		if err := activeRunError(run, c.Now); err != nil {
			policyErr = err
			return nil
		}
		if run.RequestHash != c.RequestHash {
			policyErr = interfaces.ErrAgentGovernanceConflict
			return nil
		}
		var existing models.AIToolExecution
		err := tx.Where("run_id = ? AND idempotency_key = ?", c.RunID, c.IdempotencyKey).First(&existing).Error
		if err == nil {
			if existing.RequestHash != c.RequestHash || existing.ArgumentsHash != c.ArgumentsHash || existing.ToolKey != c.ToolKey || existing.RiskClass != c.RiskClass {
				policyErr = interfaces.ErrAgentGovernanceConflict
				return nil
			}
			result = toolResult(existing, true)
			var reservation models.AISpendReservation
			if err := tx.Where("tool_execution_id = ?", existing.ID).First(&reservation).Error; err == nil {
				result.SpendReservationID = reservation.ID
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if run.ToolCallsUsed >= run.MaxToolCalls {
			policyErr = interfaces.ErrAgentGovernanceToolCallLimit
			return nil
		}
		if c.ApprovalRequired {
			update := tx.Model(&models.AIToolApproval{}).Where("id = ? AND business_id = ? AND agent_id = ? AND user_id = ? AND tool_key = ? AND risk_class = ? AND arguments_hash = ? AND resource_type = ? AND resource_id = ? AND execution_idempotency_key = ? AND request_hash = ? AND consumed_at IS NULL AND issued_at <= ? AND expires_at > ?", c.ApprovalID, c.BusinessID, c.AgentID, c.UserID, c.ToolKey, c.RiskClass, c.ArgumentsHash, c.ResourceType, c.ResourceID, c.IdempotencyKey, c.RequestHash, c.Now, c.Now).
				Where("monetary_amount_minor IS NOT DISTINCT FROM ? AND monetary_currency IS NOT DISTINCT FROM ? AND tax_amount_minor IS NOT DISTINCT FROM ? AND tax_currency IS NOT DISTINCT FROM ?", c.MonetaryAmountMinor, nullCurrency(c.MonetaryCurrency), c.TaxAmountMinor, nullCurrency(c.TaxCurrency)).
				Update("consumed_at", c.Now.UTC())
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				policyErr = interfaces.ErrAgentGovernanceApprovalInvalid
				return nil
			}
		}
		reservation, err := reserveToolCapacityTx(tx, &run, c)
		if err != nil {
			return err
		}
		var approvalID *string
		if c.ApprovalID != "" {
			approvalID = &c.ApprovalID
		}
		now := c.Now.UTC()
		execution := models.AIToolExecution{ID: c.ID, RunID: c.RunID, BusinessID: c.BusinessID, AgentID: c.AgentID, UserID: c.UserID, Sequence: c.Sequence, ToolKey: c.ToolKey, RiskClass: c.RiskClass, ApprovalRequired: c.ApprovalRequired, ApprovalID: approvalID, ArgumentsHash: c.ArgumentsHash, SanitizedArguments: c.SanitizedArguments, ResourceType: c.ResourceType, ResourceID: c.ResourceID, IdempotencyKey: c.IdempotencyKey, RequestHash: c.RequestHash, Status: "authorized", EffectDisposition: "none", AttemptCount: 1, RequestedAt: now, StartedAt: &now, UpdatedAt: now}
		if err := tx.Create(&execution).Error; err != nil {
			return err
		}
		if reservation != nil {
			reservation.ToolExecutionID = &execution.ID
			if err := tx.Create(reservation).Error; err != nil {
				return err
			}
		}
		run.ToolCallsUsed++
		run.UpdatedAt = now
		if err := tx.Save(&run).Error; err != nil {
			return err
		}
		result = toolResult(execution, false)
		if reservation != nil {
			result.SpendReservationID = reservation.ID
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("authorize agent tool: %w", err)
	}
	if policyErr != nil {
		return nil, policyErr
	}
	return result, nil
}

func (r *AgentGovernanceRepository) CompleteToolExecution(ctx context.Context, c interfaces.CompleteAgentToolCommand) (*interfaces.AgentToolExecutionResult, error) {
	if r == nil || r.db == nil || !validGovernanceScope(c.BusinessID, c.AgentID, c.UserID) || c.ExecutionID == "" || c.RunID == "" || !validHash(c.RequestHash) {
		return nil, interfaces.ErrAgentGovernanceInvalidScope
	}
	var result *interfaces.AgentToolExecutionResult
	var policyErr error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var execution models.AIToolExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND run_id = ? AND business_id = ? AND agent_id = ? AND user_id = ?", c.ExecutionID, c.RunID, c.BusinessID, c.AgentID, c.UserID).First(&execution).Error; err != nil {
			policyErr = scopedNotFound(err)
			return nil
		}
		if execution.RequestHash != c.RequestHash {
			policyErr = interfaces.ErrAgentGovernanceConflict
			return nil
		}
		if terminalToolStatus(execution.Status) {
			result = toolResult(execution, true)
			return nil
		}
		status := c.Status
		if c.EffectDisposition == "unknown" {
			status = "reconciliation_required"
		}
		if c.SpendReservationID != "" {
			var reservation models.AISpendReservation
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND run_id = ?", c.SpendReservationID, c.RunID).First(&reservation).Error; err != nil {
				return scopedNotFound(err)
			}
			if c.CostMicros > reservation.ReservedMicros {
				status = "reconciliation_required"
			}
		}
		if status == "succeeded" && execution.ApprovalRequired && (c.EffectDisposition != "confirmed" || c.ResultType == "" || c.ResultID == "") {
			policyErr = interfaces.ErrAgentGovernanceUnsafeTransition
			return nil
		}
		if !validToolTerminalStatus(status) {
			policyErr = interfaces.ErrAgentGovernanceUnsafeTransition
			return nil
		}
		if status == "reconciliation_required" {
			c.EffectDisposition = "unknown"
		}
		now := c.Now.UTC()
		execution.Status, execution.EffectDisposition = status, c.EffectDisposition
		execution.ResultType, execution.ResultID = c.ResultType, c.ResultID
		execution.FailureCode, execution.FinalDisposition = safeStoredCode(c.FailureCode), status
		execution.InputTokens, execution.OutputTokens, execution.CostMicros = c.InputTokens, c.OutputTokens, c.CostMicros
		execution.CompletedAt, execution.UpdatedAt = &now, now
		if err := tx.Save(&execution).Error; err != nil {
			return err
		}
		if c.SpendReservationID != "" {
			if err := finalizeSpendReservation(tx, c, status); err != nil {
				return err
			}
		}
		result = toolResult(execution, false)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("complete agent tool: %w", err)
	}
	if policyErr != nil {
		return nil, policyErr
	}
	return result, nil
}

func (r *AgentGovernanceRepository) CompleteRun(ctx context.Context, c interfaces.CompleteAgentRunCommand) (*interfaces.AgentRunResult, error) {
	if r == nil || r.db == nil || !validGovernanceScope(c.BusinessID, c.AgentID, c.UserID) || c.RunID == "" || !validHash(c.RequestHash) {
		return nil, interfaces.ErrAgentGovernanceInvalidScope
	}
	var result *interfaces.AgentRunResult
	var policyErr error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run models.AIAgentRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND business_id = ? AND agent_id = ? AND user_id = ?", c.RunID, c.BusinessID, c.AgentID, c.UserID).First(&run).Error; err != nil {
			policyErr = scopedNotFound(err)
			return nil
		}
		if run.RequestHash != c.RequestHash {
			policyErr = interfaces.ErrAgentGovernanceConflict
			return nil
		}
		if run.CompletedAt != nil {
			result = runResult(run, true)
			return nil
		}
		if c.Status == "completed" {
			var unresolved, reservations int64
			if err := tx.Model(&models.AIToolExecution{}).Where("run_id = ? AND status = 'reconciliation_required'", run.ID).Count(&unresolved).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.AISpendReservation{}).Where("run_id = ? AND status IN ('reserved', 'reconciliation_required')", run.ID).Count(&reservations).Error; err != nil {
				return err
			}
			if unresolved+reservations > 0 {
				policyErr = interfaces.ErrAgentGovernanceUnsafeTransition
				return nil
			}
		}
		if !validRunTerminalStatus(c.Status) {
			policyErr = interfaces.ErrAgentGovernanceUnsafeTransition
			return nil
		}
		now := c.Now.UTC()
		run.Status, run.FailureCode, run.FinalDisposition = c.Status, safeStoredCode(c.FailureCode), safeStoredCode(c.FinalDisposition)
		run.InputTokens, run.OutputTokens, run.TokenUsed, run.CostMicros = c.InputTokens, c.OutputTokens, c.InputTokens+c.OutputTokens, c.CostMicros
		if run.TokenUsed > run.TokenBudget {
			policyErr = interfaces.ErrAgentGovernanceTokenLimit
			return nil
		}
		run.TokenReserved = 0
		run.CompletedAt, run.UpdatedAt = &now, now
		if err := tx.Save(&run).Error; err != nil {
			return err
		}
		result = runResult(run, false)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if policyErr != nil {
		return nil, policyErr
	}
	return result, nil
}

func (r *AgentGovernanceRepository) RequestCancellation(ctx context.Context, c interfaces.CancelAgentRunCommand) error {
	if r == nil || r.db == nil || !validGovernanceScope(c.BusinessID, c.AgentID, c.UserID) || c.RunID == "" {
		return interfaces.ErrAgentGovernanceInvalidScope
	}
	result := r.db.WithContext(ctx).Model(&models.AIAgentRun{}).Where("id = ? AND business_id = ? AND agent_id = ? AND user_id = ? AND status IN ('initializing', 'running')", c.RunID, c.BusinessID, c.AgentID, c.UserID).Updates(map[string]any{"cancel_requested_at": c.Now.UTC(), "failure_code": safeStoredCode(c.ReasonCode), "updated_at": c.Now.UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return interfaces.ErrAgentGovernanceNotFound
	}
	return nil
}

func (r *AgentGovernanceRepository) CheckExecution(ctx context.Context, c interfaces.CheckAgentExecutionCommand) error {
	if r == nil || r.db == nil || !validGovernanceScope(c.BusinessID, c.AgentID, c.UserID) || c.RunID == "" || c.Now.IsZero() {
		return interfaces.ErrAgentGovernanceInvalidScope
	}
	var policyErr error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireExecutionGates(tx, c.BusinessID, c.AgentID); err != nil {
			policyErr = err
			return nil
		}
		var run models.AIAgentRun
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND business_id = ? AND agent_id = ? AND user_id = ?", c.RunID, c.BusinessID, c.AgentID, c.UserID).First(&run).Error; err != nil {
			policyErr = scopedNotFound(err)
			return nil
		}
		policyErr = activeRunError(run, c.Now)
		return nil
	})
	if err != nil {
		return fmt.Errorf("check agent execution: %w", err)
	}
	return policyErr
}

func (r *AgentGovernanceRepository) SetExecutionGate(ctx context.Context, c interfaces.AgentExecutionGateCommand) error {
	if r == nil || r.db == nil || !validGateCommand(c) {
		return interfaces.ErrAgentGovernanceInvalidScope
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("scope_kind = ?", c.ScopeKind)
		if c.ScopeKind == "business" {
			query = query.Where("business_id = ?", c.BusinessID)
		}
		if c.ScopeKind == "agent" {
			query = query.Where("business_id = ? AND agent_id = ?", c.BusinessID, c.AgentID)
		}
		var control models.AIGovernanceControl
		err := query.First(&control).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			control = models.AIGovernanceControl{ID: uuid.NewString(), ScopeKind: c.ScopeKind, ExecutionEnabled: c.ExecutionEnabled, ReasonCode: safeStoredCode(c.ReasonCode), Version: 1, UpdatedAt: c.Now.UTC()}
			if c.BusinessID != "" {
				control.BusinessID = &c.BusinessID
			}
			if c.AgentID != "" {
				control.AgentID = &c.AgentID
			}
			if c.UpdatedByUserID != "" {
				control.UpdatedByUserID = &c.UpdatedByUserID
			}
			if err := tx.Create(&control).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			control.ExecutionEnabled, control.ReasonCode, control.UpdatedAt = c.ExecutionEnabled, safeStoredCode(c.ReasonCode), c.Now.UTC()
			control.Version++
			if c.UpdatedByUserID != "" {
				control.UpdatedByUserID = &c.UpdatedByUserID
			}
			if err := tx.Save(&control).Error; err != nil {
				return err
			}
		}
		if c.ExecutionEnabled {
			return nil
		}
		runs := tx.Model(&models.AIAgentRun{}).Where("status IN ('initializing', 'running')")
		if c.ScopeKind != "global" {
			runs = runs.Where("business_id = ?", c.BusinessID)
		}
		if c.ScopeKind == "agent" {
			runs = runs.Where("agent_id = ?", c.AgentID)
		}
		return runs.Updates(map[string]any{"cancel_requested_at": c.Now.UTC(), "failure_code": safeStoredCode(c.ReasonCode), "updated_at": c.Now.UTC()}).Error
	})
}

func (r *AgentGovernanceRepository) AdmitProvider(ctx context.Context, c interfaces.ProviderAdmissionCommand) (*interfaces.ProviderAdmission, error) {
	if r == nil || r.db == nil || c.ProviderKey == "" || c.FailureThreshold <= 0 || c.Cooldown <= 0 || c.ProbeLease <= 0 {
		return nil, interfaces.ErrAgentGovernanceInvalidScope
	}
	var admission *interfaces.ProviderAdmission
	var policyErr error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var breaker models.AIProviderCircuitBreaker
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("provider_key = ?", c.ProviderKey).First(&breaker).Error; err != nil {
			policyErr = interfaces.ErrAgentGovernanceCircuitOpen
			return nil
		}
		switch breaker.State {
		case "closed":
			admission = &interfaces.ProviderAdmission{}
		case "open":
			if breaker.RetryAt == nil || c.Now.Before(*breaker.RetryAt) {
				policyErr = interfaces.ErrAgentGovernanceCircuitOpen
				return nil
			}
			token := c.ProbeToken
			if token == "" {
				token = uuid.NewString()
			}
			expires := c.Now.UTC().Add(c.ProbeLease)
			breaker.State, breaker.ProbeToken, breaker.ProbeExpiresAt = "half_open", &token, &expires
			breaker.Version++
			breaker.UpdatedAt = c.Now.UTC()
			if err := tx.Save(&breaker).Error; err != nil {
				return err
			}
			admission = &interfaces.ProviderAdmission{ProbeToken: token, HalfOpen: true}
		case "half_open":
			if breaker.ProbeExpiresAt == nil || breaker.ProbeExpiresAt.After(c.Now) {
				policyErr = interfaces.ErrAgentGovernanceProbeBusy
				return nil
			}
			token := c.ProbeToken
			if token == "" {
				token = uuid.NewString()
			}
			expires := c.Now.UTC().Add(c.ProbeLease)
			breaker.ProbeToken, breaker.ProbeExpiresAt = &token, &expires
			breaker.Version++
			breaker.UpdatedAt = c.Now.UTC()
			if err := tx.Save(&breaker).Error; err != nil {
				return err
			}
			admission = &interfaces.ProviderAdmission{ProbeToken: token, HalfOpen: true}
		default:
			policyErr = interfaces.ErrAgentGovernanceCircuitOpen
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if policyErr != nil {
		return nil, policyErr
	}
	return admission, nil
}

func (r *AgentGovernanceRepository) RecordProviderOutcome(ctx context.Context, c interfaces.ProviderOutcomeCommand) error {
	if r == nil || r.db == nil || c.ProviderKey == "" || c.FailureThreshold <= 0 || c.Cooldown <= 0 {
		return interfaces.ErrAgentGovernanceInvalidScope
	}
	var policyErr error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var breaker models.AIProviderCircuitBreaker
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("provider_key = ?", c.ProviderKey).First(&breaker).Error; err != nil {
			policyErr = interfaces.ErrAgentGovernanceCircuitOpen
			return nil
		}
		if breaker.State == "half_open" && (breaker.ProbeToken == nil || *breaker.ProbeToken != c.ProbeToken || breaker.ProbeExpiresAt == nil || !breaker.ProbeExpiresAt.After(c.Now)) {
			policyErr = interfaces.ErrAgentGovernanceProbeBusy
			return nil
		}
		if c.Success {
			if breaker.State != "closed" && breaker.State != "half_open" {
				return nil
			}
			breaker.State, breaker.ConsecutiveFailures, breaker.LastFailureCode = "closed", 0, ""
			breaker.OpenedAt, breaker.RetryAt, breaker.ProbeToken, breaker.ProbeExpiresAt = nil, nil, nil, nil
		} else {
			breaker.ConsecutiveFailures++
			breaker.LastFailureCode = safeStoredCode(c.FailureCode)
			if breaker.State == "half_open" || breaker.ConsecutiveFailures >= c.FailureThreshold {
				opened, retry := c.Now.UTC(), c.Now.UTC().Add(c.Cooldown)
				breaker.State, breaker.OpenedAt, breaker.RetryAt = "open", &opened, &retry
				breaker.ProbeToken, breaker.ProbeExpiresAt = nil, nil
			}
		}
		breaker.Version++
		breaker.UpdatedAt = c.Now.UTC()
		return tx.Save(&breaker).Error
	})
	if err != nil {
		return err
	}
	return policyErr
}

func requireExecutionGates(tx *gorm.DB, businessID, agentID string) error {
	var controls []models.AIGovernanceControl
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("scope_kind = 'global' OR (scope_kind = 'business' AND business_id = ?) OR (scope_kind = 'agent' AND business_id = ? AND agent_id = ?)", businessID, businessID, agentID).Find(&controls).Error; err != nil {
		return err
	}
	globalEnabled := false
	for _, control := range controls {
		if control.ScopeKind == "global" {
			globalEnabled = control.ExecutionEnabled
		}
		if control.ScopeKind != "global" && !control.ExecutionEnabled {
			return interfaces.ErrAgentGovernanceGateDisabled
		}
	}
	if !globalEnabled {
		return interfaces.ErrAgentGovernanceGateDisabled
	}
	return nil
}

func reserveToolCapacityTx(tx *gorm.DB, run *models.AIAgentRun, command interfaces.AuthorizeAgentToolCommand) (*models.AISpendReservation, error) {
	capacity := command.Capacity
	if capacity == nil || run == nil || capacity.RunID != command.RunID || capacity.BusinessID != command.BusinessID ||
		capacity.AgentID != command.AgentID || capacity.UserID != command.UserID || capacity.RequestHash != command.RequestHash ||
		capacity.ProviderKey != run.ProviderKey || capacity.ModelKey != run.ModelKey ||
		capacity.SpendMicros <= 0 || capacity.BusinessLimitMicros <= 0 || capacity.AgentDailyLimitMicros <= 0 ||
		capacity.TokenAmount < 0 || capacity.IdempotencyKey == "" || !capacity.PeriodEnd.After(capacity.PeriodStart) ||
		!capacity.ExpiresAt.After(capacity.Now) || !validCurrency(capacity.SpendCurrency) ||
		strings.ToUpper(capacity.SpendCurrency) != run.CostCurrency {
		return nil, interfaces.ErrAgentGovernanceInvalidScope
	}
	if run.TokenReserved+run.TokenUsed+capacity.TokenAmount > run.TokenBudget {
		return nil, interfaces.ErrAgentGovernanceTokenLimit
	}
	if capacity.Step && run.StepsUsed >= run.MaxSteps {
		return nil, interfaces.ErrAgentGovernanceStepLimit
	}
	if capacity.Retry && run.RetriesUsed >= run.MaxRetries {
		return nil, interfaces.ErrAgentGovernanceRetryLimit
	}
	businessBudget, err := lockBudgetPeriod(tx, "business", *capacity, nil, capacity.BusinessLimitMicros)
	if err != nil {
		return nil, err
	}
	agentID := capacity.AgentID
	agentBudget, err := lockBudgetPeriod(tx, "agent", *capacity, &agentID, capacity.AgentDailyLimitMicros)
	if err != nil {
		return nil, err
	}
	if businessBudget.ReservedMicros+businessBudget.SettledMicros+capacity.SpendMicros > businessBudget.LimitMicros {
		return nil, interfaces.ErrAgentGovernanceBusinessSpendLimit
	}
	if agentBudget.ReservedMicros+agentBudget.SettledMicros+capacity.SpendMicros > agentBudget.LimitMicros {
		return nil, interfaces.ErrAgentGovernanceAgentSpendLimit
	}
	businessBudget.ReservedMicros += capacity.SpendMicros
	agentBudget.ReservedMicros += capacity.SpendMicros
	businessBudget.UpdatedAt, agentBudget.UpdatedAt = capacity.Now.UTC(), capacity.Now.UTC()
	if err := tx.Save(&businessBudget).Error; err != nil {
		return nil, err
	}
	if err := tx.Save(&agentBudget).Error; err != nil {
		return nil, err
	}
	run.TokenReserved += capacity.TokenAmount
	if capacity.Step {
		run.StepsUsed++
	}
	if capacity.Retry {
		run.RetriesUsed++
	}
	return &models.AISpendReservation{
		ID: uuid.NewString(), RunID: capacity.RunID, BusinessBudgetPeriodID: businessBudget.ID,
		AgentBudgetPeriodID: agentBudget.ID, ProviderKey: capacity.ProviderKey, ModelKey: capacity.ModelKey,
		SpendCurrency: strings.ToUpper(capacity.SpendCurrency), ReservedMicros: capacity.SpendMicros,
		Status: "reserved", IdempotencyKey: capacity.IdempotencyKey, RequestHash: capacity.RequestHash,
		ReservedAt: capacity.Now.UTC(), ExpiresAt: capacity.ExpiresAt.UTC(), UpdatedAt: capacity.Now.UTC(),
	}, nil
}

func lockBudgetPeriod(tx *gorm.DB, scope string, c interfaces.ReserveAgentRunCapacityCommand, agentID *string, limit int64) (models.AIBudgetPeriod, error) {
	query := tx.Where("scope_kind = ? AND business_id = ? AND period_start = ? AND period_end = ? AND spend_currency = ?", scope, c.BusinessID, c.PeriodStart.UTC(), c.PeriodEnd.UTC(), strings.ToUpper(c.SpendCurrency))
	if agentID == nil {
		query = query.Where("agent_id IS NULL")
	} else {
		query = query.Where("agent_id = ?", *agentID)
	}
	var period models.AIBudgetPeriod
	err := query.First(&period).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		period = models.AIBudgetPeriod{ID: uuid.NewString(), ScopeKind: scope, BusinessID: c.BusinessID, AgentID: agentID, PeriodStart: c.PeriodStart.UTC(), PeriodEnd: c.PeriodEnd.UTC(), SpendCurrency: strings.ToUpper(c.SpendCurrency), LimitMicros: limit, Version: 1, CreatedAt: c.Now.UTC(), UpdatedAt: c.Now.UTC()}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&period).Error; err != nil {
			return period, err
		}
		err = query.Clauses(clause.Locking{Strength: "UPDATE"}).First(&period).Error
	} else if err == nil {
		err = query.Clauses(clause.Locking{Strength: "UPDATE"}).First(&period).Error
	}
	if err != nil {
		return period, err
	}
	if limit < period.LimitMicros {
		period.LimitMicros = limit
	}
	return period, nil
}

func finalizeSpendReservation(tx *gorm.DB, c interfaces.CompleteAgentToolCommand, status string) error {
	var reservation models.AISpendReservation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND run_id = ?", c.SpendReservationID, c.RunID).First(&reservation).Error; err != nil {
		return scopedNotFound(err)
	}
	if reservation.Status != "reserved" {
		return nil
	}
	var business, agent models.AIBudgetPeriod
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&business, "id = ?", reservation.BusinessBudgetPeriodID).Error; err != nil {
		return err
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&agent, "id = ?", reservation.AgentBudgetPeriodID).Error; err != nil {
		return err
	}
	now := c.Now.UTC()
	if status == "reconciliation_required" || c.CostMicros > reservation.ReservedMicros {
		reservation.Status, reservation.UpdatedAt = "reconciliation_required", now
		return tx.Save(&reservation).Error
	}
	business.ReservedMicros -= reservation.ReservedMicros
	agent.ReservedMicros -= reservation.ReservedMicros
	if business.ReservedMicros < 0 || agent.ReservedMicros < 0 {
		return interfaces.ErrAgentGovernanceUnsafeTransition
	}
	if status == "succeeded" || c.Chargeable {
		business.SettledMicros += c.CostMicros
		agent.SettledMicros += c.CostMicros
		reservation.Status, reservation.SettledMicros, reservation.SettledAt = "settled", c.CostMicros, &now
	} else {
		reservation.Status, reservation.ReleasedAt = "released", &now
	}
	business.UpdatedAt, agent.UpdatedAt, reservation.UpdatedAt = now, now, now
	if err := tx.Save(&business).Error; err != nil {
		return err
	}
	if err := tx.Save(&agent).Error; err != nil {
		return err
	}
	return tx.Save(&reservation).Error
}

func validGovernanceScope(businessID, agentID, userID string) bool {
	return validUUID(businessID) && validUUID(agentID) && validUUID(userID)
}
func validUUID(value string) bool { _, err := uuid.Parse(value); return err == nil }
func validHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
func validCurrency(value string) bool {
	value = strings.ToUpper(value)
	if len(value) != 3 {
		return false
	}
	for _, c := range value {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}
func validJSONDocument(value string) bool {
	trimmed := strings.TrimSpace(value)
	return len(value) > 1 && len(value) <= 4096 && strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")
}
func validImpact(amount *int64, currency string) bool {
	return (amount == nil && currency == "") || (amount != nil && *amount >= 0 && validCurrency(currency))
}
func nullCurrency(value string) any {
	if value == "" {
		return nil
	}
	return strings.ToUpper(value)
}
func scopedNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return interfaces.ErrAgentGovernanceNotFound
	}
	return err
}
func activeRunError(run models.AIAgentRun, now time.Time) error {
	if run.CancelRequestedAt != nil {
		return interfaces.ErrAgentGovernanceRunCancelled
	}
	if !run.DeadlineAt.After(now) {
		return interfaces.ErrAgentGovernanceRunExpired
	}
	if run.Status != "running" && run.Status != "initializing" {
		return interfaces.ErrAgentGovernanceUnsafeTransition
	}
	return nil
}
func terminalToolStatus(status string) bool {
	return status == "succeeded" || status == "failed" || status == "denied" || status == "cancelled" || status == "timed_out" || status == "reconciliation_required"
}
func validToolTerminalStatus(status string) bool {
	return terminalToolStatus(status) && status != "denied"
}
func validRunTerminalStatus(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled" || status == "timed_out" || status == "blocked" || status == "reconciliation_required"
}
func safeStoredCode(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 100 {
		return "invalid_code"
	}
	for _, c := range value {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.') {
			return "invalid_code"
		}
	}
	return value
}
func validGateCommand(c interfaces.AgentExecutionGateCommand) bool {
	switch c.ScopeKind {
	case "global":
		return c.BusinessID == "" && c.AgentID == ""
	case "business":
		return validUUID(c.BusinessID) && c.AgentID == ""
	case "agent":
		return validUUID(c.BusinessID) && validUUID(c.AgentID)
	default:
		return false
	}
}
func governanceUniqueViolation(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var postgresError *pq.Error
	return errors.As(err, &postgresError) && string(postgresError.Code) == "23505"
}
func runResult(run models.AIAgentRun, replayed bool) *interfaces.AgentRunResult {
	return &interfaces.AgentRunResult{ID: run.ID, Status: run.Status, FinalDisposition: run.FinalDisposition, Replayed: replayed}
}
func reservationResult(row models.AISpendReservation, replayed bool) *interfaces.AgentSpendReservationResult {
	return &interfaces.AgentSpendReservationResult{ID: row.ID, Status: row.Status, Replayed: replayed}
}
func toolResult(row models.AIToolExecution, replayed bool) *interfaces.AgentToolExecutionResult {
	return &interfaces.AgentToolExecutionResult{ID: row.ID, Status: row.Status, FinalDisposition: row.FinalDisposition, Replayed: replayed}
}
