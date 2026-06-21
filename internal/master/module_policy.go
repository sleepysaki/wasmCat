package master

import (
	"fmt"
	"net/url"
	"strings"
	"wasmcat/internal/shared"
)

type ModulePolicy struct {
	AllowedHosts  []string
	RequireDigest bool
}

func (p ModulePolicy) Validate(req shared.ExecutionRequest) error {
	moduleURL := strings.TrimSpace(req.ModuleURL)
	if moduleURL == "" {
		moduleURL = strings.TrimSpace(req.ModuleRegistryURL)
	}
	if moduleURL == "" {
		return fmt.Errorf("module_url or module_registry_url is required")
	}

	parsedURL, err := url.Parse(moduleURL)
	if err != nil {
		return fmt.Errorf("parse module url: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("module url scheme %q is not allowed", parsedURL.Scheme)
	}
	if parsedURL.Host == "" {
		return fmt.Errorf("module url host is required")
	}

	if len(p.AllowedHosts) > 0 && !hostAllowed(parsedURL, p.AllowedHosts) {
		return fmt.Errorf("module host %q is not allowed", parsedURL.Hostname())
	}

	if p.RequireDigest && !requestHasDigest(req, parsedURL) {
		return fmt.Errorf("module digest is required by policy")
	}

	return nil
}

func hostAllowed(parsedURL *url.URL, allowedHosts []string) bool {
	requestHost := strings.ToLower(parsedURL.Hostname())
	requestHostPort := strings.ToLower(parsedURL.Host)

	for _, allowedHost := range allowedHosts {
		allowedHost = strings.ToLower(strings.TrimSpace(allowedHost))
		if allowedHost == "" {
			continue
		}
		if allowedHost == requestHost || allowedHost == requestHostPort {
			return true
		}
	}

	return false
}

func requestHasDigest(req shared.ExecutionRequest, parsedURL *url.URL) bool {
	if strings.TrimSpace(req.ModuleDigest) != "" {
		return true
	}

	path := strings.Trim(parsedURL.Path, "/")
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if (segment == "blobs" || segment == "manifests") && i+1 < len(segments) {
			return strings.HasPrefix(segments[i+1], "sha256:")
		}
	}

	return false
}
