package repository

import (
	"database/sql"
)

type Repositories struct {
	Auth *Repository
}

func NewRepositories(db *sql.DB) *Repositories {
	repo := New(db)

	return &Repositories{
		Auth: repo,
	}
}
