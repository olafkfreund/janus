---
layout: default
---

# Welcome to Janus

**Janus** is an enterprise-grade, high-performance **Model Context Protocol (MCP) API Gateway and Web Portal** designed specifically for secure, regulated, and air-gapped systems. 

It acts as a secure, transparent reverse proxy that dynamically translates legacy REST/HTTP endpoints into standardized MCP tools that LLM clients (such as Claude, Antigravity, or Copilot) can query in real time.

> **▶ See the [Demos & Client Integration](demos.html) page** for the end-to-end Claude Code and Antigravity walkthroughs and how to run them.

### New capability guides

* **[OpenAPI → MCP Import](openapi_import.html)** — generate a connection and one tool per operation straight from an OpenAPI 3.x spec (`mcp-cli import openapi` or `POST /api/import/openapi`).
* **[OAuth 2.1 Resource Server](oauth_resource_server.html)** — accept audience-bound IdP access tokens on the MCP endpoints (RFC 9728 / 8707), alongside Janus client tokens.
* **[Governance: Tool Pinning & Redaction](governance_pinning_redaction.html)** — SHA-256 tool-definition pinning (rug-pull defense) and PII/secret redaction (DLP) over tool traffic.

---

## MCP Transports (both supported)

Janus speaks both MCP HTTP transports on the same deployment, so any compliant client connects:

* **Streamable HTTP (recommended)** — `POST /mcp` (or `POST /sse`) with a JSON-RPC body; the response returns in the body. **Stateless** — every request is authenticated by its bearer token, so any replica can serve any request. Used by **Antigravity**, and by **Claude Code** with `type: "http"`.
* **Legacy HTTP+SSE** — `GET /sse` opens the stream, JSON-RPC is `POST`ed to `/messages`; responses are pushed back over the stream. Stateful (pinned to one pod); use sticky sessions or prefer `/mcp` for scale-out.

## Security & Governance

* **Fail-closed configuration** — no usable default secrets; `JWT_SECRET` and `GATEWAY_TOKEN` must be ≥ 32 bytes or the process refuses to start. Local admin login is disabled unless `ADMIN_PASSWORD` (≥ 12) is set.
* **Role-enforced admin API (RBAC)** — every administrative endpoint requires the `admin` role, not merely a valid session.
* **SSRF egress guard** — downstream targets are validated at dial time; private/loopback/link-local ranges and DNS-rebinding are blocked (cloud-metadata endpoints unreachable).
* **Client tokens hashed at rest** (SHA-256), shown once at creation; per-IP rate limiting, request-size caps, and HTTP timeouts throughout.
* **OAuth 2.1 resource server (optional)** — with `OAUTH_ENABLED`, the MCP endpoints accept **audience-bound** IdP access tokens (RFC 9728 discovery + `WWW-Authenticate` challenge, RFC 8707 `aud` validation, fail-closed) alongside Janus client tokens. See [OAuth 2.1 Resource Server](oauth_resource_server.html).
* **Tool-definition pinning (optional)** — every tool carries a SHA-256 `definitionHash` + `version`, surfaced in `tools/list`; `TOOL_PINNING_STRICT` blocks calls to any tool that changed since it was approved (rug-pull defense).
* **PII/secret redaction — DLP (optional)** — with `REDACTION_ENABLED`, emails, cards, JWTs, cloud/API keys and IBANs are masked in tool arguments *and* downstream responses before the LLM sees them, and the masking is audit-logged. See [Tool Pinning & Redaction](governance_pinning_redaction.html).

## Credential Handling & the Encrypted Vault

Downstream APIs that need credentials never store them in the connection config — the connection holds only a **vault reference** (`auth_secret_ref`). The gateway resolves and injects the credential **server-side at call time**; the LLM never sees it, and it is never logged.

