package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

func (r *Repository) CreateUser(ctx context.Context, email, passwordHash, userLanguage string) (domain.User, error) {
	email = normalizeEmail(email)
	userLanguage = normalizeLanguage(userLanguage)
	if email == "" {
		return domain.User{}, errors.New("email is empty")
	}
	if passwordHash == "" {
		return domain.User{}, errors.New("password is empty")
	}

	userID := dbtraits.GenerateUUID()
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO users (id, email, password, user_language) VALUES (?, ?, ?, ?)`,
		userID,
		email,
		passwordHash,
		userLanguage,
	)
	if err != nil {
		return domain.User{}, fmt.Errorf("insert user: %w", err)
	}

	return r.GetUserByID(ctx, userID)
}

func (r *Repository) CreateUserWithPhone(ctx context.Context, email, phone, passwordHash, userLanguage string) (domain.User, error) {
	email = normalizeEmail(email)
	phone = normalizePhone(phone)
	userLanguage = normalizeLanguage(userLanguage)
	if email == "" {
		return domain.User{}, errors.New("email is empty")
	}
	if phone == "" {
		return domain.User{}, errors.New("phone is empty")
	}
	if passwordHash == "" {
		return domain.User{}, errors.New("password is empty")
	}

	userID := dbtraits.GenerateUUID()
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO users (id, email, phone, password, user_language) VALUES (?, ?, ?, ?, ?)`,
		userID,
		email,
		phone,
		passwordHash,
		userLanguage,
	)
	if err != nil {
		return domain.User{}, fmt.Errorf("insert user with phone: %w", err)
	}

	return r.GetUserByID(ctx, userID)
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	email = normalizeEmail(email)
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, email, COALESCE(phone, ''), password, user_language, created_at FROM users WHERE email = ?`,
		email,
	)

	var user domain.User
	var createdAt string
	if err := row.Scan(&user.UserID, &user.Email, &user.Phone, &user.Password, &user.UserLanguage, &createdAt); err != nil {
		return domain.User{}, err
	}
	user.CreatedAt = parseSQLiteTime(createdAt)
	return user, nil
}

func (r *Repository) GetUserByPhone(ctx context.Context, phone string) (domain.User, error) {
	phone = normalizePhone(phone)
	if phone == "" {
		return domain.User{}, errors.New("phone is empty")
	}

	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, email, COALESCE(phone, ''), password, user_language, created_at FROM users WHERE phone = ?`,
		phone,
	)

	var user domain.User
	var createdAt string
	if err := row.Scan(&user.UserID, &user.Email, &user.Phone, &user.Password, &user.UserLanguage, &createdAt); err != nil {
		return domain.User{}, err
	}
	user.CreatedAt = parseSQLiteTime(createdAt)
	return user, nil
}

func (r *Repository) GetUserByID(ctx context.Context, userID string) (domain.User, error) {
	if _, err := uuid.Parse(strings.TrimSpace(userID)); err != nil {
		return domain.User{}, errors.New("userId must be valid UUID")
	}

	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, email, COALESCE(phone, ''), password, user_language, created_at FROM users WHERE id = ?`,
		userID,
	)

	var user domain.User
	var createdAt string
	if err := row.Scan(&user.UserID, &user.Email, &user.Phone, &user.Password, &user.UserLanguage, &createdAt); err != nil {
		return domain.User{}, err
	}
	user.CreatedAt = parseSQLiteTime(createdAt)
	return user, nil
}

func (r *Repository) UpdateUserLanguage(ctx context.Context, userID, userLanguage string) error {
	if _, err := uuid.Parse(strings.TrimSpace(userID)); err != nil {
		return errors.New("userId must be valid UUID")
	}

	userLanguage = normalizeLanguage(userLanguage)
	res, err := r.db.ExecContext(
		ctx,
		`UPDATE users SET user_language = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		userLanguage,
		userID,
	)
	if err != nil {
		return fmt.Errorf("update user language: %w", err)
	}

	return ensureRowsAffected(res)
}

