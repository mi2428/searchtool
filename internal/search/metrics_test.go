package search

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func scrapeMetrics(t *testing.T, router *Router) string {
	t.Helper()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/plain; version=0.0.4; charset=utf-8" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("metrics response = %d, headers = %v", response.Code, response.Header())
	}
	return response.Body.String()
}

func metricValue(t *testing.T, text, sample string) float64 {
	t.Helper()
	for line := range strings.SplitSeq(text, "\n") {
		if value, found := strings.CutPrefix(line, sample+" "); found {
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil {
				t.Fatalf("invalid metric %q: %v", line, err)
			}
			return parsed
		}
	}
	t.Fatalf("metric %q missing:\n%s", sample, text)
	return 0
}

func TestMetricsTrackSearchOutcomesAndKeepContentsPrivate(t *testing.T) {
	for _, test := range []struct {
		name       string
		upstream   int
		body       string
		wantStatus int
	}{
		{name: "success", upstream: 200, body: `{"results":[{"title":"private-title","url":"https://private.example","content":"private-snippet"}]}`, wantStatus: 200},
		{name: "authentication error", upstream: 401, wantStatus: 502},
		{name: "server error", upstream: 500, wantStatus: 503},
		{name: "invalid response", upstream: 200, body: `{"error":"private-error"}`, wantStatus: 502},
		{name: "no response", upstream: 0, wantStatus: 502},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.upstream)
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()
			if test.upstream == 0 {
				server.Close()
			}
			router, err := New(map[string]string{EnvTavilyAPIKey: "private-provider-key"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			router.providers[0].endpoint = server.URL
			request := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(`{"query":"private-query"}`))
			request.Header.Set("Authorization", "Bearer private-client-token")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("search status = %d, want %d", response.Code, test.wantStatus)
			}
			text := scrapeMetrics(t, router)
			for _, sample := range []string{
				fmt.Sprintf(`searchtool_requests_total{status_code="%d"}`, test.wantStatus),
				fmt.Sprintf(`searchtool_provider_attempts_total{provider="tavily",status_code="%d"}`, test.upstream),
				`searchtool_provider_duration_seconds_count{provider="tavily"}`,
				`searchtool_provider_duration_seconds_bucket{provider="tavily",le="+Inf"}`,
			} {
				if got := metricValue(t, text, sample); got != 1 {
					t.Errorf("%s = %g, want 1", sample, got)
				}
			}
			if got := metricValue(t, text, `searchtool_in_flight_requests`); got != 0 {
				t.Errorf("in-flight requests = %g, want 0", got)
			}
			for _, secret := range []string{"private-query", "private-provider-key", "private-client-token", "private-title", "https://private.example", "private-snippet", "private-error", server.URL} {
				if strings.Contains(text, secret) {
					t.Errorf("metrics exposed %q", secret)
				}
			}
		})
	}
}

func TestMetricsTrackFailoverAndCooldown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/brave" {
			writer.Header().Set("Retry-After", "30")
			writer.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(writer, `{"results":[]}`)
	}))
	defer server.Close()
	router, err := New(map[string]string{EnvBraveAPIKey: "test", EnvTavilyAPIKey: "test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index := range router.providers {
		router.providers[index].endpoint = server.URL + "/" + router.providers[index].name
	}
	for range 2 {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(`{"query":"test"}`)))
		if response.Code != http.StatusOK {
			t.Fatalf("search status = %d", response.Code)
		}
	}
	text := scrapeMetrics(t, router)
	for sample, want := range map[string]float64{
		`searchtool_requests_total{status_code="200"}`:                            2,
		`searchtool_provider_attempts_total{provider="brave",status_code="429"}`:  1,
		`searchtool_provider_attempts_total{provider="tavily",status_code="200"}`: 2,
		`searchtool_provider_duration_seconds_count{provider="brave"}`:            1,
		`searchtool_provider_duration_seconds_count{provider="tavily"}`:           2,
		`searchtool_provider_cooldown_seconds{provider="tavily"}`:                 0,
	} {
		if got := metricValue(t, text, sample); got != want {
			t.Errorf("%s = %g, want %g", sample, got, want)
		}
	}
	if got := metricValue(t, text, `searchtool_provider_cooldown_seconds{provider="brave"}`); got <= 0 || got > 30 {
		t.Errorf("cooldown = %g, want (0, 30]", got)
	}
	router.mu.Lock()
	router.blockedUntil["brave"] = time.Now().Add(-time.Second)
	router.mu.Unlock()
	if got := metricValue(t, scrapeMetrics(t, router), `searchtool_provider_cooldown_seconds{provider="brave"}`); got != 0 {
		t.Errorf("expired cooldown = %g, want 0", got)
	}
}