* **Vault providers** — `postgres` (AES-256-GCM encrypted in the shared database — correct for multi-replica), `local` (**AES-256-GCM encrypted file at rest**, single-node/dev). Cloud providers fail closed until implemented.
* **Encrypted-at-rest, everywhere** — the `local` vault file is sealed with AES-256-GCM using `VAULT_ENCRYPTION_KEY` (falling back to `JWT_SECRET`). A pre-existing plaintext vault is **transparently migrated** on first load and re-persisted encrypted; loading with the wrong key fails closed rather than exposing secrets.
* **`auth_type` per connection** — `none` (public), `bearer` (`Authorization: Bearer …`), `basic` (`user:pass`; e.g. UK Companies House uses `APIKEY:`), or `custom_headers` (a JSON header map, e.g. `{"X-Auth-Key":"…"}`).
* **Rotate once, everywhere** — update the secret in the vault and every tool using it picks up the new value; scoped client tokens restrict which tools a given agent may call.

## Scaling & Deployment

Stateless gateway on an in-cluster **PostgreSQL** system of record, autoscaled by a **HorizontalPodAutoscaler (2→10)** with a PodDisruptionBudget on **AWS EKS**. CI/CD is GitHub Actions with **GitHub OIDC** (no static AWS keys) building to ECR and rolling out via `kubectl`. Streamable HTTP makes horizontal scale-out safe without a shared message bus.

---

## Web Portal Walkthrough

The Janus Web Portal is a premium, secure single-page application built directly into the gateway binary, running locally in a modern dark-mode aesthetic. Here is a visual showcase of the interface:

### 1. Dashboard Overview & Real-Time Telemetry
The main dashboard displays high-level operations metrics, component health diagnostics, request volume counters, and a chronological history of client access tokens.

| Dashboard Console | System Telemetry |
| :---: | :---: |
| ![Dashboard Console](assets/portal_dashboard.png) | ![System Telemetry](assets/portal_telemetry.png) |
| *Figure 1: Central gateway monitoring dashboard console.* | *Figure 2: Real-time traffic throughput and database telemetry metrics.* |

---

### 2. Managing Target Connections & MCP Tools
Administrators can register downstream REST targets, isolate them with custom tool prefixes, assign secure credentials from the vault, and map resource routes to MCP schema parameters.

| API Connections | Create Connection |
| :---: | :---: |
| ![API Connections](assets/portal_connections.png) | ![Create Connection](assets/portal_connection_modal.png) |
| *Figure 3: Registered API connection configurations list.* | *Figure 4: Connection creation form with namespace prefix setup.* |

| MCP Tool Mappings | Configure Dynamic Tool |
| :---: | :---: |
| ![MCP Tool Mappings](assets/portal_mcp_tools.png) | ![Configure Dynamic Tool](assets/portal_mcp_tool_modal.png) |
| *Figure 5: Gateway MCP dynamic tool endpoints.* | *Figure 6: Mapping dynamic paths and request body JSON schemas.* |

---

### 3. Security Vaults & Token Scoping
Manage credential stores and issue authorization bearer tokens for LLM clients (Claude, Antigravity, etc.) restricted to specific connection scopes.

| Pluggable Vaults | Scoped Client Tokens |
| :---: | :---: |
| ![Pluggable Vaults](assets/portal_settings_vault.png) | ![Scoped Client Tokens](assets/portal_client_tokens.png) |
| *Figure 7: Vault providers and secret references settings.* | *Figure 8: Issued bearer client tokens list.* |

| Configure Vault Proxy | Issue Client Token |
| :---: | :---: |
| ![Configure Vault Proxy](assets/portal_settings_vault_proxy.png) | ![Issue Client Token](assets/portal_client_token_modal.png) |
| *Figure 9: Setting credential mapping endpoints.* | *Figure 10: Generating client tokens with restricted scope.* |

---

### 4. Interactive OpenAPI & Swagger Documentation
The gateway automatically aggregates all dynamic tool endpoint parameters into a unified OpenAPI/Swagger schema, enabling interactive developer testing directly in the portal.

| OpenAPI Schema Preview | Swagger UI Endpoints |
| :---: | :---: |
| ![OpenAPI Schema Preview](assets/portal_openapi_ref.png) | ![Swagger UI Endpoints](assets/portal_swagger_ui_top.png) |
| *Figure 11: Real-time Swagger JSON spec modal.* | *Figure 12: Interactive Swagger UI explorer.* |

