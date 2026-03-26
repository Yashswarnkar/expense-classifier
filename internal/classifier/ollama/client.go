package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// generateRequest mirrors the Ollama /api/generate request body.
type generateRequest struct {
	Model   string        `json:"model"`
	Prompt  string        `json:"prompt"`
	Stream  bool          `json:"stream"`
	Options generateOpts  `json:"options"`
}

type generateOpts struct {
	Temperature float64 `json:"temperature"`
	NumPredict  int     `json:"num_predict"`
}

// generateResponse is the non-streaming Ollama /api/generate response.
type generateResponse struct {
	Model    string `json:"model"`
	Response string `json:"response"`
	Done     bool   `json:"done"`
	Error    string `json:"error,omitempty"`
}

// Client is a lightweight HTTP wrapper around the Ollama REST API.
// It is intentionally decoupled from the Classifier so the HTTP layer
// can be tested or replaced without changing classification logic.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a Client pointing at the given Ollama base URL
// (e.g. "http://localhost:11434").
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Generate sends a prompt to Ollama and returns the raw response text.
func (c *Client) Generate(ctx context.Context, cfg ModelConfig, prompt string) (string, error) {
	reqBody := generateRequest{
		Model:  cfg.Model,
		Prompt: prompt,
		Stream: false,
		Options: generateOpts{
			Temperature: cfg.Temperature,
			NumPredict:  cfg.NumPredict,
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("ollama: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/generate", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("ollama: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("ollama: status %d: %s", resp.StatusCode, string(body))
	}

	var genResp generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return "", fmt.Errorf("ollama: decode response: %w", err)
	}
	if genResp.Error != "" {
		return "", fmt.Errorf("ollama: model error: %s", genResp.Error)
	}

	return genResp.Response, nil
}

// Ping checks that Ollama is reachable and the requested model is available.
func (c *Client) Ping(ctx context.Context, model string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ollama not reachable at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	var tagsResp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return nil // tolerate non-JSON; still reachable
	}
	for _, m := range tagsResp.Models {
		if m.Name == model || m.Name == model+":latest" {
			return nil
		}
	}
	return fmt.Errorf("model %q not found in Ollama — run: ollama pull %s", model, model)
}
