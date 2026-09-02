package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/services"

	"github.com/gin-gonic/gin"
)

type AccountingHandler struct{ service *services.AccountingService }

func NewAccountingHandler(service *services.AccountingService) *AccountingHandler {
	return &AccountingHandler{service: service}
}

// GetPolicy returns the business fiscal lock and reversal policy.
// @Summary Get accounting period policy
// @Tags Accounting
// @Security BearerAuth
// @Success 200 {object} models.AccountingPeriodPolicy
// @Router /accounting/policy [get]
func (h *AccountingHandler) GetPolicy(c *gin.Context) {
	policy, err := h.service.GetPolicy(c.Request.Context(), middleware.GetBusinessID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "accounting policy unavailable"})
		return
	}
	c.JSON(http.StatusOK, policy)
}

// SetPolicy updates the business fiscal lock and reversal policy.
// @Summary Set accounting period policy
// @Tags Accounting
// @Security BearerAuth
// @Param Idempotency-Key header string true "Command identity"
// @Param X-Step-Up-Token header string true "Scoped step-up token"
// @Param X-Lock-Override-Reason header string true "Policy change reason"
// @Param input body services.AccountingPolicyInput true "Policy"
// @Success 200 {object} models.AccountingPeriodPolicy
// @Router /accounting/policy [put]
func (h *AccountingHandler) SetPolicy(c *gin.Context) {
	var input services.AccountingPolicyInput
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid accounting policy"})
		return
	}
	auth := accountingPostingAuthorization(c, "")
	policy, err := h.service.SetPolicy(c.Request.Context(), middleware.GetBusinessID(c), middleware.GetUserID(c), input, auth)
	if errors.Is(err, services.ErrAccountingPeriodLocked) {
		writeAccountingStepUpRequired(c)
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid accounting policy"})
		return
	}
	c.JSON(http.StatusOK, policy)
}

// ListAccounts returns the tenant chart of accounts hierarchy.
// @Summary List accounting accounts
// @Tags Accounting
// @Security BearerAuth
// @Success 200 {array} models.AccountingAccount
// @Router /accounting/accounts [get]
func (h *AccountingHandler) ListAccounts(c *gin.Context) {
	rows, err := h.service.ListAccounts(c.Request.Context(), middleware.GetBusinessID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "accounting accounts unavailable"})
		return
	}
	c.JSON(http.StatusOK, rows)
}

// UpsertAccount creates or updates authoritative account class and hierarchy metadata.
// @Summary Upsert accounting account
// @Tags Accounting
// @Security BearerAuth
// @Param code path string true "Account code"
// @Param input body services.AccountingAccountInput true "Account metadata"
// @Success 200 {object} models.AccountingAccount
// @Router /accounting/accounts/{code} [put]
func (h *AccountingHandler) UpsertAccount(c *gin.Context) {
	var input services.AccountingAccountInput
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid accounting account"})
		return
	}
	account, err := h.service.UpsertAccount(c.Request.Context(), middleware.GetBusinessID(c), middleware.GetUserID(c), c.Param("code"), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, account)
}

// PostOpeningBalance posts an idempotent opening journal and inventory facts.
// @Summary Post opening balances
// @Tags Accounting
// @Security BearerAuth
// @Param X-Step-Up-Token header string false "Required for locked-period override"
// @Param X-Lock-Override-Reason header string false "Required for locked-period override"
// @Param input body services.OpeningBalanceInput true "Opening balances"
// @Success 201 {object} models.Journal
// @Failure 428 {object} map[string]string
// @Router /accounting/opening-balances [post]
func (h *AccountingHandler) PostOpeningBalance(c *gin.Context) {
	var input services.OpeningBalanceInput
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid opening balance"})
		return
	}
	input.Authorization = accountingPostingAuthorization(c, "opening:"+input.IdempotencyKey)
	journal, err := h.service.PostOpeningBalance(c.Request.Context(), middleware.GetBusinessID(c), middleware.GetUserID(c), input)
	if errors.Is(err, services.ErrAccountingPeriodLocked) {
		writeAccountingStepUpRequired(c)
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "opening balance could not be posted"})
		return
	}
	c.JSON(http.StatusCreated, journal)
}

// Diagnostics returns read-only accounting reconciliation differences.
// @Summary Accounting reconciliation diagnostics
// @Tags Accounting
// @Security BearerAuth
// @Param through query string false "Through date (RFC3339)"
// @Param currency query string true "Three-letter reconciliation currency"
// @Success 200 {object} services.ReconciliationDiagnostics
// @Router /accounting/reconciliation-diagnostics [get]
func (h *AccountingHandler) Diagnostics(c *gin.Context) {
	through := time.Now().UTC()
	if raw := strings.TrimSpace(c.Query("through")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid through date"})
			return
		}
		through = parsed
	}
	currency := strings.ToUpper(strings.TrimSpace(c.Query("currency")))
	result, err := h.service.Diagnostics(c.Request.Context(), middleware.GetBusinessID(c), currency, through)
	if err != nil {
		if currency == "" || len(currency) != 3 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "valid currency is required"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reconciliation diagnostics unavailable"})
		return
	}
	c.JSON(http.StatusOK, result)
}