![Swagger UI Details](assets/portal_swagger_ui_bottom.png)
*Figure 13: Live Swagger UI explorer for schema definitions and execution verification.*

---

### 5. Audit Logging & Built-in Guides
A complete compliance audit trail records all tool executions and changes, accompanied by built-in interactive guides for quick onboarding.

| Historical Audit Logs | Developer Guides |
| :---: | :---: |
| ![Historical Audit Logs](assets/portal_audit_logs.png) | ![Developer Guides](assets/portal_docs_integration_guide.png) |
| *Figure 14: Historical compliance audit trail.* | *Figure 15: Embedded client integration instructions.* |

---

## Technical Concept & Data Flow

Below is the architectural topology and data flow diagram of the gateway:

<div class="mermaid">
graph TD
    %% Custom Styling matching Gruvbox Dark
    classDef client fill:#3c3836,stroke:#83a598,stroke-width:2px,color:#ebdbb2;
    classDef gateway fill:#282828,stroke:#fe8019,stroke-width:3px,color:#ebdbb2;
    classDef vault fill:#3c3836,stroke:#b16286,stroke-width:2px,color:#ebdbb2;
    classDef storage fill:#3c3836,stroke:#b8bb26,stroke-width:2px,color:#ebdbb2;
    classDef target fill:#3c3836,stroke:#cc241d,stroke-width:2px,color:#ebdbb2;
    
    subgraph Client Space ["Client Space"]
        C["LLM Client (Claude/Antigravity)"]:::client
        P["Admin Web Portal"]:::client
        CLI["Admin CLI Client"]:::client
    end

    subgraph Janus ["Janus (MCP Gateway Core)"]
        direction TB
        Auth{"Auth Filter Middleware"}:::gateway
        Router{"Router/Dispatcher"}:::gateway
        Renderer["Template Engine"]:::gateway
        Audit["Audit Logger"]:::gateway
        OTel["OTel Metric Tracker"]:::gateway
    end
    
    subgraph Storage ["Storage & Auditing"]
        DB[("SQLite / Postgres configs")]:::storage
        Logs[("Audit Log Tables")]:::storage
    end

    subgraph Security ["Security Vaults"]
        Local["Local Encrypted JSON"]:::vault
        Cloud["Cloud Vault (AWS/GCP/Azure)"]:::vault
    end

    subgraph Internal ["Microservice Network"]
        API1["Internal REST API 1"]:::target
        API2["Internal REST API 2"]:::target
    end

    C -- "mTLS / SSE Token (TLS 1.3)" --> Auth
    P -- "OIDC / Session JWT" --> Auth
    CLI -- "Session JWT (REST)" --> Auth

    Auth -- "Valid?" --> Router
    Router -- "Read Configs" --> DB
    Router -- "1. Resolve Target URL" --> Renderer
    Router -- "2. Check Prefix Clashing" --> Renderer
    
    Renderer -- "3. Resolve Credentials" --> VaultResolver{"Secrets Resolver"}:::gateway
    VaultResolver -- "Local Adapter" --> Local
    VaultResolver -- "IAM / Instance Role" --> Cloud

    VaultResolver -- "4. Inject Headers & Call" --> API1
    VaultResolver -- "4. Inject Headers & Call" --> API2

    API1 --> Audit
    API2 --> Audit
    Audit --> Logs
    Audit --> OTel
</div>

### Detailed Execution Sequence

Here is the exact sequence of validation, vault credentials resolution, routing execution, and metric audits during a single MCP tool call:

