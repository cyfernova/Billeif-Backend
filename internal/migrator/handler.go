package migrator

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"invoice-backend/internal/config"
	migrationbundle "invoice-backend/migrations"
)

var (
	ErrApplyMigrations = errors.New("apply database migrations")
	ErrCloseDatabase   = errors.New("close migration database")
	ErrDirtyDatabase   = errors.New("database migration state is dirty")
	ErrNoChange        = errors.New("no migration change")
	ErrNoVersion       = errors.New("no migration version")
	ErrOpenDatabase    = errors.New("open migration database")
	ErrReadVersion     = errors.New("read database migration version")
	ErrResolveDatabase = errors.New("resolve migration database")
)

const (
	StatusApplied  = "applied"
	StatusNoChange = "no_change"
)

type DatabaseResolver interface {
	Database(context.Context, *config.Config) (config.DatabaseCredentials, error)
}

type MigrationRunner interface {
	Up() error
	Version() (uint, bool, error)
	Close() error
}

type OpenMigration func(fs.FS, string, config.DatabaseCredentials) (MigrationRunner, error)

type Options struct {
	Bundle     fs.FS
	BundleRoot string
	Config     *config.Config
	Resolver   DatabaseResolver
	Open       OpenMigration
}

type Handler struct {
	bundle     fs.FS
	bundleRoot string
	config     *config.Config
	resolver   DatabaseResolver
	open       OpenMigration
}

type Event struct {
	ManifestChecksum string `json:"manifest_checksum"`
}

type Result struct {
	Status           string `json:"status"`
	Version          uint   `json:"version"`
	LatestVersion    uint   `json:"latest_version"`
	Dirty            bool   `json:"dirty"`
	ManifestChecksum string `json:"manifest_checksum"`
}

func New(options Options) (*Handler, error) {
	if options.Bundle == nil {
		return nil, fmt.Errorf("migration bundle is required")
	}
	if options.Config == nil {
		return nil, fmt.Errorf("migration config is required")
	}
	if options.Resolver == nil {
		return nil, fmt.Errorf("database resolver is required")
	}
	if options.Open == nil {
		return nil, fmt.Errorf("migration database opener is required")
	}
	root := strings.TrimSpace(options.BundleRoot)
	if root == "" {
		root = "."
	}
	return &Handler{
		bundle: options.Bundle, bundleRoot: root, config: options.Config,
		resolver: options.Resolver, open: options.Open,
	}, nil
}

func (h *Handler) Handle(ctx context.Context, event Event) (result Result, resultErr error) {
	manifest, err := migrationbundle.Verify(h.bundle, h.bundleRoot)
	if err != nil {
		return Result{}, err
	}
	if event.ManifestChecksum != manifest.Digest {
		return Result{}, fmt.Errorf("%w: invocation checksum", migrationbundle.ErrChecksumMismatch)
	}

	credentials, err := h.resolver.Database(ctx, h.config)
	if err != nil {
		return Result{}, ErrResolveDatabase
	}
	runner, err := h.open(h.bundle, h.bundleRoot, credentials)
	if err != nil {
		return Result{}, ErrOpenDatabase
	}
	defer func() {
		if err := runner.Close(); err != nil && resultErr == nil {
			result = Result{}
			resultErr = ErrCloseDatabase
		}
	}()

	_, dirty, err := runner.Version()
	if err != nil && !errors.Is(err, ErrNoVersion) {
		return Result{}, ErrReadVersion
	}
	if dirty {
		return Result{}, ErrDirtyDatabase
	}

	status := StatusApplied
	if err := runner.Up(); err != nil {
		if !errors.Is(err, ErrNoChange) {
			return Result{}, ErrApplyMigrations
		}
		status = StatusNoChange
	}

	version, dirty, err := runner.Version()
	if err != nil {
		return Result{}, ErrReadVersion
	}
	if dirty {
		return Result{}, ErrDirtyDatabase
	}
	if version != manifest.LatestVersion {
		return Result{}, ErrReadVersion
	}

	return Result{
		Status: status, Version: version, LatestVersion: manifest.LatestVersion,
		Dirty: false, ManifestChecksum: manifest.Digest,
	}, nil
}
