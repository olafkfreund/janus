package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/calitti/mcp-api-gateway/pkg/auth"
	"github.com/calitti/mcp-api-gateway/pkg/gateway"
	"github.com/calitti/mcp-api-gateway/pkg/storage"
	"github.com/calitti/mcp-api-gateway/pkg/telemetry"
	"github.com/calitti/mcp-api-gateway/pkg/vault"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MCP JSON-RPC structs
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
	ID      interface{}     `json:"id"`
}

type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type ListToolsResponse struct {
	Tools []Tool `json:"tools"`
}

type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type CallToolRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

type CallToolResponse struct {
	Content []Content `json:"content"`
}

type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Session represents an SSE client session
type Session struct {
	ID        string
	SSEWriter http.ResponseWriter
	Flusher   http.Flusher
	Ctx       context.Context
}

type MCPServer struct {
	db          *storage.DB
	client      *gateway.GatewayClient
	vault       vault.VaultProvider
	authManager *auth.AuthManager
	sessions    map[string]*Session
	mu          sync.RWMutex
}

func NewMCPServer(db *storage.DB, client *gateway.GatewayClient, vp vault.VaultProvider, am *auth.AuthManager) *MCPServer {
	return &MCPServer{
		db:          db,
		client:      client,
		vault:       vp,
		authManager: am,
		sessions:    make(map[string]*Session),
	}
}

// StartStdioMode runs the server over Stdio (useful for Claude Desktop integration)
func (s *MCPServer) StartStdioMode(ctx context.Context) {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			var req JSONRPCRequest
			if err := dec.Decode(&req); err != nil {
				if err == io.EOF {
					return
				}
				log.Printf("Stdio decode error: %v", err)
				return
			}

			resp := s.handleRequest(ctx, "stdio", &req)
			if err := enc.Encode(resp); err != nil {
				log.Printf("Stdio encode error: %v", err)
				return
			}
		}
	}
}

// ServeSSE handles the SSE endpoint connection
func (s *MCPServer) ServeSSE(w http.ResponseWriter, r *http.Request) {
	// Authenticate the gateway client
	token := r.URL.Query().Get("token")
	if token == "" {
		// Fallback to Authorization Header
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			token = authHeader[7:]
		}
	}

	if !s.authManager.VerifyGatewayToken(token) {
		http.Error(w, "Unauthorized: Invalid gateway token", http.StatusUnauthorized)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	sessionID := uuid.New().String()
	session := &Session{
		ID:        sessionID,
		SSEWriter: w,
		Flusher:   flusher,
		Ctx:       r.Context(),
	}

	s.mu.Lock()
	s.sessions[sessionID] = session
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.sessions, sessionID)
		s.mu.Unlock()
	}()

	// Send endpoint configuration redirect event to client
	// The client will use POST /messages?sessionId=sessionID to send RPC calls
	fmt.Fprintf(w, "event: endpoint\ndata: /messages?sessionId=%s\n\n", sessionID)
	flusher.Flush()

	// Keep connection alive
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-session.Ctx.Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// ServeMessages handles incoming JSON-RPC calls over HTTP POST for SSE clients
func (s *MCPServer) ServeMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	s.mu.RLock()
	_, active := s.sessions[sessionID]
	s.mu.RUnlock()

	if !active {
		http.Error(w, "Session not found or expired", http.StatusBadRequest)
		return
	}

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON-RPC request", http.StatusBadRequest)
		return
	}

	resp := s.handleRequest(r.Context(), fmt.Sprintf("sse-%s", sessionID), &req)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *MCPServer) handleRequest(ctx context.Context, clientIdentity string, req *JSONRPCRequest) *JSONRPCResponse {
	resp := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "initialize":
		// Return capabilities
		resp.Result = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "mcp-api-gateway",
				"version": "1.0.0",
			},
		}

	case "tools/list":
		tools, err := s.listTools(ctx)
		if err != nil {
			resp.Error = &JSONRPCError{Code: -32603, Message: err.Error()}
		} else {
			resp.Result = ListToolsResponse{Tools: tools}
		}

	case "tools/call":
		var callReq CallToolRequest
		if err := json.Unmarshal(req.Params, &callReq); err != nil {
			resp.Error = &JSONRPCError{Code: -32602, Message: "Invalid tools/call params"}
			return resp
		}

		startTime := time.Now()
		result, err := s.callTool(ctx, callReq.Name, callReq.Arguments)
		duration := time.Since(startTime).Milliseconds()

		logID := uuid.New().String()
		if err != nil {
			resp.Error = &JSONRPCError{Code: -32603, Message: err.Error()}
			_ = s.db.LogAudit(ctx, logID, clientIdentity, callReq.Name, "failure", duration, err.Error())
		} else {
			resp.Result = CallToolResponse{
				Content: []Content{
					{Type: "text", Text: result},
				},
			}
			_ = s.db.LogAudit(ctx, logID, clientIdentity, callReq.Name, "success", duration, "")
		}

	default:
		resp.Error = &JSONRPCError{
			Code:    -32601,
			Message: fmt.Sprintf("Method %s not found", req.Method),
		}
	}

	return resp
}

