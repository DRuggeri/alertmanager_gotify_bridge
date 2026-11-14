package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Alertmanager sends notifications for different alert groups in parallel,
// and net/http serves each request on its own goroutine. Before metricsMu,
// two webhook requests at once, or a webhook request during a /metrics
// scrape, could crash the process with "fatal error: concurrent map writes"
// (or "concurrent map iteration and map write").
func TestConcurrentWebhookRequestsAndMetricsScrapes(t *testing.T) {
	const workers = 64
	gotify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"health":"green","database":"green"}`))
	}))
	defer gotify.Close()
	debug := false
	token := "test"
	endpoint := gotify.URL + "/message"
	timeout := time.Duration(5)
	svr := &bridge{debug: &debug, gotifyToken: &token, gotifyEndpoint: &endpoint, timeout: &timeout}
	scrape := &metricsHandler{svr: svr}
	before := metricsSnapshot()

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/gotify_webhook", strings.NewReader("not json"))
			rec := httptest.NewRecorder()
			svr.handleCall(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("invalid body: got status %d, want %d", rec.Code, http.StatusBadRequest)
			}
		}()
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			scrape.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "gotify_up") {
				t.Errorf("/metrics: got status %d without gotify_up", rec.Code)
			}
		}()
	}
	wg.Wait()

	after := metricsSnapshot()
	for _, key := range []string{"requests_received", "requests_invalid"} {
		if got := after[key] - before[key]; got != workers {
			t.Errorf("%s: got %d increments, want %d", key, got, workers)
		}
	}
}
