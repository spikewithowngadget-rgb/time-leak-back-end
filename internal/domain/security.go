package domain

import "time"

type DevicePlatform string

const (
	DevicePlatformIOS     DevicePlatform = "ios"
	DevicePlatformAndroid DevicePlatform = "android"
)

type LocationSource string

const (
	LocationSourceGPS     LocationSource = "gps"
	LocationSourceNetwork LocationSource = "network"
	LocationSourceManual  LocationSource = "manual"
	LocationSourceUnknown LocationSource = "unknown"
)

type AuthDevice struct {
	DeviceID     string         `json:"device_id"`
	Platform     DevicePlatform `json:"platform"`
	AppVersion   string         `json:"app_version,omitempty"`
	OSVersion    string         `json:"os_version,omitempty"`
	DeviceModel  string         `json:"device_model,omitempty"`
	Manufacturer string         `json:"manufacturer,omitempty"`
	PushToken    string         `json:"push_token,omitempty"`
}

type AuthLocation struct {
	Latitude       float64        `json:"latitude"`
	Longitude      float64        `json:"longitude"`
	AccuracyMeters *float64       `json:"accuracy_meters,omitempty"`
	Source         LocationSource `json:"source,omitempty"`
}

type UserDevice struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	Phone        string    `json:"phone"`
	DeviceID     string    `json:"device_id"`
	Platform     string    `json:"platform"`
	AppVersion   string    `json:"app_version,omitempty"`
	OSVersion    string    `json:"os_version,omitempty"`
	DeviceModel  string    `json:"device_model,omitempty"`
	Manufacturer string    `json:"manufacturer,omitempty"`
	PushToken    string    `json:"-"`
	FirstSeenAt  time.Time `json:"first_seen_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type UserLocationEvent struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id,omitempty"`
	Phone          string    `json:"phone,omitempty"`
	DeviceID       string    `json:"device_id,omitempty"`
	EventType      string    `json:"event_type"`
	Latitude       *float64  `json:"latitude,omitempty"`
	Longitude      *float64  `json:"longitude,omitempty"`
	AccuracyMeters *float64  `json:"accuracy_meters,omitempty"`
	Source         string    `json:"source,omitempty"`
	IPAddress      string    `json:"ip_address,omitempty"`
	UserAgent      string    `json:"user_agent,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type AuthEvent struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id,omitempty"`
	Phone        string    `json:"phone,omitempty"`
	EventType    string    `json:"event_type"`
	DeviceID     string    `json:"device_id,omitempty"`
	IPAddress    string    `json:"ip_address,omitempty"`
	UserAgent    string    `json:"user_agent,omitempty"`
	MetadataJSON string    `json:"metadata_json,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type AuthEventListFilter struct {
	Phone     string
	UserID    string
	EventType string
	Limit     int
	Offset    int
	From      *time.Time
	To        *time.Time
}

type UserLocationListFilter struct {
	UserID string
	Limit  int
	Offset int
	From   *time.Time
	To     *time.Time
}