func (s *MCPServer) listTools(ctx context.Context) ([]Tool, error) {
	endpoints, err := s.db.GetAllEndpoints(ctx)
	if err != nil {
		return nil, err
	}

	conns, err := s.db.GetConnections(ctx)
	if err != nil {
		return nil, err
	}

	connMap := make(map[string]*storage.APIConnection)
	for _, c := range conns {
		connMap[c.ID] = c
	}

	var mcpTools []Tool
	for _, ep := range endpoints {
		conn, exists := connMap[ep.ConnectionID]
		if !exists || !conn.Enabled {
			continue
		}

		var schemaMap map[string]interface{}
		if ep.ParametersSchema != "" {
			if err := json.Unmarshal([]byte(ep.ParametersSchema), &schemaMap); err != nil {
				// Fallback to empty schema on error
				schemaMap = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
			}
		} else {
			schemaMap = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}

		// Prepend connection prefix if configured (solves namespace conflicts)
		toolName := ep.ToolName
		if conn.ToolPrefix != "" {
			toolName = conn.ToolPrefix + toolName
		}

		mcpTools = append(mcpTools, Tool{
			Name:        toolName,
			Description: ep.ToolDescription,
			InputSchema: schemaMap,
		})
	}

	// Expose Administrative Management Tools natively via MCP
	mcpTools = append(mcpTools, Tool{
		Name:        "admin_add_connection",
		Description: "Administratively register a new target API Connection into the Gateway.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":            map[string]interface{}{"type": "string", "description": "Human name for connection"},
				"base_url":        map[string]interface{}{"type": "string", "description": "Base target URL, e.g. https://api.stripe.com"},
				"auth_type":       map[string]interface{}{"type": "string", "description": "none, basic, bearer, custom_headers"},
				"auth_secret_ref": map[string]interface{}{"type": "string", "description": "Secret path inside the vault proxy"},
				"tool_prefix":     map[string]interface{}{"type": "string", "description": "Optional namespacing prefix prepended to all tools"},
				"enabled":         map[string]interface{}{"type": "boolean", "description": "Active state"},
			},
			"required": []string{"name", "base_url", "auth_type"},
		},
	})
	mcpTools = append(mcpTools, Tool{
		Name:        "admin_add_endpoint",
		Description: "Administratively expose a target HTTP endpoint path as an MCP tool.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"connection_id":     map[string]interface{}{"type": "string", "description": "Target API connection ID UUID"},
				"tool_name":         map[string]interface{}{"type": "string", "description": "Exposed tool method identifier, e.g. get_billing_records"},
				"tool_description":  map[string]interface{}{"type": "string", "description": "Description explaining when the LLM should invoke this tool"},
				"path":              map[string]interface{}{"type": "string", "description": "Endpoint URI path, e.g. /v1/records/{{id}}"},
				"method":            map[string]interface{}{"type": "string", "description": "GET, POST, PUT, DELETE"},
				"parameters_schema": map[string]interface{}{"type": "string", "description": "JSON Schema string defining expected variables"},
				"template":          map[string]interface{}{"type": "string", "description": "Optional JSON post template body mapping parameters"},
			},
			"required": []string{"connection_id", "tool_name", "tool_description", "path", "method"},
		},
	})
	mcpTools = append(mcpTools, Tool{
		Name:        "admin_register_vault_secret",
		Description: "Administratively insert secure private credentials directly into the integrated Vault.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key":   map[string]interface{}{"type": "string", "description": "Secret lookup path reference"},
				"value": map[string]interface{}{"type": "string", "description": "Plain private credential or JSON header map"},
			},
			"required": []string{"key", "value"},
		},
	})

	return mcpTools, nil
}

