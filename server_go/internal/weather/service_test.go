package weather

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestForecastCachesProviderResponse(t *testing.T) {
	calls := 0
	service := NewService()
	service.geocodeEndpoint = "https://open-meteo.test/v1/search"
	service.forecastEndpoint = "https://open-meteo.test/v1/forecast"
	service.ttl = time.Hour
	service.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path == "/v1/search" {
			return jsonResponse(http.StatusOK, `{
				"results": [{
					"name": "Aarhus",
					"admin1": "Central Jutland",
					"country": "Denmark",
					"latitude": 56.1567,
					"longitude": 10.2108,
					"timezone": "Europe/Copenhagen"
				}]
			}`), nil
		}
		return jsonResponse(http.StatusOK, `{
			"current": {"temperature_2m": 8.4, "weather_code": 3},
			"daily": {
				"time": ["2026-05-18", "2026-05-19", "2026-05-20"],
				"weather_code": [3, 61, 1],
				"temperature_2m_max": [9.2, 11.1, 13.4],
				"temperature_2m_min": [4.1, 6.2, 7.0]
			}
		}`), nil
	})}

	first, err := service.Forecast(context.Background(), "Aarhus")
	if err != nil {
		t.Fatalf("first forecast failed: %v", err)
	}
	second, err := service.Forecast(context.Background(), "Aarhus")
	if err != nil {
		t.Fatalf("second forecast failed: %v", err)
	}

	if calls != 2 {
		t.Fatalf("expected one geocoding call and one forecast call, got %d", calls)
	}
	if first["source"] != "open-meteo" || second["source"] != "open-meteo" {
		t.Fatalf("expected Open-Meteo source, got first=%v second=%v", first["source"], second["source"])
	}
	if first["location"] != "Aarhus, Central Jutland, Denmark" {
		t.Fatalf("expected resolved city name, got %v", first["location"])
	}
}

func TestForecastReturnsStaleCacheOnProviderFailure(t *testing.T) {
	calls := 0
	service := NewService()
	service.geocodeEndpoint = "https://open-meteo.test/v1/search"
	service.forecastEndpoint = "https://open-meteo.test/v1/forecast"
	service.ttl = -time.Second
	service.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path == "/v1/search" {
			return jsonResponse(http.StatusOK, `{
				"results": [{
					"name": "Copenhagen",
					"country": "Denmark",
					"latitude": 55.6761,
					"longitude": 12.5683,
					"timezone": "Europe/Copenhagen"
				}]
			}`), nil
		}
		if calls == 2 {
			return jsonResponse(http.StatusOK, `{
				"current": {"temperature_2m": 5, "weather_code": 0},
				"daily": {
					"time": ["2026-05-18"],
					"weather_code": [0],
					"temperature_2m_max": [6],
					"temperature_2m_min": [2]
				}
			}`), nil
		}
		return jsonResponse(http.StatusBadGateway, `provider down`), nil
	})}

	if _, err := service.Forecast(context.Background(), "Copenhagen"); err != nil {
		t.Fatalf("initial forecast failed: %v", err)
	}
	stale, err := service.Forecast(context.Background(), "Copenhagen")
	if err != nil {
		t.Fatalf("expected stale forecast, got error: %v", err)
	}
	if stale["stale"] != true {
		t.Fatalf("expected stale marker, got %v", stale["stale"])
	}
}

func TestForecastCachesPerCity(t *testing.T) {
	forecastCalls := 0
	service := NewService()
	service.geocodeEndpoint = "https://open-meteo.test/v1/search"
	service.forecastEndpoint = "https://open-meteo.test/v1/forecast"
	service.ttl = time.Hour
	service.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v1/search" {
			city := r.URL.Query().Get("name")
			return jsonResponse(http.StatusOK, `{
				"results": [{
					"name": "`+city+`",
					"country": "Denmark",
					"latitude": 55,
					"longitude": 12,
					"timezone": "Europe/Copenhagen"
				}]
			}`), nil
		}
		forecastCalls++
		return jsonResponse(http.StatusOK, `{
			"current": {"temperature_2m": 7, "weather_code": 1},
			"daily": {
				"time": ["2026-05-18"],
				"weather_code": [1],
				"temperature_2m_max": [8],
				"temperature_2m_min": [3]
			}
		}`), nil
	})}

	if _, err := service.Forecast(context.Background(), "Odense"); err != nil {
		t.Fatalf("odense forecast failed: %v", err)
	}
	if _, err := service.Forecast(context.Background(), "Odense"); err != nil {
		t.Fatalf("cached odense forecast failed: %v", err)
	}
	if _, err := service.Forecast(context.Background(), "Aalborg"); err != nil {
		t.Fatalf("aalborg forecast failed: %v", err)
	}

	if forecastCalls != 2 {
		t.Fatalf("expected one forecast call per city, got %d", forecastCalls)
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}
