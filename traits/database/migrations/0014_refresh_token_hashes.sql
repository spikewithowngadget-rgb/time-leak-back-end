CREATE TABLE IF NOT EXISTS refresh_token_sessions (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    user_id TEXT NOT NULL,
    phone TEXT NOT NULL,
    auth_type TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user',
    expires_at DATETIME NOT NULL,
    revoked INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at DATETIME,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_refresh_token_sessions_user_id
    ON refresh_token_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_token_sessions_expires_at
    ON refresh_token_sessions(expires_at);

CREATE TABLE IF NOT EXISTS admin_refresh_token_sessions (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    username TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    revoked INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_admin_refresh_token_sessions_expires_at
    ON admin_refresh_token_sessions(expires_at);
