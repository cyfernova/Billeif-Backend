package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"invoice-backend/internal/models"
	interfaces "invoice-backend/internal/repositories/interfaces"
)

type ap2Repository struct {
	db  *gorm.DB
	log *logger.Logger
}

const defaultUnpaginatedQueryLimit = 500

var activeBargainingStatuses = []string{"initiated", "in_progress", "running"}

func NewAP2Repository(db *gorm.DB) interfaces.AP2Repository {
	return &ap2Repository{
		db:  db,
		log: logger.Global().Named("ap2_repository"),
	}
}

// Intent Mandates
func (r *ap2Repository) CreateIntentMandate(ctx context.Context, mandate *models.IntentMandate) error {
	return r.db.WithContext(ctx).Create(mandate).Error
}

func (r *ap2Repository) GetIntentMandateByID(ctx context.Context, id, userID string) (*models.IntentMandate, error) {
	var mandate models.IntentMandate
	err := r.db.WithContext(ctx).Preload("CartMandates").Where("id = ? AND user_id = ?", id, userID).First(&mandate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("intent mandate not found")
	}
	return &mandate, err
}

func (r *ap2Repository) GetActiveIntentMandatesByUser(ctx context.Context, userID string) ([]*models.IntentMandate, error) {
	var mandates []models.IntentMandate
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND status = ? AND expires_at > NOW()", userID, "active").
		Find(&mandates).Error
	if err != nil {
		return nil, err
	}
	result := make([]*models.IntentMandate, len(mandates))
	for i := range mandates {
		result[i] = &mandates[i]
	}
	return result, nil
}

func (r *ap2Repository) GetIntentMandatesByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.IntentMandate, int64, error) {
	var mandates []models.IntentMandate
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.IntentMandate{}).Where("agent_id = ?", agentID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&mandates).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.IntentMandate, len(mandates))
	for i := range mandates {
		result[i] = &mandates[i]
	}
	return result, total, nil
}

func (r *ap2Repository) UpdateIntentMandateStatus(ctx context.Context, id, status string) error {
	return r.db.WithContext(ctx).Model(&models.IntentMandate{}).Where("id = ?", id).Update("status", status).Error
}

func (r *ap2Repository) RevokeIntentMandate(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&models.IntentMandate{}).Where("id = ?", id).Update("status", "revoked").Error
}

// Cart Mandates
func (r *ap2Repository) CreateCartMandate(ctx context.Context, mandate *models.CartMandate) error {
	return r.db.WithContext(ctx).Create(mandate).Error
}

func (r *ap2Repository) GetCartMandateByID(ctx context.Context, id, userID string) (*models.CartMandate, error) {
	var mandate models.CartMandate
	err := r.db.WithContext(ctx).Preload("PaymentMandates").Where("id = ? AND user_id = ?", id, userID).First(&mandate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("cart mandate not found")
	}
	return &mandate, err
}

func (r *ap2Repository) GetCartMandateByMerchant(ctx context.Context, id, merchantID string) (*models.CartMandate, error) {
	var mandate models.CartMandate
	err := r.db.WithContext(ctx).Preload("PaymentMandates").Where("id = ? AND merchant_id = ?", id, merchantID).First(&mandate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("cart mandate not found")
	}
	return &mandate, err
}

func (r *ap2Repository) GetCartMandatesByUser(ctx context.Context, userID string, page, limit int) ([]*models.CartMandate, int64, error) {
	var mandates []models.CartMandate
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.CartMandate{}).Where("user_id = ?", userID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&mandates).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.CartMandate, len(mandates))
	for i := range mandates {
		result[i] = &mandates[i]
	}
	return result, total, nil
}

func (r *ap2Repository) UpdateCartMandate(ctx context.Context, mandate *models.CartMandate) error {
	return r.db.WithContext(ctx).Save(mandate).Error
}

func (r *ap2Repository) SignCartMandate(ctx context.Context, id, signature string) error {
	return r.db.WithContext(ctx).Model(&models.CartMandate{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"merchant_signature": signature,
			"status":             "signed",
		}).Error
}

func (r *ap2Repository) GetPendingCartMandates(ctx context.Context, merchantID string) ([]*models.CartMandate, error) {
	var mandates []models.CartMandate
	err := r.db.WithContext(ctx).
		Where("merchant_id = ? AND status = ?", merchantID, "pending").
		Order("created_at DESC").
		Limit(defaultUnpaginatedQueryLimit).
		Find(&mandates).Error
	if err != nil {
		return nil, err
	}
	result := make([]*models.CartMandate, len(mandates))
	for i := range mandates {
		result[i] = &mandates[i]
	}
	return result, nil
}

// Payment Mandates
func (r *ap2Repository) CreatePaymentMandate(ctx context.Context, mandate *models.PaymentMandate) error {
	return r.db.WithContext(ctx).Create(mandate).Error
}

func (r *ap2Repository) GetPaymentMandateByID(ctx context.Context, id string) (*models.PaymentMandate, error) {
	var mandate models.PaymentMandate
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&mandate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("payment mandate not found")
	}
	return &mandate, err
}

