package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultLocation = "Copenhagen"
)

type Service struct {
	client           *http.Client
	forecastEndpoint string
	geocodeEndpoint  string
	ttl              time.Duration

	mu        sync.RWMutex
	refreshMu sync.Mutex
	cached    map[string]cachedForecast
}

type cachedForecast struct {
	data      map[string]any
	expiresAt time.Time
}

type geocodingResponse struct {
	Results []location `json:"results"`
}

type location struct {
	Name      string  `json:"name"`
	Country   string  `json:"country"`
	Admin1    string  `json:"admin1"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone"`
}

type openMeteoResponse struct {
	Current struct {
		Temperature float64 `json:"temperature_2m"`
		WeatherCode int     `json:"weather_code"`
	} `json:"current"`
	Daily struct {
		Time           []string  `json:"time"`
		WeatherCode    []int     `json:"weather_code"`
		TemperatureMax []float64 `json:"temperature_2m_max"`
		TemperatureMin []float64 `json:"temperature_2m_min"`
	} `json:"daily"`
}

func NewService() *Service {
	return &Service{
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		forecastEndpoint: "https://api.open-meteo.com/v1/forecast",
		geocodeEndpoint:  "https://geocoding-api.open-meteo.com/v1/search",
		ttl:              15 * time.Minute,
		cached:           map[string]cachedForecast{},
	}
}

func (s *Service) Forecast(ctx context.Context, city string) (map[string]any, error) {
	city = normalizeCity(city)
	now := time.Now()
	if data, ok := s.fresh(city, now); ok {
		return data, nil
	}

	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	now = time.Now()
	if data, ok := s.fresh(city, now); ok {
		return data, nil
	}

	data, err := s.fetch(ctx, city)
	if err != nil {
		if stale, ok := s.stale(city); ok {
			stale["stale"] = true
			return stale, nil
		}
		return nil, err
	}

	s.mu.Lock()
	s.cached[cacheKey(city)] = cachedForecast{
		data:      data,
		expiresAt: now.Add(s.ttl),
	}
	s.mu.Unlock()

	return data, nil
}

func (s *Service) fresh(city string, now time.Time) (map[string]any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cached := s.cached[cacheKey(city)]
	if cached.data == nil || now.After(cached.expiresAt) {
		return nil, false
	}
	return cloneMap(cached.data), true
}

func (s *Service) stale(city string) (map[string]any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cached := s.cached[cacheKey(city)]
	if cached.data == nil {
		return nil, false
	}
	return cloneMap(cached.data), true
}

func (s *Service) fetch(ctx context.Context, city string) (map[string]any, error) {
	location, err := s.geocode(ctx, city)
	if err != nil {
		return nil, err
	}

	reqURL, err := url.Parse(s.forecastEndpoint)
	if err != nil {
		return nil, err
	}

	q := reqURL.Query()
	q.Set("latitude", fmt.Sprintf("%f", location.Latitude))
	q.Set("longitude", fmt.Sprintf("%f", location.Longitude))
	q.Set("current", "temperature_2m,weather_code")
	q.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min")
	q.Set("timezone", location.Timezone)
	q.Set("forecast_days", "3")
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("open-meteo returned status %d", resp.StatusCode)
	}

	var payload openMeteoResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	return normalize(payload, location, s.ttl), nil
}

func (s *Service) geocode(ctx context.Context, city string) (location, error) {
	reqURL, err := url.Parse(s.geocodeEndpoint)
	if err != nil {
		return location{}, err
	}

	q := reqURL.Query()
	q.Set("name", city)
	q.Set("count", "1")
	q.Set("language", "en")
	q.Set("format", "json")
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return location{}, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return location{}, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return location{}, fmt.Errorf("open-meteo geocoding returned status %d", resp.StatusCode)
	}

	var payload geocodingResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return location{}, err
	}
	if len(payload.Results) == 0 {
		return location{}, fmt.Errorf("city %q not found", city)
	}
	if payload.Results[0].Timezone == "" {
		payload.Results[0].Timezone = "auto"
	}
	return payload.Results[0], nil
}

func normalize(payload openMeteoResponse, loc location, ttl time.Duration) map[string]any {
	forecast := make([]map[string]any, 0, len(payload.Daily.Time))
	for i, date := range payload.Daily.Time {
		if i >= len(payload.Daily.WeatherCode) || i >= len(payload.Daily.TemperatureMax) || i >= len(payload.Daily.TemperatureMin) {
			break
		}

		var day string
		switch i {
		case 0:
			day = "Today"
		case 1:
			day = "Tomorrow"
		default:
			day = "Day " + fmt.Sprint(i+1)
		}

		forecast = append(forecast, map[string]any{
			"date":        date,
			"day":         day,
			"condition":   weatherDescription(payload.Daily.WeatherCode[i]),
			"icon":        weatherIcon(payload.Daily.WeatherCode[i]),
			"temperature": int(math.Round(payload.Daily.TemperatureMax[i])),
			"min":         int(math.Round(payload.Daily.TemperatureMin[i])),
			"max":         int(math.Round(payload.Daily.TemperatureMax[i])),
			"unit":        "celsius",
		})
	}

	currentTemp := int(math.Round(payload.Current.Temperature))
	if currentTemp == 0 && len(forecast) > 0 {
		currentTemp, _ = forecast[0]["temperature"].(int)
	}

	return map[string]any{
		"location":            displayName(loc),
		"summary":             weatherDescription(payload.Current.WeatherCode),
		"icon":                weatherIcon(payload.Current.WeatherCode),
		"source":              "open-meteo",
		"temperature":         currentTemp,
		"temperatureUnit":     "celsius",
		"forecast":            forecast,
		"cachedSeconds":       int(ttl.Seconds()),
		"latitude":            loc.Latitude,
		"longitude":           loc.Longitude,
		"providerAttribution": "Weather data by Open-Meteo.com",
	}
}

func normalizeCity(city string) string {
	city = strings.TrimSpace(city)
	if city == "" {
		return defaultLocation
	}
	return city
}

func cacheKey(city string) string {
	return strings.ToLower(normalizeCity(city))
}

func displayName(loc location) string {
	parts := []string{loc.Name}
	if loc.Admin1 != "" && loc.Admin1 != loc.Name {
		parts = append(parts, loc.Admin1)
	}
	if loc.Country != "" {
		parts = append(parts, loc.Country)
	}
	return strings.Join(parts, ", ")
}

func weatherDescription(code int) string {
	switch code {
	case 0:
		return "Clear sky"
	case 1, 2:
		return "Partly cloudy"
	case 3:
		return "Overcast"
	case 45, 48:
		return "Fog"
	case 51, 53, 55, 56, 57:
		return "Drizzle"
	case 61, 63, 65, 66, 67:
		return "Rain"
	case 71, 73, 75, 77:
		return "Snow"
	case 80, 81, 82:
		return "Rain showers"
	case 85, 86:
		return "Snow showers"
	case 95, 96, 99:
		return "Thunderstorm"
	default:
		return "Forecast available"
	}
}

func weatherIcon(code int) string {
	switch code {
	case 0:
		return "clear_day"
	case 1, 2:
		return "partly_cloudy_day"
	case 3:
		return "cloud"
	case 45, 48:
		return "foggy"
	case 51, 53, 55, 56, 57:
		return "rainy_light"
	case 61, 63, 65, 66, 67, 80, 81, 82:
		return "rainy"
	case 71, 73, 75, 77, 85, 86:
		return "weather_snowy"
	case 95, 96, 99:
		return "thunderstorm"
	default:
		return "partly_cloudy_day"
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
