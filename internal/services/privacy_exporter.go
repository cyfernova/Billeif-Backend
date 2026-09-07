package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"invoice-backend/internal/models"

	"gorm.io/gorm"
)

type PrivacyArtifactStore interface {
	UploadIfAbsent(context.Context, string, string, []byte, string) error
	GeneratePresignedDownloadURL(context.Context, string, string, int64) (string, error)
}

func (e *DatabasePrivacyExporter) PresignExport(ctx context.Context, artifactKey string) (string, error) {
	if e == nil || e.store == nil || e.bucket == "" || !strings.HasPrefix(artifactKey, "privacy/") {
		return "", errors.New("invalid privacy artifact")
	}
	return e.store.GeneratePresignedDownloadURL(ctx, e.bucket, artifactKey, 300)
}

type DatabasePrivacyExporter struct {
	db     *gorm.DB
	store  PrivacyArtifactStore
	bucket string
}

func NewDatabasePrivacyExporter(db *gorm.DB, store PrivacyArtifactStore, bucket string) *DatabasePrivacyExporter {
	return &DatabasePrivacyExporter{db: db, store: store, bucket: strings.TrimSpace(bucket)}
}

type privacyUserExport struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	PhoneNumber string  `json:"phone_number"`
	CognitoID   string  `json:"cognito_id"`
	Name        string  `json:"name"`
	Role        string  `json:"role"`
	BusinessID  *string `json:"business_id"`
}

type privacyTeamExport struct {
	ID         string `json:"id"`
	BusinessID string `json:"business_id"`
	Role       string `json:"role"`
	Status     string `json:"status"`
	IsOwner    bool   `json:"is_owner"`
}

type privacyRequestExport struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Status       string `json:"status"`
	ArtifactHash string `json:"artifact_hash,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
}

type privacyExportDocument struct {
	SchemaVersion string                 `json:"schema_version"`
	User          privacyUserExport      `json:"user"`
	Team          []privacyTeamExport    `json:"team_memberships"`
	Requests      []privacyRequestExport `json:"privacy_requests"`
}

func (e *DatabasePrivacyExporter) Export(ctx context.Context, businessID, subject, requestID string) (string, string, error) {
	if e == nil || e.db == nil || e.store == nil || e.bucket == "" || strings.TrimSpace(businessID) == "" || strings.TrimSpace(subject) == "" || strings.TrimSpace(requestID) == "" {
		return "", "", errors.New("privacy exporter is not configured")
	}
	var user models.User
	if err := e.db.WithContext(ctx).Where("cognito_id = ? AND business_id = ?", subject, businessID).First(&user).Error; err != nil {
		return "", "", fmt.Errorf("load privacy subject: %w", err)
	}
	var members []models.TeamMember
	if err := e.db.WithContext(ctx).Where("business_id = ? AND user_id = ?", businessID, user.ID).Find(&members).Error; err != nil {
		return "", "", fmt.Errorf("load privacy memberships: %w", err)
	}
	var requests []models.PrivacyRequest
	if err := e.db.WithContext(ctx).Where("business_id = ? AND subject = ?", businessID, subject).Order("requested_at ASC").Find(&requests).Error; err != nil {
		return "", "", fmt.Errorf("load privacy requests: %w", err)
	}
	document := privacyExportDocument{
		SchemaVersion: "1",
		User:          privacyUserExport{ID: user.ID, Email: user.Email, PhoneNumber: user.PhoneNumber, CognitoID: user.CognitoID, Name: user.Name, Role: user.Role, BusinessID: user.BusinessID},
		Team:          make([]privacyTeamExport, 0, len(members)), Requests: make([]privacyRequestExport, 0, len(requests)),
	}
	for _, member := range members {
		document.Team = append(document.Team, privacyTeamExport{ID: member.ID, BusinessID: member.BusinessID, Role: member.Role, Status: member.Status, IsOwner: member.IsOwner})
	}
	for _, request := range requests {
		document.Requests = append(document.Requests, privacyRequestExport{ID: request.ID, Kind: request.Kind, Status: request.Status, ArtifactHash: request.ArtifactHash, ErrorCode: request.ErrorCode})
	}
	payload, err := json.Marshal(document)
	if err != nil {
		return "", "", fmt.Errorf("encode privacy export: %w", err)
	}
	digest := sha256.Sum256(payload)
	artifactHash := hex.EncodeToString(digest[:])
	artifactKey := privacyArtifactSubjectPrefix(businessID, subject) + requestID + "/export.json"
	if err := e.store.UploadIfAbsent(ctx, e.bucket, artifactKey, payload, "application/json"); err != nil {
		return "", "", fmt.Errorf("store privacy export: %w", err)
	}
	return artifactKey, artifactHash, nil
}