<div class="mermaid">
sequenceDiagram
    autonumber
    participant Client as LLM Client
    participant GW as MCP Gateway Core
    participant DB as SQLite DB
    participant Vault as Secret Vault
    participant Target as Downstream API

    Client->>GW: POST /messages?sessionId=... (tools/call)
    Note over Client,GW: Content-Type: json, Auth: Bearer Token / mTLS Cert
    GW->>GW: Authenticate Caller & Extract Claims
    alt Authentication Failed
        GW-->>Client: HTTP 401 Unauthorized
    else Authentication Verified
        GW->>DB: Query Connection & Tool Schema (toolName)
        DB-->>GW: Return Base URL, Path, Parameters, Auth Settings
        
        GW->>GW: Substitute Path Parameters (e.g. /users/{{id}} -> /users/123)
        
        alt Authentication Required (AuthType != none)
            GW->>Vault: Fetch Credentials (AuthSecretRef)
            Vault-->>GW: Return decrypted token/credentials
            GW->>GW: Inject headers (Bearer / Basic / Custom)
        end
        
        GW->>Target: Execute HTTP Request (GET/POST/etc.)
        alt Target API Timeout / Down
            Target-->>GW: Connection Refused / Timeout
            GW->>DB: Log Failed Execution (status=failure)
            GW-->>Client: Return JSON-RPC Error -32603
        else Target API Responds
            Target-->>GW: Return HTTP Payload (JSON/Text)
            GW->>GW: Clean and Format Response (Pretty JSON / Markdown)
            GW->>DB: Log Successful Execution (status=success, duration)
            GW-->>Client: Return JSON-RPC Response Content (Text/Markdown)
        end
    end
</div>

1. **Client Isolation**: LLM clients never communicate with the target microservices directly, nor do they possess target API credentials.
2. **Credential Redaction**: Static authorization tokens, bearer keys, and OAuth metadata are stored in secure cloud or local key vaults. They are resolved dynamically in Go memory at query execution time.
3. **Namespace Safety**: Dynamic namespace prefixes block naming conflicts when connecting identical or highly similar APIs.
4. **Audit Trail**: Every tool execution is audited, recording caller identity, response status, duration, and error traces.

---

## Installation Guide

"Janus" is packaged as a single compiled Go binary requiring no external runtime dependencies (other than its local SQLite database file).

### 1. Build and Install Locally

To download dependencies and build the server and client binaries:

```bash
# Clone the repository
git clone https://github.com/olafkfreund/janus.git
cd janus

# Using Nix & Devenv (Recommended for complete environment setups)
devenv shell
just build
```

This compiles two executables in your repository root:
* `mcp-gateway` (The API Server and SSE Endpoint)
* `mcp-cli` (The Operator / Administration Command Line Utility)

### 2. Cross-Compiling the CLI Client
To build the operator CLI client (`mcp-cli`) for macOS, Linux, and Windows:

```bash
just build-cli-all
```
This deposits compiled cross-platform binaries into the `dist/` folder:
* `dist/mcp-cli-linux-amd64` (Linux 64-bit)
* `dist/mcp-cli-darwin-amd64` (macOS Intel)
* `dist/mcp-cli-darwin-arm64` (macOS Apple Silicon M1/M2/M3)
* `dist/mcp-cli-windows-amd64.exe` (Windows 64-bit executable)

---

## Configuration Settings

Configure the gateway using standard environment variables:

| Variable | Default | Purpose |
| :--- | :--- | :--- |
| `PORT` | `8080` | Port to host the Web Portal and MCP endpoints. |
| `JWT_SECRET` | *(required, ≥32B)* | Signs portal JWT session tokens. Fail-closed if unset/weak. |
| `GATEWAY_TOKEN` | *(required, ≥32B)* | Master bearer token for MCP clients (admin / `*` scope). Fail-closed if unset/weak. |
| `ADMIN_PASSWORD` | `""` | Enables local admin login (≥12 chars). Empty = local login disabled (SSO only). |
| `DATABASE_PATH` | `./mcp-gateway.db` | Local SQLite database file location. |
| `DATABASE_URL` | `""` | PostgreSQL URI (`postgres://user:pass@host:5432/db`). Overrides `DATABASE_PATH`; required for multi-replica. |
| `VAULT_PROVIDER` | `local` | Vault backend: `postgres` (encrypted, shared), `local` (file). `aws`/`gcp`/`azure` fail closed. |
| `VAULT_ENCRYPTION_KEY` | *(JWT_SECRET)* | AES key source for the `postgres` vault. |
| `VAULT_LOCAL_PATH` | `./secrets.json` | Vault file (when provider is `local`); AES-256-GCM encrypted at rest, plaintext files auto-migrated. |
| `REDACTION_ENABLED` | `false` | Mask PII/secrets (emails, cards, JWTs, cloud/API keys, IBANs) in tool args + downstream responses; audit-logged. |
| `TOOL_PINNING_STRICT` | `false` | Reject `tools/call` when a tool's live definition no longer matches its approved SHA-256 hash. |
| `SEED_DEMO_DATA` | `false` | Seed the demo connections/tools on first boot. |
| `EGRESS_ALLOWLIST` / `EGRESS_ALLOW_PRIVATE` | `""` / `false` | SSRF egress policy: allowed downstream hosts / permit private ranges. |
| `CONFIG_CACHE_TTL` / `SECRET_CACHE_TTL` / `RESPONSE_CACHE_TTL` | `5s` / `30s` / `0s` | In-process cache TTLs (config busted on write). |
| `CORS_ALLOWED_ORIGINS` | `""` | Allowed SSE/CORS origins. |
| `METRICS_TOKEN` | `""` | Bearer token to scrape `/metrics` (empty = open). |
| `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` / `OIDC_DEFAULT_ROLE` | `""` / `admin` | OpenID Connect SSO (Okta/Keycloak) + role granted to SSO users. |
| `OAUTH_ENABLED` | `false` | Enable the OAuth 2.1 resource server on the MCP endpoints (RFC 9728/8707). Off = client-token auth only. |
| `OAUTH_RESOURCE_URI` | `""` | Canonical resource identity; access-token `aud` must contain it. Required when `OAUTH_ENABLED`. |
| `OAUTH_AUTHORIZATION_SERVERS` | `""` | Comma-separated trusted authorization-server issuers. Required when `OAUTH_ENABLED`. |
| `OAUTH_SCOPES_SUPPORTED` | `""` | Comma-separated scopes advertised in the protected-resource metadata document. |
| `PUBLIC_BASE_URL` | `""` | Public URL used to build the OIDC redirect URI. |
| `TLS_CERT_PATH` / `TLS_KEY_PATH` / `CLIENT_CA_PATH` | `""` | HTTPS cert/key; CA root activates **mTLS**. |

---

## Real-Life Scenarios

### Scenario A: Securing legacy REST APIs inside a regulated bank

In this scenario, a banking SRE team needs to expose internal customer account databases to developers using Claude Desktop, without revealing target credentials.

#### 1. Setup the connection target
Register the internal accounts database via the command line client:
```bash
# Add connection target
./mcp-cli connection add \
  --name "Accounts Database" \
  --url "https://internal.bank.net/api/v1" \
  --prefix "accounts_" \
  --desc "Protected customer banking records database" \
  --auth "bearer" \
  --secret "prod/database/accounts-key"
```

#### 2. Store the credentials securely in the vault
Write the API authorization token into the configured Vault (resolving at execution time):
```bash
./mcp-cli vault set \
  --key "prod/database/accounts-key" \
  --val "sk_secure_banking_token_558839"
```

#### 3. Define the tool endpoint mapping
Expose a specific, restricted endpoint as a structured MCP tool:
```bash
./mcp-cli endpoint add \
  --conn-id "<connection-uuid>" \
  --name "get_balance" \
  --desc "Retrieve checking and savings balances for a client ID" \
  --path "/balance/{{client_id}}" \
  --method "GET" \
  --schema '{"type":"object","properties":{"client_id":{"type":"string","description":"Client account identifier"}},"required":["client_id"]}'
```

---

### Scenario B: Dynamic image and media formatting for LLM users

LLMs like Claude, Antigravity, and Copilot render standard Markdown directly in their chat UIs. Here is how we expose dynamic image generation services for users.

#### 1. Register a public image generator API
Add the public Dog CEO API connection:
```bash
./mcp-cli connection add \
  --name "Dog Ceo Pictures" \
  --url "https://dog.ceo/api" \
  --prefix "dog_" \
  --desc "Generates random breed photos and dog images" \
  --auth "none"
```

#### 2. Register the random image endpoint
```bash
./mcp-cli endpoint add \
  --conn-id "<dog-connection-uuid>" \
  --name "random_image" \
  --desc "Fetch a random dog picture URL" \
  --path "/breeds/image/random" \
  --method "GET"
```