func (r *ap2Repository) GetPaymentMandatesByUser(ctx context.Context, userID string, page, limit int) ([]*models.PaymentMandate, int64, error) {
	var mandates []models.PaymentMandate
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.PaymentMandate{}).Where("user_id = ?", userID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&mandates).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.PaymentMandate, len(mandates))
	for i := range mandates {
		result[i] = &mandates[i]
	}
	return result, total, nil
}

func (r *ap2Repository) UpdatePaymentMandate(ctx context.Context, mandate *models.PaymentMandate) error {
	return r.db.WithContext(ctx).Save(mandate).Error
}

func (r *ap2Repository) UpdatePaymentMandateStatus(ctx context.Context, id, status string) error {
	updates := map[string]interface{}{"status": status}
	if status == "captured" {
		now := "NOW()"
		updates["processed_at"] = gorm.Expr(now)
	}
	return r.db.WithContext(ctx).Model(&models.PaymentMandate{}).Where("id = ?", id).Updates(updates).Error
}

func (r *ap2Repository) GetPaymentMandateByRazorpayOrder(ctx context.Context, orderID string) (*models.PaymentMandate, error) {
	var mandate models.PaymentMandate
	err := r.db.WithContext(ctx).Where("razorpay_order_id = ?", orderID).First(&mandate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("payment mandate not found")
	}
	return &mandate, err
}

// Agents
func (r *ap2Repository) CreateAgent(ctx context.Context, agent *models.Agent) error {
	return r.db.WithContext(ctx).Create(agent).Error
}

func (r *ap2Repository) GetAgentByID(ctx context.Context, id string) (*models.Agent, error) {
	var agent models.Agent
	err := r.db.WithContext(ctx).
		Preload("AgentCapabilities").
		Preload("MarketplaceProducts").
		Where("id = ? AND deleted_at IS NULL", id).
		First(&agent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfaces.ErrAgentNotFound
	}
	return &agent, err
}

func (r *ap2Repository) GetAgentByIDForOwnerAndBusiness(ctx context.Context, id, ownerID, businessID string) (*models.Agent, error) {
	var agent models.Agent
	err := r.db.WithContext(ctx).
		Preload("AgentCapabilities").
		Preload("MarketplaceProducts").
		Where("id = ? AND owner_id = ? AND business_id = ? AND is_active = ? AND deleted_at IS NULL", id, ownerID, businessID, true).
		First(&agent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfaces.ErrA2ANegotiationScopeNotFound
	}
	return &agent, err
}

func (r *ap2Repository) HasAgentOwnership(ctx context.Context, ownerID, agentID string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).
		Model(&models.Agent{}).
		Where("owner_id = ? AND id = ? AND deleted_at IS NULL", ownerID, agentID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *ap2Repository) GetAgentsByUser(ctx context.Context, userID string, page, limit int) ([]*models.Agent, int64, error) {
	var agents []models.Agent
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.Agent{}).Where("owner_id = ? AND deleted_at IS NULL", userID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&agents).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.Agent, len(agents))
	for i := range agents {
		result[i] = &agents[i]
	}
	return result, total, nil
}

func (r *ap2Repository) GetAgentsByBusiness(ctx context.Context, businessID string, page, limit int) ([]*models.Agent, int64, error) {
	var agents []models.Agent
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.Agent{}).Where("business_id = ? AND deleted_at IS NULL", businessID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&agents).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.Agent, len(agents))
	for i := range agents {
		result[i] = &agents[i]
	}
	return result, total, nil
}

func (r *ap2Repository) GetAgentsByType(ctx context.Context, agentType string, page, limit int) ([]*models.Agent, int64, error) {
	var agents []models.Agent
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.Agent{}).Where("type = ? AND deleted_at IS NULL", agentType)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&agents).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.Agent, len(agents))
	for i := range agents {
		result[i] = &agents[i]
	}
	return result, total, nil
}

func (r *ap2Repository) GetAgents(ctx context.Context, page, limit int) ([]*models.Agent, int64, error) {
	var agents []models.Agent
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.Agent{}).Where("deleted_at IS NULL")

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&agents).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.Agent, len(agents))
	for i := range agents {
		result[i] = &agents[i]
	}
	return result, total, nil
}

func (r *ap2Repository) SearchAgentsByCategories(ctx context.Context, categories []string, budget *float64, page, limit int) ([]*models.Agent, int64, error) {
	var agents []models.Agent
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.Agent{}).Where("deleted_at IS NULL")

	// Filter by categories (intersection: agent must have at least one matching category)
	if len(categories) > 0 {
		query = query.Where("categories && ?", pq.Array(categories))
	}

	// Filter by budget: price <= budget
	if budget != nil && *budget > 0 {
		query = query.Where("price <= ?", *budget)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&agents).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.Agent, len(agents))
	for i := range agents {
		result[i] = &agents[i]
	}
	return result, total, nil
}

func (r *ap2Repository) SearchAgentsByBudget(ctx context.Context, budget float64, page, limit int) ([]*models.Agent, int64, error) {
	var agents []models.Agent
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.Agent{}).Where("deleted_at IS NULL").Where("price <= ?", budget)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&agents).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.Agent, len(agents))
	for i := range agents {
		result[i] = &agents[i]
	}
	return result, total, nil
}