func (s *MCPServer) callTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	startTime := time.Now()

	// Start OpenTelemetry Tracer Span
	ctx, span := telemetry.Tracer.Start(ctx, fmt.Sprintf("MCP Tool execution: %s", name))
	defer span.End()

	var result string
	var execErr error

	// Direct routing to management tools
	if strings.HasPrefix(name, "admin_") {
		result, execErr = s.handleAdminTool(ctx, name, args)
	} else {
		// Fetch endpoints and connections dynamically
		endpoints, err := s.db.GetAllEndpoints(ctx)
		if err != nil {
			execErr = fmt.Errorf("failed to load endpoints: %w", err)
		} else {
			conns, err := s.db.GetConnections(ctx)
			if err != nil {
				execErr = fmt.Errorf("failed to load connections: %w", err)
			} else {
				connMap := make(map[string]*storage.APIConnection)
				for _, c := range conns {
					connMap[c.ID] = c
				}

				var matchedEP *storage.APIEndpoint
				var matchedConn *storage.APIConnection

				for _, ep := range endpoints {
					conn, exists := connMap[ep.ConnectionID]
					if !exists || !conn.Enabled {
						continue
					}

					resolvedName := ep.ToolName
					if conn.ToolPrefix != "" {
						resolvedName = conn.ToolPrefix + resolvedName
					}

					if resolvedName == name {
						matchedEP = ep
						matchedConn = conn
						break
					}
				}

				if matchedEP == nil {
					execErr = fmt.Errorf("tool %q not found or target connection is disabled", name)
				} else {
					result, execErr = s.client.ExecuteCall(ctx, matchedConn, matchedEP, args)
				}
			}
		}
	}

	// Capture metrics attributes
	status := "success"
	if execErr != nil {
		status = "failure"
		span.RecordError(execErr)
	}
	duration := time.Since(startTime).Seconds()

	// Record OpenTelemetry metrics
	telemetry.ToolCallsCounter.Add(ctx, 1, 
		metric.WithAttributes(
			attribute.String("tool_name", name),
			attribute.String("status", status),
		),
	)
	telemetry.ToolDurationHistogram.Record(ctx, duration,
		metric.WithAttributes(
			attribute.String("tool_name", name),
		),
	)

	return result, execErr
}

func (s *MCPServer) handleAdminTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	if name == "admin_add_connection" {
		conn := &storage.APIConnection{
			Name:     fmt.Sprintf("%v", args["name"]),
			BaseURL:  fmt.Sprintf("%v", args["base_url"]),
			AuthType: fmt.Sprintf("%v", args["auth_type"]),
		}
		if ref, ok := args["auth_secret_ref"]; ok {
			conn.AuthSecretRef = fmt.Sprintf("%v", ref)
		}
		if prefix, ok := args["tool_prefix"]; ok {
			conn.ToolPrefix = fmt.Sprintf("%v", prefix)
		}
		conn.Enabled = true
		if en, ok := args["enabled"].(bool); ok {
			conn.Enabled = en
		}
		conn.ID = uuid.New().String()
		if err := s.db.SaveConnection(ctx, conn); err != nil {
			return "", fmt.Errorf("failed to register connection: %w", err)
		}
		return fmt.Sprintf("Successfully registered connection %q. ID: %s", conn.Name, conn.ID), nil
	}

	if name == "admin_add_endpoint" {
		ep := &storage.APIEndpoint{
			ConnectionID:    fmt.Sprintf("%v", args["connection_id"]),
			ToolName:        fmt.Sprintf("%v", args["tool_name"]),
			ToolDescription: fmt.Sprintf("%v", args["tool_description"]),
			Path:            fmt.Sprintf("%v", args["path"]),
			Method:          fmt.Sprintf("%v", args["method"]),
		}
		if schema, ok := args["parameters_schema"]; ok {
			ep.ParametersSchema = fmt.Sprintf("%v", schema)
		}
		if temp, ok := args["template"]; ok {
			ep.Template = fmt.Sprintf("%v", temp)
		}
		ep.ID = uuid.New().String()
		if err := s.db.SaveEndpoint(ctx, ep); err != nil {
			return "", fmt.Errorf("failed to register tool endpoint: %w", err)
		}
		return fmt.Sprintf("Successfully registered tool %q. ID: %s", ep.ToolName, ep.ID), nil
	}

	if name == "admin_register_vault_secret" {
		key := fmt.Sprintf("%v", args["key"])
		val := fmt.Sprintf("%v", args["value"])
		if err := s.vault.SetSecret(ctx, key, val); err != nil {
			return "", fmt.Errorf("failed to register vault secret: %w", err)
		}
		return fmt.Sprintf("Successfully stored secret reference %q", key), nil
	}

	return "", fmt.Errorf("unknown admin management tool %q", name)
}