type bankAccountRequest struct {
	Name          string `json:"name" binding:"required"`
	Currency      string `json:"currency" binding:"required,len=3"`
	MaskedAccount string `json:"masked_account" binding:"required"`
	LedgerAccount string `json:"ledger_account" binding:"required"`
	OpeningMinor  int64  `json:"opening_minor"`
}

// CreateBankAccount creates a tenant-scoped bank account.
// @Summary Create bank account
// @Tags Banking
// @Security BearerAuth
// @Param input body bankAccountRequest true "Bank account"
// @Success 201 {object} models.BankAccount
// @Router /accounting/bank-accounts [post]
func (h *AccountingHandler) CreateBankAccount(c *gin.Context) {
	var input bankAccountRequest
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid bank account"})
		return
	}
	account, err := h.service.CreateBankAccount(c.Request.Context(), middleware.GetBusinessID(c), middleware.GetUserID(c), services.BankAccountInput{Name: input.Name, Currency: input.Currency, MaskedAccount: input.MaskedAccount, LedgerAccount: input.LedgerAccount, OpeningMinor: input.OpeningMinor})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bank account could not be created"})
		return
	}
	c.JSON(http.StatusCreated, account)
}

// ListBankAccounts lists tenant-scoped bank accounts.
// @Summary List bank accounts
// @Tags Banking
// @Security BearerAuth
// @Success 200 {array} models.BankAccount
// @Router /accounting/bank-accounts [get]
func (h *AccountingHandler) ListBankAccounts(c *gin.Context) {
	rows, err := h.service.ListBankAccounts(c.Request.Context(), middleware.GetBusinessID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bank accounts unavailable"})
		return
	}
	c.JSON(http.StatusOK, rows)
}

// ImportBankStatement imports verified statement transactions idempotently.
// @Summary Import bank statement
// @Tags Banking
// @Security BearerAuth
// @Param input body services.BankStatementInput true "Statement"
// @Success 201 {object} models.BankStatement
// @Router /accounting/bank-statements [post]
func (h *AccountingHandler) ImportBankStatement(c *gin.Context) {
	var input services.BankStatementInput
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid bank statement"})
		return
	}
	statement, err := h.service.ImportBankStatement(c.Request.Context(), middleware.GetBusinessID(c), middleware.GetUserID(c), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bank statement could not be imported"})
		return
	}
	c.JSON(http.StatusCreated, statement)
}

// ListBankTransactions lists statement transactions and match state.
// @Summary List bank statement transactions
// @Tags Banking
// @Security BearerAuth
// @Param id path string true "Bank statement ID"
// @Param unreconciled query bool false "Only unreconciled"
// @Success 200 {array} services.BankTransactionState
// @Router /accounting/bank-statements/{id}/transactions [get]
func (h *AccountingHandler) ListBankTransactions(c *gin.Context) {
	rows, err := h.service.ListBankTransactions(c.Request.Context(), middleware.GetBusinessID(c), c.Param("id"), c.Query("unreconciled") == "true")
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bank statement not found"})
		return
	}
	c.JSON(http.StatusOK, rows)
}

// ListAudit returns tenant-scoped accounting audit events.
// @Summary List accounting audit
// @Tags Accounting
// @Security BearerAuth
// @Success 200 {array} models.AccountingAuditEvent
// @Router /accounting/audit [get]
func (h *AccountingHandler) ListAudit(c *gin.Context) {
	rows, err := h.service.ListAccountingAudit(c.Request.Context(), middleware.GetBusinessID(c), 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "accounting audit unavailable"})
		return
	}
	c.JSON(http.StatusOK, rows)
}

// SuggestBankMatches returns bounded exact and fuzzy suggestions without mutation.
// @Summary Suggest bank matches
// @Tags Banking
// @Security BearerAuth
// @Param id path string true "Bank transaction ID"
// @Success 200 {array} services.BankMatchSuggestion
// @Router /accounting/bank-transactions/{id}/suggestions [get]
func (h *AccountingHandler) SuggestBankMatches(c *gin.Context) {
	rows, err := h.service.SuggestBankMatches(c.Request.Context(), middleware.GetBusinessID(c), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bank transaction not found"})
		return
	}
	c.JSON(http.StatusOK, rows)
}

