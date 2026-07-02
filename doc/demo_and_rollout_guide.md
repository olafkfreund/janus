# LCH Group Concorde: MCP Rollout & Developer Integration Guide

This guide explains how to connect local AI clients, script simulations for architects, execute a centralized rollout, and utilize custom skills to query clearing metrics and compile formatted compliance documents.

---

## 1. Connecting Clients

### A. Claude Desktop Integration
Claude Desktop connects to MCP servers over standard input/output (stdio). 
1. Open the Claude Desktop configuration file:
   * **Linux**: `~/.config/Claude/claude_desktop_config.json`
   * **macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
   * **Windows**: `%APPDATA%\Claude\claude_desktop_config.json`
2. Add the gateway as a server connection, passing the local token as an environment variable:
```json
{
  "mcpServers": {
    "lch-gateway": {
      "command": "/usr/local/bin/mcp-gateway",
      "args": ["-stdio"],
      "env": {
        "DATABASE_PATH": "/var/lib/mcp-gateway/mcp-gateway.db",
        "MCP_GATEWAY_TOKEN": "lch_member_test_token_889"
      }
    }
  }
}
```

### B. Antigravity CLI / SDK Integration
For python-based workflows and agent engines built on the Google Antigravity SDK:
```python
from antigravity_sdk import Agent, ToolRegistry

# Establish SSE stream with Authorization headers
registry = ToolRegistry.from_mcp_sse(
    url="http://localhost:8899/sse",
    headers={"Authorization": "Bearer lch_member_test_token_889"}
)

# Load the registered tools automatically into the agent framework
agent = Agent(
    name="LCH Collateral Analyst",
    instructions="Analyze member collateral balances.",
    tools=registry.list_tools()
)
```

---

## 2. Automated Local Demo Script

To demonstrate the API-to-MCP translation flow to architects and engineers without setting up a full network sandbox, we have created an automated simulation script:

