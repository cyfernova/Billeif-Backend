package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"time"
)

const advisoryLockID int64 = 724389215

//go:embed *.up.sql
var upMigrations embed.FS

func UpFiles() ([]string, error) {
	files, err := fs.Glob(upMigrations, "*.up.sql")
	if err != nil {
		return nil, fmt.Errorf("list embedded migrations: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func Up(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve migration connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", advisoryLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(unlockCtx, "SELECT pg_advisory_unlock($1)", advisoryLockID)
	}()

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("create schema migrations table: %w", err)
	}

	files, err := UpFiles()
	if err != nil {
		return err
	}

	for _, file := range files {
		var applied bool
		if err := conn.QueryRowContext(
			ctx,
			"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)",
			file,
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", file, err)
		}
		if applied {
			continue
		}

		statement, err := upMigrations.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", file, err)
		}

		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", file, err)
		}
		if _, err := tx.ExecContext(ctx, string(statement)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", file, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)",
			file,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", file, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", file, err)
		}
	}

	return nil
}
