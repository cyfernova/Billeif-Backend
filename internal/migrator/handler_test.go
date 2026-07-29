package migrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"invoice-backend/internal/config"
	migrationbundle "invoice-backend/migrations"
)

const sentinelCredential = "sentinel-migration-password"

type fakeDatabaseResolver struct {
	calls       int
	credentials config.DatabaseCredentials
	err         error
}

func (f *fakeDatabaseResolver) Database(context.Context, *config.Config) (config.DatabaseCredentials, error) {
	f.calls++
	return f.credentials, f.err
}

type fakeMigrationRunner struct {
	upCalls int
	upErr   error
	version uint
	dirty   bool
}

func (f *fakeMigrationRunner) Up() error {
	f.upCalls++
	return f.upErr
}

func (f *fakeMigrationRunner) Version() (uint, bool, error) {
	return f.version, f.dirty, nil
}

func (f *fakeMigrationRunner) Close() error {
	return nil
}

func TestHandlerVerifiesBundleBeforeResolvingOrOpeningDatabase(t *testing.T) {
	fsys := handlerTestBundle()
	fsys["000001_test.up.sql"].Data = []byte("changed SQL\n")
	resolver := &fakeDatabaseResolver{}
	openCalls := 0
	handler, err := New(Options{
		Bundle:     fsys,
		BundleRoot: ".",
		Config:     handlerTestConfig(),
		Resolver:   resolver,
		Open: func(fs.FS, string, config.DatabaseCredentials) (MigrationRunner, error) {
			openCalls++
			return &fakeMigrationRunner{}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = handler.Handle(context.Background(), Event{
		ManifestChecksum: "c607d3977696904753bf2a763b7721ec1e631c4204d20a8fa180a5fec8e97801",
	})
	if !errors.Is(err, migrationbundle.ErrChecksumMismatch) {
		t.Fatalf("Handle() error = %v, want checksum mismatch", err)
	}
	if resolver.calls != 0 {
		t.Errorf("database resolver calls = %d, want 0", resolver.calls)
	}
	if openCalls != 0 {
		t.Errorf("database open calls = %d, want 0", openCalls)
	}
}

func TestHandlerRunsUpExactlyOnceAndReturnsSanitizedStatus(t *testing.T) {
	resolver := &fakeDatabaseResolver{credentials: config.DatabaseCredentials{
		Host: "database.internal", Port: 5432, User: "sentinel-migration-user",
		Password: sentinelCredential, Name: "invoice_db", SSLMode: "require",
	}}
	runner := &fakeMigrationRunner{version: 1}
	handler := newHandlerForTest(t, resolver, func(fs.FS, string, config.DatabaseCredentials) (MigrationRunner, error) {
		return runner, nil
	})

	result, err := handler.Handle(context.Background(), Event{
		ManifestChecksum: "c607d3977696904753bf2a763b7721ec1e631c4204d20a8fa180a5fec8e97801",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if runner.upCalls != 1 {
		t.Fatalf("Up() calls = %d, want 1", runner.upCalls)
	}
	if result.Status != StatusApplied || result.Version != 1 || result.Dirty {
		t.Fatalf("Handle() result = %+v, want applied version 1 and clean", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if strings.Contains(string(encoded), sentinelCredential) || strings.Contains(string(encoded), "sentinel-migration-user") {
		t.Fatalf("handler response leaked credential material: %s", encoded)
	}
}

func TestHandlerTreatsNoChangeAsSuccess(t *testing.T) {
	runner := &fakeMigrationRunner{version: 1, upErr: ErrNoChange}
	handler := newHandlerForTest(t, &fakeDatabaseResolver{}, func(fs.FS, string, config.DatabaseCredentials) (MigrationRunner, error) {
		return runner, nil
	})

	result, err := handler.Handle(context.Background(), Event{
		ManifestChecksum: "c607d3977696904753bf2a763b7721ec1e631c4204d20a8fa180a5fec8e97801",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != StatusNoChange {
		t.Fatalf("status = %q, want %q", result.Status, StatusNoChange)
	}
	if runner.upCalls != 1 {
		t.Fatalf("Up() calls = %d, want 1", runner.upCalls)
	}
}

func TestHandlerRejectsDirtyDatabaseWithoutRunningUp(t *testing.T) {
	runner := &fakeMigrationRunner{version: 1, dirty: true}
	handler := newHandlerForTest(t, &fakeDatabaseResolver{}, func(fs.FS, string, config.DatabaseCredentials) (MigrationRunner, error) {
		return runner, nil
	})

	_, err := handler.Handle(context.Background(), Event{
		ManifestChecksum: "c607d3977696904753bf2a763b7721ec1e631c4204d20a8fa180a5fec8e97801",
	})
	if !errors.Is(err, ErrDirtyDatabase) {
		t.Fatalf("Handle() error = %v, want dirty database error", err)
	}
	if runner.upCalls != 0 {
		t.Fatalf("Up() calls = %d, want 0", runner.upCalls)
	}
}

func TestHandlerSanitizesDatabaseResolutionAndOpenErrors(t *testing.T) {
	tests := []struct {
		name     string
		resolver *fakeDatabaseResolver
		open     OpenMigration
		wantErr  error
	}{
		{
			name:     "resolution",
			resolver: &fakeDatabaseResolver{err: fmt.Errorf("resolve %s", sentinelCredential)},
			open: func(fs.FS, string, config.DatabaseCredentials) (MigrationRunner, error) {
				return &fakeMigrationRunner{}, nil
			},
			wantErr: ErrResolveDatabase,
		},
		{
			name:     "open",
			resolver: &fakeDatabaseResolver{},
			open: func(fs.FS, string, config.DatabaseCredentials) (MigrationRunner, error) {
				return nil, fmt.Errorf("dial %s", sentinelCredential)
			},
			wantErr: ErrOpenDatabase,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newHandlerForTest(t, tt.resolver, tt.open)

			_, err := handler.Handle(context.Background(), Event{
				ManifestChecksum: "c607d3977696904753bf2a763b7721ec1e631c4204d20a8fa180a5fec8e97801",
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Handle() error = %v, want %v", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), sentinelCredential) {
				t.Fatalf("handler error leaked credential material: %v", err)
			}
		})
	}
}

func newHandlerForTest(t *testing.T, resolver DatabaseResolver, open OpenMigration) *Handler {
	t.Helper()
	handler, err := New(Options{
		Bundle:     handlerTestBundle(),
		BundleRoot: ".",
		Config:     handlerTestConfig(),
		Resolver:   resolver,
		Open:       open,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func handlerTestConfig() *config.Config {
	return &config.Config{Database: config.DatabaseConfig{
		Port: 5432, Name: "invoice_db", SSLMode: "require",
	}}
}

func handlerTestBundle() fstest.MapFS {
	return fstest.MapFS{
		"000001_test.up.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE test (id integer);\n")},
		"000001_test.down.sql": &fstest.MapFile{Data: []byte("DROP TABLE test;\n")},
		migrationbundle.ManifestFilename: &fstest.MapFile{Data: []byte(
			"5111d07169d0ba3c9f4c861fa6076c786f86469e298450c641c3e70ea21df8f6  000001_test.down.sql\n" +
				"30d16a80498b1d62b4b13130c046b82dedf340b99cdb15ba0d2500a7e6a102be  000001_test.up.sql\n",
		)},
	}
}
