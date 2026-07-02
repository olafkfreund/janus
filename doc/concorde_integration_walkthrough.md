# Concorde Data Platform: Step-by-Step Janus Integration Walkthrough

This guide provides a reproducible, step-by-step walk-through for integrating the Customer's **Concorde Data Platform** (a secure clearing data platform built on Snowflake) with **Janus**.

We will walk through a real-life scenario: **Exposing daily cleared trade volumes and non-cash collateral asset valuations to clearing member AI assistants.**

---

## 1. Scenario Overview
*   **Source Data**: Snowflake database tables containing raw clearing data:
    *   `CONCORDE_DB.PUBLIC.DAILY_TRADE_VOLUMES`
    *   `CONCORDE_DB.PUBLIC.NON_CASH_COLLATERAL`
*   **Requirement**: Expose this data securely via natural language (MCP tools) to LLM clients, guaranteeing that:
    1.  The LLM has no direct SQL access to Snowflake.
    2.  Clearing member separation is strictly enforced (members can only view their own ID).
    3.  All queries are authenticated and audited.

---

## 2. Step-by-Step Implementation Guide

### Step 1: Build & Deploy the REST API Wrapper (Concorde side)
Concorde platform engineers deploy a lightweight microservice inside Amazon EKS to serve as a secure gatekeeper over Snowflake. Below is a sample Go REST API implementation query wrapper:

```go
// main.go (Concorde microservice)
package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	_ "github.com/snowflakedb/gosnowflake"
)

type TradeVolume struct {
	MemberID   string  `json:"member_id"`
	TradeDate  string  `json:"trade_date"`
	TradeCount int     `json:"trade_count"`
	VolumeUSD  float64 `json:"volume_usd"`
}

func main() {
	// Establish secure connection to Snowflake
	db, err := sql.Open("snowflake", "concorde_user:pass@my_org-my_acct/CONCORDE_DB")
	if err != nil {
		log.Fatalf("Failed to connect to Snowflake: %v", err)
	}
	defer db.Close()

	// Expose endpoint
	http.HandleFunc("/v1/dpg/trade-volume", func(w http.ResponseWriter, r *http.Request) {
		// Enforce auth header verification
		if r.Header.Get("Authorization") != "Bearer secret-concorde-token" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		memberID := r.URL.Query().Get("member_id")
		date := r.URL.Query().Get("date")

		// Query Snowflake securely using parameterized placeholders
		row := db.QueryRowContext(r.Context(), 
			"SELECT member_id, trade_date, trade_count, volume_usd FROM DAILY_TRADE_VOLUMES WHERE member_id = ? AND trade_date = ?", 
			memberID, date)

		var t TradeVolume
		if err := row.Scan(&t.MemberID, &t.TradeDate, &t.TradeCount, &t.VolumeUSD); err != nil {
			http.Error(w, "Record not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(t)
	})

	log.Println("Concorde service listening on port 8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
```

---

## Step 2: Store Service Credentials in the Vault
Store the microservice token (`Bearer secret-concorde-token`) securely in the Janus Vault.

### Using the Janus Admin Portal:
1.  Navigate to the **Settings** page.
2.  Locate the **Security Vault Proxy Configuration** card.
3.  Add a new secret:
    *   **Secret Key Reference Path**: `prod/concorde/api-token`
    *   **Secret Value**: `Bearer secret-concorde-token`
4.  Click **Store Secret Securely**.

---

## Step 3: Register the Concorde API Connection
Define the base route and credential mappings in Janus to connect it to the microservice.

### Using the SRE Command Line:
```bash
./mcp-cli connection add \
  --name "Concorde Core Service" \
  --url "https://concorde-api.eks.internal/v1" \
  --auth "bearer" \
  --secret "prod/concorde/api-token" \
  --prefix "concorde_"
```

*   **Prefix (`concorde_`)**: Ensures all exposed tools are isolated under names like `concorde_get_dpg_trade_volume` to prevent conflict with other business units.

---

## Step 4: Map the Endpoint as an MCP Tool
Expose the specific query path `/dpg/trade-volume` as a parameterized tool, translating parameters using JSON Schema.

### Using the SRE Command Line:
```bash
./mcp-cli endpoint add \
  --conn-id "<concorde-connection-uuid>" \
  --name "get_dpg_trade_volume" \
  --desc "Retrieve daily cleared trade counts and USD valuations for a member on a specific date" \
  --path "/dpg/trade-volume?member_id={{args.member_id}}&date={{args.date}}" \
  --method "GET"
```

### JSON Schema mapping automatically verified:
When this tool is called, Janus binds variables matching this schema:
```json
{
  "type": "object",
  "properties": {
    "member_id": {
      "type": "string",
      "description": "Clearing member ID (e.g. MEM-LCH-001)"
    },
    "date": {
      "type": "string",
      "description": "ISO date YYYY-MM-DD"
    }
  },
  "required": ["member_id", "date"]
}
```

