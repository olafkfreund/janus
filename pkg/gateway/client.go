package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/calitti/mcp-api-gateway/pkg/storage"
	"github.com/calitti/mcp-api-gateway/pkg/vault"
)

type GatewayClient struct {
	vault VaultProvider
	http  *http.Client
}

type VaultProvider interface {
	GetSecret(ctx context.Context, secretName string) (string, error)
}

func NewGatewayClient(vp vault.VaultProvider) *GatewayClient {
	return &GatewayClient{
		vault: vp,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ExecuteCall renders the templates, fetches credentials, runs the request, and formats the output.
func (gc *GatewayClient) ExecuteCall(ctx context.Context, conn *storage.APIConnection, ep *storage.APIEndpoint, params map[string]interface{}) (string, error) {
	// 1. Resolve path template
	renderedPath := ep.Path
	for k, v := range params {
		placeholder := fmt.Sprintf("{{%s}}", k)
		renderedPath = strings.ReplaceAll(renderedPath, placeholder, fmt.Sprintf("%v", v))
	}

	fullURLStr := strings.TrimSuffix(conn.BaseURL, "/") + "/" + strings.TrimPrefix(renderedPath, "/")
	reqURL, err := url.Parse(fullURLStr)
	if err != nil {
		return "", fmt.Errorf("invalid rendered URL %q: %w", fullURLStr, err)
	}

	// 2. Resolve query parameters from template if needed
	// (For GET requests, we can also automatically append unused parameters to the query string)
	if ep.Method == "GET" {
		query := reqURL.Query()
		for k, v := range params {
			// If the parameter was not used in the path template, add to query
			placeholder := fmt.Sprintf("{{%s}}", k)
			if !strings.Contains(ep.Path, placeholder) {
				query.Set(k, fmt.Sprintf("%v", v))
			}
		}
		reqURL.RawQuery = query.Encode()
	}

	// 3. Resolve Request Body if method permits and template is set
	var bodyReader io.Reader
	if ep.Method != "GET" && ep.Method != "DELETE" {
		renderedBody := ep.Template
		if renderedBody != "" {
			for k, v := range params {
				placeholder := fmt.Sprintf("{{%s}}", k)
				// If value is a string, let's replace but keep quotes.
				// For simple JSON replacement, we can serialize the value.
				serialized, err := json.Marshal(v)
				if err == nil {
					// Remove the surrounding quotes for standard placeholder if template has them,
					// or replace directly. We support both:
					// a) {"key": "{{value}}"} -> string replacement
					// b) {"key": {{value}}} -> raw json replacement
					rawVal := string(serialized)
					// If it is a string, rawVal will contain quotes, e.g. "my-val"
					// If the template has double quotes like "{{param}}", replace including quotes to prevent double quoting
					renderedBody = strings.ReplaceAll(renderedBody, fmt.Sprintf("\"{{%s}}\"", k), rawVal)
					renderedBody = strings.ReplaceAll(renderedBody, placeholder, strings.Trim(rawVal, "\""))
				}
			}
			bodyReader = bytes.NewReader([]byte(renderedBody))
		} else {
			// Default to encoding all params as a JSON body if no template is defined
			payload, err := json.Marshal(params)
			if err != nil {
				return "", fmt.Errorf("failed to encode body payload: %w", err)
			}
			bodyReader = bytes.NewReader(payload)
		}
	}

	// 4. Create HTTP request
	req, err := http.NewRequestWithContext(ctx, ep.Method, reqURL.String(), bodyReader)
	if err != nil {
		return "", fmt.Errorf("failed to create http request: %w", err)
	}

	// Set default headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "MCP-API-Gateway/1.0")

	// 5. Inject Authorization Credentials from Vault
	if conn.AuthType != "none" && conn.AuthSecretRef != "" {
		secretVal, err := gc.vault.GetSecret(ctx, conn.AuthSecretRef)
		if err != nil {
			return "", fmt.Errorf("failed to retrieve authorization secret from vault: %w", err)
		}

		switch conn.AuthType {
		case "bearer":
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", secretVal))
		case "basic":
			// Expects secret to be formatted as "username:password"
			parts := strings.SplitN(secretVal, ":", 2)
			if len(parts) == 2 {
				req.SetBasicAuth(parts[0], parts[1])
			} else {
				return "", fmt.Errorf("basic auth secret must be in 'username:password' format")
			}
		case "custom_headers":
			// Expects secret to be a JSON string representing map[string]string
			var headers map[string]string
			if err := json.Unmarshal([]byte(secretVal), &headers); err != nil {
				return "", fmt.Errorf("custom headers secret must be a valid JSON map: %w", err)
			}
			for k, v := range headers {
				req.Header.Set(k, v)
			}
		default:
			return "", fmt.Errorf("unsupported auth type: %s", conn.AuthType)
		}
	}

	// 6. Perform the Request
	resp, err := gc.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("target API returned error status %d: %s", resp.StatusCode, string(respBody))
	}

	// 7. Format final response output
	// Try to format JSON beautifully if it is JSON, otherwise return raw text.
	var prettyJSON bytes.Buffer
	if json.Unmarshal(respBody, &struct{}{}) == nil {
		if err := json.Indent(&prettyJSON, respBody, "", "  "); err == nil {
			return prettyJSON.String(), nil
		}
	}

	return string(respBody), nil
}
