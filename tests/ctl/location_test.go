package ctl_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"wasmcat/internal/ctl"
)

func TestResolveLocationUsesExplicitCoordinates(t *testing.T) {
	cfg := ctl.Config{AutoDetectLocation: false}

	location, err := ctl.ResolveLocation(context.Background(), cfg, 10.5, 106.7, true, true)
	if err != nil {
		t.Fatalf("resolve location: %v", err)
	}
	if location.Latitude != 10.5 || location.Longitude != 106.7 || location.Source != "explicit" {
		t.Fatalf("unexpected location: %+v", location)
	}
}

func TestResolveLocationRequiresCoordinatePair(t *testing.T) {
	cfg := ctl.Config{AutoDetectLocation: true}

	if _, err := ctl.ResolveLocation(context.Background(), cfg, 10.5, 0, true, false); err == nil {
		t.Fatalf("expected missing coordinate pair error")
	}
}

func TestResolveLocationUsesConfigBeforeProvider(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := ctl.Config{
		DefaultUserLat:      21.0278,
		DefaultUserLon:      105.8342,
		AutoDetectLocation:  true,
		LocationProviderURL: server.URL,
	}

	location, err := ctl.ResolveLocation(context.Background(), cfg, 0, 0, false, false)
	if err != nil {
		t.Fatalf("resolve location: %v", err)
	}
	if called {
		t.Fatalf("provider should not be called when config coordinates exist")
	}
	if location.Source != "config" || location.Latitude != 21.0278 || location.Longitude != 105.8342 {
		t.Fatalf("unexpected location: %+v", location)
	}
}

func TestResolveLocationAutoDetectsLatitudeLongitude(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"latitude":21.0278,"longitude":105.8342}`))
	}))
	defer server.Close()

	cfg := ctl.Config{AutoDetectLocation: true, LocationProviderURL: server.URL}
	location, err := ctl.ResolveLocation(context.Background(), cfg, 0, 0, false, false)
	if err != nil {
		t.Fatalf("resolve location: %v", err)
	}
	if location.Source != "auto" || location.Latitude != 21.0278 || location.Longitude != 105.8342 {
		t.Fatalf("unexpected location: %+v", location)
	}
}

func TestResolveLocationAutoDetectsLocString(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"loc":"10.7626,106.6602"}`))
	}))
	defer server.Close()

	cfg := ctl.Config{AutoDetectLocation: true, LocationProviderURL: server.URL}
	location, err := ctl.ResolveLocation(context.Background(), cfg, 0, 0, false, false)
	if err != nil {
		t.Fatalf("resolve location: %v", err)
	}
	if location.Source != "auto" || location.Latitude != 10.7626 || location.Longitude != 106.6602 {
		t.Fatalf("unexpected location: %+v", location)
	}
}

func TestResolveLocationErrorsWhenNoSourceExists(t *testing.T) {
	cfg := ctl.Config{AutoDetectLocation: false}

	if _, err := ctl.ResolveLocation(context.Background(), cfg, 0, 0, false, false); err == nil {
		t.Fatalf("expected missing location source error")
	}
}
