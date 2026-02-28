CREATE TABLE IF NOT EXISTS otp_requests (
    id TEXT PRIMARY KEY,
    channel TEXT NOT NULL,
    destination TEXT NOT NULL,
    code_hash TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    used_at DATETIME,
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_attempt_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_otp_requests_destination_created_at
    ON otp_requests(channel, destination, created_at DESC);

CREATE TABLE IF NOT EXISTS otp_destination_locks (
    channel TEXT NOT NULL,
    destination TEXT NOT NULL,
    failed_attempts INTEGER NOT NULL DEFAULT 0,
    locked_until DATETIME,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (channel, destination)
);
