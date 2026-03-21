CREATE TABLE IF NOT EXISTS admin_refresh_tokens (
    token TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    revoked INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_admin_refresh_tokens_expires_at ON admin_refresh_tokens(expires_at);
