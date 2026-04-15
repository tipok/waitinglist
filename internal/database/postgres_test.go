package database

import (
	"testing"
)

func TestNewPostgresDB_InvalidURL(t *testing.T) {
	_, err := NewPostgresDB("invalid://not-a-valid-url")
	if err == nil {
		t.Fatal("expected error for invalid connection URL")
	}
}

func TestNewPostgresDB_UnreachableHost(t *testing.T) {
	_, err := NewPostgresDB("postgres://localhost:59999/nonexistent?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("expected error for unreachable database host")
	}
}