func TestMetricsHistogramUsesCumulativeBuckets(t *testing.T) {
	router := newTestRouter(t)
	router.recordProviderAttempt("brave", 200, 100*time.Millisecond)
	router.recordProviderAttempt("brave", 500, 25*time.Second)
	text := scrapeMetrics(t, router)
	if !strings.Contains(text, "# TYPE searchtool_provider_duration_seconds histogram\n") {
		t.Fatal("histogram type missing")
	}
	for _, bound := range providerDurationBuckets {
		want := float64(0)
		if bound >= 0.1 {
			want = 1
		}
		sample := fmt.Sprintf(`searchtool_provider_duration_seconds_bucket{provider="brave",le="%g"}`, bound)
		if got := metricValue(t, text, sample); got != want {
			t.Errorf("%s = %g, want %g", sample, got, want)
		}
	}
	for _, sample := range []string{`searchtool_provider_duration_seconds_bucket{provider="brave",le="+Inf"}`, `searchtool_provider_duration_seconds_count{provider="brave"}`} {
		if got := metricValue(t, text, sample); got != 2 {
			t.Errorf("%s = %g, want 2", sample, got)
		}
	}
	if got := metricValue(t, text, `searchtool_provider_duration_seconds_sum{provider="brave"}`); math.Abs(got-25.1) > 1e-9 {
		t.Errorf("duration sum = %g, want 25.1", got)
	}
	if got := metricValue(t, text, `searchtool_provider_duration_seconds_count{provider="tavily"}`); got != 0 {
		t.Errorf("unused provider count = %g, want 0", got)
	}
}

func TestMetricsCountRejectionsButNotHealthOrScrapes(t *testing.T) {
	router := newTestRouter(t)
	for _, test := range []struct {
		method string
		path   string
		body   string
		want   int
	}{
		{method: http.MethodPost, path: "/search", body: `{"query":" "}`, want: 400},
		{method: http.MethodPost, path: "/search", body: strings.Repeat("x", maximumRequestBytes+1), want: 400},
		{method: http.MethodPost, path: "/search", body: `{"query":"` + strings.Repeat("x", maximumRequestBytes) + `"}`, want: 413},
		{method: http.MethodGet, path: "/search", want: 405},
		{method: http.MethodGet, path: "/health", want: 200},
		{method: http.MethodGet, path: "/readyz", want: 200},
		{method: http.MethodPost, path: "/metrics", want: 405},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
		if response.Code != test.want {
			t.Fatalf("%s %s status = %d, want %d", test.method, test.path, response.Code, test.want)
		}
		if test.path == "/metrics" && response.Header().Get("Allow") != http.MethodGet {
			t.Error("metrics method rejection omitted Allow: GET")
		}
	}
	for range maximumConcurrent {
		router.permits <- struct{}{}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(`{"query":"test"}`)))
	text := scrapeMetrics(t, router)
	for status, want := range map[int]float64{400: 2, 405: 1, 413: 1, 503: 1} {
		if got := metricValue(t, text, fmt.Sprintf(`searchtool_requests_total{status_code="%d"}`, status)); got != want {
			t.Errorf("status %d count = %g, want %g", status, got, want)
		}
	}
	if strings.Contains(text, `searchtool_requests_total{status_code="200"}`) || strings.Contains(text, "searchtool_provider_attempts_total{") {
		t.Error("health, scrapes, or rejected searches counted as successful searches/provider attempts")
	}
	if got := metricValue(t, text, "searchtool_in_flight_requests"); got != maximumConcurrent {
		t.Errorf("in-flight requests = %g, want %d", got, maximumConcurrent)
	}
}

func TestMetricsConcurrentSearchAndScrapes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{}, maximumConcurrent)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-ctx.Done()
		_, _ = io.WriteString(writer, `{"results":[]}`)
	}))
	defer server.Close()
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	router, err := New(map[string]string{EnvTavilyAPIKey: "test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	router.providers[0].endpoint = server.URL
	statuses := make(chan int, maximumConcurrent)
	for range maximumConcurrent {
		workers.Go(func() {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(`{"query":"test"}`)))
			statuses <- response.Code
		})
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for range maximumConcurrent {
		select {
		case <-entered:
		case <-timer.C:
			t.Fatal("concurrent searches did not reach the provider")
		}
	}
	if got := metricValue(t, scrapeMetrics(t, router), "searchtool_in_flight_requests"); got != maximumConcurrent {
		t.Errorf("active searches = %g, want %d", got, maximumConcurrent)
	}
	workers.Go(func() {
		for range 16 {
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/metrics", nil))
		}
	})
	cancel()
	workers.Wait()
	for range maximumConcurrent {
		if status := <-statuses; status != http.StatusOK {
			t.Errorf("concurrent search status = %d", status)
		}
	}
	text := scrapeMetrics(t, router)
	if got := metricValue(t, text, `searchtool_requests_total{status_code="200"}`); got != maximumConcurrent {
		t.Errorf("completed searches = %g, want %d", got, maximumConcurrent)
	}
	if got := metricValue(t, text, "searchtool_in_flight_requests"); got != 0 {
		t.Errorf("released permits = %g, want 0", got)
	}
}
