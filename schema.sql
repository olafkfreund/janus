-- Schema for MCP API Gateway

-- 1. Table for API Connection details
CREATE TABLE IF NOT EXISTS api_connections (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    base_url TEXT NOT NULL,
    auth_type TEXT NOT NULL,          -- 'none', 'basic', 'bearer', 'custom_headers', 'oauth2'
    auth_secret_ref TEXT,             -- reference key to the Vault provider
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 2. Table for API Endpoints (which map directly to MCP Tools)
CREATE TABLE IF NOT EXISTS api_endpoints (
    id TEXT PRIMARY KEY,
    connection_id TEXT NOT NULL,
    tool_name TEXT NOT NULL UNIQUE,
    tool_description TEXT NOT NULL,
    path TEXT NOT NULL,
    method TEXT NOT NULL DEFAULT 'GET',
    parameters_schema TEXT,           -- JSON Schema defining expected input variables
    template TEXT,                    -- JSON template mapping input parameters to the request body/headers
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (connection_id) REFERENCES api_connections(id) ON DELETE CASCADE
);

-- 3. Table for Security and Execution Audit Logs
CREATE TABLE IF NOT EXISTS audit_logs (
    id TEXT PRIMARY KEY,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    client_identity TEXT NOT NULL,     -- identifier of the calling LLM client / token
    tool_name TEXT NOT NULL,
    status TEXT NOT NULL,             -- 'success', 'failure'
    duration_ms INTEGER NOT NULL,
    error_message TEXT
);

-- Indexing for lookup performance and audit logs
CREATE INDEX IF NOT EXISTS idx_endpoints_conn ON api_endpoints(connection_id);
CREATE INDEX IF NOT EXISTS idx_audit_tool ON audit_logs(tool_name);
CREATE INDEX IF NOT EXISTS idx_audit_time ON audit_logs(timestamp);
