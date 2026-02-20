package database

import (
	"database/sql"
	"os"
	"time"
	"time-leak/config"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

var _ = time.Second

func InitDatabase(cfg *config.Config, logger *zap.Logger) (*sql.DB, error) {
	if err := os.MkdirAll(cfg.DBPath, 0o755); err != nil {
		return nil, err
	}

	dbPath := cfg.GetDatabasePath()
	dsn := dbPath + "?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000"

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
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

func CreateTables(db *sql.DB, logger *zap.Logger) error {
	usersTable := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		password TEXT NOT NULL,
		user_language TEXT NOT NULL DEFAULT 'en',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`

	notesTable := `
	CREATE TABLE IF NOT EXISTS notes (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		note_type TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);
	`

	refreshTokensTable := `
	CREATE TABLE IF NOT EXISTS refresh_tokens (
		token TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		email TEXT NOT NULL,
		expires_at DATETIME NOT NULL,
		revoked INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);
	`

	if _, err := db.Exec(usersTable); err != nil {
		logger.Error("failed creating users table", zap.Error(err))
		return err
	}
	if _, err := db.Exec(notesTable); err != nil {
		logger.Error("failed creating notes table", zap.Error(err))
		return err
	}
	if _, err := db.Exec(refreshTokensTable); err != nil {
		logger.Error("failed creating refresh_tokens table", zap.Error(err))
		return err
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);`,
		`CREATE INDEX IF NOT EXISTS idx_notes_user_id ON notes(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_notes_created_at ON notes(created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);`,
	}
	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			logger.Warn("failed creating index", zap.String("sql", idx), zap.Error(err))
		}
	}

	trigger := `
	CREATE TRIGGER IF NOT EXISTS trigger_users_updated_at
	AFTER UPDATE ON users
	FOR EACH ROW
	BEGIN
		UPDATE users SET updated_at = CURRENT_TIMESTAMP WHERE id = OLD.id;
	END;
	`
	if _, err := db.Exec(trigger); err != nil {
		logger.Warn("failed creating users updated_at trigger", zap.Error(err))
	}

	logger.Info("database schema created/verified successfully")
	return nil
}