---

## Shortcut: Onboard the whole Concorde API from its OpenAPI spec

Steps 3 and 4 register the connection and map one endpoint by hand — ideal when you want tight control
over a single tool. When the Concorde microservice already publishes an **OpenAPI 3.x** description,
you can collapse both steps into a single import that generates the connection and *one MCP tool per
operation*:

```bash
# Preview what would be created (writes nothing)
./mcp-cli import openapi https://concorde-api.eks.internal/openapi.json \
  --dry-run --prefix concorde_

# Apply — creates the "Concorde Core Service" connection + every tool
./mcp-cli import openapi https://concorde-api.eks.internal/openapi.json \
  --prefix concorde_
```

The importer reads `info.title` and `servers[0].url` for the connection, infers `auth_type` from the
spec's security schemes, and generates a JSON Schema per operation — exactly the shape Step 4 produces by
hand. **Credentials are never imported**, so you still complete **Step 2** (store
`prod/concorde/api-token` in the vault) and point the connection's `auth_secret_ref` at it. Imported
tools also carry a SHA-256 `definitionHash` + `version`, so they are eligible for strict tool pinning
(see Step 5). Full details on the [OpenAPI → MCP Import](openapi_import.html) page.

---

## Step 5: Test and Verify Client Execution
Now, clearing member developers connect their AI assistants (e.g., Claude Desktop) to Janus.

> **Client authentication.** In this walkthrough members present a Janus **client token** scoped to
> `concorde_*`. Where clearing-member developers are already federated with the Customer identity
> provider, the gateway can instead run as an **OAuth 2.1 resource server** (`OAUTH_ENABLED=true`): the
> member's IdP-issued, **audience-bound** access token is accepted directly on `/mcp`, validated against
> this gateway (RFC 9728 / 8707), and its OAuth scopes enforced. Both paths coexist — see
> [OAuth 2.1 Resource Server](oauth_resource_server.html).

### A. Client Tool Discovery (`tools/list`)
The AI assistant checks the available capabilities and discovers the tool:
```json
{
  "name": "concorde_get_dpg_trade_volume",
  "description": "Retrieve daily cleared trade counts and USD valuations for a member on a specific date",
  "inputSchema": {
    "type": "object",
    "properties": {
      "member_id": {"type": "string", "description": "Clearing member ID (e.g. MEM-LCH-001)"},
      "date": {"type": "string", "description": "ISO date YYYY-MM-DD"}
    },
    "required": ["member_id", "date"]
  }
}
```

### B. Executing the Tool call (`tools/call`)
When a user asks: *"What was our cleared volume for MEM-LCH-001 on June 29, 2026?"*, the client executes:

```json
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "concorde_get_dpg_trade_volume",
    "arguments": {
      "member_id": "MEM-LCH-001",
      "date": "2026-06-29"
    }
  },
  "id": 1
}
```

### C. Janus Dynamic Mediation Flow:
1.  Janus intercepts the call and verifies the caller's authorization — the client token's scope (e.g. `concorde_*`), or, under `OAUTH_ENABLED`, the audience-bound OAuth access token and its scopes.
2.  **Tool-pinning check (if `TOOL_PINNING_STRICT=true`)**: Janus confirms the tool's live `definitionHash` still matches the hash it was approved under. If the tool was redefined since approval, the call is refused with JSON-RPC error `-32001` before any downstream request is made — a rug-pull defense.
3.  Janus resolves `prod/concorde/api-token` from the Secrets Vault (`Bearer secret-concorde-token`).
4.  Janus binds the arguments (redacting any PII/secrets first when `REDACTION_ENABLED=true`) and issues a REST call:
    `GET https://concorde-api.eks.internal/v1/dpg/trade-volume?member_id=MEM-LCH-001&date=2026-06-29`
    *Header: Authorization: Bearer secret-concorde-token*
5.  The Concorde service fetches parameters, executes the prepared query on Snowflake, and returns structured JSON back to Janus.
6.  Janus sanitizes response headers, applies redaction to the response body (masking e.g. any stray email or account identifier as `[REDACTED:<class>]` when enabled), logs the execution latency and per-class redaction counts to Audit logs/Prometheus, and returns the payload to the LLM client:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "content": [
      {
        "type": "text",
        "text": "{\"member_id\":\"MEM-LCH-001\",\"trade_date\":\"2026-06-29\",\"trade_count\":14502,\"volume_usd\":245008000.50}"
      }
    ]
  },
  "id": 1
}
```
7.  The AI assistant parses the JSON and presents a natural language summary to the clearing member.