func (r *ap2Repository) GetActiveAgentsByType(ctx context.Context, agentType string) ([]*models.Agent, error) {
	var agents []models.Agent
	err := r.db.WithContext(ctx).
		Where("type = ? AND is_active = ? AND deleted_at IS NULL", agentType, true).
		Limit(defaultUnpaginatedQueryLimit).
		Find(&agents).Error
	if err != nil {
		return nil, err
	}
	result := make([]*models.Agent, len(agents))
	for i := range agents {
		result[i] = &agents[i]
	}
	return result, nil
}

func (r *ap2Repository) UpdateAgent(ctx context.Context, agent *models.Agent) error {
	return r.db.WithContext(ctx).Save(agent).Error
}

func (r *ap2Repository) DeleteAgent(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.Agent{}, id).Error
}

func (r *ap2Repository) CreateAgentWithCapabilities(ctx context.Context, agent *models.Agent, capabilities []*models.AgentCapability) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(agent).Error; err != nil {
			return err
		}
		for _, cap := range capabilities {
			cap.AgentID = agent.ID
			if err := tx.Create(cap).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *ap2Repository) CreateAgentWithDiscovery(ctx context.Context, agent *models.Agent, capabilities []*models.AgentCapability, registry *models.AgentRegistry) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(agent).Error; err != nil {
			return err
		}
		for _, cap := range capabilities {
			cap.AgentID = agent.ID
			if err := tx.Create(cap).Error; err != nil {
				return err
			}
		}
		if registry != nil {
			if err := tx.Create(registry).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// Agent Capabilities
func (r *ap2Repository) CreateAgentCapability(ctx context.Context, capability *models.AgentCapability) error {
	return r.db.WithContext(ctx).Create(capability).Error
}

func (r *ap2Repository) GetCapabilitiesByAgent(ctx context.Context, agentID string) ([]*models.AgentCapability, error) {
	var capabilities []models.AgentCapability
	// Capabilities are intentionally fetched without LIMIT because cardinality is expected to stay small.
	err := r.db.WithContext(ctx).Where("agent_id = ?", agentID).Find(&capabilities).Error
	if err != nil {
		return nil, err
	}
	result := make([]*models.AgentCapability, len(capabilities))
	for i := range capabilities {
		result[i] = &capabilities[i]
	}
	return result, nil
}

func (r *ap2Repository) DeleteCapability(ctx context.Context, agentID, capabilityID string) error {
	result := r.db.WithContext(ctx).
		Where("agent_id = ? AND id = ?", agentID, capabilityID).
		Delete(&models.AgentCapability{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("capability not found")
	}
	return nil
}

// Credentials
func (r *ap2Repository) CreatePaymentCredential(ctx context.Context, credential *models.PaymentCredential) error {
	return r.db.WithContext(ctx).Create(credential).Error
}

func (r *ap2Repository) GetPaymentCredentialsByUser(ctx context.Context, userID string) ([]*models.PaymentCredential, error) {
	var credentials []models.PaymentCredential
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(defaultUnpaginatedQueryLimit).
		Find(&credentials).Error
	if err != nil {
		return nil, err
	}
	result := make([]*models.PaymentCredential, len(credentials))
	for i := range credentials {
		result[i] = &credentials[i]
	}
	return result, nil
}

func (r *ap2Repository) GetPaymentCredentialByID(ctx context.Context, id string) (*models.PaymentCredential, error) {
	var credential models.PaymentCredential
	err := r.db.WithContext(ctx).Preload("CredentialTokens").Where("id = ?", id).First(&credential).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("payment credential not found")
	}
	return &credential, err
}

func (r *ap2Repository) GetDefaultCredential(ctx context.Context, userID string) (*models.PaymentCredential, error) {
	var credential models.PaymentCredential
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND is_default = ? AND is_active = ?", userID, true, true).
		First(&credential).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("default credential not found")
	}
	return &credential, err
}

func (r *ap2Repository) UpdateCredential(ctx context.Context, id string, updates map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&models.PaymentCredential{}).Where("id = ?", id).Updates(updates).Error
}

func (r *ap2Repository) DeleteCredential(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.PaymentCredential{}, id).Error
}

func (r *ap2Repository) SetDefaultCredential(ctx context.Context, userID, credentialID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.PaymentCredential{}).
			Where("user_id = ?", userID).
			Update("is_default", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.PaymentCredential{}).
			Where("id = ? AND user_id = ?", credentialID, userID).
			Update("is_default", true).Error; err != nil {
			return err
		}
		return nil
	})
}

// Credential Tokens
func (r *ap2Repository) CreateCredentialToken(ctx context.Context, token *models.CredentialToken) error {
	if token.TokenHash == "" && token.Token != "" {
		token.TokenHash = token.Token
	}
	return r.db.WithContext(ctx).Create(token).Error
}

func (r *ap2Repository) GetCredentialToken(ctx context.Context, tokenStr string) (*models.CredentialToken, error) {
	var token models.CredentialToken
	err := r.db.WithContext(ctx).Where("token_hash = ? AND is_used = ? AND expires_at > NOW()", tokenStr, false).First(&token).Error
	// Backward compatibility before token_hash migration is applied.
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "token_hash") {
		err = r.db.WithContext(ctx).Where("token = ? AND is_used = ? AND expires_at > NOW()", tokenStr, false).First(&token).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("token not found or expired")
	}
	return &token, err
}

