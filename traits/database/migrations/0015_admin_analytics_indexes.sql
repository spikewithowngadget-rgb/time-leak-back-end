-- Admin-only analytics helper indexes. All statements are idempotent and only
-- add indexes used by the read-only admin analytics/device-listing queries.
-- No tables, columns, triggers or mobile-facing behavior are changed.

CREATE INDEX IF NOT EXISTS idx_user_devices_platform
    ON user_devices(platform);

CREATE INDEX IF NOT EXISTS idx_user_devices_is_active
    ON user_devices(is_active);

CREATE INDEX IF NOT EXISTS idx_user_devices_last_seen_at
    ON user_devices(last_seen_at);

CREATE INDEX IF NOT EXISTS idx_user_location_events_source
    ON user_location_events(source);

CREATE INDEX IF NOT EXISTS idx_user_location_events_device_id
    ON user_location_events(device_id);
