package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"time-leak/internal/domain"
	dbtraits "time-leak/traits/database"
)

func (r *Repository) CreateTelegramOTPSession(ctx context.Context, session domain.TelegramOTPSession) (domain.TelegramOTPSession, error) {
	if strings.TrimSpace(session.ID) == "" {
		session.ID = dbtraits.GenerateUUID()
	}
	if strings.TrimSpace(session.RequestID) == "" {
		session.RequestID = dbtraits.GenerateUUID()
	}
	now := time.Now().UTC()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = now
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO telegram_otp_sessions (
			id, request_id, phone, purpose, deep_link_token_hash, status,
			telegram_user_id, telegram_chat_id, telegram_username, telegram_first_name, telegram_last_name,
			device_json, location_json, expires_at, opened_at, code_sent_at, verified_at, cancelled_at,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID,
		session.RequestID,
		normalizePhone(session.Phone),
		normalizeVerificationPurpose(session.Purpose),
		strings.TrimSpace(session.DeepLinkTokenHash),
		string(session.Status),
		nullableInt64Ptr(session.TelegramUserID),
		nullableInt64Ptr(session.TelegramChatID),
		nullableString(session.TelegramUsername),
		nullableString(session.TelegramFirstName),
		nullableString(session.TelegramLastName),
		nullableJSON(session.Device),
		nullableJSON(session.Location),
		session.ExpiresAt.UTC().Format(time.RFC3339Nano),
		nullableTime(session.OpenedAt),
		nullableTime(session.CodeSentAt),
		nullableTime(session.VerifiedAt),
		nullableTime(session.CancelledAt),
		session.CreatedAt.UTC().Format(time.RFC3339Nano),
		session.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return domain.TelegramOTPSession{}, fmt.Errorf("insert telegram otp session: %w", err)
	}
	return r.GetTelegramOTPSessionByRequestID(ctx, session.RequestID)
}

func (r *Repository) GetTelegramOTPSessionByRequestID(ctx context.Context, requestID string) (domain.TelegramOTPSession, error) {
	return r.getTelegramOTPSession(ctx, `request_id = ?`, strings.TrimSpace(requestID))
}

func (r *Repository) GetTelegramOTPSessionByTokenHash(ctx context.Context, tokenHash string) (domain.TelegramOTPSession, error) {
	return r.getTelegramOTPSession(ctx, `deep_link_token_hash = ?`, strings.TrimSpace(tokenHash))
}

func (r *Repository) UpdateTelegramOTPSessionOpened(
	ctx context.Context,
	requestID string,
	telegramUserID int64,
	telegramChatID int64,
	username string,
	firstName string,
	lastName string,
	openedAt time.Time,
) (domain.TelegramOTPSession, error) {
	res, err := r.db.ExecContext(
		ctx,
		`UPDATE telegram_otp_sessions
		 SET status = ?, telegram_user_id = ?, telegram_chat_id = ?, telegram_username = ?,
		     telegram_first_name = ?, telegram_last_name = ?, opened_at = ?, updated_at = ?
		 WHERE request_id = ?`,
		string(domain.TelegramOTPSessionOpened),
		telegramUserID,
		telegramChatID,
		nullableString(username),
		nullableString(firstName),
		nullableString(lastName),
		openedAt.UTC().Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(requestID),
	)
	if err != nil {
		return domain.TelegramOTPSession{}, fmt.Errorf("update telegram otp session opened: %w", err)
	}
	if err := ensureRowsAffected(res); err != nil {
		return domain.TelegramOTPSession{}, err
	}
	return r.GetTelegramOTPSessionByRequestID(ctx, requestID)
}

