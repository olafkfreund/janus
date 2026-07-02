---
layout: default
---

# OpenAPI → MCP Import

Onboarding an existing REST API used to be a per-endpoint exercise: one `connection add`, then one
`endpoint add` for every operation, hand-writing the JSON Schema each time. Janus can now do the whole
thing from the API's own **OpenAPI 3.x** description — a single import that generates a connection plus
one MCP tool per operation, in seconds.

Import is available two ways:

* **Admin API** — `POST /api/import/openapi` (behind the role-enforced admin middleware).
* **CLI** — `mcp-cli import openapi <file-or-url> [--dry-run] [--prefix <p>]`.

Both accept an OpenAPI 3.x document as **JSON or YAML**, either as a local file or an `http(s)` URL.

---

## What the import produces

The parser reads the spec and derives:

* **One connection** from `info.title` (name) and `servers[0].url` (base URL); the authentication type
  is inferred from the spec's security schemes into the connection's `auth_type`.
* **One tool endpoint per operation** — tool name, description, HTTP method, path, and a generated JSON
  Schema for the parameters — created exactly as if you had entered them through the admin API, so
  imported tools behave identically to hand-mapped ones (including [tool-definition hash
  pinning](governance_pinning_redaction.html)).

A spec missing a usable `info.title` or `servers[0].url` is rejected with a clear error rather than
producing a half-configured connection.

> **Credentials are never imported.** The import sets the connection's `auth_type`, but the actual secret
> still lives in the [vault](index.html#credential-handling--the-encrypted-vault). After importing an
> authenticated API, register its secret (`mcp-cli vault set …`) and point the connection at that
> `auth_secret_ref`. The LLM client never sees the credential.

---

## CLI usage

### Preview first with `--dry-run`

A dry run parses the spec and prints exactly what *would* be created — **no writes** hit the gateway:

```bash
mcp-cli import openapi ./petstore.yaml --dry-run --prefix petstore_
```

```text
OpenAPI Import Summary:
=======================
Source:           ./petstore.yaml
Mode:             DRY RUN (no changes were written to the gateway)

Connection:       Swagger Petstore   (https://api.example.com/v3)   auth: none
Tools (3):
  petstore_list_pets      GET   /pets
  petstore_get_pet        GET   /pets/{petId}
  petstore_create_pet     POST  /pets
```

### Apply the import

Drop `--dry-run` to persist the connection and its tool endpoints:

```bash
mcp-cli import openapi https://api.example.com/openapi.json --prefix petstore_
```

```text
OpenAPI Import Summary:
=======================
Source:           https://api.example.com/openapi.json
Mode:             APPLY (connection + tool endpoints written to the gateway)

Connection:       Swagger Petstore  (id: 4f1c…)
Endpoints created: 3
  petstore_list_pets, petstore_get_pet, petstore_create_pet
```

The `--prefix` flag namespaces every generated tool name (here `petstore_…`), preventing collisions when
you onboard multiple APIs — the same namespacing discipline used throughout Janus.

---

## Admin API usage

`POST` the raw spec body to `/api/import/openapi`. The request runs through the same role-enforced admin
authentication as every other administrative route.

```bash
curl -sS -X POST "https://janus.example.com/api/import/openapi?dry_run=true&prefix=petstore_" \
  -H "Authorization: Bearer <admin-jwt>" \
  -H "Content-Type: application/yaml" \
  --data-binary @petstore.yaml
```

| Query param | Purpose |
| :--- | :--- |
| `dry_run=true` | Return a preview only; write nothing. Omit (or `false`) to persist. |
| `prefix=<p>` | Prefix applied to every generated tool name. |

**Dry-run response** returns the proposed `connection` (`name`, `base_url`, `auth_type`), a `tool_count`,
and a `tools` array (`tool_name`, `tool_description`, `path`, `method`, `parameters_schema`).

**Apply response** returns `connection_id`, `connection_name`, `endpoints_created`, and the created
`tool_names`.

> Spec uploads are size-capped (10 MB) as an abuse guard; OpenAPI documents are text and rarely approach
> this even for large APIs.

---

## Recommended workflow

1. **`--dry-run`** the spec and eyeball the generated tool names, methods, and schemas.
2. Re-run **with `--prefix`** to apply, namespacing the API under a business-unit-friendly prefix.
3. If the API needs credentials, **register the secret in the vault** and set the connection's
   `auth_secret_ref` + `auth_type`.
4. **Issue a scoped client token** (e.g. `--scopes "petstore_*"`) so a given agent sees only these tools.
5. Optionally enable [strict tool pinning](governance_pinning_redaction.html#tool-definition-hash-pinning)
   so approved tool definitions can't silently change afterwards.

---

*Related: [Credential Handling & the Encrypted Vault](index.html#credential-handling--the-encrypted-vault) ·
[Governance: Tool Pinning & Redaction](governance_pinning_redaction.html) ·
[OAuth 2.1 Resource Server](oauth_resource_server.html)*
