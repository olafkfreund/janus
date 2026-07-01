# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Go **MCP (Model Context Protocol) API gateway** — a secure reverse proxy that turns configured REST/HTTP
endpoints into MCP tools that LLM clients (Claude, Antigravity, Copilot) can call. It ships an admin web
portal, token/JWT auth, a credential vault, OpenTelemetry, and runs on AWS EKS. The Go module is
`github.com/calitti/mcp-api-gateway`; it is deployed under the product name **janus**.

## Commands

Build requires **CGO** (the `go-sqlite3` driver) — `CGO_ENABLED=1` and a C toolchain. Go 1.26.

```bash
just validate          # go vet ./... && go test -v ./...   (primary check)
just build             # builds mcp-gateway + mcp-cli binaries
just run               # go run main.go  (server mode)
just up / just down    # docker-compose cluster

go test ./...                                   # all tests
go test ./pkg/gateway/ -run TestExecuteCall_GET -v   # a single test
go vet ./... && gofmt -l pkg/ main.go           # lint + format check (no golangci config)
```

Two run modes from the same binary: **server mode** (default — portal + MCP SSE on `:8080`) and
**stdio mode** (`go run main.go --stdio`, for Claude Desktop). There is also an admin CLI:
`mcp-gateway cli <subcommand>` (in `main.go:runCLI`) and a separate `cmd/mcp-cli`.

Demo recipes drive the live gateway: `just demo-antigravity` (`agy`) and `just demo-claude` (`claude`),
both reading the MCP config that points at the deployed SSE endpoint.

## Architecture — the big picture

A request flows across packages; understanding one file isn't enough. Two entry surfaces share the
same data and gateway client:

- **`main.go`** wires everything: `config.LoadConfig` → `storage.NewDB` → `vault.InitVault` →
  `gateway.NewGatewayClient` → `auth.NewAuthManager` → `mcp.NewMCPServer` → `portal.NewPortalServer`,
  then mounts routes on one `http.ServeMux` wrapped with rate-limit + body-limit middleware.

- **MCP path** (`pkg/mcp/server.go`): `/sse` authenticates a client token (master `GATEWAY_TOKEN` →
  admin/`*`, or a DB `client_token` → its role+scopes) and opens an SSE session held in an in-memory
  map. The client then POSTs JSON-RPC to `/messages?sessionId=…`, which **re-authenticates against the
  session's token** (anti-hijack) and dispatches `tools/list` / `tools/call`. Tool visibility and calls
  are gated by `matchScope` (glob/prefix) and an `admin_`-prefix role check.

- **Tool execution** (`pkg/gateway/client.go`): `ExecuteCall` renders the endpoint path/body templates
  with the call args, **validates egress (SSRF guard)**, resolves the connection's auth secret from the
  vault, injects it (bearer/basic/custom headers), and proxies the request. The LLM client never sees
  downstream credentials — connections store only a vault **reference** (`auth_secret_ref`).

- **Portal path** (`pkg/portal/api.go`): JSON admin API behind `AdminAuthMiddleware` (JWT, **role
  enforced — not just authenticated**) for connections, endpoints, tokens, vault, audit logs, settings,
  and a generated OpenAPI doc; plus local login / OIDC SSO and an embedded SPA (`static/`).

### Cross-cutting concepts you must respect

- **Fail-closed config** (`pkg/config/config.go`): no usable default secrets. `JWT_SECRET` and
  `GATEWAY_TOKEN` must be ≥32 bytes or the process refuses to start. Local admin login is disabled
  unless `ADMIN_PASSWORD` (≥12) is set; SSO role comes from `OIDC_DEFAULT_ROLE`.
- **Dual datastore** (`pkg/storage/db.go`): SQLite by default, **Postgres** when `DATABASE_URL` starts
  with `postgres://`. `d.query()` rewrites `?` → `$n` for Postgres; queries are parameterized. A short-TTL
  cache fronts the two hot reads (`GetConnections`/`GetAllEndpoints`) and is **purged on every write**.
- **Vault providers** (`pkg/vault/`): `local` (file, single-node/dev), `postgres` (AES-256-GCM encrypted
  in the shared DB — correct for multi-replica), and `aws/gcp/azure` which **fail closed (not
  implemented)** rather than returning fakes. Postgres-vault key derives from `VAULT_ENCRYPTION_KEY`
  (falls back to `JWT_SECRET`).
- **Client tokens are hashed at rest** (SHA-256); they're shown once at creation and looked up by hash —
  never seed or hardcode a token.
- **Security extras**: egress allowlist + private-range/DNS-rebind blocking; per-IP rate limit; HTTP
  timeouts/body caps; secret + idempotent-GET response caches; bounded retries with backoff.

## Deployment (live)

GitHub Actions (`.github/workflows/deploy.yml`) on push to `main`: builds the image, pushes to ECR
`796973489124.dkr.ecr.eu-west-2.amazonaws.com/janus`, and `kubectl apply`s to EKS cluster `sarc-aws`
(eu-west-2), namespace `janus`. Auth is GitHub OIDC via the `AWS_DEPLOY_ROLE_ARN` repo **variable**
(see `deployment/GITHUB_OIDC_SETUP.md`).

The live deployment is **stateless gateway on in-cluster Postgres + HPA (2→10)**. Manifests are split by
lifecycle and this matters:
- `k8s-janus.yaml` — pipeline-applied gateway Deployment/Service/Ingress. It **omits `replicas`** (the
  HPA owns the count) and contains **only namespaced resources** (the deploy role has namespace-scoped
  EKS edit rights).
- `k8s/janus-db.yaml` (Postgres) and `k8s/janus-scaling.yaml` (HPA + PDB) — applied **once out-of-band**.
- The `janus` Namespace and the `mcp-gateway-secrets` Secret (`jwt-secret`, `gateway-token`,
  `db-password`, `database-url`, `admin-password`) are also managed out-of-band — do not put real
  secrets in the manifests.

Multi-replica SSE relies on **nginx cookie affinity** (`route` cookie) so `/sse` and `/messages` land on
the same pod; clients (and test scripts) must reuse a cookie jar.

## Conventions

This repo's global standards target Rails/React, which do **not** apply here — follow Go idioms. Commit
or push only when asked; pushing to `main` triggers a live deploy. Run `just validate` before proposing
changes. See `SECURITY_REVIEW.md` and `SCALING_AND_CACHING.md` for the security posture and scaling
design.
