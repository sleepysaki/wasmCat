package geolocation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const DefaultProviderURL = "https://ipapi.co/json/"

// DefaultProviderURLs is the comma-separated default used by worker bootstrap.
// Listing more than one provider lets the worker fall back when the first is
// rate-limited (the 429 failure seen on Azure VMs).
const DefaultProviderURLs = "https://ipapi.co/json/,https://ipinfo.io/json"

const defaultDetectionTimeout = 2 * time.Second

type Coordinates struct {
	Latitude  float64
	Longitude float64
	Source    string
}

type providerResponse struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Lat       *float64 `json:"lat"`
	Lon       *float64 `json:"lon"`
	Loc       string   `json:"loc"`
}

// Detect asks an HTTP geolocation provider for the machine's public-IP
// coordinates. Providers are intentionally generic: wasmCat only requires a
// JSON response that contains latitude/longitude, lat/lon, or loc="lat,lon".
func Detect(ctx context.Context, providerURL string) (Coordinates, error) {
	providerURL = strings.TrimSpace(providerURL)
	if providerURL == "" {
		providerURL = DefaultProviderURL
	}

	requestCtx, cancel := context.WithTimeout(ctx, defaultDetectionTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, providerURL, nil)
	if err != nil {
		return Coordinates{}, fmt.Errorf("create provider request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Coordinates{}, fmt.Errorf("query provider: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return Coordinates{}, fmt.Errorf("provider returned %s", resp.Status)
	}

	var provider providerResponse
	if err := json.NewDecoder(resp.Body).Decode(&provider); err != nil {
		return Coordinates{}, fmt.Errorf("decode provider response: %w", err)
	}

	lat, lon, err := provider.coordinates()
	if err != nil {
		return Coordinates{}, err
	}
	if err := Validate(lat, lon, "detected"); err != nil {
		return Coordinates{}, err
	}

	return Coordinates{Latitude: lat, Longitude: lon, Source: "auto"}, nil
}

// DetectWithFallback tries each provider URL in order and returns the first
// successful result. A rate-limited or unreachable provider (for example a
// public geolocation service returning 429) no longer blocks worker startup
// when alternatives are configured. The error aggregates every provider failure
// so the operator can see exactly what was tried.
func DetectWithFallback(ctx context.Context, providerURLs []string) (Coordinates, error) {
	urls := make([]string, 0, len(providerURLs))
	for _, providerURL := range providerURLs {
		if trimmed := strings.TrimSpace(providerURL); trimmed != "" {
			urls = append(urls, trimmed)
		}
	}
	if len(urls) == 0 {
		urls = []string{DefaultProviderURL}
	}

	failures := make([]string, 0, len(urls))
	for _, providerURL := range urls {
		coords, err := Detect(ctx, providerURL)
		if err == nil {
			return coords, nil
		}
		failures = append(failures, fmt.Sprintf("%s (%v)", providerURL, err))
	}

	return Coordinates{}, fmt.Errorf("all location providers failed: %s", strings.Join(failures, "; "))
}

func (r providerResponse) coordinates() (float64, float64, error) {
	if r.Latitude != nil && r.Longitude != nil {
		return *r.Latitude, *r.Longitude, nil
	}
	if r.Lat != nil && r.Lon != nil {
		return *r.Lat, *r.Lon, nil
	}
	if strings.TrimSpace(r.Loc) != "" {
		parts := strings.Split(r.Loc, ",")
		if len(parts) != 2 {
			return 0, 0, fmt.Errorf("provider loc field must use \"lat,lon\" format")
		}
		lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse provider latitude: %w", err)
		}
		lon, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse provider longitude: %w", err)
		}
		return lat, lon, nil
	}

	return 0, 0, fmt.Errorf("provider response must include latitude/longitude, lat/lon, or loc")
}

func Validate(lat float64, lon float64, prefix string) error {
	if lat < -90 || lat > 90 {
		return fmt.Errorf("%s latitude must be between -90 and 90", prefix)
	}
	if lon < -180 || lon > 180 {
		return fmt.Errorf("%s longitude must be between -180 and 180", prefix)
	}
	return nil
}
