package geolocation_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"wasmcat/internal/geolocation"
)

func TestDetectWithFallbackUsesSecondProviderAfterRateLimit(t *testing.T) {
	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
	}))
	defer limited.Close()

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"latitude":21.0278,"longitude":105.8342}`))
	}))
	defer ok.Close()

	coords, err := geolocation.DetectWithFallback(context.Background(), []string{limited.URL, ok.URL})
	if err != nil {
		t.Fatalf("expected fallback to succeed, got %v", err)
	}
	if coords.Latitude != 21.0278 || coords.Longitude != 105.8342 {
		t.Fatalf("unexpected coordinates: %+v", coords)
	}
	if coords.Source != "auto" {
		t.Fatalf("expected source auto, got %q", coords.Source)
	}
}

func TestDetectWithFallbackAggregatesFailures(t *testing.T) {
	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
	}))
	defer limited.Close()

	_, err := geolocation.DetectWithFallback(context.Background(), []string{limited.URL, limited.URL})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
	if !strings.Contains(err.Error(), "all location providers failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}
