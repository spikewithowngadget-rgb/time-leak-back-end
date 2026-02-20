package domain

import "time"

type RefreshRecord struct {
	UserUUID  string
	Email     string
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