#### 3. Query the tool in real life
When an LLM client runs the tool `dog_random_image`, it receives the JSON response:
```json
{
  "message": "https://images.dog.ceo/breeds/terrier/n02093754_3839.jpg",
  "status": "success"
}
```
The LLM client automatically processes the image URL, translating it to a standard Markdown tag:
```markdown
Here is the random dog image:
![Dog](https://images.dog.ceo/breeds/terrier/n02093754_3839.jpg)
```
The user's chat client renders the dog picture inline immediately.

---

### Scenario C: Component Health and Live Performance Telemetry

Administrators must verify the status and monitor performance loads of the gateway under usage.

#### 1. Check Server Component Diagnostics
Run the command-line diagnostic suite to verify routing integrity:
```bash
./mcp-cli verify
```
*Verification output:*
```text
Running Gateway Component Diagnostics...
=========================================
[1/5] Checking Gateway Server Connectivity... OK
[2/5] Verifying Admin Credentials Token...    OK (Token Verified)
[3/5] Querying System Database Schema...     OK (3 Connections, 6 Tools Registered)
[4/5] Testing Vault Secret Integration...    OK (1 Secret Keys Available)
[5/5] Verifying Target API Connectivity...   
  Name                  Target URL               Status   Notes
  ----                  ----------               ------   -----
  Accounts Database     https://internal.bank... OK       HTTP 401 Unauthorized
  Dog Ceo Pictures      https://dog.ceo/api      OK       HTTP 200 OK
```

#### 2. Query Live Prometheus Telemetry
Scrape system telemetry stats directly from the active exporter stream:
```bash
./mcp-cli metrics
```
*Sample metrics payload:*
```text
MCP Gateway Monitoring Telemetry Stats
======================================
Metric Identifier                   Labels / Tags                                     Value
-----------------                   -------------                                     -----
mcp_tool_execution_count_total      status="success",tool_name="dog_random_image"     18
mcp_tool_execution_latency_seconds  quantile="0.9",tool_name="accounts_get_balance"   0.142
go_memstats_alloc_bytes             -                                                 8234810
```

---

### Scenario D: Enterprise API Restriction & Scoped Client Access

In enterprise environments, different development teams or LLM agents require restricted access to specific APIs only. We configure role-based access controls and scope globs to isolate client tokens.

#### 1. Issue a Scoped Client Token via CLI
Generate and register a token restricted only to weather APIs (tools prefixing with `weather_`):
```bash
./mcp-cli token add \
  --name "Weather Team Token" \
  --token "mcp_client_weather_dev_552" \
  --role "developer" \
  --scopes "weather_*"
```

Alternatively, this can be done visually in the **Client Tokens** section of the Web Portal, featuring a secure token generator.

#### 2. Verify Scoped Access in Stdio/SSE Client
When a client connects using the token `mcp_client_weather_dev_552`, they only see tools matching the `weather_*` pattern.

Query tools over Stdio:
```bash
export MCP_GATEWAY_TOKEN=mcp_client_weather_dev_552
echo '{"jsonrpc":"2.0","method":"tools/list","id":1}' | ./mcp-gateway -stdio
```
*Response payload:*
```json
{
  "jsonrpc": "2.0",
  "result": {
    "tools": [
      {
        "name": "weather_get_forecast",
        "description": "Retrieve real-time weather and forecast data for coordinates",
        "inputSchema": {
          "properties": {
            "current_weather": { "type": "boolean" },
            "latitude": { "type": "number" },
            "longitude": { "type": "number" }
          },
          "required": ["latitude", "longitude"],
          "type": "object"
        }
      }
    ]
  },
  "id": 1
}
```
All other connections (e.g. `stripe_*`) and administrative tools (e.g. `admin_*`) are filtered out completely from the listing and rejected with a standard JSON-RPC `-32601` error code if called directly.

---

### Scenario E: High-Availability Scale-Out in Kubernetes Cluster

For production workloads, the gateway server is deployed as multiple stateless replicas inside a Kubernetes cluster behind an Ingress controller configured with session affinity.

#### 1. Kubernetes Architecture & Traffic Flow
Below is the architectural diagram of a scaled-out Kubernetes deployment:

