-- Separate from otp_requests so delivery failures cannot reset email quotas.
CREATE TABLE IF NOT EXISTS email_otp_send_attempts (
    request_id TEXT PRIMARY KEY,
    email TEXT NOT NULL,
    requested_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_email_otp_attempts_email_time
    ON email_otp_send_attempts(email, requested_at);
