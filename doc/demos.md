---
layout: default
---

# Demos & Client Integration

Janus ships two ready-to-run demos that drive the **live** gateway from real LLM agents — one for **Claude Code** and one for **Antigravity** — plus a governed skill that produces a professional financial report. Both are invoked through the repo's `justfile`.

```bash
just demo-claude        # Claude Code (claude -p) via Streamable HTTP
just demo-antigravity   # Antigravity (agy --print) via the lch-collateral-reporting skill
just mcp-config-claude  # print the Claude Code MCP config (.mcp.json)
```

Both demos generate the same deliverable: a **Cross-Currency Collateral Valuation & Multi-Jurisdiction Rate Audit** for LCH clearing member `MEM-LCH-002`. The agent aggregates collateral, rates, FX and inflation from **five separate sources through the one governed gateway**, reconciles haircuts, and consolidates the multi-currency portfolio into a single **GBP** reporting value (~**£36.39M**). A generated example lives at [`sample_crosscurrency_collateral_report.md`](sample_crosscurrency_collateral_report.md).

> Because every figure is fetched live through the gateway, both agents independently produce the **identical** consolidated figure — deterministic, real, multi-jurisdiction data with the agent never touching a downstream credential.

---

## The tools the demos exercise

All seeded automatically when `SEED_DEMO_DATA=true`. The LLM sees them as MCP tools; the gateway proxies each to its downstream API.

| Tool | Source | Auth |
| :--- | :--- | :--- |
| `lch_get_non_cash_collateral`, `lch_get_dpg_trade_volume` | LCH clearing mock (collateral, ISINs, haircuts) | none |
| `ustreasury_get_avg_interest_rates`, `ustreasury_get_rates_of_exchange` | U.S. Treasury Fiscal Data | none |
| `coinbase_get_btc_stats`, `coinbase_get_eth_stats` | Coinbase Exchange | none |
| `boe_get_bank_rate` | **Bank of England** Bank Rate (IADB) | none |
| `fx_get_reference_rates` | **ECB** euro FX reference rates (Frankfurter) | none |
| `ecb_get_usd_eur_rate` | **ECB** Data Portal (SDMX) | none |
| `eurostat_get_hicp_inflation` | **Eurostat** HICP inflation | none |
| `ons_list_datasets` | **UK ONS** statistics | none |

Authenticated downstream APIs (e.g. UK Companies House / FCA) plug in the same way via the vault — see [Credential Handling](index.html#credential-handling--the-encrypted-vault).

---

## Demo 1 — Claude Code (`just demo-claude`)

Script: `scripts/demo_janus_claude.sh`. Runs Claude Code headlessly (`claude -p`) against the gateway using the project MCP config `.mcp.json` (transport `type: "http"`, endpoint `/mcp`).

**Prerequisites**

* The `claude` CLI installed and signed in to a **claude.ai subscription**. The script runs `env -u ANTHROPIC_API_KEY claude …` so it uses your subscription rather than a (rate-limited) API key.
* Network access to the deployed gateway.

**Run**

```bash
just demo-claude
# or with a specific client token:
JANUS_GATEWAY_TOKEN=<token> just demo-claude
```

**What it does**

1. Loads the `janus-gateway` MCP server from `.mcp.json` (`--allowedTools "mcp__janus-gateway"`).
2. Fetches collateral + US/UK/EU rates + ECB FX + euro-area inflation via the gateway tools.
3. Translates the portfolio to GBP and compiles the cross-currency margin audit, printed to your terminal.

`.mcp.json` (also shown by `just mcp-config-claude`):

```json
{
  "mcpServers": {
    "janus-gateway": {
      "type": "http",
      "url": "https://janus.13.134.88.9.nip.io/mcp",
      "headers": { "Authorization": "Bearer ${JANUS_GATEWAY_TOKEN:-<token>}" }
    }
  }
}
```

---

## Demo 2 — Antigravity (`just demo-antigravity`)

Script: `scripts/demo_janus_mcp.sh`. Runs Antigravity's `agy --print`, which activates the governed **`lch-collateral-reporting`** skill (`.agents/skills/lch-collateral-reporting/SKILL.md`) to drive a deterministic, templated report.

**Prerequisites**

* The `agy` CLI installed and authenticated.
* **Register janus in Antigravity's MCP config** — Antigravity reads `~/.gemini/antigravity/mcp_config.json`, **not** the repo's `.agents/mcp_config.json`. Add:

  ```json
  "janus-gateway": {
    "serverUrl": "https://janus.13.134.88.9.nip.io/sse",
    "headers": { "Authorization": "Bearer <gateway-token>" }
  }
  ```

  (Antigravity uses the Streamable HTTP transport: it `POST`s to `/sse`.)

**Run**

```bash
just demo-antigravity
```

**What it does**

1. Loads the `janus-gateway` tools and activates the `lch-collateral-reporting` skill.
2. Follows the skill's deterministic workflow (LCH → US Treasury → Bank of England → Eurostat → ECB FX).
3. Emits the same cross-currency GBP margin audit, following the skill's markdown template.

---

## Troubleshooting

| Symptom | Cause / Fix |
| :--- | :--- |
| `agy` times out / no tools | janus not in `~/.gemini/antigravity/mcp_config.json` (the repo's `.agents/` file is **not** read by `agy`). |
| Claude uses the API key / hits limits | The script unsets `ANTHROPIC_API_KEY`; ensure `claude` is logged into a subscription. |
| `local login disabled; use SSO` in the portal | Set `ADMIN_PASSWORD` (≥12 chars) — see the [config table](index.html#configuration-settings). |
| Tools missing for a scoped client token | Widen the token's `scopes` (e.g. add `boe_*,fx_*,ecb_*,eurostat_*`). The master `GATEWAY_TOKEN` sees all tools. |
