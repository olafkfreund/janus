# Onboarding this service into ARC (SARC portal)

Step-by-step record of how `mcp-api-gateway` (Janus) was onboarded into the ARC
portal — including live **ServiceNow Change Request** and **CMDB** integration —
so the next service can follow the same recipe.

## The one rule that makes everything line up

ARC stitches a service together across every surface using **two identity
conventions**. Get these right and the service appears everywhere at once; get
them wrong and data silently fails to join.

1. **One name, used verbatim everywhere:**
   `Service.slug` == the k8s workload `name` == the value passed to the pipeline
   as `SERVICES_DEPLOYED` == the ServiceNow **CMDB CI `name`**.
   For this service that string is **`mcp-api-gateway`** (the Deployment name in
   namespace `janus`, and the Backstage catalog `name`).
2. **One commit, used verbatim everywhere:** the same `commitSha` flows through
   the Fides trail, the CR's `u_commit_sha`, and ARC's `DeploymentRecord.commitSha`.
   That's how ARC matches a Change Request to a deployed commit (and satisfies the
   `cr_approved` compliance control).

> The GitHub **repo** can be named differently (mirror `synechron/Janus-mcp-gateway`,
> origin `olafkfreund/janus`) — that only links pipelines/issues. The **service**
> name is `mcp-api-gateway` and is what all the data surfaces key on.

## What this service is (context)

- Go MCP API gateway, deployed by **Flux** to namespace **`janus`** on cluster
  **`sarc-aws`** (AWS, account 796973489124, eu-west-2). Deployment name
  `mcp-api-gateway`, image `796973489124.dkr.ecr.eu-west-2.amazonaws.com/janus`.
- What it was **missing** for full ARC integration: the ARC service registration,
  the data surfaces, and the **ServiceNow CR + CMDB** steps.

---

## Step A — Register the service in ARC

ARC's anchor is a `Service` row (Prisma `Service` model). Create it via
**Settings -> catalog (`POST /api/services`)** as ADMIN, or a DB upsert. Fields set
for `mcp-api-gateway`:

| Field | Value | Why |
|---|---|---|
| `slug` | `mcp-api-gateway` | the join key for deployments/vulns/SBOM everywhere |
| `displayName` | `MCP API Gateway (Janus)` | catalog label |
| `kubernetesWorkloads` | `[{cloud:aws, cluster:sarc-aws, namespace:janus, name:mcp-api-gateway}]` | links the service to live pods so **topology** resolves; `name` must match the Deployment |
| `githubOwner`/`githubRepo` | `synechron` / `Janus-mcp-gateway` | links pipelines + issues |
| `fidesFlow` | `mcp-api-gateway` | which Fides trail the service attests into |
| `criticality` / `tier` | `high` / `internal` | scorecards + graph |
| `businessServiceSysId` | *(set in Step C)* | pointer to the ServiceNow CMDB CI |

