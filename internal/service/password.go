package service

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

const (
	minPasswordLength = 8
	maxPasswordBytes  = 72 // bcrypt's input limit is in bytes, not characters.
)

func validatePasswordInput(password, confirmPassword string) error {
	if password == "" || confirmPassword == "" {
		return ErrPasswordRequired
	}
	if password != confirmPassword {
		return ErrPasswordMismatch
	}
	if len(password) < minPasswordLength {
		return ErrPasswordTooShort
	}
	if len(password) > maxPasswordBytes {
		return ErrPasswordTooLong
	}
	return nil
}

func hashPassword(password string) (string, error) {
	if password == "" {
		return "", ErrPasswordRequired
	}
	if len(password) > maxPasswordBytes {
		return "", ErrPasswordTooLong
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", errors.New("password hash failed")
	}
	return string(hash), nil
}

func comparePasswordHash(passwordHash, password string) bool {
	if passwordHash == "" || password == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) == nil
}
