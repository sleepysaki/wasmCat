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

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

type acrExchangeResponse struct {
	RefreshToken string `json:"refresh_token"`
}

type acrAccessTokenResponse struct {
	AccessToken string `json:"access_token"`
}

// GenerateACRToken mints a repository-scoped ACR bearer token using the
// master node's Azure identity and the ACR OAuth exchange flow.
func GenerateACRToken(ctx context.Context, registryName string, repositoryName string) (string, error) {
	if registryName == "" {
		return "", fmt.Errorf("registryName is required")
	}
	if repositoryName == "" {
		return "", fmt.Errorf("repositoryName is required")
	}

	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return "", fmt.Errorf("create default azure credential: %w", err)
	}

	const aadScope = "https://management.core.windows.net/.default"
	aadToken, err := credential.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{aadScope}})
	if err != nil {
		return "", fmt.Errorf("acquire aad token: %w", err)
	}

	tenantID, err := extractTenantID(aadToken.Token)
	if err != nil {
		return "", fmt.Errorf("extract tenant id from aad token: %w", err)
	}

	service := fmt.Sprintf("%s.azurecr.io", registryName)
	refreshToken, err := exchangeAADTokenForRefreshToken(ctx, service, tenantID, aadToken.Token)
	if err != nil {
		return "", err
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

	resp, err := http.DefaultClient.Do(req)
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

func exchangeRefreshTokenForAccessToken(ctx context.Context, service string, repositoryName string, refreshToken string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("service", service)
	form.Set("scope", fmt.Sprintf("repository:%s:pull", repositoryName))
	form.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("https://%s/oauth2/token", service), strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create acr token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchange refresh token for access token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("acr token exchange failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var tokenResp acrAccessTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode acr token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("acr token response missing access_token")
	}

	return tokenResp.AccessToken, nil
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
