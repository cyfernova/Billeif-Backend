package migrator

import (
	"database/sql"
	"errors"
	"io/fs"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"invoice-backend/internal/config"

	"github.com/golang-migrate/migrate/v4"
	migratepostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var ErrInvalidDatabaseConfig = errors.New("invalid migration database configuration")

type postgresRunner struct {
	migrate *migrate.Migrate
}

func OpenPostgres(bundle fs.FS, bundleRoot string, credentials config.DatabaseCredentials) (MigrationRunner, error) {
	databaseURL, err := postgresURL(credentials)
	if err != nil {
		return nil, err
	}
	sourceDriver, err := iofs.New(bundle, bundleRoot)
	if err != nil {
		return nil, ErrOpenDatabase
	}
	database, err := sql.Open("postgres", databaseURL)
	if err != nil {
		_ = sourceDriver.Close()
		return nil, ErrOpenDatabase
	}
	configureMigrationDatabasePool(database)

	gormDatabase, err := openGORMPostgres(database)
	if err != nil {
		_ = sourceDriver.Close()
		_ = database.Close()
		return nil, ErrOpenDatabase
	}
	underlyingDatabase, err := gormDatabase.DB()
	if err != nil {
		_ = sourceDriver.Close()
		_ = database.Close()
		return nil, ErrOpenDatabase
	}
	databaseDriver, err := migratepostgres.WithInstance(underlyingDatabase, &migratepostgres.Config{})
	if err != nil {
		_ = sourceDriver.Close()
		_ = database.Close()
		return nil, ErrOpenDatabase
	}
	instance, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", databaseDriver)
	if err != nil {
		_ = sourceDriver.Close()
		_ = databaseDriver.Close()
		return nil, ErrOpenDatabase
	}
	return &postgresRunner{migrate: instance}, nil
}

type migrationDatabasePool interface {
	SetMaxOpenConns(int)
	SetMaxIdleConns(int)
	SetConnMaxIdleTime(time.Duration)
	SetConnMaxLifetime(time.Duration)
}

func configureMigrationDatabasePool(database migrationDatabasePool) {
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(0)
	database.SetConnMaxIdleTime(2 * time.Minute)
	database.SetConnMaxLifetime(10 * time.Minute)
}

func openGORMPostgres(database *sql.DB) (*gorm.DB, error) {
	return gorm.Open(gormpostgres.New(gormpostgres.Config{
		Conn:                 database,
		PreferSimpleProtocol: true,
	}), &gorm.Config{Logger: gormlogger.Discard})
}

func postgresURL(credentials config.DatabaseCredentials) (string, error) {
	host := strings.TrimSpace(credentials.Host)
	user := strings.TrimSpace(credentials.User)
	name := strings.TrimSpace(credentials.Name)
	sslMode := strings.TrimSpace(credentials.SSLMode)
	if host == "" || credentials.Port < 1 || credentials.Port > 65535 ||
		user == "" || credentials.Password == "" || name == "" || sslMode == "" ||
		strings.ContainsAny(name, `/\`) {
		return "", ErrInvalidDatabaseConfig
	}
	query := url.Values{}
	query.Set("connect_timeout", "10")
	query.Set("sslmode", sslMode)
	return (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, credentials.Password),
		Host:     net.JoinHostPort(host, strconv.Itoa(credentials.Port)),
		Path:     "/" + name,
		RawQuery: query.Encode(),
	}).String(), nil
}

func (r *postgresRunner) Up() error {
	if err := r.migrate.Up(); err != nil {
		switch {
		case errors.Is(err, migrate.ErrNoChange):
			return ErrNoChange
		default:
			var dirty migrate.ErrDirty
			if errors.As(err, &dirty) {
				return ErrDirtyDatabase
			}
			return ErrApplyMigrations
		}
	}
	return nil
}

func (r *postgresRunner) Version() (uint, bool, error) {
	version, dirty, err := r.migrate.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, ErrNoVersion
		}
		return 0, false, ErrReadVersion
	}
	return version, dirty, nil
}

func (r *postgresRunner) Close() error {
	sourceErr, databaseErr := r.migrate.Close()
	if sourceErr != nil || databaseErr != nil {
		return ErrCloseDatabase
	}
	return nil
}
