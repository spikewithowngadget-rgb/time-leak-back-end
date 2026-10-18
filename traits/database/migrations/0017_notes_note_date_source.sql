ALTER TABLE notes ADD COLUMN note_date_source TEXT NOT NULL DEFAULT 'created_at' CHECK (note_date_source IN ('selected', 'created_at'));
