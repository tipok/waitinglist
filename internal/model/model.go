package model

import "errors"

// Sentinel errors for repository operations.
var (
	ErrDuplicateEmail = errors.New("email already exists")
	ErrUserNotFound   = errors.New("user not found")
)

// UserEntity represents a user stored in the user_entity table.
type UserEntity struct {
	ID        string `json:"id"`
	Firstname string `json:"firstname"`
	Lastname  string `json:"lastname"`
	Email     string `json:"email"`
	HasAccess bool   `json:"has_access"`
}
