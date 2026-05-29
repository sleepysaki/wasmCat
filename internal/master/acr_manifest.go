package master

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	acrReferenceKindRepository = "repository"
	acrReferenceKindManifest   = "manifest"
	acrReferenceKindBlob       = "blob"
)

type acrReference struct {
	RegistryName   string
	RepositoryName string
	Reference      string
	Kind           string
}

type ociManifest struct {
	SchemaVersion int        `json:"schemaVersion"`
	MediaType     string     `json:"mediaType"`
	Layers        []ociLayer `json:"layers"`
}

type ociLayer struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

func isACRURL(moduleURL string) bool {
	parsedURL, err := url.Parse(moduleURL)
	if err != nil {
		return false
	}

	return strings.HasSuffix(parsedURL.Hostname(), ".azurecr.io")
}

func parseACRModuleReference(moduleRegistryURL string) (acrReference, error) {
	parsedURL, err := url.Parse(moduleRegistryURL)
	if err != nil {
		return acrReference{}, fmt.Errorf("parse module registry url: %w", err)
	}

	hostname := parsedURL.Hostname()
	if !strings.HasSuffix(hostname, ".azurecr.io") {
		return acrReference{}, fmt.Errorf("module registry url is not an azure container registry")
	}

	registryName := strings.TrimSuffix(hostname, ".azurecr.io")
	path := strings.Trim(parsedURL.Path, "/")
	if path == "" {
		return acrReference{}, fmt.Errorf("missing repository path in module registry url")
	}

	segments := strings.Split(path, "/")
	if segments[0] == "v2" {
		segments = segments[1:]
	}
	if len(segments) == 0 {
		return acrReference{}, fmt.Errorf("missing repository path in module registry url")
	}

	// ACR follows the OCI registry API. Repository names can contain slashes,
	// so the repository continues until an OCI keyword like manifests or blobs.
	repositoryEnd := len(segments)
	kind := acrReferenceKindRepository
	for i, segment := range segments {
		switch segment {
		case "manifests":
			repositoryEnd = i
			kind = acrReferenceKindManifest
		case "blobs":
			repositoryEnd = i
			kind = acrReferenceKindBlob
		case "tags", "referrers":
			repositoryEnd = i
			kind = segment
		}
		if repositoryEnd == i {
			break
		}
	}

	repositoryName := strings.Join(segments[:repositoryEnd], "/")
	if repositoryName == "" {
		return acrReference{}, fmt.Errorf("missing repository name in module registry url")
	}

	var reference string
	if kind != acrReferenceKindRepository {
		if repositoryEnd+1 >= len(segments) {
			return acrReference{}, fmt.Errorf("missing %s reference in module registry url", kind)
		}
		reference = strings.Join(segments[repositoryEnd+1:], "/")
	}

	return acrReference{
		RegistryName:   registryName,
		RepositoryName: repositoryName,
		Reference:      reference,
		Kind:           kind,
	}, nil
}

// parseACRReference keeps the older two-value parser API for callers that only
// need the registry and repository names.
func parseACRReference(moduleRegistryURL string) (string, string, error) {
	ref, err := parseACRModuleReference(moduleRegistryURL)
	if err != nil {
		return "", "", err
	}

	return ref.RegistryName, ref.RepositoryName, nil
}

func resolveACRModuleURL(ctx context.Context, moduleRegistryURL string, token string, ref acrReference) (string, error) {
	// Blob URLs already point at downloadable bytes, so the worker can fetch them directly.
	if ref.Kind != acrReferenceKindManifest {
		return moduleRegistryURL, nil
	}

	// Manifest URLs point at registry metadata.
	// The master reads that metadata and rewrites the request to the selected layer blob.
	manifest, err := fetchACRManifest(ctx, moduleRegistryURL, token)
	if err != nil {
		return "", err
	}

	layer, err := selectWASMLayer(manifest)
	if err != nil {
		return "", err
	}

	return buildACRBlobURL(moduleRegistryURL, ref.RepositoryName, layer.Digest)
}

func fetchACRManifest(ctx context.Context, manifestURL string, token string) (ociManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return ociManifest{}, fmt.Errorf("create acr manifest request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
	}, ", "))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ociManifest{}, fmt.Errorf("fetch acr manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ociManifest{}, fmt.Errorf("fetch acr manifest: unexpected status %s", resp.Status)
	}

	var manifest ociManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return ociManifest{}, fmt.Errorf("decode acr manifest: %w", err)
	}
	if len(manifest.Layers) == 0 {
		return ociManifest{}, fmt.Errorf("acr manifest has no layers")
	}

	return manifest, nil
}

func selectWASMLayer(manifest ociManifest) (ociLayer, error) {
	if len(manifest.Layers) == 0 {
		return ociLayer{}, fmt.Errorf("acr manifest has no layers")
	}
	if len(manifest.Layers) == 1 {
		return manifest.Layers[0], nil
	}

	for _, layer := range manifest.Layers {
		if isWASMLayerMediaType(layer.MediaType) {
			return layer, nil
		}
	}

	return ociLayer{}, fmt.Errorf("acr manifest has multiple layers and no known wasm layer media type")
}

func isWASMLayerMediaType(mediaType string) bool {
	switch mediaType {
	case "application/wasm",
		"application/vnd.module.wasm.content.layer.v1+wasm",
		"application/vnd.wasm.content.layer.v1+wasm":
		return true
	default:
		return false
	}
}

func buildACRBlobURL(originalURL string, repositoryName string, digest string) (string, error) {
	if repositoryName == "" {
		return "", fmt.Errorf("repository name is required")
	}
	if !strings.HasPrefix(digest, "sha256:") {
		return "", fmt.Errorf("unsupported layer digest %q", digest)
	}

	parsedURL, err := url.Parse(originalURL)
	if err != nil {
		return "", fmt.Errorf("parse acr blob base url: %w", err)
	}
	if !strings.HasSuffix(parsedURL.Hostname(), ".azurecr.io") {
		return "", fmt.Errorf("module registry url is not an azure container registry")
	}

	parsedURL.Path = fmt.Sprintf("/v2/%s/blobs/%s", repositoryName, digest)
	parsedURL.RawQuery = ""
	parsedURL.Fragment = ""

	return parsedURL.String(), nil
}
