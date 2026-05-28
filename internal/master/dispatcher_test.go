package master

import "testing"

func TestParseACRReference(t *testing.T) {
	tests := []struct {
		name           string
		moduleURL      string
		wantRegistry   string
		wantRepository string
	}{
		{
			name:           "simple repository blob URL",
			moduleURL:      "https://myregistry.azurecr.io/v2/echo/blobs/sha256:abc",
			wantRegistry:   "myregistry",
			wantRepository: "echo",
		},
		{
			name:           "nested repository blob URL",
			moduleURL:      "https://myregistry.azurecr.io/v2/team/platform/echo/blobs/sha256:abc",
			wantRegistry:   "myregistry",
			wantRepository: "team/platform/echo",
		},
		{
			name:           "manifest URL",
			moduleURL:      "https://myregistry.azurecr.io/v2/team/echo/manifests/latest",
			wantRegistry:   "myregistry",
			wantRepository: "team/echo",
		},
		{
			name:           "repository style URL without v2 prefix",
			moduleURL:      "https://myregistry.azurecr.io/team/echo",
			wantRegistry:   "myregistry",
			wantRepository: "team/echo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, repository, err := parseACRReference(tt.moduleURL)
			if err != nil {
				t.Fatalf("parseACRReference returned error: %v", err)
			}
			if registry != tt.wantRegistry {
				t.Fatalf("expected registry %q, got %q", tt.wantRegistry, registry)
			}
			if repository != tt.wantRepository {
				t.Fatalf("expected repository %q, got %q", tt.wantRepository, repository)
			}
		})
	}
}