func (r *Repository) UpdateUserEmail(ctx context.Context, userID, email string) error {
	if _, err := uuid.Parse(strings.TrimSpace(userID)); err != nil {
		return errors.New("userId must be valid UUID")
	}
	email = normalizeEmail(email)
	if email == "" {
		return errors.New("email is empty")
	}

	res, err := r.db.ExecContext(
		ctx,
		`UPDATE users SET email = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		email,
		userID,
	)
	if err != nil {
		return fmt.Errorf("update user email: %w", err)
	}

	return ensureRowsAffected(res)
}

func (r *Repository) UpdateUserPhone(ctx context.Context, userID, phone string) error {
	if _, err := uuid.Parse(strings.TrimSpace(userID)); err != nil {
		return errors.New("userId must be valid UUID")
	}
	phone = normalizePhone(phone)
	if phone == "" {
		return errors.New("phone is empty")
	}

	res, err := r.db.ExecContext(
		ctx,
		`UPDATE users SET phone = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		phone,
		userID,
	)
	if err != nil {
		return fmt.Errorf("update user phone: %w", err)
	}

	return ensureRowsAffected(res)
}

func (r *Repository) UpdateUserPasswordByPhone(ctx context.Context, phone, passwordHash string) error {
	phone = normalizePhone(phone)
	passwordHash = strings.TrimSpace(passwordHash)
	if phone == "" {
		return errors.New("phone is empty")
	}
	if passwordHash == "" {
		return errors.New("password hash is empty")
	}

	res, err := r.db.ExecContext(
		ctx,
		`UPDATE users SET password = ?, updated_at = CURRENT_TIMESTAMP WHERE phone = ?`,
		passwordHash,
		phone,
	)
	if err != nil {
		return fmt.Errorf("update user password by phone: %w", err)
	}
	return ensureRowsAffected(res)
}

func (r *Repository) CreateNote(ctx context.Context, userID, noteType string, noteFiles []string) (domain.Note, error) {
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
	noteFilesJSON, err := marshalNoteFiles(noteFiles)
	if err != nil {
		return domain.Note{}, err
	}

	noteID := dbtraits.GenerateUUID()
	_, err = r.db.ExecContext(
		ctx,
		`INSERT INTO notes (id, user_id, note_type, note_files) VALUES (?, ?, ?, ?)`,
		noteID,
		userID,
		noteType,
		noteFilesJSON,
	)
	if err != nil {
		return domain.Note{}, err
	}

	return r.GetNoteByID(ctx, noteID)
}

func (r *Repository) GetNoteByID(ctx context.Context, noteID string) (domain.Note, error) {
	return r.getNote(ctx, noteID, "")
}

func (r *Repository) GetNoteByIDForUser(ctx context.Context, noteID, userID string) (domain.Note, error) {
	return r.getNote(ctx, noteID, userID)
}

func (r *Repository) UpdateNote(ctx context.Context, noteID, userID, noteType string, noteFiles []string) (domain.Note, error) {
	noteType = strings.TrimSpace(noteType)
	if noteType == "" {
		return domain.Note{}, errors.New("note_type is empty")
	}
	if _, err := uuid.Parse(strings.TrimSpace(noteID)); err != nil {
		return domain.Note{}, errors.New("note id must be valid UUID")
	}
	if _, err := uuid.Parse(strings.TrimSpace(userID)); err != nil {
		return domain.Note{}, errors.New("userId must be valid UUID")
	}

	noteFilesJSON, err := marshalNoteFiles(noteFiles)
	if err != nil {
		return domain.Note{}, err
	}

	res, err := r.db.ExecContext(
		ctx,
		`UPDATE notes SET note_type = ?, note_files = ? WHERE id = ? AND user_id = ?`,
		noteType,
		noteFilesJSON,
		noteID,
		userID,
	)
	if err != nil {
		return domain.Note{}, err
	}
	if err := ensureRowsAffected(res); err != nil {
		return domain.Note{}, err
	}

	return r.GetNoteByIDForUser(ctx, noteID, userID)
}

func (r *Repository) DeleteNote(ctx context.Context, noteID, userID string) error {
	if _, err := uuid.Parse(strings.TrimSpace(noteID)); err != nil {
		return errors.New("note id must be valid UUID")
	}
	if _, err := uuid.Parse(strings.TrimSpace(userID)); err != nil {
		return errors.New("userId must be valid UUID")
	}

	res, err := r.db.ExecContext(
		ctx,
		`DELETE FROM notes WHERE id = ? AND user_id = ?`,
		noteID,
		userID,
	)
	if err != nil {
		return err
	}

	return ensureRowsAffected(res)
}