func (r *ap2Repository) MarkTokenAsUsed(ctx context.Context, tokenID string) error {
	return r.db.WithContext(ctx).Model(&models.CredentialToken{}).Where("id = ?", tokenID).Update("is_used", true).Error
}

func (r *ap2Repository) GetValidTokensByCredential(ctx context.Context, credentialID string) ([]*models.CredentialToken, error) {
	var tokens []models.CredentialToken
	err := r.db.WithContext(ctx).
		Where("credential_id = ? AND is_used = ? AND expires_at > NOW()", credentialID, false).
		Order("expires_at DESC").
		Limit(defaultUnpaginatedQueryLimit).
		Find(&tokens).Error
	if err != nil {
		return nil, err
	}
	result := make([]*models.CredentialToken, len(tokens))
	for i := range tokens {
		result[i] = &tokens[i]
	}
	return result, nil
}

// Marketplace
func (r *ap2Repository) CreateMarketplaceProduct(ctx context.Context, product *models.MarketplaceProduct) error {
	return r.db.WithContext(ctx).Create(product).Error
}

func (r *ap2Repository) GetMarketplaceProductByID(ctx context.Context, id string) (*models.MarketplaceProduct, error) {
	var product models.MarketplaceProduct
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&product).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("product not found")
	}
	return &product, err
}

func (r *ap2Repository) GetMarketplaceProducts(ctx context.Context, filters map[string]interface{}, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	var products []models.MarketplaceProduct
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.MarketplaceProduct{})

	for key, value := range filters {
		query = query.Where(key+" = ?", value)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&products).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.MarketplaceProduct, len(products))
	for i := range products {
		result[i] = &products[i]
	}
	return result, total, nil
}

func (r *ap2Repository) SearchMarketplaceProducts(ctx context.Context, query string, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	var products []models.MarketplaceProduct
	var total int64

	offset := (page - 1) * limit
	dbQuery := r.db.WithContext(ctx).Model(&models.MarketplaceProduct{}).
		Where("is_available = ?", true)

	if query != "" {
		dbQuery = dbQuery.Where("name ILIKE ? OR description ILIKE ?", "%"+query+"%", "%"+query+"%")
	}

	if err := dbQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := dbQuery.Order("created_at DESC").Offset(offset).Limit(limit).Find(&products).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.MarketplaceProduct, len(products))
	for i := range products {
		result[i] = &products[i]
	}
	return result, total, nil
}

func (r *ap2Repository) GetProductsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	var products []models.MarketplaceProduct
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.MarketplaceProduct{}).Where("agent_id = ?", agentID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&products).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.MarketplaceProduct, len(products))
	for i := range products {
		result[i] = &products[i]
	}
	return result, total, nil
}

func (r *ap2Repository) GetAvailableProducts(ctx context.Context, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	var products []models.MarketplaceProduct
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.MarketplaceProduct{}).Where("is_available = ? AND inventory_count > ?", true, 0)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&products).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.MarketplaceProduct, len(products))
	for i := range products {
		result[i] = &products[i]
	}
	return result, total, nil
}

func (r *ap2Repository) UpdateMarketplaceProduct(ctx context.Context, product *models.MarketplaceProduct) error {
	return r.db.WithContext(ctx).Save(product).Error
}

func (r *ap2Repository) DeleteMarketplaceProduct(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.MarketplaceProduct{}, id).Error
}

func (r *ap2Repository) ReserveMarketplaceInventory(ctx context.Context, productID string, quantity int) error {
	if quantity <= 0 {
		return errors.New("quantity must be positive")
	}

	result := r.db.WithContext(ctx).
		Model(&models.MarketplaceProduct{}).
		Where("id = ? AND is_available = ? AND inventory_count - reserved_inventory_count >= ?", productID, true, quantity).
		Update("reserved_inventory_count", gorm.Expr("reserved_inventory_count + ?", quantity))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("insufficient inventory to reserve")
	}
	return nil
}

func (r *ap2Repository) ReleaseMarketplaceInventory(ctx context.Context, productID string, quantity int) error {
	if quantity <= 0 {
		return nil
	}

	result := r.db.WithContext(ctx).
		Model(&models.MarketplaceProduct{}).
		Where("id = ? AND reserved_inventory_count >= ?", productID, quantity).
		Update("reserved_inventory_count", gorm.Expr("reserved_inventory_count - ?", quantity))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("reserved inventory not found")
	}
	return nil
}

func (r *ap2Repository) CommitMarketplaceInventory(ctx context.Context, productID string, quantity int) error {
	if quantity <= 0 {
		return nil
	}

	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var product models.MarketplaceProduct
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", productID).First(&product).Error; err != nil {
		tx.Rollback()
		return err
	}

	if product.ReservedInventoryCount < quantity {
		tx.Rollback()
		return errors.New("reserved inventory below requested quantity")
	}
	if product.InventoryCount < quantity {
		tx.Rollback()
		return errors.New("inventory below requested quantity")
	}

	product.ReservedInventoryCount -= quantity
	product.InventoryCount -= quantity
	if product.InventoryCount <= 0 {
		product.IsAvailable = false
	}

	if err := tx.Save(&product).Error; err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit().Error
}

