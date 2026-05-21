package db

import (
	"context"
	"errors"
	"testing"
)

func TestCreateUser_And_GetByUsername(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)

	if err := CreateUser(ctx, pool, "alice", "alice@example.com", "abc123hash"); err != nil {
		t.Fatal(err)
	}

	u, err := GetUserByUsername(ctx, pool, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "alice" {
		t.Errorf("expected username 'alice', got %q", u.Username)
	}
	if u.Email != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got %q", u.Email)
	}
	if u.PasswordHash != "abc123hash" {
		t.Errorf("expected password hash 'abc123hash', got %q", u.PasswordHash)
	}
}

func TestGetUserByUsername_NotFound(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)

	_, err := GetUserByUsername(ctx, pool, "nobody")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestGetUserByID(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)

	if err := CreateUser(ctx, pool, "bob", "bob@example.com", "somehash"); err != nil {
		t.Fatal(err)
	}

	u, err := GetUserByUsername(ctx, pool, "bob")
	if err != nil {
		t.Fatal(err)
	}

	u2, err := GetUserByID(ctx, pool, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u2.Username != "bob" {
		t.Errorf("expected username 'bob', got %q", u2.Username)
	}
}

func TestUpdateUserPassword_ClearsPasswordChangeRequired(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)

	if err := CreateUser(ctx, pool, "resetuser", "reset@example.com", "oldhash"); err != nil {
		t.Fatal(err)
	}

	u, err := GetUserByUsername(ctx, pool, "resetuser")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, "UPDATE users SET password_change_required = true WHERE id = $1", u.ID); err != nil {
		t.Fatal(err)
	}

	if err := UpdateUserPassword(ctx, pool, u.ID, "newhash"); err != nil {
		t.Fatal(err)
	}

	updated, err := GetUserByID(ctx, pool, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.PasswordHash != "newhash" {
		t.Errorf("expected updated password hash, got %q", updated.PasswordHash)
	}
	if updated.PasswordChangeRequired {
		t.Error("expected password change requirement to be cleared")
	}
}

func TestGetUserByID_NotFound(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)

	_, err := GetUserByID(ctx, pool, 9999)
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestCreateUser_DuplicateUsername(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)

	if err := CreateUser(ctx, pool, "charlie", "charlie@example.com", "hash1"); err != nil {
		t.Fatal(err)
	}
	if err := CreateUser(ctx, pool, "charlie", "other@example.com", "hash2"); err == nil {
		t.Error("expected error for duplicate username, got nil")
	}
}
