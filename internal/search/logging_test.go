package search

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProviderFailureLogsDoNotExposeContents(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		cancel bool
	}{
		{name: "timeout"},
		{name: "canceled", cancel: true},
		{name: "upstream body", status: http.StatusUnauthorized},
		{name: "retry header", status: http.StatusTooManyRequests},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if test.status == 0 {
					<-request.Context().Done()
					return
				}
				writer.Header().Set("Retry-After", "private-retry-marker")
				writer.WriteHeader(test.status)
				_, _ = io.WriteString(writer, "private-query-marker private-response-marker")
			}))
			defer server.Close()
			var logs bytes.Buffer
			router, err := New(map[string]string{EnvBraveAPIKey: "test"}, slog.New(slog.NewJSONHandler(&logs, nil)))
			if err != nil {
				t.Fatal(err)
			}
			router.providers[0].endpoint = server.URL
			router.client.Timeout = 40 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancel {
				cancel()
			}
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(`{"query":"private-query-marker"}`)).WithContext(ctx)
			router.ServeHTTP(response, request)
			for _, secret := range []string{"private-query-marker", "private-response-marker", "private-retry-marker", server.URL} {
				if strings.Contains(logs.String(), secret) {
					t.Errorf("failure log exposed %q", secret)
				}
			}
			if !strings.Contains(logs.String(), `"reason":`) || !strings.Contains(logs.String(), `"provider":"brave"`) {
				t.Error("failure log omitted safe diagnostics")
			}
		})
	}
}
