CREATE TABLE IF NOT EXISTS ads (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    image_url TEXT NOT NULL,
    target_url TEXT NOT NULL,
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ads_active_created_at ON ads(is_active, created_at ASC, id ASC);

CREATE TABLE IF NOT EXISTS user_ads_state (
    user_id TEXT PRIMARY KEY,
    last_ad_id TEXT,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (last_ad_id) REFERENCES ads(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_user_ads_state_updated_at ON user_ads_state(updated_at);

CREATE TRIGGER IF NOT EXISTS trigger_ads_updated_at
AFTER UPDATE ON ads
FOR EACH ROW
BEGIN
    UPDATE ads SET updated_at = CURRENT_TIMESTAMP WHERE id = OLD.id;
END;
