package services

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/internal/repositories/postgres"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type renderProfileEncryptionResolver struct {
	key   string
	calls []config.SecretKind
}

func (r *renderProfileEncryptionResolver) ResolveProvider(
	_ context.Context,
	cfg *config.Config,
	kind config.SecretKind,
) (*config.Config, error) {
	r.calls = append(r.calls, kind)
	resolved := *cfg
	resolved.Credentials.EncryptionKey = r.key
	return &resolved, nil
}

func TestRenderProfilePasswordEncryptionIsAuthenticatedAndDomainSeparated(t *testing.T) {
	resolver := &renderProfileEncryptionResolver{
		key: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")),
	}
	service := &DocumentService{cfg: &config.Config{}, resolver: resolver}
	ctx := context.Background()
	const (
		businessID = "11111111-1111-4111-8111-111111111111"
		profileID  = "22222222-2222-4222-8222-222222222222"
		plaintext  = "synthetic-render-password"
	)

	ciphertext, err := service.encryptRenderProfilePassword(ctx, businessID, profileID, plaintext)
	if err != nil {
		t.Fatalf("encrypt render profile password: %v", err)
	}
	if !strings.HasPrefix(ciphertext, renderProfilePasswordEnvelopePrefix) {
		t.Fatalf("ciphertext is missing the versioned envelope prefix")
	}
	if strings.Contains(ciphertext, plaintext) {
		t.Fatal("ciphertext contains plaintext password material")
	}
	decrypted, err := service.decryptRenderProfilePassword(ctx, businessID, profileID, ciphertext)
	if err != nil {
		t.Fatalf("decrypt render profile password: %v", err)
	}
	if decrypted != plaintext {
		t.Fatal("decrypted password does not match the original")
	}

	for name, candidate := range map[string][3]string{
		"different business": {"33333333-3333-4333-8333-333333333333", profileID, ciphertext},
		"different profile":  {businessID, "44444444-4444-4444-8444-444444444444", ciphertext},
		"tampered payload":   {businessID, profileID, tamperRenderProfileCiphertext(ciphertext)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.decryptRenderProfilePassword(
				ctx,
				candidate[0],
				candidate[1],
				candidate[2],
			); err == nil {
				t.Fatal("decrypt succeeded outside the authenticated row domain")
			}
		})
	}
	if len(resolver.calls) != 5 {
		t.Fatalf("credential resolver calls = %d, want one per crypto operation", len(resolver.calls))
	}
	for _, kind := range resolver.calls {
		if kind != config.SecretCredentialEncryption {
			t.Fatalf("resolved secret kind = %q, want credential-encryption", kind)
		}
	}
	if service.cfg.Credentials.EncryptionKey != "" {
		t.Fatal("crypto operation mutated the shared bootstrap config")
	}
}

func TestCreateRenderProfileEncryptsPasswordAtRest(t *testing.T) {
	service, db, resolver := newRenderProfilePasswordService(t)
	ctx := context.Background()
	businessID := uuid.NewString()
	const password = "synthetic-create-password"

	profile, err := service.CreateRenderProfileByBusiness(ctx, businessID, CreateRenderProfileInput{
		Name:              "Protected profile",
		PasswordProtected: true,
		Password:          password,
	})
	if err != nil {
		t.Fatalf("create render profile: %v", err)
	}
	if profile.Password != "" {
		t.Fatal("public create result retained plaintext password material")
	}
	if !profile.PasswordConfigured {
		t.Fatal("public create result did not report the configured password")
	}
	var legacy, ciphertext sql.NullString
	if err := db.Raw(
		"SELECT password, password_ciphertext FROM render_profiles WHERE id = ?",
		profile.ID,
	).Row().Scan(&legacy, &ciphertext); err != nil {
		t.Fatalf("load stored render profile password: %v", err)
	}
	if legacy.Valid && legacy.String != "" {
		t.Fatal("render profile password remained in the legacy plaintext column")
	}
	if !ciphertext.Valid || !strings.HasPrefix(ciphertext.String, renderProfilePasswordEnvelopePrefix) {
		t.Fatal("render profile password was not stored as a versioned ciphertext")
	}
	if strings.Contains(ciphertext.String, password) {
		t.Fatal("stored render profile ciphertext contains plaintext password material")
	}
	if len(resolver.calls) != 1 || resolver.calls[0] != config.SecretCredentialEncryption {
		t.Fatalf("credential resolver calls = %v, want one credential-encryption lookup", resolver.calls)
	}
}

