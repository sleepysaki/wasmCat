package master

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseACRModuleReference(t *testing.T) {
	tests := []struct {
		name           string
		moduleURL      string
		wantRegistry   string
		wantRepository string
		wantReference  string
		wantKind       string
	}{
		{
			name:           "manifest tag URL",
			moduleURL:      "https://reg.azurecr.io/v2/team/echo/manifests/latest",
			wantRegistry:   "reg",
			wantRepository: "team/echo",
			wantReference:  "latest",
			wantKind:       acrReferenceKindManifest,
		},
		{
			name:           "manifest digest URL",
			moduleURL:      "https://reg.azurecr.io/v2/team/echo/manifests/sha256:abc",
			wantRegistry:   "reg",
			wantRepository: "team/echo",
			wantReference:  "sha256:abc",
			wantKind:       acrReferenceKindManifest,
		},
		{
			name:           "blob URL",
			moduleURL:      "https://reg.azurecr.io/v2/team/echo/blobs/sha256:abc",
			wantRegistry:   "reg",
			wantRepository: "team/echo",
			wantReference:  "sha256:abc",
			wantKind:       acrReferenceKindBlob,
		},
		{
			name:           "repository style URL",
			moduleURL:      "https://reg.azurecr.io/team/echo",
			wantRegistry:   "reg",
			wantRepository: "team/echo",
			wantReference:  "",
			wantKind:       acrReferenceKindRepository,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseACRModuleReference(tt.moduleURL)
			if err != nil {
				t.Fatalf("parseACRModuleReference returned error: %v", err)
			}
			if got.RegistryName != tt.wantRegistry {
				t.Fatalf("expected registry %q, got %q", tt.wantRegistry, got.RegistryName)
			}
			if got.RepositoryName != tt.wantRepository {
				t.Fatalf("expected repository %q, got %q", tt.wantRepository, got.RepositoryName)
			}
			if got.Reference != tt.wantReference {
				t.Fatalf("expected reference %q, got %q", tt.wantReference, got.Reference)
			}
			if got.Kind != tt.wantKind {
				t.Fatalf("expected kind %q, got %q", tt.wantKind, got.Kind)
			}
		})
	}
}

func TestFetchACRManifestUsesBearerTokenAndAcceptHeader(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		if !strings.Contains(r.Header.Get("Accept"), "application/vnd.oci.image.manifest.v1+json") {
			http.Error(w, "missing oci manifest accept header", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		w.Write([]byte(`{
			"schemaVersion": 2,
			"mediaType": "application/vnd.oci.image.manifest.v1+json",
			"layers": [
				{
					"mediaType": "application/wasm",
					"digest": "sha256:abc",
					"size": 123
				}
			]
		}`))
	}))
	defer server.Close()

	manifest, err := fetchACRManifest(ctx, server.URL, "test-token")
	if err != nil {
		t.Fatalf("fetchACRManifest returned error: %v", err)
	}
	if len(manifest.Layers) != 1 {
		t.Fatalf("expected one layer, got %d", len(manifest.Layers))
	}
	if manifest.Layers[0].Digest != "sha256:abc" {
		t.Fatalf("expected digest sha256:abc, got %q", manifest.Layers[0].Digest)
	}
}

func TestSelectWASMLayer(t *testing.T) {
	tests := []struct {
		name       string
		manifest   ociManifest
		wantDigest string
		wantErr    bool
	}{
		{
			name: "single layer is selected",
			manifest: ociManifest{Layers: []ociLayer{
				{MediaType: "application/octet-stream", Digest: "sha256:single"},
			}},
			wantDigest: "sha256:single",
		},
		{
			name: "known wasm layer is selected from many",
			manifest: ociManifest{Layers: []ociLayer{
				{MediaType: "application/octet-stream", Digest: "sha256:other"},
				{MediaType: "application/wasm", Digest: "sha256:wasm"},
			}},
			wantDigest: "sha256:wasm",
		},
		{
			name:     "no layers is rejected",
			manifest: ociManifest{},
			wantErr:  true,
		},
		{
			name: "multiple unknown layers are rejected",
			manifest: ociManifest{Layers: []ociLayer{
				{MediaType: "application/octet-stream", Digest: "sha256:one"},
				{MediaType: "application/octet-stream", Digest: "sha256:two"},
			}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layer, err := selectWASMLayer(tt.manifest)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("selectWASMLayer returned error: %v", err)
			}
			if layer.Digest != tt.wantDigest {
				t.Fatalf("expected digest %q, got %q", tt.wantDigest, layer.Digest)
			}
		})
	}
}

func TestBuildACRBlobURL(t *testing.T) {
	got, err := buildACRBlobURL(
		"https://reg.azurecr.io/v2/team/echo/manifests/latest?ignored=true",
		"team/echo",
		"sha256:abc",
	)
	if err != nil {
		t.Fatalf("buildACRBlobURL returned error: %v", err)
	}

	want := "https://reg.azurecr.io/v2/team/echo/blobs/sha256:abc"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestResolveACRModuleURLResolvesManifestToBlob(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"schemaVersion": 2,
			"layers": [
				{
					"mediaType": "application/wasm",
					"digest": "sha256:resolved",
					"size": 99
				}
			]
		}`))
	}))
	defer server.Close()

	ref := acrReference{
		RegistryName:   "reg",
		RepositoryName: "team/echo",
		Reference:      "latest",
		Kind:           acrReferenceKindManifest,
	}

	// Use the test server for the manifest fetch, then still build the final
	// worker URL from the ACR-looking manifest URL.
	manifest, err := fetchACRManifest(ctx, server.URL, "token")
	if err != nil {
		t.Fatalf("fetchACRManifest returned error: %v", err)
	}
	layer, err := selectWASMLayer(manifest)
	if err != nil {
		t.Fatalf("selectWASMLayer returned error: %v", err)
	}
	got, err := buildACRBlobURL("https://reg.azurecr.io/v2/team/echo/manifests/latest", ref.RepositoryName, layer.Digest)
	if err != nil {
		t.Fatalf("buildACRBlobURL returned error: %v", err)
	}

	want := "https://reg.azurecr.io/v2/team/echo/blobs/sha256:resolved"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
