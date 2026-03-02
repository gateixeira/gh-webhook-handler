package github

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AppClient handles GitHub App authentication and API operations.
// This is used for:
// 1. Reading config from a private GitHub repository
// 2. Optionally managing webhooks on orgs/repos via the API
type AppClient struct {
	appID          int64
	installationID int64
	privateKey     *rsa.PrivateKey
	httpClient     *http.Client
	baseURL        string
}

// AppConfig holds the configuration for creating an AppClient.
type AppConfig struct {
	AppID          int64
	InstallationID int64
	PrivateKeyPath string // path to .pem file
	BaseURL        string // optional, for GHES
}

// Webhook represents a GitHub webhook.
type Webhook struct {
	ID     int64    `json:"id"`
	Name   string   `json:"name"`
	Active bool     `json:"active"`
	Events []string `json:"events"`
	Config struct {
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
	} `json:"config"`
}

// NewAppClient creates a new AppClient from the given configuration.
func NewAppClient(cfg AppConfig) (*AppClient, error) {
	if cfg.AppID == 0 {
		return nil, fmt.Errorf("github: app ID is required")
	}
	if cfg.InstallationID == 0 {
		return nil, fmt.Errorf("github: installation ID is required")
	}
	if cfg.PrivateKeyPath == "" {
		return nil, fmt.Errorf("github: private key path is required")
	}

	keyData, err := os.ReadFile(cfg.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("github: reading private key: %w", err)
	}

	key, err := jwt.ParseRSAPrivateKeyFromPEM(keyData)
	if err != nil {
		return nil, fmt.Errorf("github: parsing private key: %w", err)
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}

	return &AppClient{
		appID:          cfg.AppID,
		installationID: cfg.InstallationID,
		privateKey:     key,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		baseURL:        baseURL,
	}, nil
}

// GenerateJWT creates a JWT for GitHub App authentication.
// JWT claims: iss = appID, iat = now-60s, exp = now+10min.
// Signed with RS256 using the private key.
func (c *AppClient) GenerateJWT() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    fmt.Sprintf("%d", c.appID),
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(c.privateKey)
}

// GetInstallationToken exchanges a JWT for an installation access token.
// POST /app/installations/{id}/access_tokens
func (c *AppClient) GetInstallationToken(ctx context.Context) (string, error) {
	jwtToken, err := c.GenerateJWT()
	if err != nil {
		return "", fmt.Errorf("github: generating JWT: %w", err)
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", c.baseURL, c.installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("github: creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github: requesting installation token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("github: installation token request failed (status %d): %s", resp.StatusCode, body)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("github: decoding installation token response: %w", err)
	}

	return result.Token, nil
}

// GetFileContent fetches a file from a repository.
// GET /repos/{owner}/{repo}/contents/{path}
func (c *AppClient) GetFileContent(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	token, err := c.GetInstallationToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.baseURL, owner, repo, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: creating request: %w", err)
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	if ref != "" {
		q := req.URL.Query()
		q.Set("ref", ref)
		req.URL.RawQuery = q.Encode()
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: fetching file content: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github: file content request failed (status %d): %s", resp.StatusCode, body)
	}

	var result struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("github: decoding file content response: %w", err)
	}

	if result.Encoding != "base64" {
		return nil, fmt.Errorf("github: unexpected content encoding: %s", result.Encoding)
	}

	decoded, err := base64.StdEncoding.DecodeString(result.Content)
	if err != nil {
		return nil, fmt.Errorf("github: decoding base64 content: %w", err)
	}

	return decoded, nil
}

// ListOrgWebhooks lists webhooks for an organization.
// GET /orgs/{org}/hooks
func (c *AppClient) ListOrgWebhooks(ctx context.Context, org string) ([]Webhook, error) {
	token, err := c.GetInstallationToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/orgs/%s/hooks", c.baseURL, org)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: creating request: %w", err)
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: listing org webhooks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github: list org webhooks failed (status %d): %s", resp.StatusCode, body)
	}

	var webhooks []Webhook
	if err := json.NewDecoder(resp.Body).Decode(&webhooks); err != nil {
		return nil, fmt.Errorf("github: decoding webhooks response: %w", err)
	}

	return webhooks, nil
}
