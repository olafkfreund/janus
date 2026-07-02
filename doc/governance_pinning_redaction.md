---
layout: default
---

# Governance: Tool Pinning & Redaction

Two governance controls harden the *content* of an MCP session — one defends the integrity of the tools
an agent was approved to use, the other keeps sensitive data out of the model's context and out of
downstream calls. Both are **configurable and OFF by default**, so you opt into them per environment.

* **[Tool-definition hash pinning](#tool-definition-hash-pinning)** — a SHA-256 fingerprint and version
  per tool, exposed to clients, with an optional strict mode that blocks calls to any tool that changed
  since it was approved (a "rug-pull" defense).
* **[PII / secret redaction (DLP)](#pii--secret-redaction-dlp)** — masks emails, cards, JWTs, cloud keys,
  API keys and IBANs in tool arguments *and* downstream responses before the LLM ever sees them.

---

## Tool-definition hash pinning

Every tool endpoint carries a **`definitionHash`** — a SHA-256 content hash over the behaviour-relevant
fields of the tool (name, description, method, path, parameter schema) — and an integer **`version`**
that increments whenever that hash changes on save. This gives each tool a stable, verifiable identity.

### Exposed in `tools/list`

The hash is surfaced to clients under the MCP **`_meta`** convention, so a client (or a human reviewer)
can record the exact definition it approved:

```json
{
  "name": "concorde_get_dpg_trade_volume",
  "description": "Retrieve daily cleared trade counts and USD valuations for a member",
  "inputSchema": { "type": "object", "properties": { "member_id": { "type": "string" } } },
  "_meta": {
    "definitionHash": "9f2b1c…e07a",
    "version": 3
  }
}
```

The `_meta` block is **omitted entirely** when there is nothing to report, so tool-pinning-unaware
clients see no change in the response shape.

### Strict mode — `TOOL_PINNING_STRICT`

The "rug-pull" attack: a tool an agent trusted yesterday is quietly redefined today to do something else.
With `TOOL_PINNING_STRICT=true`, the gateway refuses to execute a tool whose **live** definition no
longer matches the hash it was approved under, returning a JSON-RPC error:

```json
{
  "jsonrpc": "2.0",
  "error": {
    "code": -32001,
    "message": "tool \"concorde_get_dpg_trade_volume\" definition has changed since it was approved; refusing call (strict tool pinning enabled)"
  },
  "id": 1
}
```

* **Off by default** (`false`): `tools/list` still reports hashes and versions, but calls are never
  blocked — useful for observing drift before enforcing.
* A tool with **no recorded baseline hash never blocks**, so enabling strict mode won't break freshly
  created tools.
* An administrator re-approving a legitimately changed tool re-establishes the baseline (a new hash +
  bumped version) and calls resume.

<div class="mermaid">
graph LR
    classDef ok fill:#3c3836,stroke:#b8bb26,stroke-width:2px,color:#ebdbb2;
    classDef bad fill:#3c3836,stroke:#cc241d,stroke-width:2px,color:#ebdbb2;
    classDef gw fill:#282828,stroke:#fe8019,stroke-width:3px,color:#ebdbb2;

    A["tools/call"]:::gw --> B{"live hash == approved hash?"}:::gw
    B -- "match" --> C["Execute tool"]:::ok
    B -- "changed (strict)" --> D["Reject -32001"]:::bad
    B -- "changed (non-strict)" --> C
</div>

---

## PII / secret redaction (DLP)

With `REDACTION_ENABLED=true`, Janus runs a lightweight, dependency-free data-loss-prevention pass on
tool traffic. It masks sensitive values **in both directions**:

* **On tool arguments — before execution.** A value lifted from the model's context can't be smuggled out
  through a downstream API call.
* **On tool results — before the response is returned.** Sensitive data in a downstream payload is masked
  before it ever reaches the LLM's context window.

Each match is replaced with a class-tagged mask, e.g. `[REDACTED:email]`, `[REDACTED:credit_card]`.

### Built-in detectors

| Class | Matches |
| :--- | :--- |
| `email` | Email addresses |
| `jwt` | JSON Web Tokens (`eyJ…` three-segment) |
| `aws_access_key` | AWS access key IDs (`AKIA…`) |
| `credit_card` | 13–19 digit card numbers, **Luhn-validated** to cut false positives |
| `iban` | IBAN account numbers |
| *(generic)* | `api_key=`/`token=`/`secret=`/`password=` assignments, `Bearer <token>` headers, OpenAI-style `sk-…` keys |

The credit-card detector applies the Luhn checksum before masking, so ordinary long digit runs (order
IDs, timestamps) aren't misidentified as card numbers.

### Audit-logged, never leaked

Redaction is **audit-logged as per-class hit counts** — you can see *that* two emails and one card were
redacted on a call, without the raw values ever being written to logs or the audit trail. The redactor
returns only a masked copy plus a findings summary; it never persists what it found.

> **Governance note.** When `REDACTION_ENABLED` is `false`, the gateway logs a startup reminder that
> sensitive fields are **not** being redacted, and recommends enabling it in production for data
> governance. Turn it on before exposing tools over any downstream that may return customer data.

### Enable it

```bash
export REDACTION_ENABLED=true
```

---

## Configuration summary

| Variable | Default | Purpose |
| :--- | :--- | :--- |
| `TOOL_PINNING_STRICT` | `false` | Reject `tools/call` when a tool's live definition no longer matches its approved hash. `tools/list` reports hashes regardless. |
| `REDACTION_ENABLED` | `false` | Mask PII/secrets in tool arguments and downstream responses before the LLM sees them; audit-logged. |

Both pair naturally with the rest of the Janus governance stack — [scoped client
tokens](index.html#security--governance), the [SSRF egress
guard](index.html#security--governance), the [encrypted
vault](index.html#credential-handling--the-encrypted-vault), and the full
[audit trail](index.html#5-audit-logging--built-in-guides).

---

*Related: [OAuth 2.1 Resource Server](oauth_resource_server.html) ·
[OpenAPI → MCP Import](openapi_import.html) ·
[Security & Governance overview](index.html#security--governance)*