func TestPublicRenderProfileReadMigratesLegacyPasswordWithoutExposingIt(t *testing.T) {
	service, db, resolver := newRenderProfilePasswordService(t)
	ctx := context.Background()
	businessID := uuid.NewString()
	profileID := uuid.NewString()
	const legacyPassword = "synthetic-legacy-password"
	if err := db.Exec(`
		INSERT INTO render_profiles (
			id, business_id, name, password_protected, password, is_default
		) VALUES (?, ?, 'Legacy protected profile', TRUE, ?, TRUE)`,
		profileID,
		businessID,
		legacyPassword,
	).Error; err != nil {
		t.Fatalf("seed legacy render profile: %v", err)
	}

	profile, err := service.GetRenderProfileByBusiness(ctx, businessID, profileID)
	if err != nil {
		t.Fatalf("get legacy render profile: %v", err)
	}
	if profile.Password != "" || profile.LegacyPassword != nil || profile.PasswordCiphertext != nil {
		t.Fatal("public render profile result retained password storage material")
	}
	if !profile.PasswordConfigured {
		t.Fatal("public render profile result did not preserve password configuration state")
	}
	var legacy, ciphertext sql.NullString
	if err := db.Raw(
		"SELECT password, password_ciphertext FROM render_profiles WHERE id = ?",
		profileID,
	).Row().Scan(&legacy, &ciphertext); err != nil {
		t.Fatalf("load migrated render profile password: %v", err)
	}
	if legacy.Valid && legacy.String != "" {
		t.Fatal("legacy plaintext render profile password was not cleared")
	}
	if !ciphertext.Valid || !strings.HasPrefix(ciphertext.String, renderProfilePasswordEnvelopePrefix) {
		t.Fatal("legacy render profile password was not migrated to ciphertext")
	}
	if strings.Contains(ciphertext.String, legacyPassword) {
		t.Fatal("migrated render profile ciphertext contains plaintext password material")
	}
	if len(resolver.calls) != 1 || resolver.calls[0] != config.SecretCredentialEncryption {
		t.Fatalf("credential resolver calls = %v, want one legacy migration lookup", resolver.calls)
	}
}

func TestLegacyMigrationRecoversFromInvalidExistingCiphertext(t *testing.T) {
	service, db, _ := newRenderProfilePasswordService(t)
	ctx := context.Background()
	businessID := uuid.NewString()
	profileID := uuid.NewString()
	const legacyPassword = "synthetic-recoverable-legacy-password"
	if err := db.Exec(`
		INSERT INTO render_profiles (
			id, business_id, name, password_protected, password, password_ciphertext
		) VALUES (?, ?, 'Recoverable legacy profile', TRUE, ?, 'rpw:v1:invalid')`,
		profileID,
		businessID,
		legacyPassword,
	).Error; err != nil {
		t.Fatalf("seed mixed legacy render profile: %v", err)
	}

	profile, err := service.GetRenderProfileByBusiness(ctx, businessID, profileID)
	if err != nil {
		t.Fatalf("migrate mixed legacy render profile: %v", err)
	}
	if !profile.PasswordConfigured {
		t.Fatal("public render profile lost its password configuration state")
	}
	resolved, err := service.GetRenderProfileForRenderingByBusiness(ctx, businessID, profileID)
	if err != nil {
		t.Fatalf("resolve recovered render profile password: %v", err)
	}
	if resolved.Password != legacyPassword {
		t.Fatal("legacy migration did not preserve the recoverable password")
	}
}