*   **Location**: [scripts/demo_lch_mcp.sh](file:///home/olafkfreund/Source/Calitti/mcp-api-gateway/scripts/demo_lch_mcp.sh)
*   **What it does**:
    1. Compiles the Go server locally.
    2. Starts the compiled binary in `stdio` mode.
    3. Simulates a client by sending a standard JSON-RPC `tools/list` request, printing the discovered schemas (showing inputs like `member_id` and `date`).
    4. Simulates a client calling `lch_get_non_cash_collateral` with parameter `{"member_id": "MEM-LCH-002"}`, printing the JSON result from the downstream mock API.
    5. Outputs python setup examples.

### To Run the Demo
```bash
./scripts/demo_lch_mcp.sh
```

---

## 3. Centralized Developer Rollout Strategy

To onboard hundreds of LCH Group developers and clearing members securely and efficiently, use a **Hybrid Deployment Model**:

### Web Portal Integration (SSE)
*   Deploy a cluster of gateway containers to Customer's EKS.
*   Enforce **mTLS (Mutual TLS)** for all incoming connections.
*   Configure the **portal** to authenticate administrators via **OIDC (Okta/Active Directory)**.
*   For **MCP client auth**, choose per team: Janus **client tokens** (hashed-at-rest, scoped by tool-name glob), or the **OAuth 2.1 resource server** (`OAUTH_ENABLED=true`) so developers present their existing IdP **audience-bound** access tokens directly on `/mcp` (RFC 9728 discovery + `WWW-Authenticate` challenge + RFC 8707 `aud` validation, fail-closed). Both coexist — see [OAuth 2.1 Resource Server](oauth_resource_server.html).
*   Developers query tools over the central gateway URL via SSE (`/sse` endpoint). The database and vaults are stored in RDS and AWS Secrets Manager, eliminating local files.

### Onboarding member/business-unit APIs at scale (OpenAPI import)
Rather than hand-mapping each downstream API, platform engineers onboard any API that ships an OpenAPI
3.x description in one command, namespacing it under a per-unit prefix:
```bash
# Preview, then apply — generates a connection + one MCP tool per operation
mcp-cli import openapi https://unit-api.internal/openapi.json --dry-run --prefix lchdata_
mcp-cli import openapi https://unit-api.internal/openapi.json --prefix lchdata_
```
Credentials are never imported — register the secret in the vault afterward and set the connection's
`auth_secret_ref`. See the [OpenAPI → MCP Import](openapi_import.html) guide.

### Desktop Distribution (Stdio Configuration Script)
*   For developers running local IDE tools (Claude Desktop, VS Code Copilot extensions, Codex wrappers):
*   Provide a centralized setup script in Customer's developer portal (DX1) that compiles the binary (or pulls the container image) and updates the local user configuration folder automatically:
```bash
#!/usr/bin/env bash
# Central developer setup template
curl -fsSL https://dx1.customer.internal/mcp/install.sh | sh
# Updates Claude config to inject the correct local binary path and issued OIDC token
```

---

## 4. Structuring Information via "Skills"

### Do we need a Custom Skill?
**Yes.** While MCP provides the raw, structured data (e.g. JSON yields and ISIN lists), an LLM requires instructions (a "Skill") to interpret the data correctly, calculate margin ratios, apply haircuts, and format the output into formal reports.

### Workspace Skill Implementation
We have defined a workspace-scoped skill inside this project:
*   **Path**: [.agents/skills/lch-collateral-reporting/SKILL.md](file:///home/olafkfreund/Source/Calitti/mcp-api-gateway/.agents/skills/lch-collateral-reporting/SKILL.md)

This instructs the LLM to:
1.  **Orchestrate sequentially**: Call `lch_get_non_cash_collateral` first, then call `ustreasury_get_avg_interest_rates` to check yields.
2.  **Enforce Math Formulas**: Calculate Net Collateral values mathematically.
3.  **Enforce Document Outlines**: Output the final summary using the designated LCH Ltd Markdown Template (H1/H2 structure, tables, and audit trail compliance notes).

---

## 5. Generating Documents & Reports Securely

When a user asks: *"Check MEM-LCH-002 collateral and draft a compliance report."*

1.  **Intent Match**: The LLM matches the request to the `lch-collateral-reporting` skill.
2.  **Tool Calls**:
    *   LLM retrieves non-cash collateral data:
        ```json
        [{"isin":"US912828GD97","market_value_eur":25000000,"haircut_pct":2}]
        ```
    *   LLM retrieves current Treasury yields: `3.690%`.
3.  **Security Auditing & Governance**: The gateway tracks the token, logs the duration, and registers the transaction in the RDS audit log. Two optional controls harden this step in regulated tiers:
    *   **Redaction (DLP)** — with `REDACTION_ENABLED=true`, any PII/secret in the tool arguments or the downstream response (emails, cards, JWTs, cloud/API keys, IBANs) is masked as `[REDACTED:<class>]` before it reaches the LLM or the audit log; only per-class hit counts are recorded.
    *   **Tool pinning** — with `TOOL_PINNING_STRICT=true`, a tool whose definition changed since it was approved (a mismatched SHA-256 `definitionHash`) is refused with error `-32001`, so a redefined `lch_get_non_cash_collateral` can't silently alter a compliance report. See [Tool Pinning & Redaction](governance_pinning_redaction.html).
4.  **Drafting the Report**: The LLM compiles the markdown file based on the skill template:

```markdown
# LCH Ltd: Member Collateral Valuation & Audit Report
**Date**: 2026-06-29  
**Member ID**: MEM-LCH-002  
**Status**: ACTIVE

## 1. Executive Summary
LCH Member MEM-LCH-002 holds U.S. Treasury securities with a total market valuation of €25,000,000.00 EUR. After applying LCH clearing haircuts, the net collateral value is €24,500,000.00 EUR.

## 2. Collateral Portfolio Breakdown
| Asset Name | ISIN | Type | Market Value (EUR) | Haircut | Net Collateral Value (EUR) |
| :--- | :--- | :--- | :--- | :---: | :--- |
| US TREASURY N/B 2.000% | US912828GD97 | Government Bond | €25,000,000.00 | 2.0% | €24,500,000.00 |
```
5.  **PDF/Document Export**: The generated Markdown text can be passed to downstream document generation services (e.g. Weasyprint or Pandoc REST wrappers) to compile a formal corporate PDF report for the clearing member.