// Procurement
func (r *ap2Repository) CreateProcurementRun(ctx context.Context, run *models.ProcurementRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}

func (r *ap2Repository) GetProcurementRunByID(ctx context.Context, id, userID string) (*models.ProcurementRun, error) {
	var run models.ProcurementRun
	err := r.db.WithContext(ctx).
		Preload("Candidates").
		Preload("Candidates.MerchantAgent").
		Preload("Candidates.MarketplaceProduct").
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", id, userID).
		First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("procurement run not found")
	}
	return &run, err
}

func (r *ap2Repository) GetProcurementRunByIdempotencyKey(ctx context.Context, userID, shoppingAgentID, idempotencyKey string) (*models.ProcurementRun, error) {
	var run models.ProcurementRun
	err := r.db.WithContext(ctx).
		Preload("Candidates").
		Preload("Candidates.MerchantAgent").
		Preload("Candidates.MarketplaceProduct").
		Where("user_id = ? AND shopping_agent_id = ? AND idempotency_key = ? AND deleted_at IS NULL", userID, shoppingAgentID, idempotencyKey).
		First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("procurement run not found")
	}
	return &run, err
}

func (r *ap2Repository) UpdateProcurementRun(ctx context.Context, run *models.ProcurementRun) error {
	return r.db.WithContext(ctx).Save(run).Error
}

func (r *ap2Repository) CreateProcurementCandidate(ctx context.Context, candidate *models.ProcurementCandidate) error {
	return r.db.WithContext(ctx).Create(candidate).Error
}

func (r *ap2Repository) UpdateProcurementCandidate(ctx context.Context, candidate *models.ProcurementCandidate) error {
	return r.db.WithContext(ctx).Save(candidate).Error
}

func (r *ap2Repository) GetProcurementCandidatesByRun(ctx context.Context, runID string) ([]*models.ProcurementCandidate, error) {
	var candidates []models.ProcurementCandidate
	if err := r.db.WithContext(ctx).
		Preload("MerchantAgent").
		Preload("MarketplaceProduct").
		Where("procurement_run_id = ? AND deleted_at IS NULL", runID).
		Order("match_score DESC, created_at ASC").
		Find(&candidates).Error; err != nil {
		return nil, fmt.Errorf("get procurement candidates: %w", err)
	}

	result := make([]*models.ProcurementCandidate, len(candidates))
	for i := range candidates {
		result[i] = &candidates[i]
	}
	return result, nil
}

// Marketplace Orders
func (r *ap2Repository) CreateOrder(ctx context.Context, order *models.MarketplaceOrder) error {
	return r.db.WithContext(ctx).Preload("CartMandate").Create(order).Error
}

func (r *ap2Repository) GetOrderByID(ctx context.Context, id string) (*models.MarketplaceOrder, error) {
	var order models.MarketplaceOrder
	err := r.db.WithContext(ctx).Preload("CartMandate").Where("id = ?", id).First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("order not found")
	}
	return &order, err
}

func (r *ap2Repository) GetOrderByIDForUser(ctx context.Context, id, userID string) (*models.MarketplaceOrder, error) {
	var order models.MarketplaceOrder
	err := r.db.WithContext(ctx).Preload("CartMandate").Where("id = ? AND user_id = ?", id, userID).First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("order not found")
	}
	return &order, err
}

func (r *ap2Repository) GetOrdersByUser(ctx context.Context, userID string, page, limit int) ([]*models.MarketplaceOrder, int64, error) {
	var orders []models.MarketplaceOrder
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.MarketplaceOrder{}).Where("user_id = ?", userID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&orders).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.MarketplaceOrder, len(orders))
	for i := range orders {
		result[i] = &orders[i]
	}
	return result, total, nil
}

func (r *ap2Repository) GetOrdersByUserAndStatus(ctx context.Context, userID, status string, page, limit int) ([]*models.MarketplaceOrder, int64, error) {
	var orders []models.MarketplaceOrder
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.MarketplaceOrder{}).Where("user_id = ? AND status = ?", userID, status)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&orders).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.MarketplaceOrder, len(orders))
	for i := range orders {
		result[i] = &orders[i]
	}
	return result, total, nil
}

func (r *ap2Repository) GetOrdersByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.MarketplaceOrder, int64, error) {
	var orders []models.MarketplaceOrder
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.MarketplaceOrder{}).Where("shopping_agent_id = ? OR merchant_agent_id = ?", agentID, agentID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&orders).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.MarketplaceOrder, len(orders))
	for i := range orders {
		result[i] = &orders[i]
	}
	return result, total, nil
}

func (r *ap2Repository) UpdateOrder(ctx context.Context, order *models.MarketplaceOrder) error {
	return r.db.WithContext(ctx).Save(order).Error
}

func (r *ap2Repository) UpdateOrderStatus(ctx context.Context, orderID, status string) error {
	updates := map[string]interface{}{"status": status}
	if status == "delivered" {
		now := "NOW()"
		updates["delivered_at"] = gorm.Expr(now)
	}
	return r.db.WithContext(ctx).Model(&models.MarketplaceOrder{}).Where("id = ?", orderID).Updates(updates).Error
}

