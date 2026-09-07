package services

import (
	"context"
	"testing"

	"invoice-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type privacyArtifactStoreFake struct {
	bucket, key, contentType string
	payload                  []byte
}

func (f *privacyArtifactStoreFake) UploadIfAbsent(_ context.Context, bucket, key string, payload []byte, contentType string) error {
	f.bucket, f.key, f.payload, f.contentType = bucket, key, append([]byte(nil), payload...), contentType
	return nil
}
func (*privacyArtifactStoreFake) GeneratePresignedDownloadURL(context.Context, string, string, int64) (string, error) {
	return "https://storage.example/privacy-export", nil
}

func TestDatabasePrivacyExporterScopesSubjectAndStoresImmutableArtifact(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT, phone_number TEXT, cognito_id TEXT, name TEXT, profile_picture_url TEXT, role TEXT, business_id TEXT, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE team_members (id TEXT PRIMARY KEY, business_id TEXT, user_id TEXT, role TEXT, status TEXT, is_owner BOOLEAN, deleted_at DATETIME)`,
		`CREATE TABLE privacy_requests (id TEXT PRIMARY KEY, business_id TEXT, subject TEXT, kind TEXT, status TEXT, artifact_hash TEXT, error_code TEXT, requested_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	businessID, subject := uuid.NewString(), "cognito-subject"
	user := &models.User{ID: uuid.NewString(), Email: "user@example.com", CognitoID: subject, Name: "User", Role: "viewer", BusinessID: &businessID}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	store := &privacyArtifactStoreFake{}
	exporter := NewDatabasePrivacyExporter(db, store, "private-drive")
	key, hash, err := exporter.Export(context.Background(), businessID, subject, uuid.NewString())
	if err != nil || key == "" || len(hash) != 64 || store.bucket != "private-drive" || store.contentType != "application/json" || len(store.payload) == 0 {
		t.Fatalf("key=%q hash=%q store=%+v err=%v", key, hash, store, err)
	}
	if _, _, err := exporter.Export(context.Background(), uuid.NewString(), subject, uuid.NewString()); err == nil {
		t.Fatal("cross-tenant export must fail")
	}
}
