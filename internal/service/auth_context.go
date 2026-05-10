package service

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"time-leak/internal/domain"
)

var (
	ErrAuthPurposeRequired   = errors.New("auth purpose is required")
	ErrInvalidAuthPurpose    = errors.New("auth purpose is invalid")
	ErrInvalidDeviceID       = errors.New("device_id is required")
	ErrInvalidDevicePlatform = errors.New("device platform is invalid")
	ErrInvalidLatitude       = errors.New("latitude is invalid")
	ErrInvalidLongitude      = errors.New("longitude is invalid")
	ErrInvalidLocationSource = errors.New("location source is invalid")
	ErrContactPhoneMismatch  = errors.New("telegram contact phone does not match")
	ErrTelegramLinkExpired   = errors.New("telegram verification link expired")
	ErrTelegramLinkInvalid   = errors.New("telegram verification link is invalid")
	ErrTelegramSessionState  = errors.New("telegram verification session state is invalid")
	ErrTelegramBotNotReady   = errors.New("telegram verification is not configured")
)

type AuthRequestContext struct {
	IPAddress string
	UserAgent string
}

type AuthDeviceInput struct {
	DeviceID     string `json:"device_id"`
	Platform     string `json:"platform"`
	AppVersion   string `json:"app_version,omitempty"`
	OSVersion    string `json:"os_version,omitempty"`
	DeviceModel  string `json:"device_model,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	PushToken    string `json:"push_token,omitempty"`
}

type AuthLocationInput struct {
	Latitude       float64  `json:"latitude"`
	Longitude      float64  `json:"longitude"`
	AccuracyMeters *float64 `json:"accuracy_meters,omitempty"`
	Source         string   `json:"source,omitempty"`
}

func normalizeAuthPurpose(raw string) (domain.AuthVerificationPurpose, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "":
		return "", ErrAuthPurposeRequired
	case string(domain.AuthVerificationPurposeRegistration):
		return domain.AuthVerificationPurposeRegistration, nil
	case string(domain.AuthVerificationPurposeLogin):
		return domain.AuthVerificationPurposeLogin, nil
	case string(domain.AuthVerificationPurposePasswordReset):
		return domain.AuthVerificationPurposePasswordReset, nil
	default:
		return "", ErrInvalidAuthPurpose
	}
}

func normalizeDeviceInput(in *AuthDeviceInput) (*domain.AuthDevice, error) {
	if in == nil {
		return nil, nil
	}

	deviceID := strings.TrimSpace(in.DeviceID)
	if deviceID == "" {
		return nil, ErrInvalidDeviceID
	}

	platform := strings.TrimSpace(strings.ToLower(in.Platform))
	if platform != string(domain.DevicePlatformIOS) && platform != string(domain.DevicePlatformAndroid) {
		return nil, ErrInvalidDevicePlatform
	}

	return &domain.AuthDevice{
		DeviceID:     deviceID,
		Platform:     domain.DevicePlatform(platform),
		AppVersion:   strings.TrimSpace(in.AppVersion),
		OSVersion:    strings.TrimSpace(in.OSVersion),
		DeviceModel:  strings.TrimSpace(in.DeviceModel),
		Manufacturer: strings.TrimSpace(in.Manufacturer),
		PushToken:    strings.TrimSpace(in.PushToken),
	}, nil
}

func normalizeLocationInput(in *AuthLocationInput) (*domain.AuthLocation, error) {
	if in == nil {
		return nil, nil
	}

	if in.Latitude < -90 || in.Latitude > 90 {
		return nil, ErrInvalidLatitude
	}
	if in.Longitude < -180 || in.Longitude > 180 {
		return nil, ErrInvalidLongitude
	}

	source := strings.TrimSpace(strings.ToLower(in.Source))
	if source == "" {
		source = string(domain.LocationSourceUnknown)
	}
	switch source {
	case string(domain.LocationSourceGPS),
		string(domain.LocationSourceNetwork),
		string(domain.LocationSourceManual),
		string(domain.LocationSourceUnknown):
	default:
		return nil, ErrInvalidLocationSource
	}

	var accuracy *float64
	if in.AccuracyMeters != nil {
		val := *in.AccuracyMeters
		if val < 0 {
			val = 0
		}
		accuracy = &val
	}

	return &domain.AuthLocation{
		Latitude:       in.Latitude,
		Longitude:      in.Longitude,
		AccuracyMeters: accuracy,
		Source:         domain.LocationSource(source),
	}, nil
}

func marshalJSON(v any) string {
	if v == nil {
		return ""
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

func metadataWithRequestID(requestID string, extra map[string]any) string {
	if extra == nil {
		extra = make(map[string]any, 1)
	}
	extra["request_id"] = strings.TrimSpace(requestID)
	return marshalJSON(extra)
}

func ptrTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	copy := t
	return &copy
}