func (r *ap2Repository) GetOrderByCartMandate(ctx context.Context, cartMandateID string) (*models.MarketplaceOrder, error) {
	var order models.MarketplaceOrder
	err := r.db.WithContext(ctx).Where("cart_mandate_id = ?", cartMandateID).First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("order not found")
	}
	return &order, err
}

// A2A Messages
func (r *ap2Repository) CreateA2AMessage(ctx context.Context, message *models.A2AMessage) error {
	return r.db.WithContext(ctx).Create(message).Error
}

func (r *ap2Repository) GetA2AMessageByID(ctx context.Context, id string) (*models.A2AMessage, error) {
	var message models.A2AMessage
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&message).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("message not found")
	}
	return &message, err
}

func (r *ap2Repository) GetMessagesBySender(ctx context.Context, senderAgentID string, page, limit int) ([]*models.A2AMessage, int64, error) {
	var messages []models.A2AMessage
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.A2AMessage{}).Where("sender_agent_id = ?", senderAgentID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&messages).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.A2AMessage, len(messages))
	for i := range messages {
		result[i] = &messages[i]
	}
	return result, total, nil
}

func (r *ap2Repository) GetMessagesByReceiver(ctx context.Context, receiverAgentID string, page, limit int) ([]*models.A2AMessage, int64, error) {
	var messages []models.A2AMessage
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.A2AMessage{}).Where("receiver_agent_id = ?", receiverAgentID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&messages).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.A2AMessage, len(messages))
	for i := range messages {
		result[i] = &messages[i]
	}
	return result, total, nil
}

func (r *ap2Repository) UpdateMessageStatus(ctx context.Context, id, status string) error {
	updates := map[string]interface{}{"status": status}
	if status == "delivered" {
		now := "NOW()"
		updates["processed_at"] = gorm.Expr(now)
	}
	return r.db.WithContext(ctx).Model(&models.A2AMessage{}).Where("id = ?", id).Updates(updates).Error
}

func (r *ap2Repository) UpdateMessageResponse(ctx context.Context, id string, response string) error {
	return r.db.WithContext(ctx).Model(&models.A2AMessage{}).Where("id = ?", id).Update("response", response).Error
}

// Agent Transactions
func (r *ap2Repository) CreateAgentTransaction(ctx context.Context, transaction *models.AgentTransaction) error {
	return r.db.WithContext(ctx).Create(transaction).Error
}

func (r *ap2Repository) GetTransactionsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.AgentTransaction, int64, error) {
	var transactions []models.AgentTransaction
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.AgentTransaction{}).Where("agent_id = ?", agentID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&transactions).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.AgentTransaction, len(transactions))
	for i := range transactions {
		result[i] = &transactions[i]
	}
	return result, total, nil
}

func (r *ap2Repository) GetTransactionsByUser(ctx context.Context, userID string, page, limit int) ([]*models.AgentTransaction, int64, error) {
	var transactions []models.AgentTransaction
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.AgentTransaction{}).Where("user_id = ?", userID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&transactions).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.AgentTransaction, len(transactions))
	for i := range transactions {
		result[i] = &transactions[i]
	}
	return result, total, nil
}

// Bargaining Negotiations
func (r *ap2Repository) CreateBargainingNegotiation(ctx context.Context, negotiation *models.BargainingNegotiation) error {
	return r.db.WithContext(ctx).Create(negotiation).Error
}

func (r *ap2Repository) GetBargainingNegotiationByID(ctx context.Context, id string) (*models.BargainingNegotiation, error) {
	var negotiation models.BargainingNegotiation
	err := r.db.WithContext(ctx).
		Preload("BuyerAgent").
		Preload("SellerAgent").
		Where("id = ?", id).
		First(&negotiation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("negotiation not found")
	}
	return &negotiation, err
}

// GetBargainingNegotiationByIDForActor returns a negotiation only when the
// actor owns it or an active negotiation agent belongs to the actor's effective
// business scope. The query intentionally treats inaccessible and missing
// negotiations alike.
func (r *ap2Repository) GetBargainingNegotiationByIDForActor(ctx context.Context, id, userID, businessID string) (*models.BargainingNegotiation, error) {
	var negotiation models.BargainingNegotiation
	err := r.db.WithContext(ctx).
		Preload("BuyerAgent").
		Preload("SellerAgent").
		Where("bargaining_negotiations.id = ?", id).
		Where(`bargaining_negotiations.user_id = ? OR (
			? <> '' AND EXISTS (
				SELECT 1 FROM agents
				WHERE agents.id IN (bargaining_negotiations.buyer_agent_id, bargaining_negotiations.seller_agent_id)
					AND agents.business_id = ?
					AND agents.deleted_at IS NULL
			)
		)`, userID, businessID, businessID).
		First(&negotiation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("negotiation not found")
	}
	return &negotiation, err
}

