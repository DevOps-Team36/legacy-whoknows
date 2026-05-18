package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"whoknows_variations/server_go/internal/auth"
	"whoknows_variations/server_go/internal/db"
	"whoknows_variations/server_go/internal/metrics"
	"whoknows_variations/server_go/internal/searchlog"
)

type contextKey string

const userContextKey contextKey = "user"

type AuthResponse struct {
	StatusCode *int    `json:"statusCode"`
	Message    *string `json:"message"`
}

type SearchResponse struct {
	Data []map[string]any `json:"data"`
}

type StandardResponse struct {
	Data map[string]any `json:"data"`
}

type RequestValidationError struct {
	StatusCode int     `json:"statusCode"`
	Message    *string `json:"message"`
}

type ValidationError struct {
	Loc  []any  `json:"loc"`
	Msg  string `json:"msg"`
	Type string `json:"type"`
}

type HTTPValidationError struct {
	Detail []ValidationError `json:"detail"`
}

type User struct {
	ID       int64
	Username string
	Email    string
}

type ViewData struct {
	User    *User
	Flashes []string
	Error   string
	Results []map[string]any
	Query   string
	Weather map[string]any
}

// UserFromSession is chi middleware that loads the logged-in user (if any)
// from the session cookie and stores it in the request context.
func (s *Server) UserFromSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, _ := s.Sessions.Get(r, SessionName)
		uid, ok := sess.Values["user_id"]
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		userID, ok := uid.(int64)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		row, err := db.GetUserByID(r.Context(), s.DB, userID)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		u := &User{ID: row.ID, Username: row.Username, Email: row.Email}
		ctx := context.WithValue(r.Context(), userContextKey, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func currentUser(r *http.Request) *User {
	u, _ := r.Context().Value(userContextKey).(*User)
	return u
}

func (s *Server) getFlashes(w http.ResponseWriter, r *http.Request) []string {
	sess, _ := s.Sessions.Get(r, SessionName)
	raw := sess.Flashes()
	if len(raw) == 0 {
		return nil
	}
	_ = sess.Save(r, w)
	out := make([]string, len(raw))
	for i, v := range raw {
		out[i], _ = v.(string)
	}
	return out
}

// flashAndRedirect stashes a message in the session and redirects to `to`.
// The message renders via layout.html's `.Flashes` on the next page load.
func (s *Server) flashAndRedirect(w http.ResponseWriter, r *http.Request, msg, to string) {
	sess, _ := s.Sessions.Get(r, SessionName)
	sess.AddFlash(msg)
	_ = sess.Save(r, w)
	http.Redirect(w, r, to, http.StatusSeeOther)
}

var (
	// per-page template cache
	pageTemplates   = map[string]*template.Template{}
	pageTemplatesMu sync.RWMutex

	// resolved template directory (cached)
	foundTemplateDir    string
	foundTemplateDirMu  sync.Once
	foundTemplateDirErr error
)

func findTemplateDir() (string, error) {
	foundTemplateDirMu.Do(func() {
		candidates := []string{
			"./templates",
			"templates",
			"../templates",
			"./server_go/templates",
			"server_go/templates",
			"../server_go/templates",
		}
		for _, c := range candidates {
			if info, err := os.Stat(c); err == nil && info.IsDir() {
				foundTemplateDir = c
				break
			}
			_, _ = os.Stderr.WriteString("templates: checked candidate '" + c + "' -> not found\n")
		}
		if foundTemplateDir == "" {
			foundTemplateDirErr = os.ErrNotExist
			_, _ = os.Stderr.WriteString("templates: no templates directory found among candidates\n")
		}
	})
	return foundTemplateDir, foundTemplateDirErr
}

func loadTemplateFor(pageFilename string) (*template.Template, error) {
	// cache fast-path
	pageTemplatesMu.RLock()
	t := pageTemplates[pageFilename]
	pageTemplatesMu.RUnlock()
	if t != nil {
		return t, nil
	}

	// build and cache
	dir, err := findTemplateDir()
	if err != nil {
		return nil, err
	}

	layoutPath := filepath.Join(dir, "layout.html")
	pagePath := filepath.Join(dir, pageFilename)

	// parse layout first, then page so page definitions override blocks
	t, err = template.ParseFiles(layoutPath, pagePath)
	if err != nil {
		_, _ = os.Stderr.WriteString("templates: ParseFiles error for " + pageFilename + ": " + err.Error() + "\n")
		return nil, err
	}

	pageTemplatesMu.Lock()
	pageTemplates[pageFilename] = t
	pageTemplatesMu.Unlock()
	return t, nil
}

// render helper — parses layout + requested page, executes the page template
func renderTemplate(w http.ResponseWriter, name string, data any) {
	t, err := loadTemplateFor(name)
	if err != nil {
		_, _ = os.Stderr.WriteString("templates load error: " + err.Error() + "\n")
		// fallback if templates failed to load
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><head></head><body><h1>WhoKnows</h1></body></html>"))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// First try to execute the page's own template (e.g. "about.html")
	if execErr := t.ExecuteTemplate(w, name, data); execErr != nil {
		// fallback to executing layout directly
		_, _ = os.Stderr.WriteString("templates: ExecuteTemplate(" + name + ") error: " + execErr.Error() + "\n")
		if execErr2 := t.ExecuteTemplate(w, "layout", data); execErr2 != nil {
			_, _ = os.Stderr.WriteString("templates: ExecuteTemplate(layout) error: " + execErr2.Error() + "\n")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("<html><body><h1>Template execution error</h1></body></html>"))
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeLoginRegisterValidationError(w http.ResponseWriter, field string) {
	writeJSON(w, http.StatusUnprocessableEntity, HTTPValidationError{
		Detail: []ValidationError{
			{
				Loc:  []any{"body", field},
				Msg:  "Field required",
				Type: "missing",
			},
		},
	})
}

func requireFormFields(w http.ResponseWriter, r *http.Request, requiredFields ...string) bool {
	if err := r.ParseForm(); err != nil {
		writeLoginRegisterValidationError(w, requiredFields[0])
		return false
	}

	for _, field := range requiredFields {
		if _, ok := r.PostForm[field]; !ok {
			writeLoginRegisterValidationError(w, field)
			return false
		}
	}
	return true
}

func writeSearchValidationError(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusUnprocessableEntity, RequestValidationError{
		StatusCode: http.StatusUnprocessableEntity,
		Message:    &msg,
	})
}

// ServeRootPage godoc
// @Summary Serve Root Page
// @Description Serves the main search page as HTML.
// @Tags pages
// @Produce html
// @Success 200 {string} string "HTML page"
// @Router / [get]
func (s *Server) ServeRootPage(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	langParam := strings.TrimSpace(r.URL.Query().Get("language"))
	var lang *string
	if langParam != "" {
		lang = &langParam
	}

	var results []map[string]any
	if q != "" {
		started := time.Now()
		var err error
		results, err = db.SearchPages(r.Context(), s.DB, q, lang)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		metrics.ObserveSearch(time.Since(started), len(results))
		if err := searchlog.LogSearch(q, lang, len(results)); err != nil {
			log.Printf("search log write failed: %v", err)
		}
	}

	renderTemplate(w, "search.html", ViewData{
		User:    currentUser(r),
		Flashes: s.getFlashes(w, r),
		Results: results,
		Query:   q,
	})
}

func (s *Server) ServeSearchPage(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "search.html", ViewData{User: currentUser(r), Flashes: s.getFlashes(w, r)})
}

func (s *Server) ServeAboutPage(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "about.html", ViewData{User: currentUser(r), Flashes: s.getFlashes(w, r)})
}

// ServeWeatherPage godoc
// @Summary Serve Weather Page
// @Description Serves the weather forecast page as HTML.
// @Tags pages
// @Produce html
// @Param city query string false "City name"
// @Success 200 {string} string "HTML page"
// @Router /weather [get]
func (s *Server) ServeWeatherPage(w http.ResponseWriter, r *http.Request) {
	city := strings.TrimSpace(r.URL.Query().Get("city"))
	renderTemplate(w, "weather.html", ViewData{
		User:    currentUser(r),
		Flashes: s.getFlashes(w, r),
		Query:   city,
		Weather: s.weatherForecast(r, city),
	})
}

// ServeRegisterPage godoc
// @Summary Serve Register Page
// @Description Serves the registration page as HTML.
// @Tags pages
// @Produce html
// @Success 200 {string} string "HTML page"
// @Router /register [get]
func (s *Server) ServeRegisterPage(w http.ResponseWriter, r *http.Request) {
	if currentUser(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	renderTemplate(w, "register.html", ViewData{Flashes: s.getFlashes(w, r)})
}

// ServeLoginPage godoc
// @Summary Serve Login Page
// @Description Serves the login page as HTML.
// @Tags pages
// @Produce html
// @Success 200 {string} string "HTML page"
// @Router /login [get]
func (s *Server) ServeLoginPage(w http.ResponseWriter, r *http.Request) {
	if currentUser(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	renderTemplate(w, "login.html", ViewData{Flashes: s.getFlashes(w, r)})
}

// Search godoc
// @Summary Search
// @Description Search wiki pages by title. Returns matching pages as JSON.
// @Tags search
// @Produce json
// @Param q query string true "Search query"
// @Param language query string false "Language code (e.g., 'en')"
// @Success 200 {object} SearchResponse
// @Failure 422 {object} RequestValidationError "Unprocessable Entity"
// @Router /api/search [get]
func (s *Server) Search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		msg := "Missing required query parameter: q"
		writeSearchValidationError(w, msg)
		return
	}

	langParam := r.URL.Query().Get("language")
	var lang *string
	if langParam != "" {
		lang = &langParam
	}

	started := time.Now()
	results, err := db.SearchPages(r.Context(), s.DB, q, lang)
	if err != nil {
		log.Printf("search query failed: %v", err)
		writeJSON(w, http.StatusOK, SearchResponse{Data: []map[string]any{}})
		return
	}
	metrics.ObserveSearch(time.Since(started), len(results))
	if err := searchlog.LogSearch(q, lang, len(results)); err != nil {
		log.Printf("search log write failed: %v", err)
	}

	writeJSON(w, http.StatusOK, SearchResponse{Data: results})
}

// TriggerScrape godoc
// @Summary Trigger Scrape
// @Description Manually enqueue a scrape job for a specific query. Use this after reviewing
// @Description zero-result searches in Grafana to index missing content.
// @Tags scrape
// @Accept json
// @Produce json
// @Param Authorization header string true "Bearer <api-key>"
// @Param body body object true "Query to scrape: {query, language}"
// @Success 202 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /api/scrape [post]
func (s *Server) TriggerScrape(w http.ResponseWriter, r *http.Request) {
	expected := []byte("Bearer " + s.ScrapeKey)
	actual := []byte(r.Header.Get("Authorization"))
	if s.ScrapeKey == "" || subtle.ConstantTimeCompare(actual, expected) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var body struct {
		Query    string `json:"query"`
		Language string `json:"language"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}
	if body.Language == "" {
		body.Language = "en"
	}
	if body.Language != "en" && body.Language != "da" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "language must be 'en' or 'da'"})
		return
	}

	if s.Queue == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "queue not configured"})
		return
	}

	if err := s.Queue.Send(map[string]string{"query": body.Query, "language": body.Language}); err != nil {
		log.Printf("failed to queue scrape request: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to queue scrape request"})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"message": "scrape queued"})
}

// AddPage godoc
// @Summary Add Page
// @Description Inserts a scraped page into the database. Used by the Azure Function web scraper.
// @Tags pages
// @Accept json
// @Produce json
// @Param Authorization header string true "Bearer <api-key>"
// @Param page body object true "Page data"
// @Success 201 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /api/pages [post]
func (s *Server) AddPage(w http.ResponseWriter, r *http.Request) {
	expected := []byte("Bearer " + s.ScraperKey)
	actual := []byte(r.Header.Get("Authorization"))
	if s.ScraperKey == "" || subtle.ConstantTimeCompare(actual, expected) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 2<<20) // 2 MB cap

	var page struct {
		Title    string `json:"title"`
		URL      string `json:"url"`
		Language string `json:"language"`
		Content  string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&page); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if page.Title == "" || page.URL == "" || page.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title, url and content are required"})
		return
	}
	if page.Language == "" {
		page.Language = "en"
	}
	if page.Language != "en" && page.Language != "da" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "language must be 'en' or 'da'"})
		return
	}

	if err := db.InsertPage(r.Context(), s.DB, page.Title, page.URL, page.Language, page.Content); err != nil {
		log.Printf("insert page failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	log.Printf("indexed page: %s", page.Title)
	writeJSON(w, http.StatusCreated, map[string]string{"message": "page indexed"})
}

// Weather godoc
// @Summary Weather
// @Description Returns the current weather forecast.
// @Tags weather
// @Produce json
// @Param city query string false "City name"
// @Success 200 {object} StandardResponse
// @Router /api/weather [get]
func (s *Server) Weather(w http.ResponseWriter, r *http.Request) {
	city := strings.TrimSpace(r.URL.Query().Get("city"))
	writeJSON(w, http.StatusOK, StandardResponse{Data: s.weatherForecast(r, city)})
}

func (s *Server) weatherForecast(r *http.Request, city string) map[string]any {
	if s.WeatherService == nil {
		return fallbackWeatherForecast(city)
	}

	forecast, err := s.WeatherService.Forecast(r.Context(), city)
	if err != nil {
		log.Printf("weather forecast failed: %v", err)
		return fallbackWeatherForecast(city)
	}
	return forecast
}

func fallbackWeatherForecast(city string) map[string]any {
	location := city
	if location == "" {
		location = "Copenhagen"
	}
	return map[string]any{
		"location":        location,
		"summary":         "Forecast temporarily unavailable.",
		"icon":            "partly_cloudy_day",
		"source":          "placeholder",
		"temperature":     12,
		"temperatureUnit": "celsius",
		"forecast": []map[string]any{
			{
				"day":         "Today",
				"condition":   "Cloudy",
				"icon":        "cloud",
				"temperature": 12,
				"unit":        "celsius",
			},
			{
				"day":         "Tomorrow",
				"condition":   "Light rain",
				"icon":        "rainy_light",
				"temperature": 10,
				"unit":        "celsius",
			},
			{
				"day":         "Day 3",
				"condition":   "Partly sunny",
				"icon":        "partly_cloudy_day",
				"temperature": 13,
				"unit":        "celsius",
			},
		},
	}
}

// Register godoc
// @Summary Register
// @Description Create a new user account. Validates input and checks for duplicate usernames.
// @Tags auth
// @Accept x-www-form-urlencoded
// @Produce json
// @Param username formData string true "Username"
// @Param email formData string true "Email"
// @Param password formData string true "Password"
// @Param password2 formData string false "Password2"
// @Success 200 {object} AuthResponse
// @Failure 422 {object} HTTPValidationError "Validation Error"
// @Router /api/register [post]
func (s *Server) Register(w http.ResponseWriter, r *http.Request) {
	if !requireFormFields(w, r, "username", "email", "password") {
		return
	}

	if currentUser(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	password2 := r.FormValue("password2")

	var formError string
	switch {
	case username == "":
		formError = "You have to enter a username"
	case email == "" || !strings.Contains(email, "@"):
		formError = "You have to enter a valid email address"
	case password == "":
		formError = "You have to enter a password"
	case password != password2:
		formError = "The two passwords do not match"
	default:
		_, err := db.GetUserByUsername(r.Context(), s.DB, username)
		if err == nil {
			formError = "The username is already taken"
		} else if !errors.Is(err, db.ErrUserNotFound) {
			log.Printf("register username lookup failed: %v", err)
			s.flashAndRedirect(w, r, "Internal error, please try again", "/register")
			return
		}
	}

	if formError != "" {
		s.flashAndRedirect(w, r, formError, "/register")
		return
	}

	hash := auth.HashPassword(password)
	if err := db.CreateUser(r.Context(), s.DB, username, email, hash); err != nil {
		log.Printf("register create user failed: %v", err)
		s.flashAndRedirect(w, r, "Internal error, please try again", "/register")
		return
	}

	s.flashAndRedirect(w, r, "You were successfully registered and can login now", "/login")
}

// Login godoc
// @Summary Login
// @Description Authenticate a user with username and password. Sets a session cookie on success.
// @Tags auth
// @Accept x-www-form-urlencoded
// @Produce json
// @Param username formData string true "Username"
// @Param password formData string true "Password"
// @Success 200 {object} AuthResponse
// @Failure 422 {object} HTTPValidationError "Validation Error"
// @Router /api/login [post]
func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	if !requireFormFields(w, r, "username", "password") {
		return
	}

	if currentUser(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	user, err := db.GetUserByUsername(r.Context(), s.DB, username)
	if err != nil {
		if errors.Is(err, db.ErrUserNotFound) {
			s.flashAndRedirect(w, r, "Invalid username", "/login")
			return
		}
		log.Printf("login username lookup failed: %v", err)
		s.flashAndRedirect(w, r, "Internal error, please try again", "/login")
		return
	}

	if !auth.VerifyPassword(user.PasswordHash, password) {
		s.flashAndRedirect(w, r, "Invalid password", "/login")
		return
	}

	sess, _ := s.Sessions.Get(r, SessionName)
	sess.Values["user_id"] = user.ID
	_ = sess.Save(r, w)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Logout godoc
// @Summary Logout
// @Description Clear the session and log the user out.
// @Tags auth
// @Produce json
// @Success 200 {object} AuthResponse
// @Router /api/logout [get]
func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	sess, _ := s.Sessions.Get(r, SessionName)
	delete(sess.Values, "user_id")
	_ = sess.Save(r, w)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