func (r *Repository) ListNotesByUserID(ctx context.Context, userID string) ([]domain.Note, error) {
	if _, err := uuid.Parse(strings.TrimSpace(userID)); err != nil {
		return nil, errors.New("userId must be valid UUID")
	}

	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, user_id, note_type, COALESCE(note_files, '[]'), created_at FROM notes WHERE user_id = ? ORDER BY created_at ASC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notes := make([]domain.Note, 0)
	for rows.Next() {
		var note domain.Note
		var noteFilesJSON string
		var createdAt string
		if err := rows.Scan(&note.ID, &note.UserID, &note.NoteType, &noteFilesJSON, &createdAt); err != nil {
			return nil, err
		}
		note.NoteFiles = unmarshalNoteFiles(noteFilesJSON)
		note.CreatedAt = parseSQLiteTime(createdAt)
		notes = append(notes, note)
	}

	return notes, rows.Err()
}

func (r *Repository) getNote(ctx context.Context, noteID, userID string) (domain.Note, error) {
	if _, err := uuid.Parse(strings.TrimSpace(noteID)); err != nil {
		return domain.Note{}, errors.New("note id must be valid UUID")
	}

	query := `SELECT id, user_id, note_type, COALESCE(note_files, '[]'), created_at FROM notes WHERE id = ?`
	args := []any{noteID}
	if userID != "" {
		if _, err := uuid.Parse(strings.TrimSpace(userID)); err != nil {
			return domain.Note{}, errors.New("userId must be valid UUID")
		}
		query += ` AND user_id = ?`
		args = append(args, userID)
	}

	row := r.db.QueryRowContext(ctx, query, args...)

	var note domain.Note
	var noteFilesJSON string
	var createdAt string
	if err := row.Scan(&note.ID, &note.UserID, &note.NoteType, &noteFilesJSON, &createdAt); err != nil {
		return domain.Note{}, err
	}

	note.NoteFiles = unmarshalNoteFiles(noteFilesJSON)
	note.CreatedAt = parseSQLiteTime(createdAt)
	return note, nil
}

func marshalNoteFiles(noteFiles []string) (string, error) {
	if len(noteFiles) == 0 {
		return "[]", nil
	}

	cleaned := make([]string, 0, len(noteFiles))
	for _, file := range noteFiles {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		cleaned = append(cleaned, file)
	}

	raw, err := json.Marshal(cleaned)
	if err != nil {
		return "", fmt.Errorf("marshal note files: %w", err)
	}
	return string(raw), nil
}

func unmarshalNoteFiles(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}

	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []string{}
	}

	cleaned := make([]string, 0, len(out))
	for _, file := range out {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		cleaned = append(cleaned, file)
	}
	return cleaned
}

func (r *Repository) GetOrCreateUUIDByEmail(ctx context.Context, email string) (string, error) {
	email = normalizeEmail(email)
	if email == "" {
		return "", errors.New("email is empty")
	}

	user, err := r.GetUserByEmail(ctx, email)
	if err == nil {
		return user.UserID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	created, err := r.CreateUser(ctx, email, dbtraits.GenerateUUID(), "en")
	if err == nil {
		return created.UserID, nil
	}

	// Handle race on unique(email): fetch again.
	if user, queryErr := r.GetUserByEmail(ctx, email); queryErr == nil {
		return user.UserID, nil
	}

	return "", err
}

func (r *Repository) Save(ctx context.Context, token string, rec domain.RefreshRecord) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("refresh token is empty")
	}
	phone := normalizePhone(rec.Phone)
	if phone == "" {
		return errors.New("refresh phone is empty")
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO refresh_tokens (token, user_id, email, auth_type, role, expires_at, revoked) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		token,
		rec.UserUUID,
		phone,
		strings.TrimSpace(rec.AuthType),
		strings.TrimSpace(rec.Role),
		rec.ExpiresAt.UTC().Format(time.RFC3339Nano),
		boolToInt(rec.Revoked),
	)
	if err != nil {
		return fmt.Errorf("save refresh token: %w", err)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, token string) (domain.RefreshRecord, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return domain.RefreshRecord{}, sql.ErrNoRows
	}

	row := r.db.QueryRowContext(
		ctx,
		`SELECT user_id, email, COALESCE(auth_type, 'otp_whatsapp'), COALESCE(role, 'user'), expires_at, revoked FROM refresh_tokens WHERE token = ?`,
		token,
	)

	var (
		rec       domain.RefreshRecord
		expiresAt string
		revoked   int
	)
	if err := row.Scan(&rec.UserUUID, &rec.Phone, &rec.AuthType, &rec.Role, &expiresAt, &revoked); err != nil {
		return domain.RefreshRecord{}, err
	}
	rec.Phone = normalizePhone(rec.Phone)

	rec.ExpiresAt = parseSQLiteTime(expiresAt)
	rec.Revoked = revoked != 0
	return rec, nil
}

