package master

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"wasmcat/internal/shared"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

const (
	defaultACRTokenTTL    = 5 * time.Minute
	defaultACRRefreshSkew = 30 * time.Second
)

type acrExchangeResponse struct {
	RefreshToken string `json:"refresh_token"`
}

type acrAccessTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type acrCachedToken struct {
	token     string
	expiresAt time.Time
}

// ACRTokenMintFunc is the network-backed operation that creates a fresh
// repository-scoped ACR pull token. Tests replace it with a small fake so the
// cache behavior can be verified without contacting Azure.
type ACRTokenMintFunc func(ctx context.Context, registryName string, repositoryName string) (string, time.Duration, error)

// ACRTokenProvider owns the master-side ACR token cache.
//
// ACR access tokens are scoped to one registry repository, so the cache key is
// registryName + repositoryName. That keeps a token for team/api from being
// reused for team/worker, even when both repositories live in the same registry.
type ACRTokenProvider struct {
	mu sync.Mutex

	tokens map[string]acrCachedToken

	now func() time.Time

	mint ACRTokenMintFunc
}

var defaultACRTokenProvider = NewACRTokenProvider()

// NewACRTokenProvider creates the production token provider used by
// GenerateACRToken. It uses Azure DefaultAzureCredential and the ACR OAuth
// exchange flow when a cache miss or refresh is required.
func NewACRTokenProvider() *ACRTokenProvider {
	return NewACRTokenProviderWithOptions(generateACRTokenUncached, time.Now)
}

// NewACRTokenProviderWithOptions builds a provider with injectable minting and
// clock behavior. The production code uses the default constructor, while tests
// use this form to drive expiry and error paths deterministically.
func NewACRTokenProviderWithOptions(mint ACRTokenMintFunc, now func() time.Time) *ACRTokenProvider {
	if mint == nil {
		mint = generateACRTokenUncached
	}
	if now == nil {
		now = time.Now
	}

	return &ACRTokenProvider{
		tokens: make(map[string]acrCachedToken),
		now:    now,
		mint:   mint,
	}
}

// Token returns a valid repository-scoped ACR pull token. A cached token is used
// until it reaches the refresh window, which avoids sending nearly-expired
// credentials to workers that still need time to download the module blob.
func (p *ACRTokenProvider) Token(ctx context.Context, registryName string, repositoryName string) (string, error) {
	if registryName == "" {
		return "", fmt.Errorf("registryName is required")
	}
	if repositoryName == "" {
		return "", fmt.Errorf("repositoryName is required")
	}

	key := acrTokenCacheKey(registryName, repositoryName)

	p.mu.Lock()
	if cached, ok := p.tokens[key]; ok && p.cachedTokenUsable(cached) {
		token := cached.token
		p.mu.Unlock()
		return token, nil
	}
	p.mu.Unlock()

	token, ttl, err := p.mint(ctx, registryName, repositoryName)
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", fmt.Errorf("acr token mint returned empty token")
	}
	if ttl <= 0 {
		ttl = defaultACRTokenTTL
	}

	p.mu.Lock()
	p.tokens[key] = acrCachedToken{
		token:     token,
		expiresAt: p.now().Add(ttl),
	}
	p.mu.Unlock()

	return token, nil
}

func (p *ACRTokenProvider) cachedTokenUsable(cached acrCachedToken) bool {
	refreshAt := cached.expiresAt.Add(-defaultACRRefreshSkew)
	if !refreshAt.After(p.now()) {
		return false
	}

	return cached.token != ""
}

func acrTokenCacheKey(registryName string, repositoryName string) string {
	return registryName + "/" + repositoryName
}

// GenerateACRToken returns a repository-scoped ACR bearer token using the
// master node's Azure identity. Repeated calls for the same registry repository
// reuse a process-local cached token until it nears expiry.
func GenerateACRToken(ctx context.Context, registryName string, repositoryName string) (string, error) {
	return defaultACRTokenProvider.Token(ctx, registryName, repositoryName)
}

func generateACRTokenUncached(ctx context.Context, registryName string, repositoryName string) (string, time.Duration, error) {
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return "", 0, fmt.Errorf("create default azure credential: %w", err)
	}

	const aadScope = "https://management.core.windows.net/.default"
	aadToken, err := credential.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{aadScope}})
	if err != nil {
		return "", 0, fmt.Errorf("acquire aad token: %w", err)
	}

	tenantID, err := extractTenantID(aadToken.Token)
	if err != nil {
		return "", 0, fmt.Errorf("extract tenant id from aad token: %w", err)
	}

	service := fmt.Sprintf("%s.azurecr.io", registryName)
	refreshToken, err := exchangeAADTokenForRefreshToken(ctx, service, tenantID, aadToken.Token)
	if err != nil {
		return "", 0, err
	}

	return exchangeRefreshTokenForAccessToken(ctx, service, repositoryName, refreshToken)
}

func exchangeAADTokenForRefreshToken(ctx context.Context, service string, tenantID string, aadToken string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "access_token")
	form.Set("service", service)
	form.Set("tenant", tenantID)
	form.Set("access_token", aadToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("https://%s/oauth2/exchange", service), strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create acr exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := shared.DoWithRetry(acrHTTPClient, req)
	if err != nil {
		return "", fmt.Errorf("exchange aad token for refresh token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("acr exchange failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var exchangeResp acrExchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&exchangeResp); err != nil {
		return "", fmt.Errorf("decode acr exchange response: %w", err)
	}
	if exchangeResp.RefreshToken == "" {
		return "", fmt.Errorf("acr exchange response missing refresh_token")
	}

	return exchangeResp.RefreshToken, nil
}

func exchangeRefreshTokenForAccessToken(ctx context.Context, service string, repositoryName string, refreshToken string) (string, time.Duration, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("service", service)
	form.Set("scope", fmt.Sprintf("repository:%s:pull", repositoryName))
	form.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("https://%s/oauth2/token", service), strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("create acr token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := shared.DoWithRetry(acrHTTPClient, req)
	if err != nil {
		return "", 0, fmt.Errorf("exchange refresh token for access token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("acr token exchange failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var tokenResp acrAccessTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", 0, fmt.Errorf("decode acr token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", 0, fmt.Errorf("acr token response missing access_token")
	}

	ttl := defaultACRTokenTTL
	if tokenResp.ExpiresIn > 0 {
		ttl = time.Duration(tokenResp.ExpiresIn) * time.Second
	}

	return tokenResp.AccessToken, ttl, nil
}

func extractTenantID(jwtToken string) (string, error) {
	parts := strings.Split(jwtToken, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid jwt format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode jwt payload: %w", err)
	}

	var claims struct {
		TenantID string `json:"tid"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("unmarshal jwt payload: %w", err)
	}
	if claims.TenantID == "" {
		return "", fmt.Errorf("tenant id not present in jwt claims")
	}

	return claims.TenantID, nil
}
