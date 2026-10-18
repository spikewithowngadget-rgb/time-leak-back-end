package database

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time-leak/config"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func InitDatabase(cfg *config.Config, logger *zap.Logger) (*sql.DB, error) {
	if err := os.MkdirAll(cfg.DBPath, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir db path: %w", err)
	}

	dbPath := cfg.GetDatabasePath()
	dsn := dbPath + "?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000"

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	logger.Info("database initialized",
		zap.String("path", dbPath),
		zap.Int("max_open_conns", cfg.MaxOpenConns),
		zap.Int("max_idle_conns", cfg.MaxIdleConns),
	)

	if err := CreateTables(db, logger); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func GenerateUUID() string {
	return uuid.New().String()
}

// CreateTables is kept for backward compatibility and now applies SQL migrations.
func CreateTables(db *sql.DB, logger *zap.Logger) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := getAppliedMigrations(db)
	if err != nil {
		return err
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		if applied[name] {
			continue
		}

		raw, readErr := migrationsFS.ReadFile("migrations/" + name)
		if readErr != nil {
			return fmt.Errorf("read migration %s: %w", name, readErr)
		}

		tx, beginErr := db.Begin()
		if beginErr != nil {
			return fmt.Errorf("begin migration %s: %w", name, beginErr)
		}

		var execErr error
		if name == "0020_notes_metadata_backfill.sql" {
			execErr = backfillNoteMetadata(tx)
		}
		if execErr == nil {
			execErr = applyMigrationSQL(tx, string(raw))
		}
		if execErr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("exec migration %s: %w", name, execErr)
		}

		if _, insErr := tx.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, name); insErr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, insErr)
		}

		if commitErr := tx.Commit(); commitErr != nil {
			return fmt.Errorf("commit migration %s: %w", name, commitErr)
		}

		logger.Info("migration applied", zap.String("migration", name))
	}

	logger.Info("database schema created/verified successfully")
	return nil
}

func getAppliedMigrations(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	out := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		out[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}
	return out, nil
}

var leadingAddColumn = regexp.MustCompile(`(?is)^\s*ALTER\s+TABLE\s+([a-z_][a-z0-9_]*)\s+ADD\s+COLUMN\s+([a-z_][a-z0-9_]*)\b[^;]*;`)

// Recover an already-added column without skipping the rest of a migration
// (notably the index following ALTER TABLE in migration 0012). Other SQL errors
// abort the transaction; they must never mark a partial migration as applied.
func applyMigrationSQL(tx *sql.Tx, raw string) error {
	for {
		match := leadingAddColumn.FindStringSubmatch(raw)
		if match == nil {
			break
		}
		rows, err := tx.Query(`PRAGMA table_info("` + match[1] + `")`)
		if err != nil {
			return err
		}
		exists := false
		for rows.Next() {
			var cid, notNull, pk int
			var name, columnType string
			var defaultValue sql.NullString
			if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
				rows.Close()
				return err
			}
			exists = exists || strings.EqualFold(name, match[2])
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if !exists {
			if _, err := tx.Exec(match[0]); err != nil {
				return err
			}
		}
		raw = raw[len(match[0]):]
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	_, err := tx.Exec(raw)
	return err
}