func (r *Repository) Revoke(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return sql.ErrNoRows
	}

	res, err := r.db.ExecContext(ctx, `UPDATE refresh_tokens SET revoked = 1 WHERE token = ?`, token)
	if err != nil {
		return err
	}

	return ensureRowsAffected(res)
}

func (r *Repository) CreateOTPRequest(
	ctx context.Context,
	requestID string,
	channel domain.OTPChannel,
	destination string,
	codeHash string,
	expiresAt time.Time,
	maxAttempts int,
) (domain.OTPRequest, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		requestID = dbtraits.GenerateUUID()
	}
	destination = normalizeDestination(channel, destination)
	if destination == "" {
		return domain.OTPRequest{}, errors.New("destination is empty")
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO otp_requests (id, channel, destination, code_hash, expires_at, max_attempts) VALUES (?, ?, ?, ?, ?, ?)`,
		requestID,
		string(channel),
		destination,
		codeHash,
		expiresAt.UTC().Format(time.RFC3339Nano),
		maxAttempts,
	)
	if err != nil {
		return domain.OTPRequest{}, fmt.Errorf("insert otp request: %w", err)
	}

	return r.GetOTPRequestByID(ctx, requestID)
}

func (r *Repository) GetOTPRequestByID(ctx context.Context, requestID string) (domain.OTPRequest, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, channel, destination, code_hash, expires_at, used_at, attempts, max_attempts, created_at, last_attempt_at
		 FROM otp_requests WHERE id = ?`,
		strings.TrimSpace(requestID),
	)

	var req domain.OTPRequest
	var expiresAt string
	var usedAt sql.NullString
	var createdAt string
	var lastAttemptAt sql.NullString
	if err := row.Scan(
		&req.ID,
		&req.Channel,
		&req.Destination,
		&req.CodeHash,
		&expiresAt,
		&usedAt,
		&req.Attempts,
		&req.MaxAttempts,
		&createdAt,
		&lastAttemptAt,
	); err != nil {
		return domain.OTPRequest{}, err
	}

	req.ExpiresAt = parseSQLiteTime(expiresAt)
	req.CreatedAt = parseSQLiteTime(createdAt)
	if usedAt.Valid {
		t := parseSQLiteTime(usedAt.String)
		req.UsedAt = &t
	}
	if lastAttemptAt.Valid {
		t := parseSQLiteTime(lastAttemptAt.String)
		req.LastAttempt = &t
	}

	return req, nil
}