<div class="mermaid">
graph TD
    %% Custom Styling matching Gruvbox Dark
    classDef lb fill:#3c3836,stroke:#458588,stroke-width:2px,color:#ebdbb2;
    classDef pod fill:#282828,stroke:#b8bb26,stroke-width:3px,color:#ebdbb2;
    classDef db fill:#3c3836,stroke:#d3869b,stroke-width:2px,color:#ebdbb2;
    classDef client fill:#3c3836,stroke:#fe8019,stroke-width:2px,color:#ebdbb2;
    
    C["LLM Client (Claude)"]:::client
    
    subgraph K8s ["Kubernetes Cluster Namespace"]
        Ing["NGINX Ingress Controller <br/> [Sticky Session Affinity Cookie]"]:::lb
        
        subgraph Pods ["Stateless Pod Replicas"]
            Pod1["Gateway Pod 1"]:::pod
            Pod2["Gateway Pod 2"]:::pod
            Pod3["Gateway Pod N"]:::pod
        end
        
        Service["K8s ClusterIP Service"]:::lb
    end
    
    SharedDB[("Shared PostgreSQL Cluster")]:::db
    Vault["Cloud Secrets Manager (AWS/GCP/Azure)"]:::db
    
    C -- "1. Establish SSE stream" --> Ing
    Ing -- "2. Sticks connection using cookie" --> Pod1
    
    C -- "3. POST /messages" --> Ing
    Ing -- "4. Routes back to active session" --> Pod1
    
    Pods -- "Fetch Configs & Tokens" --> SharedDB
    Pods -- "Resolve Vault Credentials" --> Vault
</div>

#### 2. Deploy Stateless Pods with PostgreSQL
Update the `mcp-gateway-secrets` Secret to include your database configuration, and apply `k8s-deployment.yaml` with a PostgreSQL connection string:
```bash
# Apply deployments
kubectl apply -f k8s-deployment.yaml
```

Set the environment variable:
```yaml
- name: DATABASE_URL
  value: "postgres://postgres:secure_db_pass@postgres-service.default.svc.cluster.local:5432/mcp_db?sslmode=disable"
```
Because the storage is delegated to a shared PostgreSQL cluster, pods run completely stateless. You can scale replicas on the fly:
```bash
# Scale gateway instances to 5 pods
kubectl scale deployment mcp-api-gateway --replicas=5
```

#### 3. Establish Ingress Session Affinity
The included `Ingress` controller resource configures sticky sessions. NGINX will automatically insert a routing cookie (`route`) on client SSE requests and forward corresponding JSON-RPC POST calls back to the same pod instance, preventing "Session not found" discrepancies during execution.

---

### Scenario F: LCH Group Concorde API & MCP Integration (DPG Trade Volume & Non-Cash Collateral)

Exposes daily cleared trade statistics and non-cash asset breakdown (market values and haircuts) as governed MCP tools.

#### 1. Out-of-the-Box Simulated Targets
The gateway automatically registers the mock services database mapping upon initial startup. It exposes two downstream endpoints under `http://127.0.0.1:<port>/api/mock`:
* `/dpg/trade-volume`: Daily trade volumes and currency breakdown.
* `/collateral/non-cash`: ISIN listings and valuations.

#### 2. Querying via REST API
LCH applications can query data directly over standard HTTP/REST:
```bash
# Query daily trade volume
curl http://localhost:8899/api/mock/dpg/trade-volume?member_id=MEM-LCH-001

# Query non-cash collateral asset breakdown
curl http://localhost:8899/api/mock/collateral/non-cash?member_id=MEM-LCH-001
```

#### 3. Invoking via MCP Facade
LLM clients (such as Claude Desktop or Concorde Portal agents) communicate over the standard Stdio or SSE stream using a client token (e.g. `lch_member_test_token_889`):
```bash
export MCP_GATEWAY_TOKEN=lch_member_test_token_889
echo '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"lch_get_dpg_trade_volume","arguments":{"member_id":"MEM-LCH-001"}},"id":1}' | ./mcp-gateway -stdio
```
The gateway parses the parameter `member_id`, forwards the query to the underlying REST service, validates outputs, and returns clean, structured data to the client.

