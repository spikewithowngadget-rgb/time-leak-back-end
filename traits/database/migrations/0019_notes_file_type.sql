ALTER TABLE notes ADD COLUMN file_type TEXT NOT NULL DEFAULT '' CHECK (file_type IN ('', 'image', 'video', 'audio', 'document'));
