package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func SearchPages(ctx context.Context, conn *pgxpool.Pool, q string, language *string) ([]map[string]any, error) {
	q = strings.TrimSpace(q)

	// Legacy default: "en"
	lang := "en"
	if language != nil && strings.TrimSpace(*language) != "" {
		lang = strings.TrimSpace(*language)
	}

	out := make([]map[string]any, 0)
	seen := make(map[string]struct{})

	// First keep the old behavior: find pages where the title contains the full query.
	titleResults, err := searchPagesByTitle(ctx, conn, q, lang, 30)
	if err != nil {
		return nil, err
	}
	appendUniqueSearchResults(&out, seen, titleResults)

	remaining := 30 - len(out)
	if remaining <= 0 {
		return out, nil
	}

	// Then reduce the query to keywords in search_cleaning.go and search for those in content.
	terms := extractSearchTerms(q)
	if len(terms) == 0 {
		return out, nil
	}

	contentResults, err := searchPagesByContent(ctx, conn, terms, lang, remaining)
	if err != nil {
		return nil, err
	}
	appendUniqueSearchResults(&out, seen, contentResults)

	return out, nil
}

func searchPagesByTitle(ctx context.Context, conn *pgxpool.Pool, q string, lang string, limit int) ([]map[string]any, error) {
	rows, err := conn.Query(ctx, `
		SELECT title, url, language, last_updated, content
		FROM pages
		WHERE language = $1 AND title ILIKE $2
		LIMIT $3
	`, lang, "%"+q+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSearchRows(rows)
}

func searchPagesByContent(ctx context.Context, conn *pgxpool.Pool, terms []string, lang string, limit int) ([]map[string]any, error) {
	args := []any{lang}
	conditions := make([]string, 0, len(terms))

	// Build one safe parameterized whole-word condition per keyword from extractSearchTerms.
	for _, term := range terms {
		args = append(args, wholeWordSearchPattern(term))
		conditions = append(conditions, fmt.Sprintf("content ~* $%d", len(args)))
	}

	args = append(args, limit)
	query := fmt.Sprintf(`
		SELECT title, url, language, last_updated, content
		FROM pages
		WHERE language = $1 AND %s
		LIMIT $%d
	`, strings.Join(conditions, " AND "), len(args))

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSearchRows(rows)
}

// scanSearchRows converts database rows from title/content searches into the
// map format returned by SearchPages and the /api/search response.
func scanSearchRows(rows pgx.Rows) ([]map[string]any, error) {
	out := make([]map[string]any, 0)
	for rows.Next() {
		var title, url, language string
		var lastUpdated *time.Time
		var content string

		if err := rows.Scan(&title, &url, &language, &lastUpdated, &content); err != nil {
			return nil, err
		}

		row := map[string]any{
			"title":    title,
			"url":      url,
			"language": language,
			"content":  content,
		}
		if lastUpdated != nil {
			row["last_updated"] = lastUpdated.Format(time.RFC3339)
		} else {
			row["last_updated"] = nil
		}

		out = append(out, row)
	}

	return out, rows.Err()
}

// appendUniqueSearchResults appends new pages while preserving the current
// result order, so title matches stay before content matches and duplicates are skipped.
func appendUniqueSearchResults(out *[]map[string]any, seen map[string]struct{}, pages []map[string]any) {
	for _, page := range pages {
		url, _ := page["url"].(string)
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}

		seen[url] = struct{}{}
		*out = append(*out, page)
	}
}
