---
layout: default
---

# Welcome to The Black Hole

**The Black Hole** is an enterprise-grade, high-performance **Model Context Protocol (MCP) API Gateway and Web Portal** designed specifically for secure, regulated, and air-gapped systems. 

It acts as a secure, transparent reverse proxy that dynamically translates legacy REST/HTTP endpoints into standardized MCP tools that LLM clients (such as Claude, Antigravity, or Copilot) can query in real time.

---

## Technical Concept: How It Works

```
+------------------+                   +--------------------+
|  LLM Client      |  (mTLS / Token)   |  "The Black Hole"  |
|  (Claude/AGY)    |==================>|  MCP API Gateway   |
+------------------+                   +---------+----------+
                                                 |
                       +-------------------------+-------------------------+
                       |                         |                         |
            (Dynamic Secrets)            (Audit Logging)             (Route Proxying)
                       v                         v                         v
            +--------------------+     +-------------------+     +-------------------+
            |  Enterprise Vault  |     |   Local SQLite/   |     |    Target APIs    |
            |  (AWS/Azure/GCP)   |     |     Postgres      |     |  (Internal REST)  |
            +--------------------+     +-------------------+     +-------------------+
```

1. **Client Isolation**: LLM clients never communicate with the target microservices directly, nor do they possess target API credentials.
2. **Credential Redaction**: Static authorization tokens, bearer keys, and OAuth metadata are stored in secure cloud or local key vaults. They are resolved dynamically in Go memory at query execution time.
3. **Namespace Safety**: Dynamic namespace prefixes block naming conflicts when connecting identical or highly similar APIs.
4. **Audit Trail**: Every tool execution is audited, recording caller identity, response status, duration, and error traces.

---

## Installation Guide

"The Black Hole" is packaged as a single compiled Go binary requiring no external runtime dependencies (other than its local SQLite database file).

### 1. Build and Install Locally

To download dependencies and build the server and client binaries:

```bash
# Clone the repository
git clone https://github.com/olafkfreund/the-black-hole.git
cd the-black-hole

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
| `PORT` | `8899` | Port to host the Web Portal and SSE endpoints. |
| `DATABASE_PATH` | `./mcp-gateway.db` | Local SQLite database file location. |
| `VAULT_PROVIDER` | `local` | Pluggable vault provider (`local`, `aws`, `gcp`, `azure`). |
| `VAULT_LOCAL_PATH` | `./secrets.json` | JSON vault secrets file (used when provider is `local`). |
| `JWT_SECRET` | *(Random)* | Secret key used to sign portal JWT session tokens. |
| `TLS_CERT_PATH` | `""` | Path to HTTPS server certificate. |
| `TLS_KEY_PATH` | `""` | Path to HTTPS private key. |
| `CLIENT_CA_PATH` | `""` | Path to CA root (activates **Mutual TLS (mTLS)**). |
| `OIDC_ISSUER` | `""` | OpenID Connect identity provider URL (e.g. Okta, Keycloak). |

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