type bankMatchRequest struct {
	LedgerEntryID string `json:"ledger_entry_id" binding:"required,uuid"`
	Reason        string `json:"reason" binding:"required"`
}
type bankReasonRequest struct {
	Reason string `json:"reason" binding:"required"`
}
type reconcileStatementRequest struct {
	ReconciliationDate time.Time `json:"reconciliation_date" binding:"required"`
}

// MatchBankTransaction manually matches one bank transaction.
// @Summary Match bank transaction
// @Tags Banking
// @Security BearerAuth
// @Param id path string true "Bank transaction ID"
// @Param input body bankMatchRequest true "Match"
// @Success 201 {object} models.BankMatch
// @Router /accounting/bank-transactions/{id}/match [post]
func (h *AccountingHandler) MatchBankTransaction(c *gin.Context) {
	var input bankMatchRequest
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid bank match"})
		return
	}
	row, err := h.service.MatchBankTransaction(c.Request.Context(), middleware.GetBusinessID(c), middleware.GetUserID(c), c.Param("id"), input.LedgerEntryID, input.Reason)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "bank transaction could not be matched"})
		return
	}
	c.JSON(http.StatusCreated, row)
}

// UnmatchBankTransaction removes one active manual match with audit.
// @Summary Unmatch bank transaction
// @Tags Banking
// @Security BearerAuth
// @Param id path string true "Bank transaction ID"
// @Param input body bankReasonRequest true "Reason"
// @Success 204
// @Router /accounting/bank-transactions/{id}/match [delete]
func (h *AccountingHandler) UnmatchBankTransaction(c *gin.Context) {
	var input bankReasonRequest
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid unmatch"})
		return
	}
	if err := h.service.UnmatchBankTransaction(c.Request.Context(), middleware.GetBusinessID(c), middleware.GetUserID(c), c.Param("id"), input.Reason); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "bank transaction could not be unmatched"})
		return
	}
	c.Status(http.StatusNoContent)
}

// CreateBankAdjustment posts a fee or interest adjustment and matches it.
// @Summary Create bank adjustment
// @Tags Banking
// @Security BearerAuth
// @Param id path string true "Bank transaction ID"
// @Param X-Step-Up-Token header string false "Required for locked-period override"
// @Param X-Lock-Override-Reason header string false "Required for locked-period override"
// @Param input body services.BankAdjustmentInput true "Adjustment"
// @Success 201 {object} models.Journal
// @Failure 428 {object} map[string]string
// @Router /accounting/bank-transactions/{id}/adjustment [post]
func (h *AccountingHandler) CreateBankAdjustment(c *gin.Context) {
	var input services.BankAdjustmentInput
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid bank adjustment"})
		return
	}
	input.Authorization = accountingPostingAuthorization(c, "bank-adjustment:"+c.Param("id"))
	journal, err := h.service.CreateBankAdjustment(c.Request.Context(), middleware.GetBusinessID(c), middleware.GetUserID(c), c.Param("id"), input)
	if errors.Is(err, services.ErrAccountingPeriodLocked) {
		writeAccountingStepUpRequired(c)
		return
	}
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "bank adjustment could not be posted"})
		return
	}
	c.JSON(http.StatusCreated, journal)
}

// ReconcileStatement closes a fully matched statement.
// @Summary Reconcile bank statement
// @Tags Banking
// @Security BearerAuth
// @Param id path string true "Bank statement ID"
// @Param input body reconcileStatementRequest true "Reconciliation date"
// @Success 200 {object} models.BankStatement
// @Router /accounting/bank-statements/{id}/reconcile [post]
func (h *AccountingHandler) ReconcileStatement(c *gin.Context) {
	var input reconcileStatementRequest
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reconciliation date"})
		return
	}
	row, err := h.service.ReconcileStatement(c.Request.Context(), middleware.GetBusinessID(c), middleware.GetUserID(c), c.Param("id"), input.ReconciliationDate)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "bank statement could not be reconciled"})
		return
	}
	c.JSON(http.StatusOK, row)
}

func accountingPostingAuthorization(c *gin.Context, resource string) services.PostingAuthorization {
	return services.PostingAuthorization{Subject: middleware.GetUserID(c), Action: "accounting_lock_override", Resource: resource, CommandIdentity: strings.TrimSpace(c.GetHeader("Idempotency-Key")), Token: strings.TrimSpace(c.GetHeader("X-Step-Up-Token")), Reason: strings.TrimSpace(c.GetHeader("X-Lock-Override-Reason"))}
}

func writeAccountingStepUpRequired(c *gin.Context) {
	c.JSON(http.StatusPreconditionRequired, gin.H{"error": "scoped step-up and override reason are required", "code": "accounting_period_locked"})
}
