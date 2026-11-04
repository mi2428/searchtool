package search

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDelayedFallbackRechecksProviderCooldown(t *testing.T) {
	gate, release := context.WithCancel(context.Background())
	entered := make(chan struct{})
	var tavilyCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/brave":
			close(entered)
			<-gate.Done()
			writer.WriteHeader(http.StatusServiceUnavailable)
		case "/tavily":
			tavilyCalls.Add(1)
			writer.Header().Set("Retry-After", "30")
			writer.WriteHeader(http.StatusTooManyRequests)
		case "/exa":
			_, _ = io.WriteString(writer, `{"results":[]}`)
		}
	}))
	defer server.Close()
	defer release()
	router, err := New(map[string]string{EnvBraveAPIKey: "test", EnvTavilyAPIKey: "test", EnvExaAPIKey: "test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index := range router.providers {
		router.providers[index].endpoint = server.URL + "/" + router.providers[index].name
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := router.search(context.Background(), "first", 5)
		firstDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first search did not enter Brave")
	}
	if _, err := router.search(context.Background(), "second", 5); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("first search did not finish")
	}
	if got := tavilyCalls.Load(); got != 1 {
		t.Errorf("Tavily calls = %d, want 1", got)
	}
	if got := metricValue(t, scrapeMetrics(t, router), `searchtool_provider_attempts_total{provider="tavily",status_code="429"}`); got != 1 {
		t.Errorf("Tavily attempt count = %g, want 1", got)
	}
}
