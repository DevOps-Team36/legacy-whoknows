package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/sessions"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	_ "whoknows_variations/server_go/docs"
	"whoknows_variations/server_go/internal/db"
	"whoknows_variations/server_go/internal/httpapi"
	"whoknows_variations/server_go/internal/queue"
)

// @title WhoKnows API
// @version 1.0
// @description API for the WhoKnows search application
// @host huw.dk
// @BasePath /
func main() {
	ctx := context.Background()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}

	if err := runMigrations(dsn); err != nil {
		log.Fatalf("migrations failed: %v", err)
	}

	pool, err := db.Open(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	secretKey := os.Getenv("WHOKNOWS_SECRET_KEY")
	if secretKey == "" {
		secretKey = "default-secret-change-me"
	}
	store := sessions.NewCookieStore([]byte(secretKey))

	var queueClient *queue.Client
	if queueSASURL := os.Getenv("AZURE_QUEUE_SAS_URL"); queueSASURL != "" {
		queueClient = queue.New(queueSASURL)
		log.Printf("Azure Storage Queue configured")
	} else {
		log.Printf("AZURE_QUEUE_SAS_URL not set — missed searches will not be queued")
	}

	scraperKey := os.Getenv("WHOKNOWS_SCRAPER_API_KEY")
	if scraperKey == "" {
		log.Printf("WHOKNOWS_SCRAPER_API_KEY not set — POST /api/pages will return 401")
	}

	scrapeKey := os.Getenv("WHOKNOWS_SCRAPE_TRIGGER_KEY")
	if scrapeKey == "" {
		log.Printf("WHOKNOWS_SCRAPE_TRIGGER_KEY not set — POST /api/scrape will return 401")
	}

	s := &httpapi.Server{
		DB:         pool,
		Sessions:   store,
		Queue:      queueClient,
		ScraperKey: scraperKey,
		ScrapeKey:  scrapeKey,
	}
	router := httpapi.NewRouter(s)

	port := os.Getenv("WHOKNOWS_PORT")
	if port == "" {
		port = "8080"
	}
	addr := os.Getenv("WHOKNOWS_ADDR")
	if addr == "" {
		addr = "0.0.0.0"
	}
	log.Printf("listening on %s:%s", sanitizeLogValue(addr), sanitizeLogValue(port)) // #nosec G706 -- Values are newline-sanitized before logging; sources are deployment configuration.

	srv := &http.Server{
		Addr:              addr + ":" + port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Fatal(srv.ListenAndServe())
}

func runMigrations(dsn string) error {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			log.Printf("close migration connection failed: %v", err)
		}
	}()

	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	migrationsDir := os.Getenv("WHOKNOWS_MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "./migrations"
	}

	return goose.Up(sqlDB, migrationsDir)
}

func sanitizeLogValue(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	return strings.ReplaceAll(value, "\n", "")
}
