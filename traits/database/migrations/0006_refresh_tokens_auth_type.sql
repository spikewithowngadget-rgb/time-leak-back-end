ALTER TABLE refresh_tokens ADD COLUMN auth_type TEXT NOT NULL DEFAULT 'password';
