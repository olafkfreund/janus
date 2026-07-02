---
layout: default
---

# OAuth 2.1 Resource Server

Janus can act as an **OAuth 2.1 protected resource** for its MCP endpoints, sitting alongside the
existing bearer **client-token** path. When enabled, MCP clients that already hold an access token from
your enterprise identity provider (Okta, Entra ID, Keycloak, Auth0, …) can present that token directly —
the gateway validates it, binds it to *this* resource, and enforces its scopes. No Janus-issued client
token is required for those callers.

> **Off by default.** OAuth is only active when `OAUTH_ENABLED=true`. With it off, the MCP endpoints
> behave exactly as before (master `GATEWAY_TOKEN` and DB-issued client tokens). Turning OAuth on is
> **additive** — client tokens continue to work.

The implementation follows three IETF specifications:

* **RFC 9728** — OAuth 2.0 Protected Resource Metadata (the `/.well-known/oauth-protected-resource`
  discovery document and the `WWW-Authenticate` challenge).
* **RFC 8707** — Resource Indicators: the access token must be **audience-bound** to this gateway.
* **RFC 6749 / 7519** — the underlying OAuth 2.0 / JWT semantics (`scope`/`scp`, `aud`, `iss`).

---

## How it works

### 1. Discovery — `/.well-known/oauth-protected-resource`

When `OAUTH_ENABLED=true`, the gateway publishes an **unauthenticated, publicly discoverable** metadata
document so clients can find the authorization server(s) *before* they hold a token:

```bash
curl https://janus.example.com/.well-known/oauth-protected-resource
```

```json
{
  "resource": "https://janus.example.com/mcp",
  "authorization_servers": [
    "https://login.example.com"
  ],
  "scopes_supported": [
    "mcp.read",
    "mcp.tools"
  ],
  "bearer_methods_supported": ["header"]
}
```

* `resource` comes from `OAUTH_RESOURCE_URI` — the canonical identity of this gateway that tokens must be
  bound to.
* `authorization_servers` comes from `OAUTH_AUTHORIZATION_SERVERS`.
* `scopes_supported` comes from `OAUTH_SCOPES_SUPPORTED` (omitted from the document if unset).
* `bearer_methods_supported` is fixed to `["header"]` — tokens are presented in the `Authorization`
  header.

### 2. Challenge — `WWW-Authenticate`

An MCP request that arrives without a usable token receives a `401` carrying an RFC 9728 §5.1 challenge
that points the client back at the metadata document:

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer resource_metadata="https://janus.example.com/mcp/.well-known/oauth-protected-resource", error="invalid_token"
```

A conformant OAuth 2.1 client reads this header, fetches the metadata, runs its authorization-code +
PKCE flow against the advertised authorization server, and retries with a bearer access token.

### 3. Audience-bound validation (RFC 8707)

The gateway validates the presented JWT against the trusted issuer(s) and then enforces that the token's
`aud` (audience) claim **contains** `OAUTH_RESOURCE_URI`. A token minted for a *different* resource — even
by the same identity provider — is rejected. Validation **fails closed**: any signature, issuer, expiry,
or audience error results in a `401`, never a silent pass. Scopes are read from the `scope` claim
(space-delimited, RFC 6749 §3.3) or, failing that, the `scp` claim.

<div class="mermaid">
sequenceDiagram
    autonumber
    participant Client as MCP Client
    participant GW as Janus (Resource Server)
    participant AS as Authorization Server

    Client->>GW: POST /mcp (no token)
    GW-->>Client: 401 + WWW-Authenticate: Bearer resource_metadata="…"
    Client->>GW: GET /.well-known/oauth-protected-resource
    GW-->>Client: { resource, authorization_servers, scopes_supported }
    Client->>AS: Authorization Code + PKCE (resource = OAUTH_RESOURCE_URI)
    AS-->>Client: Access token (aud = OAUTH_RESOURCE_URI)
    Client->>GW: POST /mcp  (Authorization: Bearer <access_token>)
    GW->>GW: Verify signature + issuer + expiry + aud ∋ resource
    alt Valid & audience-bound
        GW-->>Client: JSON-RPC result (scopes enforced)
    else Invalid / wrong audience
        GW-->>Client: 401 + WWW-Authenticate challenge
    end
</div>

---

## Configuration

| Variable | Default | Purpose |
| :--- | :--- | :--- |
| `OAUTH_ENABLED` | `false` | Master switch for the OAuth 2.1 resource server. Off = client-token auth only. |
| `OAUTH_RESOURCE_URI` | `""` | This gateway's canonical resource identifier; tokens must carry it in `aud`. **Required** when enabled. |
| `OAUTH_AUTHORIZATION_SERVERS` | `""` | Comma-separated trusted authorization-server issuer URLs. **Required** when enabled. |
| `OAUTH_SCOPES_SUPPORTED` | `""` | Comma-separated scopes advertised in the metadata document (optional). |

> **Fail-closed startup.** If `OAUTH_ENABLED=true` but `OAUTH_RESOURCE_URI` or
> `OAUTH_AUTHORIZATION_SERVERS` is missing, the process refuses to start — consistent with the rest of
> Janus's [fail-closed configuration](index.html#security--governance).

### Example

```bash
export OAUTH_ENABLED=true
export OAUTH_RESOURCE_URI="https://janus.example.com/mcp"
export OAUTH_AUTHORIZATION_SERVERS="https://login.example.com"
export OAUTH_SCOPES_SUPPORTED="mcp.read,mcp.tools"
```

In Kubernetes these map to plain container `env` entries on the gateway Deployment; the resource URI
should match your public MCP endpoint (typically `PUBLIC_BASE_URL` + `/mcp`).

---

## When to use OAuth vs. client tokens

| | Janus client tokens | OAuth 2.1 access tokens |
| :--- | :--- | :--- |
| Issued by | Janus (portal / CLI), hashed at rest | Your enterprise IdP |
| Best for | Service agents, demos, machine-to-machine | End-user MCP clients already federated with SSO |
| Scoping | Tool-name globs (`weather_*`) + role | OAuth `scope`/`scp` claims |
| Rotation | Re-issue token in the portal | Standard IdP token lifecycle |
| Enabled | Always available | Only when `OAUTH_ENABLED=true` |

Both mechanisms coexist on the same `/mcp` and `/sse` endpoints, so you can migrate clients gradually.

---

*Related: [Security & Governance overview](index.html#security--governance) ·
[Tool Pinning & Redaction](governance_pinning_redaction.html) ·
[OpenAPI → MCP Import](openapi_import.html)*