func TestLegacyMigrationTreatsRollingDeployPlaintextAsLatestPassword(t *testing.T) {
	service, db, _ := newRenderProfilePasswordService(t)
	ctx := context.Background()
	businessID := uuid.NewString()
	created, err := service.CreateRenderProfileByBusiness(ctx, businessID, CreateRenderProfileInput{
		Name:              "Rolling deployment profile",
		PasswordProtected: true,
		Password:          "synthetic-earlier-ciphertext-password",
	})
	if err != nil {
		t.Fatalf("create encrypted render profile: %v", err)
	}
	const rollingDeployPassword = "synthetic-latest-legacy-password"
	if err := db.Exec(
		"UPDATE render_profiles SET password = ? WHERE id = ?",
		rollingDeployPassword,
		created.ID,
	).Error; err != nil {
		t.Fatalf("simulate rolling-deploy legacy update: %v", err)
	}

	resolved, err := service.GetRenderProfileForRenderingByBusiness(ctx, businessID, created.ID)
	if err != nil {
		t.Fatalf("resolve rolling-deploy password: %v", err)
	}
	if resolved.Password != rollingDeployPassword {
		t.Fatal("legacy compatibility migration did not preserve the latest rolling-deploy password")
	}
}

func TestLegacyMigrationCompareAndSwapDoesNotOverwriteConcurrentPassword(t *testing.T) {
	service, db, _ := newRenderProfilePasswordService(t)
	ctx := context.Background()
	businessID := uuid.NewString()
	profileID := uuid.NewString()
	const legacyPassword = "synthetic-stale-legacy-password"
	if err := db.Exec(`
		INSERT INTO render_profiles (id, business_id, name, password, password_protected)
		VALUES (?, ?, 'Concurrent profile', ?, TRUE)`,
		profileID,
		businessID,
		legacyPassword,
	).Error; err != nil {
		t.Fatalf("seed concurrent render profile: %v", err)
	}

	concurrentCiphertext, err := service.encryptRenderProfilePassword(
		ctx,
		businessID,
		profileID,
		"synthetic-concurrent-password",
	)
	if err != nil {
		t.Fatalf("encrypt concurrent password: %v", err)
	}
	if err := db.Exec(
		"UPDATE render_profiles SET password = NULL, password_ciphertext = ? WHERE id = ?",
		concurrentCiphertext,
		profileID,
	).Error; err != nil {
		t.Fatalf("store concurrent password: %v", err)
	}

	migrated, err := service.repo.MigrateRenderProfilePassword(
		ctx,
		businessID,
		profileID,
		legacyPassword,
		"rpw:v1:stale-candidate",
	)
	if err != nil {
		t.Fatalf("compare-and-swap legacy password: %v", err)
	}
	if migrated {
		t.Fatal("stale legacy migration overwrote a concurrent password update")
	}
	var legacy, storedCiphertext sql.NullString
	if err := db.Raw(
		"SELECT password, password_ciphertext FROM render_profiles WHERE id = ?",
		profileID,
	).Row().Scan(&legacy, &storedCiphertext); err != nil {
		t.Fatalf("load concurrent render profile: %v", err)
	}
	if legacy.Valid || !storedCiphertext.Valid || storedCiphertext.String != concurrentCiphertext {
		t.Fatal("stale legacy migration changed the concurrent password state")
	}
}

