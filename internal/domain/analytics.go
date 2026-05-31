package domain

import "time"

// AdsStats holds aggregate counts for the ads inventory.
type AdsStats struct {
	Total    int `json:"total"`
	Active   int `json:"active"`
	Inactive int `json:"inactive"`
}

// DeviceStats holds aggregate counts for stored user devices.
type DeviceStats struct {
	Total   int `json:"total"`
	IOS     int `json:"ios"`
	Android int `json:"android"`
	Active  int `json:"active"`
}

// LocationStats holds aggregate counts for stored location events.
type LocationStats struct {
	Total         int `json:"total"`
	UniqueUsers   int `json:"unique_users"`
	UniqueDevices int `json:"unique_devices"`
}

// AnalyticsOverview is the response for the admin overview endpoint.
type AnalyticsOverview struct {
	Ads       AdsStats      `json:"ads"`
	Devices   DeviceStats   `json:"devices"`
	Locations LocationStats `json:"locations"`
}

// GeoFilter narrows the geo aggregation query. All fields are optional.
type GeoFilter struct {
	From       *time.Time
	To         *time.Time
	Platform   string // "", "ios", "android"
	Source     string // "", "gps", "network", "manual", "unknown"
	ActiveOnly bool
	Limit      int
}

// GeoPoint is a single stored location event prepared for the admin map.
// Phone is never serialized; only PhoneMasked is exposed.
type GeoPoint struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id,omitempty"`
	Phone          string    `json:"-"`
	PhoneMasked    string    `json:"phone_masked,omitempty"`
	DeviceID       string    `json:"device_id,omitempty"`
	Platform       string    `json:"platform,omitempty"`
	DeviceModel    string    `json:"device_model,omitempty"`
	Latitude       float64   `json:"latitude"`
	Longitude      float64   `json:"longitude"`
	AccuracyMeters *float64  `json:"accuracy_meters,omitempty"`
	Source         string    `json:"source,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// GeoCenter is an approximate center coordinate for a grouped region.
type GeoCenter struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// GeoRegion is an approximate, server-side grouping of location points.
// Names are derived from the nearest known city or labeled approximate;
// they are never invented exact administrative names.
type GeoRegion struct {
	Name        string    `json:"name"`
	Approximate bool      `json:"approximate"`
	Count       int       `json:"count"`
	UniqueUsers int       `json:"unique_users"`
	Percentage  float64   `json:"percentage"`
	Center      GeoCenter `json:"center"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

// GeoSummary is the headline aggregation for the geo analytics page.
type GeoSummary struct {
	TotalEvents    int    `json:"total_events"`
	UniqueUsers    int    `json:"unique_users"`
	UniqueDevices  int    `json:"unique_devices"`
	IOSDevices     int    `json:"ios_devices"`
	AndroidDevices int    `json:"android_devices"`
	TopRegion      string `json:"top_region"`
}

// GeoResult is the full response for the geo analytics endpoint.
type GeoResult struct {
	Summary GeoSummary  `json:"summary"`
	Regions []GeoRegion `json:"regions"`
	Points  []GeoPoint  `json:"points"`
}

// AdminDeviceListFilter narrows the global admin device listing.
type AdminDeviceListFilter struct {
	Platform string
	Active   *bool
	Search   string
	Limit    int
	Offset   int
}
