package database

import (
	"database/sql"
	"testing"

	"go.uber.org/zap"
)

func TestNoteFeaturesMigrationPreservesExistingRows(t *testing.T) {
	db := migrationTestDB(t, true)
	if _, err := db.Exec(`INSERT INTO users(id,email,password) VALUES ('owner','owner@test.invalid','hash');
	 INSERT INTO notes(id,user_id,note_type,note_files,created_at) VALUES ('old-note','owner','old-title','["audio/keep.m4a"]','2024-02-29 12:00:00')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := CreateTables(db, zap.NewNop()); err != nil {
			t.Fatal(err)
		}
	}
	var id, kind, files, date, lifecycle string
	var title, audio, rule sql.NullString
	var completed bool
	if err := db.QueryRow(`SELECT id,note_type,note_files,note_date,title,title_audio_url,lifecycle,recurrence_rule,is_completed FROM notes WHERE id='old-note'`).Scan(&id, &kind, &files, &date, &title, &audio, &lifecycle, &rule, &completed); err != nil {
		t.Fatal(err)
	}
	if id != "old-note" || kind != "old-title" || files != `["audio/keep.m4a"]` || date != "2024-02-29" || title.Valid || audio.Valid || rule.Valid || lifecycle != "one_time" || completed {
		t.Fatal("upgrade changed existing data or new field defaults")
	}
	if _, err := db.Exec(`INSERT INTO share_tokens(id,note_id,token,created_by,expires_at) VALUES('share','old-note','testtoken123','owner','2030-01-01T00:00:00Z'); DELETE FROM users WHERE id='owner'`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM share_tokens`).Scan(&count); err != nil || count != 0 {
		t.Fatal("share token survived owner cascade")
	}
}
