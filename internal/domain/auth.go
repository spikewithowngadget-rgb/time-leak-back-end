package domain

import "time"

type RefreshRecord struct {
	UserUUID  string
	Phone     string
	AuthType  string
	Role      string
	ExpiresAt time.Time
	Revoked   bool
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type User struct {
	UserID       string    `json:"userId"`
	Email        string    `json:"email"`
	Phone        string    `json:"phone,omitempty"`
	Password     string    `json:"-"`
	UserLanguage string    `json:"userLanguage"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Note struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	NoteType  string    `json:"note_type"`
	CreatedAt time.Time `json:"createdAt"`
}

type OTPChannel string

const (
	OTPChannelWhatsApp OTPChannel = "whatsapp"
)

type OTPRequest struct {
	ID          string
	Channel     OTPChannel
	Destination string
	CodeHash    string
	ExpiresAt   time.Time
	UsedAt      *time.Time
	Attempts    int
	MaxAttempts int
	CreatedAt   time.Time
	LastAttempt *time.Time
}

type OTPLockState struct {
	Channel        OTPChannel
	Destination    string
	FailedAttempts int
	LockedUntil    *time.Time
	UpdatedAt      time.Time
}

type OTPRequestResult struct {
	RequestID        string `json:"request_id"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

type OTPVerifyResult struct {
	Channel     OTPChannel
	Destination string
}

type OTPTestingCode struct {
	RequestID   string     `json:"request_id"`
	Channel     OTPChannel `json:"channel"`
	Destination string     `json:"destination"`
	Code        string     `json:"code"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
}

type Ad struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	ImageURL  string    `json:"image_url"`
	TargetURL string    `json:"target_url"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AdCreateInput struct {
	Title     string
	ImageURL  string
	TargetURL string
	IsActive  bool
}

type AdUpdateInput struct {
	Title     *string
	ImageURL  *string
	TargetURL *string
	IsActive  *bool
}

type UserAdState struct {
	UserID    string
	LastAdID  string
	UpdatedAt time.Time
}
