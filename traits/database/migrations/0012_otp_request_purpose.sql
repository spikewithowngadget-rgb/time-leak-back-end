ALTER TABLE otp_requests ADD COLUMN purpose TEXT NOT NULL DEFAULT 'registration';

CREATE INDEX IF NOT EXISTS idx_otp_requests_purpose_created_at
    ON otp_requests(purpose, created_at DESC);
