package database

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"go.uber.org/zap"
)

func migrationTestDB(t *testing.T, legacy bool) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "test.db")+"?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if !legacy {
		return db
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at DATETIME DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if entry.Name() < "0016" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		raw, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := applyMigrationSQL(tx, string(raw)); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(name) VALUES (?)`, name); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestNotesMigrations_FreshAndRepeat(t *testing.T) {
	db := migrationTestDB(t, false)
	for i := 0; i < 2; i++ {
		if err := CreateTables(db, zap.NewNop()); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	entries, _ := migrationsFS.ReadDir("migrations")
	if count != len(entries) {
		t.Fatalf("applied %d migrations, want %d", count, len(entries))
	}
	var indexes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='idx_notes_user_date_created_id'`).Scan(&indexes); err != nil || indexes != 1 {
		t.Fatalf("index count=%d err=%v", indexes, err)
	}
}

func TestNotesMigrations_UpgradePreservesLegacyData(t *testing.T) {
	db := migrationTestDB(t, true)
	if _, err := db.Exec(`INSERT INTO users (id,email,password) VALUES ('owner','owner@example.test','hash')`); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		id, files, created, date, kind string
		count                          int
	}{
		{"image", `["  photo/A.JPG  ", ""]`, "2026-10-15 23:59:59", "2026-10-15", "image", 1},
		{"mixed", `["audio/a.m4a","photo/b.png"]`, "2026-10-15 00:00:00", "2026-10-15", "audio", 2},
		{"none", `[]`, "2026-10-15 12:00:00", "2026-10-15", "", 0},
		{"malformed", `not-json`, "2026-10-15 12:00:00", "2026-10-15", "", 0},
		{"wrong-array", `[42]`, "2026-10-15 12:00:00", "2026-10-15", "", 0},
		{"offset", `["document/a.pdf"]`, "2026-10-15T01:30:00+05:00", "2026-10-14", "document", 1},
	}
	for _, tc := range tests {
		if _, err := db.Exec(`INSERT INTO notes (id,user_id,note_type,note_files,created_at) VALUES (?,'owner','unchanged',?,?)`, tc.id, tc.files, tc.created); err != nil {
			t.Fatal(err)
		}
	}
	readOriginal := func() [][5]string {
		rows, err := db.Query(`SELECT id,user_id,note_type,note_files,CAST(created_at AS TEXT) FROM notes ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var result [][5]string
		for rows.Next() {
			var v [5]string
			if err := rows.Scan(&v[0], &v[1], &v[2], &v[3], &v[4]); err != nil {
				t.Fatal(err)
			}
			result = append(result, v)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := readOriginal()
	// Simulate a column added before its migration was recorded.
	if _, err := db.Exec(`ALTER TABLE notes ADD COLUMN file_type TEXT NOT NULL DEFAULT ''`); err != nil {
		t.Fatal(err)
	}
	if err := CreateTables(db, zap.NewNop()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, readOriginal()) {
		t.Fatal("legacy note values changed during migration")
	}
	for _, tc := range tests {
		var date, source, kind string
		var count int
		if err := db.QueryRow(`SELECT note_date,note_date_source,file_count,file_type FROM notes WHERE id=?`, tc.id).Scan(&date, &source, &count, &kind); err != nil {
			t.Fatal(err)
		}
		if date != tc.date || source != "created_at" || count != tc.count || kind != tc.kind {
			t.Errorf("%s: got %s %s %d %s", tc.id, date, source, count, kind)
		}
	}
	if _, err := db.Exec(`UPDATE notes SET note_date='2030-03-20', note_date_source='selected' WHERE id='image'`); err != nil {
		t.Fatal(err)
	}
	// Recovered/replayed data migration must preserve both selected dates and provenance.
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE name='0020_notes_metadata_backfill.sql'`); err != nil {
		t.Fatal(err)
	}
	if err := CreateTables(db, zap.NewNop()); err != nil {
		t.Fatal(err)
	}
	var date, source string
	if err := db.QueryRow(`SELECT note_date,note_date_source FROM notes WHERE id='image'`).Scan(&date, &source); err != nil {
		t.Fatal(err)
	}
	if date != "2030-03-20" || source != "selected" {
		t.Fatal("selected date was overwritten")
	}
	if err := db.QueryRow(`SELECT note_date_source FROM notes WHERE id='offset'`).Scan(&source); err != nil || source != "created_at" {
		t.Fatal("fallback provenance was overwritten")
	}
}

func TestMigrationSQL_ExistingColumnStillCreatesFollowingIndex(t *testing.T) {
	db := migrationTestDB(t, false)
	if _, err := db.Exec(`CREATE TABLE fixture (id TEXT, purpose TEXT)`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMigrationSQL(tx, `ALTER TABLE fixture ADD COLUMN purpose TEXT; CREATE INDEX fixture_purpose ON fixture(purpose);`); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='fixture_purpose'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("index count=%d err=%v", count, err)
	}
}

func TestMigrationSQL_UnrelatedErrorIsNotIgnored(t *testing.T) {
	db := migrationTestDB(t, false)
	if _, err := db.Exec(`CREATE TABLE existing (id TEXT)`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	err = applyMigrationSQL(tx, `CREATE TABLE partial (id TEXT); CREATE TABLE existing (id TEXT);`)
	if err == nil {
		t.Fatal("expected duplicate table error")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='partial'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial transaction survived: %d %v", count, err)
	}
}

func TestNotesMigrations_InvalidDateFailsWithoutRecordingBackfill(t *testing.T) {
	db := migrationTestDB(t, true)
	if _, err := db.Exec(`INSERT INTO users(id,email,password) VALUES ('owner','owner@example.test','hash'); INSERT INTO notes(id,user_id,note_type,created_at) VALUES ('bad','owner','note','invalid')`); err != nil {
		t.Fatal(err)
	}
	if err := CreateTables(db, zap.NewNop()); err == nil {
		t.Fatal("expected invalid legacy timestamp to fail migration")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name='0020_notes_metadata_backfill.sql'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed migration recorded: %d %v", count, err)
	}
	if _, err := db.Exec(`UPDATE notes SET created_at='2026-10-15 00:00:00' WHERE id='bad'`); err != nil {
		t.Fatal(err)
	}
	if err := CreateTables(db, zap.NewNop()); err != nil {
		t.Fatal(err)
	}
}
