package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"time-leak/internal/domain"
)

// AdsCounts returns total / active / inactive ad counts from the database.
func (r *Repository) AdsCounts(ctx context.Context) (domain.AdsStats, error) {
	var total, active int
	err := r.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN is_active = 1 THEN 1 ELSE 0 END), 0) FROM ads`,
	).Scan(&total, &active)
	if err != nil {
		return domain.AdsStats{}, fmt.Errorf("count ads: %w", err)
	}
	return domain.AdsStats{Total: total, Active: active, Inactive: total - active}, nil
}

// DeviceCounts returns aggregate counts for stored user devices.
func (r *Repository) DeviceCounts(ctx context.Context) (domain.DeviceStats, error) {
	var stats domain.DeviceStats
	err := r.db.QueryRowContext(
		ctx,
		`SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN platform = 'ios' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN platform = 'android' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN is_active = 1 THEN 1 ELSE 0 END), 0)
		FROM user_devices`,
	).Scan(&stats.Total, &stats.IOS, &stats.Android, &stats.Active)
	if err != nil {
		return domain.DeviceStats{}, fmt.Errorf("count devices: %w", err)
	}
	return stats, nil
}

// LocationCounts returns aggregate counts for stored location events.
func (r *Repository) LocationCounts(ctx context.Context) (domain.LocationStats, error) {
	var stats domain.LocationStats
	err := r.db.QueryRowContext(
		ctx,
		`SELECT
			COUNT(*),
			COUNT(DISTINCT user_id),
			COUNT(DISTINCT device_id)
		FROM user_location_events`,
	).Scan(&stats.Total, &stats.UniqueUsers, &stats.UniqueDevices)
	if err != nil {
		return domain.LocationStats{}, fmt.Errorf("count locations: %w", err)
	}
	return stats, nil
}

// geoWhere builds the shared WHERE clause and args for geo queries.
// The location-events table is aliased le and the devices table d.
func geoWhere(filter domain.GeoFilter) (string, []any) {
	clause := ` WHERE le.latitude IS NOT NULL AND le.longitude IS NOT NULL`
	args := make([]any, 0, 5)

	if filter.From != nil {
		clause += ` AND le.created_at >= ?`
		args = append(args, filter.From.UTC().Format(time.RFC3339Nano))
	}
	if filter.To != nil {
		clause += ` AND le.created_at <= ?`
		args = append(args, filter.To.UTC().Format(time.RFC3339Nano))
	}
	if platform := strings.TrimSpace(strings.ToLower(filter.Platform)); platform != "" {
		clause += ` AND d.platform = ?`
		args = append(args, platform)
	}
	if source := strings.TrimSpace(strings.ToLower(filter.Source)); source != "" {
		clause += ` AND le.source = ?`
		args = append(args, source)
	}
	if filter.ActiveOnly {
		clause += ` AND d.is_active = 1`
	}
	return clause, args
}

// ListGeoPoints returns location events (most recent first) prepared for the map.
func (r *Repository) ListGeoPoints(ctx context.Context, filter domain.GeoFilter) ([]domain.GeoPoint, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 1000
	}
	if limit > 5000 {
		limit = 5000
	}

	where, args := geoWhere(filter)
	query := `SELECT le.id, COALESCE(le.user_id, ''), COALESCE(le.phone, ''), COALESCE(le.device_id, ''),
			COALESCE(d.platform, ''), COALESCE(d.device_model, ''),
			le.latitude, le.longitude, le.accuracy_meters, COALESCE(le.source, ''), le.created_at
		FROM user_location_events le
		LEFT JOIN user_devices d ON d.user_id = le.user_id AND d.device_id = le.device_id` +
		where + ` ORDER BY le.created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list geo points: %w", err)
	}
	defer rows.Close()

	out := make([]domain.GeoPoint, 0)
	for rows.Next() {
		var (
			point     domain.GeoPoint
			accuracy  sql.NullFloat64
			createdAt string
		)
		if err := rows.Scan(
			&point.ID,
			&point.UserID,
			&point.Phone,
			&point.DeviceID,
			&point.Platform,
			&point.DeviceModel,
			&point.Latitude,
			&point.Longitude,
			&accuracy,
			&point.Source,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan geo point: %w", err)
		}
		point.Phone = normalizePhone(point.Phone)
		if accuracy.Valid {
			val := accuracy.Float64
			point.AccuracyMeters = &val
		}
		point.CreatedAt = parseSQLiteTime(createdAt)
		out = append(out, point)
	}
	return out, rows.Err()
}

// GeoCounts returns total / unique-user / unique-device counts for the filter.
func (r *Repository) GeoCounts(ctx context.Context, filter domain.GeoFilter) (domain.LocationStats, error) {
	where, args := geoWhere(filter)
	query := `SELECT COUNT(*), COUNT(DISTINCT le.user_id), COUNT(DISTINCT le.device_id)
		FROM user_location_events le
		LEFT JOIN user_devices d ON d.user_id = le.user_id AND d.device_id = le.device_id` + where

	var stats domain.LocationStats
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&stats.Total, &stats.UniqueUsers, &stats.UniqueDevices); err != nil {
		return domain.LocationStats{}, fmt.Errorf("geo counts: %w", err)
	}
	return stats, nil
}

// GeoPlatformDeviceCounts returns distinct device counts per platform for the filter.
func (r *Repository) GeoPlatformDeviceCounts(ctx context.Context, filter domain.GeoFilter) (ios int, android int, err error) {
	where, args := geoWhere(filter)
	query := `SELECT COALESCE(d.platform, ''), COUNT(DISTINCT le.device_id)
		FROM user_location_events le
		LEFT JOIN user_devices d ON d.user_id = le.user_id AND d.device_id = le.device_id` +
		where + ` GROUP BY d.platform`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, 0, fmt.Errorf("geo platform counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			platform string
			count    int
		)
		if err := rows.Scan(&platform, &count); err != nil {
			return 0, 0, fmt.Errorf("scan geo platform count: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(platform)) {
		case "ios":
			ios = count
		case "android":
			android = count
		}
	}
	return ios, android, rows.Err()
}

// ListDevices returns devices across all users with optional filters.
// It also returns the real total count matching the filter (not just the page size).
func (r *Repository) ListDevices(ctx context.Context, filter domain.AdminDeviceListFilter) ([]domain.UserDevice, int, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	where := ` WHERE 1=1`
	args := make([]any, 0, 6)

	if platform := strings.TrimSpace(strings.ToLower(filter.Platform)); platform != "" {
		where += ` AND platform = ?`
		args = append(args, platform)
	}
	if filter.Active != nil {
		where += ` AND is_active = ?`
		args = append(args, boolToInt(*filter.Active))
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		like := "%" + search + "%"
		where += ` AND (phone LIKE ? OR device_id LIKE ? OR device_model LIKE ? OR app_version LIKE ? OR manufacturer LIKE ?)`
		args = append(args, like, like, like, like, like)
	}

	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_devices`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count devices: %w", err)
	}

	query := `SELECT id, user_id, phone, COALESCE(device_id, ''), platform, COALESCE(app_version, ''),
			COALESCE(os_version, ''), COALESCE(device_model, ''), COALESCE(manufacturer, ''),
			COALESCE(push_token, ''), first_seen_at, last_seen_at, is_active, created_at, updated_at
		FROM user_devices` + where + ` ORDER BY last_seen_at DESC, created_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	out := make([]domain.UserDevice, 0)
	for rows.Next() {
		item, err := scanUserDevice(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}
