ALTER TABLE notes ADD COLUMN title TEXT CHECK (title IS NULL OR length(title) <= 200);
ALTER TABLE notes ADD COLUMN title_audio_url TEXT;
ALTER TABLE notes ADD COLUMN lifecycle TEXT NOT NULL DEFAULT 'one_time' CHECK (lifecycle IN ('one_time', 'recurring'));
ALTER TABLE notes ADD COLUMN recurrence_rule TEXT;
ALTER TABLE notes ADD COLUMN is_completed INTEGER NOT NULL DEFAULT 0 CHECK (is_completed IN (0, 1));

CREATE INDEX IF NOT EXISTS idx_notes_user_lifecycle_date ON notes(user_id, lifecycle, note_date);
CREATE INDEX IF NOT EXISTS idx_notes_incomplete ON notes(note_date) WHERE lifecycle = 'one_time' AND is_completed = 0;

CREATE TABLE IF NOT EXISTS share_tokens (
    id TEXT PRIMARY KEY,
    note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    token TEXT NOT NULL UNIQUE,
    created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    views_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_share_tokens_note ON share_tokens(note_id);
