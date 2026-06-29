package ctl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"wasmcat/internal/security"
	"wasmcat/internal/shared"
)

type Client struct {
	Config Config
	HTTP   *http.Client
}

func NewClient(cfg Config) (*Client, error) {
	if err := cfg.ValidateForRequest(); err != nil {
		return nil, err
	}

	httpClient, err := security.NewMTLSHTTPClient(cfg.ClientCert, cfg.ClientKey, cfg.CACert)
	if err != nil {
		return nil, err
	}

	return &Client{
		Config: cfg,
		HTTP:   httpClient,
	}, nil
}

func NewClientWithHTTP(cfg Config, httpClient *http.Client) (*Client, error) {
	if httpClient == nil {
		return NewClient(cfg)
	}
	cfg.normalize()

	return &Client{
		Config: cfg,
		HTTP:   httpClient,
	}, nil
}

func (c *Client) Health(ctx context.Context) (shared.HealthResponse, error) {
	var response shared.HealthResponse
	if err := c.doJSON(ctx, http.MethodGet, "/wasmcat/health", nil, &response); err != nil {
		return shared.HealthResponse{}, err
	}

	return response, nil
}

func (c *Client) Ready(ctx context.Context) (shared.HealthResponse, error) {
	var response shared.HealthResponse
	if err := c.doJSON(ctx, http.MethodGet, "/wasmcat/ready", nil, &response); err != nil {
		return shared.HealthResponse{}, err
	}

	return response, nil
}

func (c *Client) Metrics(ctx context.Context) (shared.MetricsResponse, error) {
	var response shared.MetricsResponse
	if err := c.doJSON(ctx, http.MethodGet, "/wasmcat/metrics", nil, &response); err != nil {
		return shared.MetricsResponse{}, err
	}

	return response, nil
}

func (c *Client) Workers(ctx context.Context) ([]shared.WorkerNode, error) {
	var workers []shared.WorkerNode
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/workers", nil, &workers); err != nil {
		return nil, err
	}

	return workers, nil
}

func (c *Client) Execute(ctx context.Context, req shared.ExecutionRequest) (shared.ExecutionResponse, error) {
	var response shared.ExecutionResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/execute", req, &response); err != nil {
		return shared.ExecutionResponse{}, err
	}

	return response, nil
}

func (c *Client) CreateJob(ctx context.Context, req shared.ExecutionRequest) (shared.JobResponse, error) {
	var response shared.JobResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/jobs", req, &response); err != nil {
		return shared.JobResponse{}, err
	}

	return response, nil
}

func (c *Client) GetJob(ctx context.Context, requestID string) (shared.JobResponse, error) {
	requestID = strings.TrimSpace(requestID)
	if err := shared.ValidateRequestID(requestID); err != nil {
		return shared.JobResponse{}, err
	}
	if requestID == "" {
		return shared.JobResponse{}, fmt.Errorf("request_id is required")
	}

	var response shared.JobResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/jobs/"+requestID, nil, &response); err != nil {
		return shared.JobResponse{}, err
	}

	return response, nil
}

func (c *Client) Drain(ctx context.Context, workerID string) (shared.APIResponse, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return shared.APIResponse{}, fmt.Errorf("worker_id is required")
	}

	// Operators drain through the operator-facing /api/v1 surface, which is
	// authorized by the execute-client allowlist. The worker-identity-gated
	// /internal/drain endpoint is reserved for a worker draining itself.
	var response shared.APIResponse
	path := "/api/v1/workers/" + url.PathEscape(workerID) + "/drain"
	if err := c.doJSON(ctx, http.MethodPost, path, nil, &response); err != nil {
		return shared.APIResponse{}, err
	}

	return response, nil
}

func (c *Client) doJSON(ctx context.Context, method string, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.Config.MasterURL+path, reader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := shared.DoWithRetry(c.HTTP, req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return decodeAPIError(resp)
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

func decodeAPIError(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("request failed with %s", resp.Status)
	}

	var apiErr shared.ErrorResponse
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error != "" {
		if apiErr.Code != "" {
			return fmt.Errorf("%s: %s", apiErr.Code, apiErr.Error)
		}
		return fmt.Errorf("%s", apiErr.Error)
	}

	text := strings.TrimSpace(string(body))
	if text == "" {
		return fmt.Errorf("request failed with %s", resp.Status)
	}

	return fmt.Errorf("request failed with %s: %s", resp.Status, text)
}