func TestTrustedRendererResolvesPlaintextOnlyInMemory(t *testing.T) {
	service, db, resolver := newRenderProfilePasswordService(t)
	ctx := context.Background()
	businessID := uuid.NewString()
	const password = "synthetic-renderer-password"
	created, err := service.CreateRenderProfileByBusiness(ctx, businessID, CreateRenderProfileInput{
		Name:              "Renderer protected profile",
		PasswordProtected: true,
		Password:          password,
	})
	if err != nil {
		t.Fatalf("create render profile: %v", err)
	}

	profile, err := service.GetRenderProfileForRenderingByBusiness(ctx, businessID, created.ID)
	if err != nil {
		t.Fatalf("resolve render profile for renderer: %v", err)
	}
	if profile.Password != password {
		t.Fatal("trusted renderer did not receive the configured plaintext password")
	}
	if profile.LegacyPassword != nil || profile.PasswordCiphertext != nil {
		t.Fatal("trusted renderer result retained persistence-only password fields")
	}
	serialized, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("marshal trusted renderer profile: %v", err)
	}
	if strings.Contains(string(serialized), password) || strings.Contains(string(serialized), `"password"`) {
		t.Fatal("trusted renderer profile could serialize plaintext password material")
	}
	var legacy, ciphertext sql.NullString
	if err := db.Raw(
		"SELECT password, password_ciphertext FROM render_profiles WHERE id = ?",
		created.ID,
	).Row().Scan(&legacy, &ciphertext); err != nil {
		t.Fatalf("load stored renderer profile password: %v", err)
	}
	if legacy.Valid && legacy.String != "" {
		t.Fatal("trusted renderer resolution restored plaintext at rest")
	}
	if !ciphertext.Valid || strings.Contains(ciphertext.String, password) {
		t.Fatal("trusted renderer resolution did not preserve encrypted-at-rest storage")
	}
	if len(resolver.calls) != 2 {
		t.Fatalf("credential resolver calls = %d, want create encryption plus renderer decryption", len(resolver.calls))
	}
}

func TestUpdateRenderProfileReplacesPasswordWithCiphertext(t *testing.T) {
	service, db, resolver := newRenderProfilePasswordService(t)
	ctx := context.Background()
	businessID := uuid.NewString()
	created, err := service.CreateRenderProfileByBusiness(ctx, businessID, CreateRenderProfileInput{
		Name:              "Rotated protected profile",
		PasswordProtected: true,
		Password:          "synthetic-original-password",
	})
	if err != nil {
		t.Fatalf("create render profile: %v", err)
	}
	var originalCiphertext string
	if err := db.Raw(
		"SELECT password_ciphertext FROM render_profiles WHERE id = ?",
		created.ID,
	).Row().Scan(&originalCiphertext); err != nil {
		t.Fatalf("load original render profile ciphertext: %v", err)
	}

	const replacement = "synthetic-replacement-password"
	updated, err := service.UpdateRenderProfileByBusiness(ctx, businessID, created.ID, UpdateRenderProfileInput{
		Password: replacement,
	})
	if err != nil {
		t.Fatalf("update render profile: %v", err)
	}
	if updated.Password != "" || !updated.PasswordConfigured {
		t.Fatal("public update result did not keep the password write-only")
	}
	var legacy, replacementCiphertext sql.NullString
	if err := db.Raw(
		"SELECT password, password_ciphertext FROM render_profiles WHERE id = ?",
		created.ID,
	).Row().Scan(&legacy, &replacementCiphertext); err != nil {
		t.Fatalf("load updated render profile password: %v", err)
	}
	if legacy.Valid && legacy.String != "" {
		t.Fatal("updated render profile password was persisted in plaintext")
	}
	if !replacementCiphertext.Valid || replacementCiphertext.String == originalCiphertext {
		t.Fatal("updated render profile password did not replace the ciphertext")
	}
	resolved, err := service.GetRenderProfileForRenderingByBusiness(ctx, businessID, created.ID)
	if err != nil {
		t.Fatalf("resolve updated render profile: %v", err)
	}
	if resolved.Password != replacement {
		t.Fatal("trusted renderer did not receive the replacement password")
	}
	if len(resolver.calls) != 3 {
		t.Fatalf("credential resolver calls = %d, want create, update, and renderer operations", len(resolver.calls))
	}
}

