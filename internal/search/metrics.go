package search

import (
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"
)

var providerDurationBuckets = [...]float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20}

// All metric state is protected by Router.mu. Labels use only configured
// provider names and HTTP status codes, never request or response contents.
type searchMetrics struct {
	requests  map[int]uint64
	providers map[string]*providerMetrics
}

type providerMetrics struct {
	attempts map[int]uint64
	buckets  [len(providerDurationBuckets)]uint64
	count    uint64
	sum      float64
}

func newSearchMetrics(providers []provider) searchMetrics {
	metrics := searchMetrics{
		requests:  make(map[int]uint64),
		providers: make(map[string]*providerMetrics, len(providers)),
	}
	for _, provider := range providers {
		metrics.providers[provider.name] = &providerMetrics{attempts: make(map[int]uint64)}
	}
	return metrics
}

func (router *Router) recordRequest(status int) {
	router.mu.Lock()
	router.metrics.requests[status]++
	router.mu.Unlock()
}

func (router *Router) recordProviderAttempt(name string, status int, elapsed time.Duration) {
	router.mu.Lock()
	defer router.mu.Unlock()
	metrics := router.metrics.providers[name]
	metrics.attempts[status]++
	seconds := elapsed.Seconds()
	for index, bound := range providerDurationBuckets {
		if seconds <= bound {
			metrics.buckets[index]++
		}
	}
	metrics.count++
	metrics.sum += seconds
}

func (router *Router) serveMetrics(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(writer, router.metricsText())
}

func (router *Router) metricsText() string {
	router.mu.Lock()
	defer router.mu.Unlock()
	var output strings.Builder
	appendMetricHeader(&output, "searchtool_requests_total", "Completed /search requests, including invalid requests and capacity rejections.", "counter")
	for _, status := range slices.Sorted(maps.Keys(router.metrics.requests)) {
		_, _ = fmt.Fprintf(&output, "searchtool_requests_total{status_code=\"%d\"} %d\n", status, router.metrics.requests[status])
	}
	appendMetricHeader(&output, "searchtool_provider_attempts_total", "Provider attempts by HTTP response status; status code 0 means no HTTP response was received.", "counter")
	for _, provider := range router.providers {
		metrics := router.metrics.providers[provider.name]
		for _, status := range slices.Sorted(maps.Keys(metrics.attempts)) {
			_, _ = fmt.Fprintf(&output, "searchtool_provider_attempts_total{provider=%q,status_code=\"%d\"} %d\n", provider.name, status, metrics.attempts[status])
		}
	}
	appendMetricHeader(&output, "searchtool_provider_duration_seconds", "Provider attempt duration including response body reading and decoding.", "histogram")
	for _, provider := range router.providers {
		metrics := router.metrics.providers[provider.name]
		for index, bound := range providerDurationBuckets {
			_, _ = fmt.Fprintf(&output, "searchtool_provider_duration_seconds_bucket{provider=%q,le=\"%g\"} %d\n", provider.name, bound, metrics.buckets[index])
		}
		_, _ = fmt.Fprintf(&output, "searchtool_provider_duration_seconds_bucket{provider=%q,le=\"+Inf\"} %d\n", provider.name, metrics.count)
		_, _ = fmt.Fprintf(&output, "searchtool_provider_duration_seconds_sum{provider=%q} %g\n", provider.name, metrics.sum)
		_, _ = fmt.Fprintf(&output, "searchtool_provider_duration_seconds_count{provider=%q} %d\n", provider.name, metrics.count)
	}
	appendMetricHeader(&output, "searchtool_in_flight_requests", "Search requests currently holding a concurrency permit.", "gauge")
	_, _ = fmt.Fprintf(&output, "searchtool_in_flight_requests %d\n", len(router.permits))
	appendMetricHeader(&output, "searchtool_provider_cooldown_seconds", "Remaining rate-limit cooldown in seconds for each configured provider.", "gauge")
	now := time.Now()
	for _, provider := range router.providers {
		_, _ = fmt.Fprintf(&output, "searchtool_provider_cooldown_seconds{provider=%q} %g\n", provider.name, max(0, router.blockedUntil[provider.name].Sub(now).Seconds()))
	}
	return output.String()
}

func appendMetricHeader(output *strings.Builder, name, help, metricType string) {
	_, _ = fmt.Fprintf(output, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, metricType)
}
