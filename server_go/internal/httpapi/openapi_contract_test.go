package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/sessions"
)

func testServer() *Server {
	return &Server{
		DB:       nil,
		Sessions: sessions.NewCookieStore([]byte("test-secret")),
	}
}

func TestAPISearchWithoutQReturns422RequestValidationError(t *testing.T) {
	s := testServer()
	r := NewRouter(s)

	req := httptest.NewRequest(http.MethodGet, "/api/search", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d", rec.Code)
	}

	var body RequestValidationError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("expected valid JSON response, got error: %v", err)
	}
	if body.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected statusCode 422 in body, got %d", body.StatusCode)
	}
	if body.Message == nil || *body.Message == "" {
		t.Fatal("expected non-empty message in validation error body")
	}
}

func TestAPILoginMissingFieldsReturns422HTTPValidationError(t *testing.T) {
	s := testServer()
	r := NewRouter(s)

	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d", rec.Code)
	}

	var body HTTPValidationError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("expected valid JSON response, got error: %v", err)
	}
	if len(body.Detail) == 0 {
		t.Fatal("expected at least one validation error detail")
	}
}

func TestAPIRegisterMissingEmailReturns422HTTPValidationError(t *testing.T) {
	s := testServer()
	r := NewRouter(s)

	form := url.Values{}
	form.Set("username", "alice")
	form.Set("password", "secret")

	req := httptest.NewRequest(http.MethodPost, "/api/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d", rec.Code)
	}

	var body HTTPValidationError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("expected valid JSON response, got error: %v", err)
	}
	if len(body.Detail) == 0 {
		t.Fatal("expected at least one validation error detail")
	}
}

func TestAPIWeatherReturnsStandardResponse(t *testing.T) {
	s := testServer()
	r := NewRouter(s)

	req := httptest.NewRequest(http.MethodGet, "/api/weather", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body StandardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("expected valid JSON response, got error: %v", err)
	}
	if body.Data == nil {
		t.Fatal("expected data object in weather response")
	}
}
