# MCP API Gateway & Portal

An enterprise-grade, high-performance API Gateway and Web Portal that translates standard REST/HTTP APIs into Model Context Protocol (MCP) tools dynamically. Specifically designed for highly secured, regulated, and air-gapped environments.

## Features
- **Dynamic MCP Tool Translation**: Declare connection endpoints and map request body/parameters to JSON Schema templates. The gateway automatically generates MCP-compliant tool definitions.
- **Enterprise Security**:
  - **OIDC/OAuth2 SSO**: Integrate Okta, Keycloak, or Azure AD for Portal authentication.
  - **Token-Authenticated MCP Clients**: Restrict Claude, Copilot, or Antigravity connections via encrypted bearer tokens or JWTs.
  - **mTLS & SSL/TLS 1.3**: Direct support for Mutual TLS client verification and encrypted network paths.
- **Pluggable Security Vaults**: Read credentials dynamically at runtime using AWS Secrets Manager, Azure Key Vault, Google Cloud Secret Manager, or a local encrypted JSON store.
- **Air-Gap Preparedness**: A self-contained Go binary with embedded single-page assets (`//go:embed`), fully local database capabilities (SQLite), and zero-compilation build flows.

---

## Technical Architecture

```mermaid
graph TD
    Client[LLM Client: Claude, Antigravity, etc.] -- MCP over SSE / TLS + Token / mTLS --> Gateway[MCP API Gateway]
    Portal[Admin Web Portal] -- HTTPS / OIDC SSO --> Gateway
    Gateway -- Read Schema --> DB[(SQLite/Postgres encrypted)]
    Gateway -- Fetch Credentials --> Vault{AWS / Azure / GCP / HashiCorp Vault}
    Gateway -- Execute API Call with Auth --> TargetAPIs[Internal / External APIs]
```

---

## Quick Start

### 1. Using Nix & Devenv
The repository comes equipped with a declarative Nix Flake and a `devenv` shell environment.

To activate the development shell:
```bash
# Allow direnv to auto-enter shell
direnv allow

# Or enter manually using devenv
devenv shell
```

Available scripts inside the devenv shell:
- `run-dev`: Starts the gateway web portal on `http://localhost:8080` (or https depending on configuration).
- `build`: Compiles the binary to `./mcp-gateway`.
- `lint`: Runs Golangci-lint checking rules.
- `test`: Executes backend unit tests.

### 2. Using Docker Compose
Run the stack using the pre-configured compose setup:
```bash
docker-compose up -d --build
```
This builds the multi-stage production container, mounts a persistent volume for the local SQLite file (`secrets.json` and `mcp-gateway.db`), and exposes the UI at `http://localhost:8080`.

---

## Operating Modes

### A. Server Mode (Portal & SSE) - Default
Exposes the web configuration dashboard (Portal) and the Server-Sent Events (SSE) stream listener. 

To run:
```bash
go run main.go
```

**SSO Configuration**: Set the following environment variables to activate OIDC:
- `OIDC_ISSUER`: Issuer URL (e.g. `https://keycloak.company.com/realms/internal`)
- `OIDC_CLIENT_ID`: OAuth client identifier
- `OIDC_CLIENT_SECRET`: OAuth client secret token

**SSL/TLS & mTLS Configuration**:
- `TLS_CERT_PATH`: Path to server certificate PEM.
- `TLS_KEY_PATH`: Path to server private key PEM.
- `CLIENT_CA_PATH`: Path to CA bundle (activates Mutual TLS).

### B. Stdio Mode (CLI Wrapper)
Used by local desktop clients (like Claude Desktop) to invoke tools through stdin/stdout.

To configure Claude Desktop to use this gateway:
Add the following connection to `claude_desktop_config.json`:
```json
{
  "mcpServers": {
    "api-gateway": {
      "command": "/path/to/mcp-gateway",
      "args": ["-stdio"],
      "env": {
        "DATABASE_PATH": "/path/to/mcp-gateway.db",
        "VAULT_PROVIDER": "local",
        "VAULT_LOCAL_PATH": "/path/to/secrets.json"
      }
    }
  }
}
```
---

