package repository

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"time-leak/internal/domain"
	dbtraits "time-leak/traits/database"
)

type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateUser(email, passwordHash, userLanguage string) (domain.User, error) {
	email = normalizeEmail(email)
	userLanguage = normalizeLanguage(userLanguage)
	if email == "" {
		return domain.User{}, errors.New("email is empty")
	}
	if passwordHash == "" {
		return domain.User{}, errors.New("password is empty")
	}

	userID := dbtraits.GenerateUUID()
	_, err := r.db.Exec(
		`INSERT INTO users (id, email, password, user_language) VALUES (?, ?, ?, ?)`,
		userID,
		email,
		passwordHash,
		userLanguage,
	)
	if err != nil {
		return domain.User{}, err
	}

	return r.GetUserByID(userID)
}

func (r *Repository) GetUserByEmail(email string) (domain.User, error) {
	email = normalizeEmail(email)
	row := r.db.QueryRow(
		`SELECT id, email, password, user_language, created_at FROM users WHERE email = ?`,
		email,
	)

	var user domain.User
	var createdAt string
	if err := row.Scan(&user.UserID, &user.Email, &user.Password, &user.UserLanguage, &createdAt); err != nil {
		return domain.User{}, err
	}
	user.CreatedAt = parseSQLiteTime(createdAt)
	return user, nil
}

func (r *Repository) GetUserByID(userID string) (domain.User, error) {
	if _, err := uuid.Parse(strings.TrimSpace(userID)); err != nil {
		return domain.User{}, errors.New("userId must be valid UUID")
	}

	row := r.db.QueryRow(
		`SELECT id, email, password, user_language, created_at FROM users WHERE id = ?`,
		userID,
	)

	var user domain.User
	var createdAt string
	if err := row.Scan(&user.UserID, &user.Email, &user.Password, &user.UserLanguage, &createdAt); err != nil {
		return domain.User{}, err
	}
	user.CreatedAt = parseSQLiteTime(createdAt)
	return user, nil
}

func (r *Repository) UpdateUserLanguage(userID, userLanguage string) error {
	if _, err := uuid.Parse(strings.TrimSpace(userID)); err != nil {
		return errors.New("userId must be valid UUID")
	}

	userLanguage = normalizeLanguage(userLanguage)
	res, err := r.db.Exec(
		`UPDATE users SET user_language = ? WHERE id = ?`,
		userLanguage,
		userID,
	)
	if err != nil {
		return err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (r *Repository) CreateNote(userID, noteType string) (domain.Note, error) {
	noteType = strings.TrimSpace(noteType)
	if userID == "" {
		return domain.Note{}, errors.New("userId is empty")
	}
	if _, err := uuid.Parse(strings.TrimSpace(userID)); err != nil {
		return domain.Note{}, errors.New("userId must be valid UUID")
	}
	if noteType == "" {
		return domain.Note{}, errors.New("note_type is empty")
	}

	noteID := dbtraits.GenerateUUID()
	_, err := r.db.Exec(
		`INSERT INTO notes (id, user_id, note_type) VALUES (?, ?, ?)`,
		noteID,
		userID,
		noteType,
	)
	if err != nil {
		return domain.Note{}, err
	}

	row := r.db.QueryRow(
		`SELECT id, user_id, note_type, created_at FROM notes WHERE id = ?`,
		noteID,
	)

	var note domain.Note
	var createdAt string
	if err := row.Scan(&note.ID, &note.UserID, &note.NoteType, &createdAt); err != nil {
		return domain.Note{}, err
	}
	note.CreatedAt = parseSQLiteTime(createdAt)
	return note, nil
}

func (r *Repository) ListNotesByUserID(userID string) ([]domain.Note, error) {
	if _, err := uuid.Parse(strings.TrimSpace(userID)); err != nil {
		return nil, errors.New("userId must be valid UUID")
	}

	rows, err := r.db.Query(
		`SELECT id, user_id, note_type, created_at FROM notes WHERE user_id = ? ORDER BY created_at ASC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notes := make([]domain.Note, 0)
	for rows.Next() {
		var note domain.Note
		var createdAt string
		if err := rows.Scan(&note.ID, &note.UserID, &note.NoteType, &createdAt); err != nil {
			return nil, err
		}
		note.CreatedAt = parseSQLiteTime(createdAt)
		notes = append(notes, note)
	}

	return notes, rows.Err()
}

func (r *Repository) GetOrCreateUUIDByEmail(email string) (string, error) {
	email = normalizeEmail(email)
	if email == "" {
		return "", errors.New("email is empty")
	}

	user, err := r.GetUserByEmail(email)
	if err == nil {
		return user.UserID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	created, err := r.CreateUser(email, dbtraits.GenerateUUID(), "en")
	if err == nil {
		return created.UserID, nil
	}

	// Handle race on unique(email): fetch again.
	if user, queryErr := r.GetUserByEmail(email); queryErr == nil {
		return user.UserID, nil
	}

	return "", err
}

func (r *Repository) Save(token string, rec domain.RefreshRecord) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("refresh token is empty")
	}

	_, err := r.db.Exec(
		`INSERT INTO refresh_tokens (token, user_id, email, expires_at, revoked) VALUES (?, ?, ?, ?, ?)`,
		token,
		rec.UserUUID,
		normalizeEmail(rec.Email),
		rec.ExpiresAt.UTC().Format(time.RFC3339Nano),
		boolToInt(rec.Revoked),
	)
	return err
}

func (r *Repository) Get(token string) (domain.RefreshRecord, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return domain.RefreshRecord{}, sql.ErrNoRows
	}

	row := r.db.QueryRow(
		`SELECT user_id, email, expires_at, revoked FROM refresh_tokens WHERE token = ?`,
		token,
	)

	var (
		rec       domain.RefreshRecord
		expiresAt string
		revoked   int
	)
	if err := row.Scan(&rec.UserUUID, &rec.Email, &expiresAt, &revoked); err != nil {
		return domain.RefreshRecord{}, err
	}

	rec.ExpiresAt = parseSQLiteTime(expiresAt)
	rec.Revoked = revoked != 0
	return rec, nil
}

func (r *Repository) Revoke(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return sql.ErrNoRows
	}

	res, err := r.db.Exec(`UPDATE refresh_tokens SET revoked = 1 WHERE token = ?`, token)
	if err != nil {
		return err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func normalizeEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}

func normalizeLanguage(userLanguage string) string {
	userLanguage = strings.TrimSpace(strings.ToLower(userLanguage))
	if userLanguage == "" {
		return "en"
	}
	return userLanguage
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func parseSQLiteTime(v string) time.Time {
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05-07:00",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t
		}
	}
	return time.Time{}
}
