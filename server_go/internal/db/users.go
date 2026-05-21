package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserRow struct {
	ID                     int64
	Username               string
	Email                  string
	PasswordHash           string
	PasswordChangeRequired bool
}

var ErrUserNotFound = errors.New("user not found")

func GetUserByUsername(ctx context.Context, conn *pgxpool.Pool, username string) (*UserRow, error) {
	row := conn.QueryRow(ctx,
		"SELECT id, username, email, password, password_change_required FROM users WHERE username = $1",
		username,
	)

	u := &UserRow{}
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.PasswordChangeRequired); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return u, nil
}

func GetUserByID(ctx context.Context, conn *pgxpool.Pool, id int64) (*UserRow, error) {
	row := conn.QueryRow(ctx,
		"SELECT id, username, email, password, password_change_required FROM users WHERE id = $1",
		id,
	)

	u := &UserRow{}
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.PasswordChangeRequired); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return u, nil
}

func CreateUser(ctx context.Context, conn *pgxpool.Pool, username, email, passwordHash string) error {
	_, err := conn.Exec(ctx,
		"INSERT INTO users (username, email, password) VALUES ($1, $2, $3)",
		username, email, passwordHash,
	)
	return err
}

func UpdateUserPassword(ctx context.Context, conn *pgxpool.Pool, id int64, passwordHash string) error {
	_, err := conn.Exec(ctx,
		"UPDATE users SET password = $1, password_change_required = false WHERE id = $2",
		passwordHash, id,
	)
	return err
}
