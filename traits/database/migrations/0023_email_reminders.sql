-- Additive reminder state; existing users, notes and Flutter contracts stay intact.
CREATE TABLE IF NOT EXISTS email_reminder_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    days_before INTEGER NOT NULL DEFAULT 3 CHECK (days_before BETWEEN 1 AND 30),
    send_hour INTEGER NOT NULL DEFAULT 9 CHECK (send_hour BETWEEN 0 AND 23),
    timezone TEXT NOT NULL DEFAULT 'Asia/Almaty',
    revision INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT OR IGNORE INTO email_reminder_settings(id) VALUES (1);

-- Delivery requires explicit consent and ownership of a mailbox proved by OTP.
CREATE TABLE IF NOT EXISTS user_email_reminder_contacts (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    verified_at TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    language TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_email_reminder_contacts_enabled
    ON user_email_reminder_contacts(enabled, user_id);

CREATE TABLE IF NOT EXISTS email_reminder_deliveries (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    occurrence_date TEXT NOT NULL,
    days_before INTEGER NOT NULL CHECK (days_before BETWEEN 1 AND 30),
    notification_date TEXT NOT NULL,
    email TEXT NOT NULL,
    language TEXT NOT NULL,
    title TEXT NOT NULL,
    subject TEXT NOT NULL,
    text_body TEXT NOT NULL,
    html_body TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'sending', 'retry', 'sent', 'failed', 'cancelled')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    lease_token TEXT NOT NULL DEFAULT '',
    lease_until INTEGER NOT NULL DEFAULT 0,
    next_attempt_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    sent_at INTEGER,
    provider_id TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    UNIQUE(user_id, note_id, occurrence_date, days_before)
);
CREATE INDEX IF NOT EXISTS idx_email_reminder_deliveries_due
    ON email_reminder_deliveries(notification_date, status, next_attempt_at, lease_until);
CREATE INDEX IF NOT EXISTS idx_email_reminder_deliveries_user
    ON email_reminder_deliveries(user_id, status);
