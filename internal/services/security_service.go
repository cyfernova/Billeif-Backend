package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
)

const (
	AssuranceTOTP         = "totp"
	maximumStepUpTTL      = 10 * time.Minute
	maximumAssuranceAge   = 5 * time.Minute
	minimumStepUpTokenLen = 24
)

var securityScopePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.:-]{0,254}$`)

var ErrInvalidStepUp = errors.New("invalid step-up request")

type SecurityServiceOptions struct {
	Now   func() time.Time
	Token func() (string, error)
}

type SecurityService struct {
	repository interfaces.SecurityRepository
	now        func() time.Time
	token      func() (string, error)
}

type StepUpIssueRequest struct {
	Subject         string
	BusinessID      string
	Action          string
	Resource        string
	CommandIdentity string
	Assurance       string
	AuthenticatedAt time.Time
	TTL             time.Duration
}

type StepUpIssued struct {
	ID        string    `json:"id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func NewSecurityService(repository interfaces.SecurityRepository, options SecurityServiceOptions) *SecurityService {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	token := options.Token
	if token == nil {
		token = randomStepUpToken
	}
	return &SecurityService{repository: repository, now: now, token: token}
}

func (s *SecurityService) IssueStepUp(ctx context.Context, request StepUpIssueRequest) (*StepUpIssued, error) {
	if s == nil || s.repository == nil {
		return nil, ErrInvalidStepUp
	}
	now := s.now().UTC()
	request.Subject = strings.TrimSpace(request.Subject)
	request.BusinessID = strings.TrimSpace(request.BusinessID)
	request.Action = strings.TrimSpace(request.Action)
	request.Resource = strings.TrimSpace(request.Resource)
	request.CommandIdentity = strings.TrimSpace(request.CommandIdentity)
	request.Assurance = strings.ToLower(strings.TrimSpace(request.Assurance))
	if request.Subject == "" || uuid.Validate(request.BusinessID) != nil ||
		!securityScopePattern.MatchString(request.Action) || !securityScopePattern.MatchString(request.Resource) ||
		request.CommandIdentity == "" || len(request.CommandIdentity) > 180 ||
		request.Assurance != AssuranceTOTP || request.TTL <= 0 || request.TTL > maximumStepUpTTL ||
		request.AuthenticatedAt.IsZero() || request.AuthenticatedAt.After(now.Add(time.Minute)) ||
		now.Sub(request.AuthenticatedAt) > maximumAssuranceAge {
		return nil, ErrInvalidStepUp
	}
	token, err := s.token()
	if err != nil || len(token) < minimumStepUpTokenLen {
		return nil, fmt.Errorf("issue step-up token: %w", ErrInvalidStepUp)
	}
	id := uuid.NewString()
	expiresAt := now.Add(request.TTL)
	grant := &models.StepUpGrant{
		ID: id, Subject: request.Subject, BusinessID: request.BusinessID,
		Action: request.Action, Resource: request.Resource, Assurance: request.Assurance,
		CommandHash: hashStepUpCommand(request.CommandIdentity),
		TokenHash:   hashStepUpToken(token), AuthenticatedAt: request.AuthenticatedAt.UTC(),
		IssuedAt: now, ExpiresAt: expiresAt,
	}
	if err := s.repository.CreateStepUpGrant(ctx, grant); err != nil {
		return nil, fmt.Errorf("create step-up grant: %w", err)
	}
	return &StepUpIssued{ID: id, Token: token, ExpiresAt: expiresAt}, nil
}

func (s *SecurityService) VerifyOperationStepUp(ctx context.Context, request OperationStepUpRequest) error {
	return s.ConsumeStepUp(ctx, StepUpConsumeRequest{
		Subject: request.OperatorSubject, BusinessID: request.BusinessID, Action: request.Action,
		Resource: request.OperationID, CommandIdentity: request.CommandIdentity, Token: request.Token,
	})
}

type StepUpConsumeRequest struct {
	Subject, BusinessID, Action, Resource, CommandIdentity, Token string
}

func (s *SecurityService) ConsumeStepUp(ctx context.Context, request StepUpConsumeRequest) error {
	if s == nil || s.repository == nil || strings.TrimSpace(request.Token) == "" {
		return ErrOperationStepUpRequired
	}
	consumed, err := s.repository.ConsumeStepUpGrant(ctx, interfaces.StepUpConsumeRequest{
		TokenHash: hashStepUpToken(strings.TrimSpace(request.Token)), Subject: strings.TrimSpace(request.Subject),
		BusinessID: strings.TrimSpace(request.BusinessID), Action: strings.TrimSpace(request.Action),
		Resource: strings.TrimSpace(request.Resource), CommandHash: hashStepUpCommand(strings.TrimSpace(request.CommandIdentity)), ConsumedAt: s.now().UTC(),
	})
	if err != nil || !consumed {
		return ErrOperationStepUpRequired
	}
	return nil
}

func hashStepUpCommand(identity string) string {
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func hashStepUpToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func randomStepUpToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "su_" + base64.RawURLEncoding.EncodeToString(raw), nil
}
