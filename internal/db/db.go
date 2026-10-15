package db

import (
	"database/sql"
	"embed"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type DB struct {
	*sql.DB
}

func Connect(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	// SQLite connection pool configuration for WAL mode
	conn.SetMaxOpenConns(1) // Single writer safe for SQLite
	conn.SetMaxIdleConns(1)

	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA synchronous = NORMAL;",
	}
	for _, pragma := range pragmas {
		if _, err := conn.Exec(pragma); err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to execute pragma %s: %w", pragma, err)
		}
	}

	d := &DB{DB: conn}
	if err := d.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return d, nil
}

func (d *DB) migrate() error {
	_, err := d.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations dir: %w", err)
	}

	type migrationFile struct {
		version  int
		fileName string
	}
	var files []migrationFile

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}
		ver, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		files = append(files, migrationFile{version: ver, fileName: entry.Name()})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].version < files[j].version
	})

	for _, mf := range files {
		var count int
		err := d.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", mf.version).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to query migration status for version %d: %w", mf.version, err)
		}
		if count > 0 {
			continue
		}

		content, err := migrationsFS.ReadFile(path.Join("migrations", mf.fileName))
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", mf.fileName, err)
		}

		tx, err := d.Begin()
		if err != nil {
			return fmt.Errorf("failed to start tx for migration %s: %w", mf.fileName, err)
		}

		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to execute migration %s: %w", mf.fileName, err)
		}

		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", mf.version, now); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to record migration %s: %w", mf.fileName, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit tx for migration %s: %w", mf.fileName, err)
		}
	}

	return nil
}
