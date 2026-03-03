CREATE TABLE IF NOT EXISTS auth_verifications (
    id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL UNIQUE,
    purpose TEXT NOT NULL,
    phone TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    used_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (request_id) REFERENCES otp_requests(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_auth_verifications_phone_purpose_created_at
    ON auth_verifications(phone, purpose, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_auth_verifications_expires_at
    ON auth_verifications(expires_at);
