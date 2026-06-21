package ctl

import (
	"context"
	"fmt"
	"wasmcat/internal/geolocation"
)

type Location struct {
	Latitude  float64
	Longitude float64
	Source    string
}

// ResolveLocation chooses the execution origin used by the master's geo scheduler.
// The order is intentional: a one-off CLI value is the strongest signal, a saved
// fallback is useful for fixed operators or lab demos, and IP lookup is the last
// resort when the operator does not want to type coordinates for every request.
func ResolveLocation(ctx context.Context, cfg Config, userLat float64, userLon float64, hasUserLat bool, hasUserLon bool) (Location, error) {
	cfg.normalize()
	if hasUserLat != hasUserLon {
		return Location{}, fmt.Errorf("both user-lat and user-lon are required when overriding execution location")
	}
	if hasUserLat {
		if err := geolocation.Validate(userLat, userLon, "user"); err != nil {
			return Location{}, err
		}
		return Location{Latitude: userLat, Longitude: userLon, Source: "explicit"}, nil
	}

	if cfg.DefaultUserLat != 0 || cfg.DefaultUserLon != 0 {
		if err := geolocation.Validate(cfg.DefaultUserLat, cfg.DefaultUserLon, "default_user"); err != nil {
			return Location{}, err
		}
		return Location{Latitude: cfg.DefaultUserLat, Longitude: cfg.DefaultUserLon, Source: "config"}, nil
	}

	if !cfg.AutoDetectLocation {
		return Location{}, fmt.Errorf("execution location is not configured; pass --user-lat and --user-lon, set default_user_lat/default_user_lon, or enable auto_detect_location")
	}

	location, err := geolocation.Detect(ctx, cfg.LocationProviderURL)
	if err != nil {
		return Location{}, fmt.Errorf("auto-detect execution location: %w", err)
	}
	return Location{Latitude: location.Latitude, Longitude: location.Longitude, Source: location.Source}, nil
}