func (r *Repository) GetLatestOTPRequestByDestination(
	ctx context.Context,
	channel domain.OTPChannel,
	destination string,
) (domain.OTPRequest, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, channel, destination, code_hash, expires_at, used_at, attempts, max_attempts, created_at, last_attempt_at
		 FROM otp_requests
		 WHERE channel = ? AND destination = ?
		 ORDER BY created_at DESC
		 LIMIT 1`,
		string(channel),
		normalizeDestination(channel, destination),
	)

	var req domain.OTPRequest
	var expiresAt string
	var usedAt sql.NullString
	var createdAt string
	var lastAttemptAt sql.NullString
	if err := row.Scan(
		&req.ID,
		&req.Channel,
		&req.Destination,
		&req.CodeHash,
		&expiresAt,
		&usedAt,
		&req.Attempts,
		&req.MaxAttempts,
		&createdAt,
		&lastAttemptAt,
	); err != nil {
		return domain.OTPRequest{}, err
	}

	req.ExpiresAt = parseSQLiteTime(expiresAt)
	req.CreatedAt = parseSQLiteTime(createdAt)
	if usedAt.Valid {
		t := parseSQLiteTime(usedAt.String)
		req.UsedAt = &t
	}
	if lastAttemptAt.Valid {
		t := parseSQLiteTime(lastAttemptAt.String)
		req.LastAttempt = &t
	}

	return req, nil
}

func (r *Repository) IncrementOTPAttempt(ctx context.Context, requestID string) (int, error) {
	res, err := r.db.ExecContext(
		ctx,
		`UPDATE otp_requests SET attempts = attempts + 1, last_attempt_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(requestID),
	)
	if err != nil {
		return 0, fmt.Errorf("increment otp attempts: %w", err)
	}
	if err := ensureRowsAffected(res); err != nil {
		return 0, err
	}

	row := r.db.QueryRowContext(ctx, `SELECT attempts FROM otp_requests WHERE id = ?`, strings.TrimSpace(requestID))
	var attempts int
	if err := row.Scan(&attempts); err != nil {
		return 0, err
	}
	return attempts, nil
}

func (r *Repository) MarkOTPUsed(ctx context.Context, requestID string, usedAt time.Time) error {
	res, err := r.db.ExecContext(
		ctx,
		`UPDATE otp_requests SET used_at = ? WHERE id = ? AND used_at IS NULL`,
		usedAt.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(requestID),
	)
	if err != nil {
		return fmt.Errorf("mark otp used: %w", err)
	}
	return ensureRowsAffected(res)
}

func (r *Repository) GetOTPLockState(ctx context.Context, channel domain.OTPChannel, destination string) (domain.OTPLockState, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT failed_attempts, locked_until, updated_at
		 FROM otp_destination_locks
		 WHERE channel = ? AND destination = ?`,
		string(channel),
		normalizeDestination(channel, destination),
	)

	var out domain.OTPLockState
	var lockedUntil sql.NullString
	var updatedAt string
	if err := row.Scan(&out.FailedAttempts, &lockedUntil, &updatedAt); err != nil {
		return domain.OTPLockState{}, err
	}

	out.Channel = channel
	out.Destination = normalizeDestination(channel, destination)
	out.UpdatedAt = parseSQLiteTime(updatedAt)
	if lockedUntil.Valid {
		t := parseSQLiteTime(lockedUntil.String)
		out.LockedUntil = &t
	}
	return out, nil
}

func (r *Repository) UpsertOTPLockState(ctx context.Context, state domain.OTPLockState) error {
	var lockedUntil any
	if state.LockedUntil != nil {
		lockedUntil = state.LockedUntil.UTC().Format(time.RFC3339Nano)
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO otp_destination_locks (channel, destination, failed_attempts, locked_until, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(channel, destination) DO UPDATE SET
			failed_attempts = excluded.failed_attempts,
			locked_until = excluded.locked_until,
			updated_at = excluded.updated_at`,
		string(state.Channel),
		normalizeDestination(state.Channel, state.Destination),
		state.FailedAttempts,
		lockedUntil,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("upsert otp lock state: %w", err)
	}
	return nil
}

