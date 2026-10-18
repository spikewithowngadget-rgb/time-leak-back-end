-- backfillNoteMetadata runs in this migration's transaction before this SQL.
-- Existing note_files and created_at values are preserved byte for byte.
CREATE INDEX IF NOT EXISTS idx_notes_user_date_created_id
    ON notes(user_id, note_date, created_at, id);
