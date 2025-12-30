//go:build ignore
// +build ignore

package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"invoice-backend/internal/config"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Migration represents a database migration
type Migration struct {
	Version   int
	Name      string
	Direction string
	SQL       string
}

func main() {
	direction := flag.String("direction", "", "Migration direction: up or down")
	create := flag.String("create", "", "Create new migration with given name")
	steps := flag.Int("steps", 0, "Number of migrations to run (0 = all)")
	flag.Parse()

	if *create != "" {
		createMigration(*create)
		return
	}

	if *direction == "" {
		log.Fatal("Please specify -direction=up or -direction=down")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.Name,
		cfg.Database.SSLMode,
	)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal("Failed to ping database:", err)
	}

	// Ensure migrations table exists
	if err := createMigrationsTable(db); err != nil {
		log.Fatal("Failed to create migrations table:", err)
	}

	migrations, err := loadMigrations(*direction)
	if err != nil {
		log.Fatal("Failed to load migrations:", err)
	}

	if *direction == "up" {
		runUp(db, migrations, *steps)
	} else if *direction == "down" {
		runDown(db, migrations, *steps)
	} else {
		log.Fatal("Invalid direction. Use 'up' or 'down'")
	}
}

func createMigrationsTable(db *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			applied_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		)
	`
	_, err := db.Exec(query)
	return err
}

func loadMigrations(direction string) ([]Migration, error) {
	var migrations []Migration

	migrationsDir := "./migrations"
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		if !strings.HasSuffix(filename, "."+direction+".sql") {
			continue
		}

		content, err := os.ReadFile(filepath.Join(migrationsDir, filename))
		if err != nil {
			return nil, fmt.Errorf("failed to read migration %s: %w", filename, err)
		}

		var version int
		var name string
		_, err = fmt.Sscanf(filename, "%06d_%s", &version, &name)
		if err != nil {
			continue
		}

		// Remove the direction suffix from name
		name = strings.TrimSuffix(name, "."+direction+".sql")

		migrations = append(migrations, Migration{
			Version:   version,
			Name:      name,
			Direction: direction,
			SQL:       string(content),
		})
	}

	// Sort migrations
	if direction == "up" {
		sort.Slice(migrations, func(i, j int) bool {
			return migrations[i].Version < migrations[j].Version
		})
	} else {
		sort.Slice(migrations, func(i, j int) bool {
			return migrations[i].Version > migrations[j].Version
		})
	}

	return migrations, nil
}

func runUp(db *sql.DB, migrations []Migration, steps int) {
	applied := getAppliedMigrations(db)
	count := 0

	for _, m := range migrations {
		if steps > 0 && count >= steps {
			break
		}

		if _, ok := applied[m.Version]; ok {
			continue
		}

		log.Printf("Applying migration %06d_%s...\n", m.Version, m.Name)

		tx, err := db.Begin()
		if err != nil {
			log.Fatalf("Failed to begin transaction: %v", err)
		}

		if _, err := tx.Exec(m.SQL); err != nil {
			tx.Rollback()
			log.Fatalf("Failed to execute migration %06d_%s: %v", m.Version, m.Name, err)
		}

		if _, err := tx.Exec(
			"INSERT INTO schema_migrations (version, name) VALUES ($1, $2)",
			m.Version, m.Name,
		); err != nil {
			tx.Rollback()
			log.Fatalf("Failed to record migration: %v", err)
		}

		if err := tx.Commit(); err != nil {
			log.Fatalf("Failed to commit transaction: %v", err)
		}

		log.Printf("Applied migration %06d_%s\n", m.Version, m.Name)
		count++
	}

	if count == 0 {
		log.Println("No migrations to apply")
	} else {
		log.Printf("Applied %d migration(s)\n", count)
	}
}

func runDown(db *sql.DB, migrations []Migration, steps int) {
	applied := getAppliedMigrations(db)
	count := 0

	if steps == 0 {
		steps = 1 // Default to rolling back only 1 migration
	}

	for _, m := range migrations {
		if steps > 0 && count >= steps {
			break
		}

		if _, ok := applied[m.Version]; !ok {
			continue
		}

		log.Printf("Rolling back migration %06d_%s...\n", m.Version, m.Name)

		tx, err := db.Begin()
		if err != nil {
			log.Fatalf("Failed to begin transaction: %v", err)
		}

		if _, err := tx.Exec(m.SQL); err != nil {
			tx.Rollback()
			log.Fatalf("Failed to execute rollback %06d_%s: %v", m.Version, m.Name, err)
		}

		if _, err := tx.Exec(
			"DELETE FROM schema_migrations WHERE version = $1",
			m.Version,
		); err != nil {
			tx.Rollback()
			log.Fatalf("Failed to remove migration record: %v", err)
		}

		if err := tx.Commit(); err != nil {
			log.Fatalf("Failed to commit transaction: %v", err)
		}

		log.Printf("Rolled back migration %06d_%s\n", m.Version, m.Name)
		count++
	}

	if count == 0 {
		log.Println("No migrations to roll back")
	} else {
		log.Printf("Rolled back %d migration(s)\n", count)
	}
}

func getAppliedMigrations(db *sql.DB) map[int]bool {
	applied := make(map[int]bool)

	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return applied
	}
	defer rows.Close()

	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			continue
		}
		applied[version] = true
	}

	return applied
}

func createMigration(name string) {
	migrationsDir := "./migrations"

	// Get the next version number
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		log.Fatal("Failed to read migrations directory:", err)
	}

	maxVersion := 0
	for _, entry := range entries {
		var version int
		if _, err := fmt.Sscanf(entry.Name(), "%06d_", &version); err == nil {
			if version > maxVersion {
				maxVersion = version
			}
		}
	}

	nextVersion := maxVersion + 1
	timestamp := time.Now().Format("20060102150405")
	safeName := strings.ReplaceAll(strings.ToLower(name), " ", "_")

	upFile := filepath.Join(migrationsDir, fmt.Sprintf("%06d_%s.up.sql", nextVersion, safeName))
	downFile := filepath.Join(migrationsDir, fmt.Sprintf("%06d_%s.down.sql", nextVersion, safeName))

	upContent := fmt.Sprintf(`-- Migration: %s
-- Created at: %s
-- Description: Add description here

-- Write your UP migration SQL here

`, name, timestamp)

	downContent := fmt.Sprintf(`-- Migration: %s (rollback)
-- Created at: %s
-- Description: Rollback for %s

-- Write your DOWN migration SQL here

`, name, timestamp, name)

	if err := os.WriteFile(upFile, []byte(upContent), 0644); err != nil {
		log.Fatal("Failed to create up migration file:", err)
	}

	if err := os.WriteFile(downFile, []byte(downContent), 0644); err != nil {
		log.Fatal("Failed to create down migration file:", err)
	}

	log.Printf("Created migration files:\n  %s\n  %s\n", upFile, downFile)
}
