package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"invoice-backend/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"gorm.io/gorm"
)

type DatabasePrivacyCleanupTarget struct{ db *gorm.DB }

func NewDatabasePrivacyCleanupTarget(db *gorm.DB) *DatabasePrivacyCleanupTarget {
	return &DatabasePrivacyCleanupTarget{db: db}
}

func (*DatabasePrivacyCleanupTarget) Name() string { return "database" }

func (t *DatabasePrivacyCleanupTarget) Preflight(ctx context.Context, businessID, subject, _ string) error {
	if t == nil || t.db == nil || strings.TrimSpace(businessID) == "" || strings.TrimSpace(subject) == "" {
		return errors.New("database privacy cleanup is not configured")
	}
	var ownerCount int64
	if err := t.db.WithContext(ctx).Model(&models.BusinessProfile{}).Where("owner_id = ?", subject).Count(&ownerCount).Error; err != nil {
		return fmt.Errorf("check business ownership: %w", err)
	}
	if ownerCount != 0 {
		return errors.New("business ownership transfer is required")
	}
	var user models.User
	if err := t.db.WithContext(ctx).Unscoped().Where("cognito_id = ? AND business_id = ?", subject, businessID).First(&user).Error; err != nil {
		return fmt.Errorf("load privacy subject: %w", err)
	}
	var otherMemberships int64
	if err := t.db.WithContext(ctx).Model(&models.TeamMember{}).Where("user_id = ? AND business_id <> ?", user.ID, businessID).Count(&otherMemberships).Error; err != nil {
		return fmt.Errorf("check cross-tenant memberships: %w", err)
	}
	if otherMemberships != 0 {
		return errors.New("other tenant memberships must be removed first")
	}
	return nil
}

func (t *DatabasePrivacyCleanupTarget) Purge(ctx context.Context, businessID, subject, _ string) error {
	return t.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Unscoped().Where("cognito_id = ? AND business_id = ?", subject, businessID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if err := tx.Where("business_id = ? AND user_id = ?", businessID, user.ID).Delete(&models.TeamMember{}).Error; err != nil {
			return err
		}
		return tx.Delete(&user).Error
	})
}

type cognitoPrivacyAPI interface {
	AdminDeleteUser(context.Context, *cognitoidentityprovider.AdminDeleteUserInput, ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.AdminDeleteUserOutput, error)
}

type CognitoPrivacyCleanupTarget struct {
	client      cognitoPrivacyAPI
	userPoolID  string
	phonePoolID string
}

func NewCognitoPrivacyCleanupTarget(client cognitoPrivacyAPI, userPoolID, phonePoolID string) *CognitoPrivacyCleanupTarget {
	return &CognitoPrivacyCleanupTarget{client: client, userPoolID: strings.TrimSpace(userPoolID), phonePoolID: strings.TrimSpace(phonePoolID)}
}

func (*CognitoPrivacyCleanupTarget) Name() string { return "identity-provider" }

func (t *CognitoPrivacyCleanupTarget) Purge(ctx context.Context, _ string, subject string, _ string) error {
	if t == nil || t.client == nil || t.userPoolID == "" || strings.TrimSpace(subject) == "" {
		return errors.New("cognito privacy cleanup is not configured")
	}
	poolID, username := t.userPoolID, strings.TrimSpace(subject)
	if t.phonePoolID != "" && strings.HasPrefix(username, t.phonePoolID+":") {
		poolID, username = t.phonePoolID, strings.TrimPrefix(username, t.phonePoolID+":")
	}
	_, err := t.client.AdminDeleteUser(ctx, &cognitoidentityprovider.AdminDeleteUserInput{UserPoolId: aws.String(poolID), Username: aws.String(username)})
	var notFound *cognitotypes.UserNotFoundException
	if errors.As(err, &notFound) {
		return nil
	}
	return err
}

type privacyObjectStore interface {
	ListObjects(context.Context, string, string) ([]string, error)
	Delete(context.Context, string, string) error
}

type S3PrivacyCleanupTarget struct {
	store  privacyObjectStore
	bucket string
}

func NewS3PrivacyCleanupTarget(store privacyObjectStore, bucket string) *S3PrivacyCleanupTarget {
	return &S3PrivacyCleanupTarget{store: store, bucket: strings.TrimSpace(bucket)}
}

func (*S3PrivacyCleanupTarget) Name() string { return "object-storage" }

func (t *S3PrivacyCleanupTarget) Purge(ctx context.Context, businessID, subject, _ string) error {
	if t == nil || t.store == nil || t.bucket == "" || strings.TrimSpace(businessID) == "" || strings.TrimSpace(subject) == "" {
		return errors.New("object privacy cleanup is not configured")
	}
	prefix := privacyArtifactSubjectPrefix(businessID, subject)
	keys, err := t.store.ListObjects(ctx, t.bucket, prefix)
	if err != nil {
		return fmt.Errorf("list privacy artifacts: %w", err)
	}
	if len(keys) >= 1000 {
		return errors.New("privacy artifact cleanup exceeds bounded batch")
	}
	var failures []error
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			failures = append(failures, fmt.Errorf("object outside privacy prefix: %s", key))
			continue
		}
		if err := t.store.Delete(ctx, t.bucket, key); err != nil {
			failures = append(failures, fmt.Errorf("delete privacy artifact: %w", err))
		}
	}
	return errors.Join(failures...)
}

func privacyArtifactSubjectPrefix(businessID, subject string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(subject)))
	return fmt.Sprintf("privacy/%s/subjects/%s/", strings.TrimSpace(businessID), hex.EncodeToString(digest[:]))
}