func (r *Repository) UpdateTelegramOTPSessionCodeSent(ctx context.Context, requestID string, codeSentAt time.Time) (domain.TelegramOTPSession, error) {
	res, err := r.db.ExecContext(
		ctx,
		`UPDATE telegram_otp_sessions
		 SET status = ?, code_sent_at = ?, updated_at = ?
		 WHERE request_id = ?`,
		string(domain.TelegramOTPSessionCodeSent),
		codeSentAt.UTC().Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(requestID),
	)
	if err != nil {
		return domain.TelegramOTPSession{}, fmt.Errorf("update telegram otp session code_sent: %w", err)
	}
	if err := ensureRowsAffected(res); err != nil {
		return domain.TelegramOTPSession{}, err
	}
	return r.GetTelegramOTPSessionByRequestID(ctx, requestID)
}

func (r *Repository) UpdateTelegramOTPSessionVerified(ctx context.Context, requestID string, verifiedAt time.Time) error {
	res, err := r.db.ExecContext(
		ctx,
		`UPDATE telegram_otp_sessions
		 SET status = ?, verified_at = ?, updated_at = ?
		 WHERE request_id = ?`,
		string(domain.TelegramOTPSessionVerified),
		verifiedAt.UTC().Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(requestID),
	)
	if err != nil {
		return fmt.Errorf("update telegram otp session verified: %w", err)
	}
	return ensureRowsAffected(res)
}

func (r *Repository) UpdateTelegramOTPSessionCancelled(ctx context.Context, requestID string, cancelledAt time.Time) error {
	res, err := r.db.ExecContext(
		ctx,
		`UPDATE telegram_otp_sessions
		 SET status = ?, cancelled_at = ?, updated_at = ?
		 WHERE request_id = ?`,
		string(domain.TelegramOTPSessionCancelled),
		cancelledAt.UTC().Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(requestID),
	)
	if err != nil {
		return fmt.Errorf("update telegram otp session cancelled: %w", err)
	}
	return ensureRowsAffected(res)
}

func (r *Repository) UpdateTelegramOTPSessionExpired(ctx context.Context, requestID string) error {
	res, err := r.db.ExecContext(
		ctx,
		`UPDATE telegram_otp_sessions
		 SET status = ?, updated_at = ?
		 WHERE request_id = ?`,
		string(domain.TelegramOTPSessionExpired),
		time.Now().UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(requestID),
	)
	if err != nil {
		return fmt.Errorf("update telegram otp session expired: %w", err)
	}
	return ensureRowsAffected(res)
}

func (r *Repository) ListTelegramOTPSessions(ctx context.Context, filter domain.TelegramOTPSessionListFilter) ([]domain.TelegramOTPSession, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	query := `SELECT id, request_id, phone, purpose, deep_link_token_hash, status,
		telegram_user_id, telegram_chat_id, COALESCE(telegram_username, ''), COALESCE(telegram_first_name, ''),
		COALESCE(telegram_last_name, ''), COALESCE(device_json, ''), COALESCE(location_json, ''),
		expires_at, opened_at, code_sent_at, verified_at, cancelled_at, created_at, updated_at
		FROM telegram_otp_sessions WHERE 1=1`
	args := make([]any, 0, 5)
	if phone := normalizePhone(filter.Phone); phone != "" {
		query += ` AND phone = ?`
		args = append(args, phone)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	if purpose := normalizeVerificationPurpose(domain.AuthVerificationPurpose(filter.Purpose)); purpose != "" {
		query += ` AND purpose = ?`
		args = append(args, purpose)
	}
	query += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, filter.Limit, filter.Offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list telegram otp sessions: %w", err)
	}
	defer rows.Close()

	out := make([]domain.TelegramOTPSession, 0)
	for rows.Next() {
		session, err := scanTelegramOTPSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, session)
	}

	return out, rows.Err()
}