func TestBackfillLegacyRenderProfilePasswordsEncryptsEveryStoredValue(t *testing.T) {
	service, db, resolver := newRenderProfilePasswordService(t)
	ctx := context.Background()
	businessID := uuid.NewString()
	for _, fixture := range []struct {
		id       string
		password string
		deleted  bool
	}{
		{id: uuid.NewString(), password: "synthetic-backfill-password-one"},
		{id: uuid.NewString(), password: "synthetic-backfill-password-two"},
		{id: uuid.NewString(), password: "synthetic-backfill-password-deleted", deleted: true},
	} {
		if err := db.Exec(`
			INSERT INTO render_profiles (id, business_id, name, password, password_protected)
			VALUES (?, ?, 'Legacy backfill profile', ?, TRUE)`,
			fixture.id,
			businessID,
			fixture.password,
		).Error; err != nil {
			t.Fatalf("seed legacy render profile: %v", err)
		}
		if fixture.deleted {
			if err := db.Exec(
				"UPDATE render_profiles SET deleted_at = CURRENT_TIMESTAMP WHERE id = ?",
				fixture.id,
			).Error; err != nil {
				t.Fatalf("soft-delete legacy render profile: %v", err)
			}
		}
	}

	migrated, err := service.BackfillLegacyRenderProfilePasswords(ctx)
	if err != nil {
		t.Fatalf("backfill legacy render profile passwords: %v", err)
	}
	if migrated != 3 {
		t.Fatalf("migrated profiles = %d, want 3", migrated)
	}
	var legacyCount, ciphertextCount int64
	if err := db.Model(struct{ Password string }{}).
		Table("render_profiles").
		Where("password IS NOT NULL AND password <> ''").
		Count(&legacyCount).Error; err != nil {
		t.Fatalf("count remaining legacy passwords: %v", err)
	}
	if err := db.Table("render_profiles").
		Where("password_ciphertext LIKE ?", renderProfilePasswordEnvelopePrefix+"%").
		Count(&ciphertextCount).Error; err != nil {
		t.Fatalf("count encrypted render profile passwords: %v", err)
	}
	if legacyCount != 0 || ciphertextCount != 3 {
		t.Fatalf("legacy/encrypted stored passwords = %d/%d, want 0/3", legacyCount, ciphertextCount)
	}
	if len(resolver.calls) != 3 {
		t.Fatalf("credential resolver calls = %d, want one per legacy password", len(resolver.calls))
	}

	migrated, err = service.BackfillLegacyRenderProfilePasswords(ctx)
	if err != nil {
		t.Fatalf("repeat legacy render profile password backfill: %v", err)
	}
	if migrated != 0 || len(resolver.calls) != 3 {
		t.Fatalf("repeat backfill migrated/calls = %d/%d, want 0/3", migrated, len(resolver.calls))
	}
}

func newRenderProfilePasswordService(t *testing.T) (*DocumentService, *gorm.DB, *renderProfileEncryptionResolver) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	if err := db.Exec(`CREATE TABLE render_profiles (
		id TEXT PRIMARY KEY,
		business_id TEXT NOT NULL,
		name TEXT NOT NULL,
		header_html TEXT,
		footer_html TEXT,
		watermark_text TEXT,
		banner_text TEXT,
		font_family TEXT,
		page_size TEXT,
		layout_config TEXT,
		password_protected BOOLEAN NOT NULL DEFAULT FALSE,
		password TEXT,
		password_ciphertext TEXT,
		copy_allowed BOOLEAN NOT NULL DEFAULT TRUE,
		print_allowed BOOLEAN NOT NULL DEFAULT TRUE,
		custom_labels TEXT,
		visibility_config TEXT,
		is_default BOOLEAN NOT NULL DEFAULT FALSE,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create render profiles: %v", err)
	}
	resolver := &renderProfileEncryptionResolver{
		key: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")),
	}
	service := &DocumentService{
		cfg:      &config.Config{},
		resolver: resolver,
		repo:     postgres.NewDocumentRepository(db),
		log:      logger.NewWithEnv("test"),
	}
	return service, db, resolver
}

func tamperRenderProfileCiphertext(value string) string {
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, renderProfilePasswordEnvelopePrefix))
	if err != nil || len(payload) == 0 {
		panic("test ciphertext is not a valid render profile password envelope")
	}
	payload[len(payload)-1] ^= 0x01
	return renderProfilePasswordEnvelopePrefix + base64.RawURLEncoding.EncodeToString(payload)
}