## Administrative & Monitoring CLI (`mcp-cli`)

For administrators and operators, the gateway includes a standalone, cross-platform CLI tool (`mcp-cli`) compiled for macOS (Intel/Apple Silicon), Linux, and Windows. The CLI connects remotely to the Gateway REST API over HTTPS/HTTP, providing complete administration, verification, and performance monitoring capabilities.

### 1. Build Instructions
To build the CLI for your current platform:
```bash
just build-cli
```
To cross-compile for all systems (outputs saved in `dist/`):
```bash
just build-cli-all
```

### 2. Available Commands

* **Authentication**:
  ```bash
  mcp-cli login <username> --addr <gateway-url>
  ```
  Authenticates with the gateway server and caches the session token in the user configuration directory (`~/.config/mcp-gateway/cli.json`).

* **Diagnostics & Verification**:
  ```bash
  mcp-cli verify
  ```
  Runs a comprehensive health check: pings the gateway server, verifies database schemas, checks vault integration, and validates outbound network connectivity for all downstream API endpoints.

* **Performance & Telemetry Monitoring**:
  ```bash
  mcp-cli status    # Shows gateway settings, active port, vault provider, and mTLS status
  mcp-cli metrics   # Fetches and parses scrapable Prometheus metrics for live status tracking
  mcp-cli logs      # Lists the last 100 tool execution audit logs (status, duration, error messages)
  ```

* **API Connections CRUD**:
  ```bash
  mcp-cli connection list
  mcp-cli connection add --name <name> --url <url> [--prefix <prefix>] [--auth <type>] [--secret <ref>]
  mcp-cli connection modify --id <uuid> [--name <name>] [--url <url>] [--prefix <prefix>] [--enabled <true|false>]
  mcp-cli connection delete --id <uuid>
  ```

* **Tool Endpoints CRUD**:
  ```bash
  mcp-cli endpoint list
  mcp-cli endpoint add --conn-id <conn-uuid> --name <tool-name> --desc <description> --path <route> --method <HTTP-method>
  mcp-cli endpoint modify --id <endpoint-uuid> [--name <name>] [--path <route>] [--method <HTTP-method>]
  mcp-cli endpoint delete --id <endpoint-uuid>
  ```

* **Vault Secrets Management**:
  ```bash
  mcp-cli vault list
  mcp-cli vault set --key <secret-path> --val <secret-value>
  mcp-cli vault delete --key <secret-path>
  ```

---

## Integrating with Secret Vaults

Sensitive auth tokens (Basic, Bearer, Custom Headers) are retrieved at query runtime from your chosen vault.

| Provider (`VAULT_PROVIDER`) | Config Requirements | Auth Mechanism |
| :--- | :--- | :--- |
| `local` | `VAULT_LOCAL_PATH` | Local JSON dictionary |
| `aws` | AWS environment keys | IAM Instance Profile / IRSA |
| `gcp` | GCP environment keys | Workload Identity / ADC |
| `azure` | Azure credentials | Managed Identity |

### Storing a Secret
Open the Portal UI, navigate to the **Security Vault** view, and insert the secret mapping:
- **Secret Path**: `prod/billing-service/api-key`
- **Secret Value**: `Bearer xoxb-123456789-abcdef`

When setting up your **API Connection**, map `Auth Secret Ref` to `prod/billing-service/api-key`.

---

## File Structure
- `main.go`: Application lifecycle.
- `schema.sql`: Local DB table layouts.
- `pkg/config`: Configuration schemas.
- `pkg/storage`: DB connector, CRUD actions, and audit logs.
- `pkg/vault`: Interface for AWS, GCP, Azure, and Local vaults.
- `pkg/auth`: JWT issuers, SSO authentication, and TLS profiles.
- `pkg/gateway`: Dynamic parameter rendering and HTTP call execution.
- `pkg/mcp`: Model Context Protocol JSON-RPC implementation.
- `pkg/portal`: REST API and static single page serving.
- `pkg/portal/static`: Front-end HTML/CSS/JS files.