func (r *Repository) UpsertUserDevice(ctx context.Context, device domain.UserDevice) (domain.UserDevice, error) {
	now := time.Now().UTC()
	if strings.TrimSpace(device.ID) == "" {
		device.ID = dbtraits.GenerateUUID()
	}
	device.UserID = strings.TrimSpace(device.UserID)
	device.Phone = normalizePhone(device.Phone)
	device.DeviceID = strings.TrimSpace(device.DeviceID)
	device.Platform = strings.TrimSpace(strings.ToLower(device.Platform))
	device.AppVersion = strings.TrimSpace(device.AppVersion)
	device.OSVersion = strings.TrimSpace(device.OSVersion)
	device.DeviceModel = strings.TrimSpace(device.DeviceModel)
	device.Manufacturer = strings.TrimSpace(device.Manufacturer)
	device.PushToken = strings.TrimSpace(device.PushToken)
	if device.FirstSeenAt.IsZero() {
		device.FirstSeenAt = now
	}
	device.LastSeenAt = now
	device.CreatedAt = now
	device.UpdatedAt = now
	device.IsActive = true

	if device.DeviceID != "" {
		existing, err := r.getUserDeviceByUserAndDeviceID(ctx, device.UserID, device.DeviceID)
		if err == nil {
			_, execErr := r.db.ExecContext(
				ctx,
				`UPDATE user_devices
				 SET phone = ?, platform = ?, app_version = ?, os_version = ?, device_model = ?,
				     manufacturer = ?, push_token = ?, last_seen_at = ?, is_active = 1, updated_at = ?
				 WHERE id = ?`,
				device.Phone,
				device.Platform,
				nullableString(device.AppVersion),
				nullableString(device.OSVersion),
				nullableString(device.DeviceModel),
				nullableString(device.Manufacturer),
				nullableString(device.PushToken),
				device.LastSeenAt.UTC().Format(time.RFC3339Nano),
				device.UpdatedAt.UTC().Format(time.RFC3339Nano),
				existing.ID,
			)
			if execErr != nil {
				return domain.UserDevice{}, fmt.Errorf("update user device: %w", execErr)
			}
			return r.getUserDeviceByID(ctx, existing.ID)
		}
		if err != nil && err != sql.ErrNoRows {
			return domain.UserDevice{}, err
		}
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO user_devices (
			id, user_id, phone, device_id, platform, app_version, os_version,
			device_model, manufacturer, push_token, first_seen_at, last_seen_at,
			is_active, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		device.ID,
		device.UserID,
		device.Phone,
		nullableString(device.DeviceID),
		device.Platform,
		nullableString(device.AppVersion),
		nullableString(device.OSVersion),
		nullableString(device.DeviceModel),
		nullableString(device.Manufacturer),
		nullableString(device.PushToken),
		device.FirstSeenAt.UTC().Format(time.RFC3339Nano),
		device.LastSeenAt.UTC().Format(time.RFC3339Nano),
		boolToInt(device.IsActive),
		device.CreatedAt.UTC().Format(time.RFC3339Nano),
		device.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return domain.UserDevice{}, fmt.Errorf("insert user device: %w", err)
	}
	return r.getUserDeviceByID(ctx, device.ID)
}

func (r *Repository) ListUserDevicesByUserID(ctx context.Context, userID string) ([]domain.UserDevice, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, user_id, phone, COALESCE(device_id, ''), platform, COALESCE(app_version, ''),
		        COALESCE(os_version, ''), COALESCE(device_model, ''), COALESCE(manufacturer, ''),
		        COALESCE(push_token, ''), first_seen_at, last_seen_at, is_active, created_at, updated_at
		 FROM user_devices
		 WHERE user_id = ?
		 ORDER BY last_seen_at DESC, created_at DESC`,
		strings.TrimSpace(userID),
	)
	if err != nil {
		return nil, fmt.Errorf("list user devices: %w", err)
	}
	defer rows.Close()

	out := make([]domain.UserDevice, 0)
	for rows.Next() {
		item, err := scanUserDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) DeactivateUserDevice(ctx context.Context, userID, deviceID string) error {
	res, err := r.db.ExecContext(
		ctx,
		`UPDATE user_devices SET is_active = 0, updated_at = ?
		 WHERE user_id = ? AND device_id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(userID),
		strings.TrimSpace(deviceID),
	)
	if err != nil {
		return fmt.Errorf("deactivate user device: %w", err)
	}
	return ensureRowsAffected(res)
}

