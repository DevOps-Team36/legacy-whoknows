package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/sessions"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	httpSwagger "github.com/swaggo/http-swagger"

	"whoknows_variations/server_go/internal/metrics"
	"whoknows_variations/server_go/internal/queue"
)

const SessionName = "session"

type Server struct {
	DB             *pgxpool.Pool
	Sessions       *sessions.CookieStore
	Queue          *queue.Client
	ScraperKey     string // POST /api/pages — used by Azure Function scraper
	ScrapeKey      string // POST /api/scrape — used by admins to trigger jobs
	WeatherService WeatherProvider
}

type WeatherProvider interface {
	Forecast(ctx context.Context, city string) (map[string]any, error)
}

func NewRouter(s *Server) http.Handler {
	r := chi.NewRouter()

	r.Use(s.UserFromSession)
	r.Use(s.RequirePasswordChange)
	r.Use(observeHTTPMetrics)

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))
	r.Handle("/metrics", promhttp.Handler())

	// HTML routes
	r.Get("/", s.ServeRootPage)
	r.Get("/about", s.ServeAboutPage)
	r.Get("/weather", s.ServeWeatherPage)
	r.Get("/status", s.ServeStatusPage)
	r.Get("/register", s.ServeRegisterPage)
	r.Get("/login", s.ServeLoginPage)
	r.Get("/change-password", s.ServeChangePasswordPage)

	// API routes
	r.Get("/api/search", s.Search)
	r.Get("/api/weather", s.Weather)
	r.Post("/api/register", s.Register)
	r.Post("/api/login", s.Login)
	r.Post("/api/change-password", s.ChangePassword)
	r.Get("/api/logout", s.Logout)
	r.Post("/api/pages", s.AddPage)
	r.Post("/api/scrape", s.TriggerScrape)

	// Swagger UI
	r.Get("/swagger/*", httpSwagger.WrapHandler)

	return r
}

func observeHTTPMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unknown"
		}
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		metrics.ObserveHTTPRequest(r.Method, route, status, started)
	})
}
