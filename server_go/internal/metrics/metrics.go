package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "whoknows_http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"method", "route", "status"},
	)

	httpRequestDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "whoknows_http_request_duration_seconds",
			Help: "HTTP request duration in seconds.",
		},
		[]string{"route"},
	)

	searchRequestsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "whoknows_search_requests_total",
			Help: "Total number of search requests.",
		},
	)

	searchZeroResultsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "whoknows_search_zero_results_total",
			Help: "Total number of search requests that returned zero results.",
		},
	)

	searchDurationSeconds = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name: "whoknows_search_duration_seconds",
			Help: "Search request duration in seconds.",
		},
	)

	searchResultsCount = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "whoknows_search_results_count",
			Help:    "Number of search results returned per search.",
			Buckets: []float64{0, 1, 2, 5, 10, 20, 50, 100},
		},
	)
)

func ObserveHTTPRequest(method, route string, statusCode int, started time.Time) {
	status := strconv.Itoa(statusCode)
	httpRequestsTotal.WithLabelValues(method, route, status).Inc()
	httpRequestDurationSeconds.WithLabelValues(route).Observe(time.Since(started).Seconds())
}

func ObserveSearch(duration time.Duration, resultCount int) {
	searchRequestsTotal.Inc()
	searchDurationSeconds.Observe(duration.Seconds())
	searchResultsCount.Observe(float64(resultCount))
	if resultCount == 0 {
		searchZeroResultsTotal.Inc()
	}
}