func (r *Repository) CreateUserLocationEvent(ctx context.Context, event domain.UserLocationEvent) error {
	if strings.TrimSpace(event.ID) == "" {
		event.ID = dbtraits.GenerateUUID()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO user_location_events (
			id, user_id, phone, device_id, event_type, latitude, longitude,
			accuracy_meters, source, ip_address, user_agent, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID,
		nullableString(event.UserID),
		nullableString(normalizePhone(event.Phone)),
		nullableString(event.DeviceID),
		strings.TrimSpace(event.EventType),
		nullableFloat64(event.Latitude),
		nullableFloat64(event.Longitude),
		nullableFloat64(event.AccuracyMeters),
		nullableString(event.Source),
		nullableString(event.IPAddress),
		nullableString(event.UserAgent),
		event.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert user location event: %w", err)
	}
	return nil
}

func (r *Repository) ListUserLocationEvents(ctx context.Context, filter domain.UserLocationListFilter) ([]domain.UserLocationEvent, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	query := `SELECT id, COALESCE(user_id, ''), COALESCE(phone, ''), COALESCE(device_id, ''), event_type,
		latitude, longitude, accuracy_meters, COALESCE(source, ''), COALESCE(ip_address, ''),
		COALESCE(user_agent, ''), created_at
		FROM user_location_events WHERE user_id = ?`
	args := []any{strings.TrimSpace(filter.UserID)}
	if filter.From != nil {
		query += ` AND created_at >= ?`
		args = append(args, filter.From.UTC().Format(time.RFC3339Nano))
	}
	if filter.To != nil {
		query += ` AND created_at <= ?`
		args = append(args, filter.To.UTC().Format(time.RFC3339Nano))
	}
	query += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, filter.Limit, filter.Offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list user location events: %w", err)
	}
	defer rows.Close()

	out := make([]domain.UserLocationEvent, 0)
	for rows.Next() {
		item, err := scanUserLocationEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) CreateAuthEvent(ctx context.Context, event domain.AuthEvent) error {
	if strings.TrimSpace(event.ID) == "" {
		event.ID = dbtraits.GenerateUUID()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO auth_events (
			id, user_id, phone, event_type, device_id, telegram_user_id,
			ip_address, user_agent, metadata_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID,
		nullableString(event.UserID),
		normalizePhone(event.Phone),
		strings.TrimSpace(event.EventType),
		nullableString(event.DeviceID),
		nullableInt64Ptr(event.TelegramUserID),
		nullableString(event.IPAddress),
		nullableString(event.UserAgent),
		nullableString(event.MetadataJSON),
		event.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert auth event: %w", err)
	}
	return nil
}

func (r *Repository) ListAuthEvents(ctx context.Context, filter domain.AuthEventListFilter) ([]domain.AuthEvent, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	query := `SELECT id, COALESCE(user_id, ''), COALESCE(phone, ''), event_type, COALESCE(device_id, ''),
		telegram_user_id, COALESCE(ip_address, ''), COALESCE(user_agent, ''), COALESCE(metadata_json, ''), created_at
		FROM auth_events WHERE 1=1`
	args := make([]any, 0, 6)
	if phone := normalizePhone(filter.Phone); phone != "" {
		query += ` AND phone = ?`
		args = append(args, phone)
	}
	if userID := strings.TrimSpace(filter.UserID); userID != "" {
		query += ` AND user_id = ?`
		args = append(args, userID)
	}
	if eventType := strings.TrimSpace(filter.EventType); eventType != "" {
		query += ` AND event_type = ?`
		args = append(args, eventType)
	}
	if filter.From != nil {
		query += ` AND created_at >= ?`
		args = append(args, filter.From.UTC().Format(time.RFC3339Nano))
	}
	if filter.To != nil {
		query += ` AND created_at <= ?`
		args = append(args, filter.To.UTC().Format(time.RFC3339Nano))
	}
	query += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, filter.Limit, filter.Offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list auth events: %w", err)
	}
	defer rows.Close()

	out := make([]domain.AuthEvent, 0)
	for rows.Next() {
		item, err := scanAuthEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) getTelegramOTPSession(ctx context.Context, where string, arg any) (domain.TelegramOTPSession, error) {
	query := `SELECT id, request_id, phone, purpose, deep_link_token_hash, status,
		telegram_user_id, telegram_chat_id, COALESCE(telegram_username, ''), COALESCE(telegram_first_name, ''),
		COALESCE(telegram_last_name, ''), COALESCE(device_json, ''), COALESCE(location_json, ''),
		expires_at, opened_at, code_sent_at, verified_at, cancelled_at, created_at, updated_at
		FROM telegram_otp_sessions WHERE ` + where
	row := r.db.QueryRowContext(ctx, query, arg)
	return scanTelegramOTPSession(row)
}

func scanTelegramOTPSession(scanner interface {
	Scan(dest ...any) error
}) (domain.TelegramOTPSession, error) {
	var (
		out          domain.TelegramOTPSession
		purpose      string
		status       string
		deviceJSON   string
		locationJSON string
		expiresAt    string
		openedAt     sql.NullString
		codeSentAt   sql.NullString
		verifiedAt   sql.NullString
		cancelledAt  sql.NullString
		createdAt    string
		updatedAt    string
		tgUserID     sql.NullInt64
		tgChatID     sql.NullInt64
	)
	if err := scanner.Scan(
		&out.ID,
		&out.RequestID,
		&out.Phone,
		&purpose,
		&out.DeepLinkTokenHash,
		&status,
		&tgUserID,
		&tgChatID,
		&out.TelegramUsername,
		&out.TelegramFirstName,
		&out.TelegramLastName,
		&deviceJSON,
		&locationJSON,
		&expiresAt,
		&openedAt,
		&codeSentAt,
		&verifiedAt,
		&cancelledAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return domain.TelegramOTPSession{}, err
	}
	out.Phone = normalizePhone(out.Phone)
	out.Purpose = domain.AuthVerificationPurpose(normalizeVerificationPurpose(domain.AuthVerificationPurpose(purpose)))
	out.Status = domain.TelegramOTPSessionStatus(strings.TrimSpace(status))
	out.ExpiresAt = parseSQLiteTime(expiresAt)
	out.CreatedAt = parseSQLiteTime(createdAt)
	out.UpdatedAt = parseSQLiteTime(updatedAt)
	if tgUserID.Valid {
		val := tgUserID.Int64
		out.TelegramUserID = &val
	}
	if tgChatID.Valid {
		val := tgChatID.Int64
		out.TelegramChatID = &val
	}
	if openedAt.Valid {
		out.OpenedAt = ptrTimeValue(parseSQLiteTime(openedAt.String))
	}
	if codeSentAt.Valid {
		out.CodeSentAt = ptrTimeValue(parseSQLiteTime(codeSentAt.String))
	}
	if verifiedAt.Valid {
		out.VerifiedAt = ptrTimeValue(parseSQLiteTime(verifiedAt.String))
	}
	if cancelledAt.Valid {
		out.CancelledAt = ptrTimeValue(parseSQLiteTime(cancelledAt.String))
	}
	if deviceJSON != "" {
		var device domain.AuthDevice
		if err := json.Unmarshal([]byte(deviceJSON), &device); err == nil {
			out.Device = &device
		}
	}
	if locationJSON != "" {
		var location domain.AuthLocation
		if err := json.Unmarshal([]byte(locationJSON), &location); err == nil {
			out.Location = &location
		}
	}
	return out, nil
}

func (r *Repository) getUserDeviceByUserAndDeviceID(ctx context.Context, userID, deviceID string) (domain.UserDevice, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, user_id, phone, COALESCE(device_id, ''), platform, COALESCE(app_version, ''),
		        COALESCE(os_version, ''), COALESCE(device_model, ''), COALESCE(manufacturer, ''),
		        COALESCE(push_token, ''), first_seen_at, last_seen_at, is_active, created_at, updated_at
		 FROM user_devices WHERE user_id = ? AND device_id = ?`,
		strings.TrimSpace(userID),
		strings.TrimSpace(deviceID),
	)
	return scanUserDevice(row)
}

func (r *Repository) getUserDeviceByID(ctx context.Context, id string) (domain.UserDevice, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, user_id, phone, COALESCE(device_id, ''), platform, COALESCE(app_version, ''),
		        COALESCE(os_version, ''), COALESCE(device_model, ''), COALESCE(manufacturer, ''),
		        COALESCE(push_token, ''), first_seen_at, last_seen_at, is_active, created_at, updated_at
		 FROM user_devices WHERE id = ?`,
		strings.TrimSpace(id),
	)
	return scanUserDevice(row)
}

func scanUserDevice(scanner interface {
	Scan(dest ...any) error
}) (domain.UserDevice, error) {
	var (
		out       domain.UserDevice
		isActive  int
		firstSeen string
		lastSeen  string
		createdAt string
		updatedAt string
	)
	if err := scanner.Scan(
		&out.ID,
		&out.UserID,
		&out.Phone,
		&out.DeviceID,
		&out.Platform,
		&out.AppVersion,
		&out.OSVersion,
		&out.DeviceModel,
		&out.Manufacturer,
		&out.PushToken,
		&firstSeen,
		&lastSeen,
		&isActive,
		&createdAt,
		&updatedAt,
	); err != nil {
		return domain.UserDevice{}, err
	}
	out.Phone = normalizePhone(out.Phone)
	out.IsActive = isActive != 0
	out.FirstSeenAt = parseSQLiteTime(firstSeen)
	out.LastSeenAt = parseSQLiteTime(lastSeen)
	out.CreatedAt = parseSQLiteTime(createdAt)
	out.UpdatedAt = parseSQLiteTime(updatedAt)
	return out, nil
}

func scanUserLocationEvent(scanner interface {
	Scan(dest ...any) error
}) (domain.UserLocationEvent, error) {
	var (
		out         domain.UserLocationEvent
		latitude    sql.NullFloat64
		longitude   sql.NullFloat64
		accuracy    sql.NullFloat64
		createdAt   string
	)
	if err := scanner.Scan(
		&out.ID,
		&out.UserID,
		&out.Phone,
		&out.DeviceID,
		&out.EventType,
		&latitude,
		&longitude,
		&accuracy,
		&out.Source,
		&out.IPAddress,
		&out.UserAgent,
		&createdAt,
	); err != nil {
		return domain.UserLocationEvent{}, err
	}
	out.Phone = normalizePhone(out.Phone)
	if latitude.Valid {
		val := latitude.Float64
		out.Latitude = &val
	}
	if longitude.Valid {
		val := longitude.Float64
		out.Longitude = &val
	}
	if accuracy.Valid {
		val := accuracy.Float64
		out.AccuracyMeters = &val
	}
	out.CreatedAt = parseSQLiteTime(createdAt)
	return out, nil
}

func scanAuthEvent(scanner interface {
	Scan(dest ...any) error
}) (domain.AuthEvent, error) {
	var (
		out       domain.AuthEvent
		createdAt string
		tgUserID  sql.NullInt64
	)
	if err := scanner.Scan(
		&out.ID,
		&out.UserID,
		&out.Phone,
		&out.EventType,
		&out.DeviceID,
		&tgUserID,
		&out.IPAddress,
		&out.UserAgent,
		&out.MetadataJSON,
		&createdAt,
	); err != nil {
		return domain.AuthEvent{}, err
	}
	out.Phone = normalizePhone(out.Phone)
	if tgUserID.Valid {
		val := tgUserID.Int64
		out.TelegramUserID = &val
	}
	out.CreatedAt = parseSQLiteTime(createdAt)
	return out, nil
}

func nullableTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func nullableJSON(value any) any {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return string(raw)
}

func nullableInt64Ptr(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloat64(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func ptrTimeValue(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