**Also add a Git Repo** (Settings -> **Git Repos**) for `github:synechron/Janus-mcp-gateway`
so the pipelines + Issue Sync fan-out includes it. (ARC's `getTenantRepos()`
resolves it; falls back to the tenant's primary repo only if no row exists.)

After Step A the service shows in the **catalog**, **service graph**, and once its
pods are read via the cluster IRSA role, on the **topology** page (select
`sarc-aws` in the cluster picker).

## Step B — Produce the data surfaces

ARC's DORA / security / SBOM surfaces read per-service records. In production these
are written by CI (see Step D); for onboarding they were seeded so the surfaces
light up immediately:

- **`DeploymentRecord`** (feeds DORA: deployment frequency, lead time,
  change-failure rate) — `service=mcp-api-gateway`, `provider=github`,
  `environment`, `commitSha`, `crNumber`, `status` (mostly `SUCCESS`, one `FAILED`
  for change-failure rate).
- **`VulnerabilityRecord`** — `service=mcp-api-gateway`, severity/status/scanType
  (mirrors what the repo's Trivy/Grype scan finds).
- **`SbomRecord`** — `service=mcp-api-gateway`, `format=cyclonedx-json` (mirrors the
  repo's CycloneDX SBOM job).

## Step C — ServiceNow: CMDB CI + Change Request

Done live against the demo instance (`calitiiltddemo3.service-now.com`, integration
user `github_integration`).

1. **CMDB CI** — create/upsert a `cmdb_ci_service` whose **`name` is
   `mcp-api-gateway`** (must equal the service slug so the CR can anchor to it).
   In ARC this is normally done by **Settings -> ServiceNow CMDB -> Sync Now** with
   **`CI Class = cmdb_ci_service`** (not the default `cmdb_ci_application` — the
   portal's CR/PA/allowlist scoping keys on `cmdb_ci_service`; see the
   *ServiceNow CMDB config* doc in ARC's GitLab Pages). The CI carries
   `discovery_source = karc-portal` so re-syncs reconcile instead of duplicating.
2. **Change Request** — create a `change_request` with:
   - `business_service` + `cmdb_ci` = the CI's `sys_id` (the anchor),
   - `u_commit_sha` = the deployed commit,
   - `u_services_deployed` = `mcp-api-gateway`,
   - `u_target_cloud` / `u_deploy_env`.
   Then **approve** it (`approval=approved`, `u_auto_approved=true`) — auto-approve
   is driven by the Fides risk score being under threshold.
3. **Link it in ARC** — set `Service.businessServiceSysId` = the CI sys_id, and set
   the matching `DeploymentRecord.crNumber` = the CR number. Because the CR's
   `u_commit_sha` equals that deployment's `commitSha`, ARC correlates the CR to the
   deployment and the `cr_approved` compliance control is satisfied.

Concrete result for `mcp-api-gateway`: CI `mcp-api-gateway` (`cmdb_ci_service`) and
CR **CHG0032935** (approved, `u_commit_sha` = the prod deploy commit), linked to the
prod `DeploymentRecord`.

**ServiceNow prerequisites** (already present on this instance): the custom
`u_commit_sha` / `u_services_deployed` / `u_target_cloud` / `u_deploy_env` /
`u_auto_approved` fields on `change_request`, a `cmdb_ci_service` table, and an
assignment group. If a new instance lacks the `u_*` fields, run
`scripts/servicenow/create-custom-fields.sh` from the SARC repo first.

## Step D — Make it self-sustaining (wire the pipeline)

Seeding gets the service visible today; to keep it live, the pipeline must emit the
chain on every deploy. Add to the deploy workflow (the Fides steps already exist):

1. **Deployment record -> ARC**: after deploy, `POST` to ARC's deployment webhook
   with `service=mcp-api-gateway`, the `commitSha`, `environment`, `status`, and the
   CR number — so `DeploymentRecord` populates automatically (and DORA metrics stay
   live). Pass the service name explicitly; the default webhook derives it from the
   repo name (`janus`/`Janus-mcp-gateway`), which would not match the slug
   `mcp-api-gateway`.
2. **ServiceNow CR**: in the attest phase (after Fides so risk scoring works), call
   the CR create/approve/close flow (mirror SARC's `scripts/ci/servicenow-cr.sh`
   subcommands `create` -> `update-state` -> `close`) with
   `SERVICES_DEPLOYED=mcp-api-gateway` and `CI_COMMIT_SHA` set. It stamps
   `u_commit_sha`, anchors `business_service`/`service_offering` by name, and on
   auto-approve writes both `u_auto_approved=true` **and** the standard
   `approval="approved"` field (required for the `cr_approved` control).
3. **CMDB sync**: trigger ARC's `Sync Now` (or the IRE upsert) post-deploy so the CI
   version updates.

Required CI secrets/vars: `SERVICENOW_INSTANCE_URL` / `SERVICENOW_USERNAME` /
`SERVICENOW_PASSWORD`, `FIDES_SERVER_URL` / `FIDES_API_TOKEN` / `FIDES_FLOW_ID`,
`SN_RISK_THRESHOLD_{DEV,QA,PROD}`, and an ARC ingest token for the webhook. Each
step no-ops cleanly when its secrets are unset.

---

## Onboarding checklist (copy for the next service)

- [ ] Pick **one name** = slug = workload name = `SERVICES_DEPLOYED` = CMDB CI name.
- [ ] Create the ARC `Service` row (slug, `kubernetesWorkloads`, repo fields,
      `fidesFlow`, criticality/tier).
- [ ] Add the **Git Repo** in ARC settings.
- [ ] Ensure the app's pods are readable by ARC (cluster onboarded + IRSA/metrics —
      install `metrics-server` on the target cluster for live CPU/mem).
- [ ] ServiceNow: `CI Class = cmdb_ci_service`, Sync Now -> CI created; ensure `u_*`
      fields exist.
- [ ] Wire the pipeline (Step D): deployment webhook, ServiceNow CR create/close,
      CMDB sync; keep `commitSha`/`u_commit_sha` identical across trail/CR/deploy.
- [ ] Verify: catalog + topology + DORA + security + SBOM show the service, and an
      **approved CR** exists whose `u_commit_sha` matches a deployed commit.