func (r *Repository) ResetOTPLockState(ctx context.Context, channel domain.OTPChannel, destination string) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO otp_destination_locks (channel, destination, failed_attempts, locked_until, updated_at)
		 VALUES (?, ?, 0, NULL, ?)
		 ON CONFLICT(channel, destination) DO UPDATE SET
			failed_attempts = 0,
			locked_until = NULL,
			updated_at = excluded.updated_at`,
		string(channel),
		normalizeDestination(channel, destination),
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("reset otp lock state: %w", err)
	}
	return nil
}

func (r *Repository) CreateAuthVerification(
	ctx context.Context,
	verificationID string,
	requestID string,
	purpose domain.AuthVerificationPurpose,
	phone string,
	expiresAt time.Time,
) (domain.AuthVerification, error) {
	verificationID = strings.TrimSpace(verificationID)
	if verificationID == "" {
		verificationID = dbtraits.GenerateUUID()
	}
	requestID = strings.TrimSpace(requestID)
	phone = normalizePhone(phone)
	purposeRaw := normalizeVerificationPurpose(purpose)
	if requestID == "" {
		return domain.AuthVerification{}, errors.New("request id is empty")
	}
	if phone == "" {
		return domain.AuthVerification{}, errors.New("phone is empty")
	}
	if purposeRaw == "" {
		return domain.AuthVerification{}, errors.New("verification purpose is empty")
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO auth_verifications (id, request_id, purpose, phone, expires_at) VALUES (?, ?, ?, ?, ?)`,
		verificationID,
		requestID,
		purposeRaw,
		phone,
		expiresAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return domain.AuthVerification{}, fmt.Errorf("insert auth verification: %w", err)
	}

	return r.GetAuthVerificationByID(ctx, verificationID)
}

func (r *Repository) GetAuthVerificationByID(ctx context.Context, verificationID string) (domain.AuthVerification, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, request_id, purpose, phone, expires_at, used_at, created_at
		 FROM auth_verifications
		 WHERE id = ?`,
		strings.TrimSpace(verificationID),
	)

	var out domain.AuthVerification
	var purposeRaw string
	var expiresAt string
	var usedAt sql.NullString
	var createdAt string
	if err := row.Scan(
		&out.ID,
		&out.RequestID,
		&purposeRaw,
		&out.Phone,
		&expiresAt,
		&usedAt,
		&createdAt,
	); err != nil {
		return domain.AuthVerification{}, err
	}

	out.Purpose = domain.AuthVerificationPurpose(normalizeVerificationPurpose(domain.AuthVerificationPurpose(purposeRaw)))
	out.Phone = normalizePhone(out.Phone)
	out.ExpiresAt = parseSQLiteTime(expiresAt)
	out.CreatedAt = parseSQLiteTime(createdAt)
	if usedAt.Valid {
		t := parseSQLiteTime(usedAt.String)
		out.UsedAt = &t
	}
	return out, nil
}

func (r *Repository) MarkAuthVerificationUsed(ctx context.Context, verificationID string, usedAt time.Time) error {
	res, err := r.db.ExecContext(
		ctx,
		`UPDATE auth_verifications SET used_at = ? WHERE id = ? AND used_at IS NULL`,
		usedAt.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(verificationID),
	)
	if err != nil {
		return fmt.Errorf("mark auth verification used: %w", err)
	}
	return ensureRowsAffected(res)
}

func (r *Repository) CreateAd(ctx context.Context, in domain.AdCreateInput) (domain.Ad, error) {
	id := dbtraits.GenerateUUID()
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO ads (id, title, image_url, target_url, is_active) VALUES (?, ?, ?, ?, ?)`,
		id,
		strings.TrimSpace(in.Title),
		strings.TrimSpace(in.ImageURL),
		strings.TrimSpace(in.TargetURL),
		boolToInt(in.IsActive),
	)
	if err != nil {
		return domain.Ad{}, fmt.Errorf("insert ad: %w", err)
	}
	return r.getAdByID(ctx, id)
}

func (r *Repository) UpdateAd(ctx context.Context, id string, in domain.AdUpdateInput) (domain.Ad, error) {
	ad, err := r.getAdByID(ctx, strings.TrimSpace(id))
	if err != nil {
		return domain.Ad{}, err
	}

	if in.Title != nil {
		ad.Title = strings.TrimSpace(*in.Title)
	}
	if in.ImageURL != nil {
		ad.ImageURL = strings.TrimSpace(*in.ImageURL)
	}
	if in.TargetURL != nil {
		ad.TargetURL = strings.TrimSpace(*in.TargetURL)
	}
	if in.IsActive != nil {
		ad.IsActive = *in.IsActive
	}

	_, err = r.db.ExecContext(
		ctx,
		`UPDATE ads SET title = ?, image_url = ?, target_url = ?, is_active = ?, updated_at = ? WHERE id = ?`,
		ad.Title,
		ad.ImageURL,
		ad.TargetURL,
		boolToInt(ad.IsActive),
		time.Now().UTC().Format(time.RFC3339Nano),
		ad.ID,
	)
	if err != nil {
		return domain.Ad{}, fmt.Errorf("update ad: %w", err)
	}

	return r.getAdByID(ctx, ad.ID)
}