// GetBargainingNegotiationByIDForActorAndAgent returns a negotiation only when
// the requested participant belongs to the actor's active business scope or
// the actor owns the negotiation. It keeps the requested agent tied to the
// negotiation in the repository query so a business cannot request decisions
// for the opposite participant.
func (r *ap2Repository) GetBargainingNegotiationByIDForActorAndAgent(ctx context.Context, id, userID, businessID, agentID string) (*models.BargainingNegotiation, error) {
	var negotiation models.BargainingNegotiation
	err := r.db.WithContext(ctx).
		Preload("BuyerAgent").
		Preload("SellerAgent").
		Where("bargaining_negotiations.id = ?", id).
		Where("bargaining_negotiations.buyer_agent_id = ? OR bargaining_negotiations.seller_agent_id = ?", agentID, agentID).
		Where(`bargaining_negotiations.user_id = ? OR (
			? <> '' AND EXISTS (
				SELECT 1 FROM agents
				WHERE agents.id = ?
					AND agents.business_id = ?
					AND agents.deleted_at IS NULL
			)
		)`, userID, businessID, agentID, businessID).
		First(&negotiation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("negotiation not found")
	}
	return &negotiation, err
}

func (r *ap2Repository) GetBargainingNegotiationBySessionID(ctx context.Context, sessionID string) (*models.BargainingNegotiation, error) {
	var negotiation models.BargainingNegotiation
	err := r.db.WithContext(ctx).
		Preload("BuyerAgent").
		Preload("SellerAgent").
		Where("session_id = ?", sessionID).
		First(&negotiation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("negotiation not found")
	}
	return &negotiation, err
}

func (r *ap2Repository) GetBargainingNegotiationByIDForScope(ctx context.Context, id, userID, businessID string) (*models.BargainingNegotiation, error) {
	var negotiation models.BargainingNegotiation
	err := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ? AND business_id = ?", id, userID, businessID).
		First(&negotiation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfaces.ErrA2ANegotiationScopeNotFound
	}
	return &negotiation, err
}

func (r *ap2Repository) GetBargainingNegotiationBySessionIDForScope(ctx context.Context, sessionID, userID, businessID string) (*models.BargainingNegotiation, error) {
	var negotiation models.BargainingNegotiation
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND user_id = ? AND business_id = ?", sessionID, userID, businessID).
		First(&negotiation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfaces.ErrA2ANegotiationScopeNotFound
	}
	return &negotiation, err
}

func (r *ap2Repository) GetBargainingNegotiationBySessionAndID(ctx context.Context, sessionID, id string) (*models.BargainingNegotiation, error) {
	var negotiation models.BargainingNegotiation
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND id = ?", sessionID, id).
		First(&negotiation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfaces.ErrA2ANegotiationScopeNotFound
	}
	return &negotiation, err
}

func (r *ap2Repository) GetNegotiationsByUser(ctx context.Context, userID string, page, limit int) ([]*models.BargainingNegotiation, int64, error) {
	var negotiations []models.BargainingNegotiation
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.BargainingNegotiation{}).Where("user_id = ?", userID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.
		Preload("BuyerAgent").
		Preload("SellerAgent").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&negotiations).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.BargainingNegotiation, len(negotiations))
	for i := range negotiations {
		result[i] = &negotiations[i]
	}
	return result, total, nil
}

func (r *ap2Repository) GetNegotiationsInProgress(ctx context.Context, limit int) ([]*models.BargainingNegotiation, error) {
	var negotiations []models.BargainingNegotiation
	if err := r.db.WithContext(ctx).Model(&models.BargainingNegotiation{}).
		Where("status = ?", "in_progress").
		Where("expires_at > ?", time.Now()).
		Where("rounds < max_rounds").
		Preload("BuyerAgent").
		Preload("SellerAgent").
		Order("created_at ASC").
		Limit(limit).
		Find(&negotiations).Error; err != nil {
		return nil, err
	}
	result := make([]*models.BargainingNegotiation, len(negotiations))
	for i := range negotiations {
		result[i] = &negotiations[i]
	}
	return result, nil
}

func (r *ap2Repository) GetNegotiationsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.BargainingNegotiation, int64, error) {
	var negotiations []models.BargainingNegotiation
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.BargainingNegotiation{}).
		Where("buyer_agent_id = ? OR seller_agent_id = ?", agentID, agentID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.
		Preload("BuyerAgent").
		Preload("SellerAgent").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&negotiations).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.BargainingNegotiation, len(negotiations))
	for i := range negotiations {
		result[i] = &negotiations[i]
	}
	return result, total, nil
}

func (r *ap2Repository) UpdateNegotiationStatus(ctx context.Context, id, status string) error {
	updates := map[string]interface{}{"status": status}
	if status == "expired" {
		updates["completed_at"] = time.Now().UTC()
	}
	result := r.db.WithContext(ctx).
		Model(&models.BargainingNegotiation{}).
		Where("id = ? AND status IN ?", id, activeBargainingStatuses).
		Updates(updates)
	return bargainingMutationError(result)
}

func (r *ap2Repository) UpdateNegotiationAmountAndRounds(ctx context.Context, id string, amount float64, rounds int, status string) error {
	result := r.db.WithContext(ctx).Model(&models.BargainingNegotiation{}).
		Where("id = ? AND status IN ?", id, activeBargainingStatuses).
		Updates(map[string]interface{}{
			"current_amount": amount,
			"rounds":         rounds,
			"status":         status,
		})
	return bargainingMutationError(result)
}

func (r *ap2Repository) CompleteNegotiation(ctx context.Context, id, status string, finalAmount float64, completedAt *time.Time) error {
	result := r.db.WithContext(ctx).Model(&models.BargainingNegotiation{}).
		Where("id = ? AND status IN ?", id, activeBargainingStatuses).
		Updates(map[string]interface{}{
			"status":         status,
			"current_amount": finalAmount,
			"completed_at":   completedAt,
		})
	return bargainingMutationError(result)
}

