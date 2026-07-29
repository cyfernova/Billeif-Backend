package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

var (
	ErrCredentialNotFound    = errors.New("credential not found")
	ErrInvalidCredentialType = errors.New("invalid credential type")
	ErrEncryptionFailed      = errors.New("encryption failed")
)

type CredentialProviderService struct {
	ap2Repo       interfaces.AP2Repository
	encryptionKey []byte
	cfg           *config.Config
	resolver      ProviderConfigResolver
	log           *logger.Logger
}

func NewCredentialProviderService(ap2Repo interfaces.AP2Repository, encryptionKey string, log *logger.Logger) (*CredentialProviderService, error) {
	key, err := base64.StdEncoding.DecodeString(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("invalid credential encryption key encoding: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("credential encryption key must decode to 32 bytes")
	}
	return &CredentialProviderService{
		ap2Repo:       ap2Repo,
		encryptionKey: key,
		log:           log,
	}, nil
}

func NewCredentialProviderServiceWithResolver(ap2Repo interfaces.AP2Repository, cfg *config.Config, resolver ProviderConfigResolver, log *logger.Logger) *CredentialProviderService {
	return &CredentialProviderService{
		ap2Repo:  ap2Repo,
		cfg:      cfg,
		resolver: resolver,
		log:      log,
	}
}

type AddPaymentMethodRequest struct {
	UserID             string
	CredentialType     string
	RazorpayCustomerID string
	MaskedCardNumber   string
	CardBrand          string
	CardToken          string
	IsDefault          bool
}

func (s *CredentialProviderService) AddPaymentMethod(ctx context.Context, req *AddPaymentMethodRequest) (*models.PaymentCredential, error) {
	encryptedData, err := s.encryptCredential(ctx, req.CardToken)
	if err != nil {
		s.log.Error("failed to encrypt credential", "error", err, "user_id", req.UserID)
		return nil, fmt.Errorf("%w: %v", ErrEncryptionFailed, err)
	}

	credential := &models.PaymentCredential{
		UserID:             req.UserID,
		CredentialType:     req.CredentialType,
		RazorpayCustomerID: &req.RazorpayCustomerID,
		MaskedCardNumber:   &req.MaskedCardNumber,
		CardBrand:          &req.CardBrand,
		EncryptedData:      &encryptedData,
		IsDefault:          req.IsDefault,
		IsActive:           true,
	}

	if req.IsDefault {
		if err := s.setDefaultCredential(ctx, req.UserID); err != nil {
			return nil, fmt.Errorf("failed to set default credential: %w", err)
		}
	}

	if err := s.ap2Repo.CreatePaymentCredential(ctx, credential); err != nil {
		s.log.Error("failed to create payment credential", "error", err, "user_id", req.UserID)
		return nil, fmt.Errorf("failed to create payment credential: %w", err)
	}

	s.log.Info("added payment method", "credential_id", credential.ID, "user_id", req.UserID, "type", req.CredentialType)
	return credential, nil
}

func (s *CredentialProviderService) GetPaymentMethods(ctx context.Context, userID string) ([]*models.PaymentCredential, error) {
	credentials, err := s.ap2Repo.GetPaymentCredentialsByUser(ctx, userID)
	if err != nil {
		s.log.Error("failed to get payment methods", "error", err, "user_id", userID)
		return nil, err
	}
	return credentials, nil
}

func (s *CredentialProviderService) GetDefaultPaymentMethod(ctx context.Context, userID string) (*models.PaymentCredential, error) {
	credential, err := s.ap2Repo.GetDefaultCredential(ctx, userID)
	if err != nil {
		return nil, ErrCredentialNotFound
	}
	return credential, nil
}

func (s *CredentialProviderService) GetPaymentMethodByID(ctx context.Context, credentialID string) (*models.PaymentCredential, error) {
	credential, err := s.ap2Repo.GetPaymentCredentialByID(ctx, credentialID)
	if err != nil {
		s.log.Error("failed to get payment method", "error", err, "credential_id", credentialID)
		return nil, ErrCredentialNotFound
	}
	return credential, nil
}

func (s *CredentialProviderService) SetDefaultPaymentMethod(ctx context.Context, userID, credentialID string) error {
	credential, err := s.ap2Repo.GetPaymentCredentialByID(ctx, credentialID)
	if err != nil {
		return ErrCredentialNotFound
	}

	if credential.UserID != userID {
		return errors.New("unauthorized: credential does not belong to user")
	}

	if err := s.ap2Repo.SetDefaultCredential(ctx, userID, credentialID); err != nil {
		s.log.Error("failed to set default credential", "error", err, "credential_id", credentialID)
		return fmt.Errorf("failed to set default credential: %w", err)
	}

	s.log.Info("set default payment method", "credential_id", credentialID, "user_id", userID)
	return nil
}

func (s *CredentialProviderService) DeletePaymentMethod(ctx context.Context, userID, credentialID string) error {
	credential, err := s.ap2Repo.GetPaymentCredentialByID(ctx, credentialID)
	if err != nil {
		return ErrCredentialNotFound
	}

	if credential.UserID != userID {
		return errors.New("unauthorized: credential does not belong to user")
	}

	if err := s.ap2Repo.DeleteCredential(ctx, credentialID); err != nil {
		s.log.Error("failed to delete payment method", "error", err, "credential_id", credentialID)
		return fmt.Errorf("failed to delete payment method: %w", err)
	}

	s.log.Info("deleted payment method", "credential_id", credentialID, "user_id", userID)
	return nil
}

type GenerateTokenRequest struct {
	CredentialID     string
	PaymentMandateID string
	UserID           string
}

func (s *CredentialProviderService) GenerateCredentialToken(ctx context.Context, req *GenerateTokenRequest) (*models.CredentialToken, error) {
	credential, err := s.ap2Repo.GetPaymentCredentialByID(ctx, req.CredentialID)
	if err != nil {
		return nil, ErrCredentialNotFound
	}

	if credential.UserID != req.UserID {
		return nil, errors.New("unauthorized: credential does not belong to user")
	}

	if !credential.IsActive {
		return nil, errors.New("credential is not active")
	}

	token, err := generateSecureToken()
	if err != nil {
		s.log.Error("failed to generate token", "error", err, "credential_id", req.CredentialID)
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}
	tokenHash := hashToken(token)

	credentialToken := &models.CredentialToken{
		CredentialID:     req.CredentialID,
		PaymentMandateID: &req.PaymentMandateID,
		Token:            tokenHash,
		TokenHash:        tokenHash,
		ExpiresAt:        time.Now().Add(30 * time.Minute),
		IsUsed:           false,
	}

	if err := s.ap2Repo.CreateCredentialToken(ctx, credentialToken); err != nil {
		s.log.Error("failed to create credential token", "error", err, "credential_id", req.CredentialID)
		return nil, fmt.Errorf("failed to create credential token: %w", err)
	}

	// Return raw token once to caller; only hash is persisted.
	credentialToken.Token = token
	s.log.Info("generated credential token", "token_id", credentialToken.ID, "credential_id", req.CredentialID)
	return credentialToken, nil
}

func (s *CredentialProviderService) ValidateToken(ctx context.Context, token string) (*models.CredentialToken, error) {
	credentialToken, err := s.ap2Repo.GetCredentialToken(ctx, hashToken(token))
	if err != nil {
		return nil, errors.New("invalid or expired token")
	}

	if credentialToken.IsUsed {
		return nil, errors.New("token already used")
	}

	return credentialToken, nil
}

func (s *CredentialProviderService) UseToken(ctx context.Context, tokenID string) error {
	if err := s.ap2Repo.MarkTokenAsUsed(ctx, tokenID); err != nil {
		s.log.Error("failed to mark token as used", "error", err, "token_id", tokenID)
		return fmt.Errorf("failed to use token: %w", err)
	}

	s.log.Info("used credential token", "token_id", tokenID)
	return nil
}

func (s *CredentialProviderService) encryptCredential(ctx context.Context, plaintext string) (string, error) {
	key := s.encryptionKey
	if s.resolver != nil {
		resolved, err := s.resolver.ResolveProvider(ctx, s.cfg, config.SecretCredentialEncryption)
		if err != nil {
			return "", err
		}
		key, err = base64.StdEncoding.DecodeString(resolved.Credentials.EncryptionKey)
		if err != nil || len(key) != 32 {
			return "", errors.New("credential encryption key is invalid")
		}
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (s *CredentialProviderService) setDefaultCredential(ctx context.Context, userID string) error {
	credentials, err := s.ap2Repo.GetPaymentCredentialsByUser(ctx, userID)
	if err != nil {
		return err
	}

	for _, cred := range credentials {
		if cred.IsDefault {
			updates := map[string]interface{}{"is_default": false}
			if err := s.ap2Repo.UpdateCredential(ctx, cred.ID, updates); err != nil {
				return err
			}
		}
	}

	return nil
}

func generateSecureToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