func (r *Repository) DeleteAd(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM ads WHERE id = ?`, strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("delete ad: %w", err)
	}
	return ensureRowsAffected(res)
}

func (r *Repository) ListAds(ctx context.Context, limit, offset int, active *bool) ([]domain.Ad, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	query := `SELECT id, title, image_url, target_url, is_active, created_at, updated_at FROM ads`
	args := make([]any, 0, 3)
	if active != nil {
		query += ` WHERE is_active = ?`
		args = append(args, boolToInt(*active))
	}
	query += ` ORDER BY created_at ASC, id ASC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list ads: %w", err)
	}
	defer rows.Close()

	ads := make([]domain.Ad, 0)
	for rows.Next() {
		var ad domain.Ad
		var createdAt string
		var updatedAt string
		var isActive int
		if err := rows.Scan(&ad.ID, &ad.Title, &ad.ImageURL, &ad.TargetURL, &isActive, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		ad.IsActive = isActive != 0
		ad.CreatedAt = parseSQLiteTime(createdAt)
		ad.UpdatedAt = parseSQLiteTime(updatedAt)
		ads = append(ads, ad)
	}

	return ads, rows.Err()
}

func (r *Repository) ListActiveAds(ctx context.Context) ([]domain.Ad, error) {
	active := true
	return r.ListAds(ctx, 1000, 0, &active)
}

func (r *Repository) GetUserAdState(ctx context.Context, userID string) (domain.UserAdState, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT user_id, COALESCE(last_ad_id, ''), updated_at FROM user_ads_state WHERE user_id = ?`,
		strings.TrimSpace(userID),
	)

	var state domain.UserAdState
	var updatedAt string
	if err := row.Scan(&state.UserID, &state.LastAdID, &updatedAt); err != nil {
		return domain.UserAdState{}, err
	}
	state.UpdatedAt = parseSQLiteTime(updatedAt)
	return state, nil
}

func (r *Repository) UpsertUserAdState(ctx context.Context, userID, lastAdID string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO user_ads_state (user_id, last_ad_id, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
			last_ad_id = excluded.last_ad_id,
			updated_at = excluded.updated_at`,
		strings.TrimSpace(userID),
		strings.TrimSpace(lastAdID),
		updatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("upsert user ad state: %w", err)
	}
	return nil
}

func (r *Repository) getAdByID(ctx context.Context, id string) (domain.Ad, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, title, image_url, target_url, is_active, created_at, updated_at FROM ads WHERE id = ?`,
		strings.TrimSpace(id),
	)

	var ad domain.Ad
	var createdAt string
	var updatedAt string
	var isActive int
	if err := row.Scan(&ad.ID, &ad.Title, &ad.ImageURL, &ad.TargetURL, &isActive, &createdAt, &updatedAt); err != nil {
		return domain.Ad{}, err
	}
	ad.IsActive = isActive != 0
	ad.CreatedAt = parseSQLiteTime(createdAt)
	ad.UpdatedAt = parseSQLiteTime(updatedAt)
	return ad, nil
}

func normalizeEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}

func normalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}

	var b strings.Builder
	for i, r := range phone {
		if r == '+' && i == 0 {
			b.WriteRune(r)
			continue
		}
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeDestination(channel domain.OTPChannel, destination string) string {
	switch channel {
	case domain.OTPChannelWhatsApp:
		return normalizePhone(destination)
	default:
		return strings.TrimSpace(destination)
	}
}

func normalizeVerificationPurpose(purpose domain.AuthVerificationPurpose) string {
	switch strings.TrimSpace(strings.ToLower(string(purpose))) {
	case string(domain.AuthVerificationPurposeRegistration):
		return string(domain.AuthVerificationPurposeRegistration)
	case string(domain.AuthVerificationPurposePasswordReset):
		return string(domain.AuthVerificationPurposePasswordReset)
	default:
		return ""
	}
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

func ensureRowsAffected(res sql.Result) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
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