func bargainingMutationError(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return interfaces.ErrBargainingNegotiationNotActive
	}
	return nil
}

func (r *ap2Repository) StopBargainingNegotiationForScope(
	ctx context.Context,
	id,
	userID,
	businessID string,
	completedAt time.Time,
) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&models.BargainingNegotiation{}).
		Where("id = ? AND user_id = ? AND business_id = ?", id, userID, businessID).
		Where("status IN ?", activeBargainingStatuses).
		Updates(map[string]interface{}{
			"status":       "stopped",
			"completed_at": completedAt,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *ap2Repository) StopBargainingNegotiationBySessionAndID(
	ctx context.Context,
	sessionID,
	id string,
	completedAt time.Time,
) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&models.BargainingNegotiation{}).
		Where("session_id = ? AND id = ?", sessionID, id).
		Where("status IN ?", activeBargainingStatuses).
		Updates(map[string]interface{}{
			"status":       "stopped",
			"completed_at": completedAt,
		})
	return result.RowsAffected == 1, result.Error
}

// Bargaining Rounds
func (r *ap2Repository) CreateBargainingRound(ctx context.Context, round *models.BargainingRound) error {
	if round.ID == "" {
		round.ID = uuid.NewString()
	}
	result := r.db.WithContext(ctx).Exec(`
		INSERT INTO bargaining_rounds (
			id,
			negotiation_id,
			agent_id,
			round_number,
			proposed_amount,
			previous_amount,
			agent_type,
			action,
			reason,
			volatility_factor,
			metadata,
			created_at
		)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		FROM bargaining_negotiations
		WHERE id = ? AND status IN ('initiated', 'in_progress', 'running')
	`,
		round.ID,
		round.NegotiationID,
		round.AgentID,
		round.RoundNumber,
		round.ProposedAmount,
		round.PreviousAmount,
		round.AgentType,
		round.Action,
		round.Reason,
		round.VolatilityFactor,
		round.Metadata,
		time.Now().UTC(),
		round.NegotiationID,
	)
	return bargainingMutationError(result)
}

func (r *ap2Repository) ClaimBargainingRound(
	ctx context.Context,
	sessionID string,
	negotiationID string,
	roundNumber int,
	leaseOwner string,
	now time.Time,
	leaseExpiresAt time.Time,
) (bool, error) {
	var claimedOwner string
	err := r.db.WithContext(ctx).Raw(`
		INSERT INTO bargaining_round_claims (
			negotiation_id,
			round_number,
			lease_owner,
			lease_expires_at,
			created_at,
			updated_at
		)
		SELECT ?, ?, ?, ?, ?, ?
		FROM bargaining_negotiations
		WHERE id = ?
			AND session_id = ?
			AND status IN ('initiated', 'in_progress', 'running')
		ON CONFLICT (negotiation_id, round_number) DO UPDATE
		SET
			lease_owner = EXCLUDED.lease_owner,
			lease_expires_at = EXCLUDED.lease_expires_at,
			updated_at = EXCLUDED.updated_at
		WHERE bargaining_round_claims.completed_at IS NULL
			AND bargaining_round_claims.lease_expires_at <= EXCLUDED.updated_at
		RETURNING lease_owner
	`, negotiationID, roundNumber, leaseOwner, leaseExpiresAt, now, now, negotiationID, sessionID).Scan(&claimedOwner).Error
	if err != nil {
		return false, err
	}
	return claimedOwner == leaseOwner, nil
}

func (r *ap2Repository) CompleteBargainingRoundClaim(
	ctx context.Context,
	sessionID string,
	negotiationID string,
	roundNumber int,
	leaseOwner string,
	completedAt time.Time,
) (bool, error) {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE bargaining_round_claims
		SET completed_at = ?, updated_at = ?
		WHERE negotiation_id = ?
			AND round_number = ?
			AND lease_owner = ?
			AND completed_at IS NULL
			AND EXISTS (
				SELECT 1
				FROM bargaining_negotiations
				WHERE id = ? AND session_id = ?
			)
	`, completedAt, completedAt, negotiationID, roundNumber, leaseOwner, negotiationID, sessionID)
	return result.RowsAffected == 1, result.Error
}

func (r *ap2Repository) GetBargainingRounds(ctx context.Context, negotiationID string) ([]*models.BargainingRound, error) {
	var rounds []models.BargainingRound
	// Negotiations are bounded by max rounds, so returning the full history here is intentional.
	err := r.db.WithContext(ctx).
		Where("negotiation_id = ?", negotiationID).
		Order("round_number ASC").
		Find(&rounds).Error
	if err != nil {
		return nil, err
	}
	result := make([]*models.BargainingRound, len(rounds))
	for i := range rounds {
		result[i] = &rounds[i]
	}
	return result, nil
}

func (r *ap2Repository) GetBargainingRoundsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.BargainingRound, int64, error) {
	var rounds []models.BargainingRound
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.BargainingRound{}).Where("agent_id = ?", agentID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&rounds).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.BargainingRound, len(rounds))
	for i := range rounds {
		result[i] = &rounds[i]
	}
	return result, total, nil
}
