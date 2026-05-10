CREATE TABLE IF NOT EXISTS telegram_otp_sessions (
    id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL UNIQUE,
    phone TEXT NOT NULL,
    purpose TEXT NOT NULL,
    deep_link_token_hash TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL,
    telegram_user_id INTEGER,
    telegram_chat_id INTEGER,
    telegram_username TEXT,
    telegram_first_name TEXT,
    telegram_last_name TEXT,
    device_json TEXT,
    location_json TEXT,
    expires_at DATETIME NOT NULL,
    opened_at DATETIME,
    code_sent_at DATETIME,
    verified_at DATETIME,
    cancelled_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_telegram_otp_sessions_request_id
    ON telegram_otp_sessions(request_id);
CREATE INDEX IF NOT EXISTS idx_telegram_otp_sessions_phone
    ON telegram_otp_sessions(phone);
CREATE INDEX IF NOT EXISTS idx_telegram_otp_sessions_status
    ON telegram_otp_sessions(status);
CREATE INDEX IF NOT EXISTS idx_telegram_otp_sessions_expires_at
    ON telegram_otp_sessions(expires_at);

CREATE TABLE IF NOT EXISTS user_devices (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    phone TEXT NOT NULL,
    device_id TEXT,
    platform TEXT NOT NULL,
    app_version TEXT,
    os_version TEXT,
    device_model TEXT,
    manufacturer TEXT,
    push_token TEXT,
    first_seen_at DATETIME NOT NULL,
    last_seen_at DATETIME NOT NULL,
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_user_devices_user_id
    ON user_devices(user_id);
CREATE INDEX IF NOT EXISTS idx_user_devices_phone
    ON user_devices(phone);
CREATE INDEX IF NOT EXISTS idx_user_devices_device_id
    ON user_devices(device_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_devices_user_device_unique
    ON user_devices(user_id, device_id);

CREATE TABLE IF NOT EXISTS user_location_events (
    id TEXT PRIMARY KEY,
    user_id TEXT,
    phone TEXT,
    device_id TEXT,
    event_type TEXT NOT NULL,
    latitude REAL,
    longitude REAL,
    accuracy_meters REAL,
    source TEXT,
    ip_address TEXT,
    user_agent TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_user_location_events_user_id
    ON user_location_events(user_id);
CREATE INDEX IF NOT EXISTS idx_user_location_events_phone
    ON user_location_events(phone);
CREATE INDEX IF NOT EXISTS idx_user_location_events_created_at
    ON user_location_events(created_at);

CREATE TABLE IF NOT EXISTS auth_events (
    id TEXT PRIMARY KEY,
    user_id TEXT,
    phone TEXT NOT NULL,
    event_type TEXT NOT NULL,
    device_id TEXT,
    telegram_user_id INTEGER,
    ip_address TEXT,
    user_agent TEXT,
    metadata_json TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_auth_events_user_id
    ON auth_events(user_id);
CREATE INDEX IF NOT EXISTS idx_auth_events_phone
    ON auth_events(phone);
CREATE INDEX IF NOT EXISTS idx_auth_events_event_type
    ON auth_events(event_type);
CREATE INDEX IF NOT EXISTS idx_auth_events_created_at
    ON auth_events(created_at);
