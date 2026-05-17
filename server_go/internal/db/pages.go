package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func InsertPage(ctx context.Context, conn *pgxpool.Pool, title, url, language, content string) error {
	_, err := conn.Exec(ctx, `
		INSERT INTO pages (title, url, language, content, last_updated)
		VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)
		ON CONFLICT DO NOTHING
	`, title, url, language, content)
	return err
}
