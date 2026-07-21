# v0.5.0 (2026-07-21)

### Features

* **medplum_project_secret:** new resource managing a single named entry in the session project's `Project.secret[]` settings (the values bots receive as `event.secrets`). Schema: `name` (forces replacement), `value_string` (sensitive), computed `id`/`project_id`. Each entry is an independent resource, so unmanaged sibling entries are always preserved; writes to the shared Project resource are read-modify-write guarded by optimistic concurrency (`If-Match` on the project version, bounded retries with jitter), making parallel applies of many secrets safe. Creating a name that already exists fails instead of silently adopting the entry; import by name (`terraform import medplum_project_secret.x <name>`).
* **medplum_bot:** new `admin` attribute (default `false`) that promotes the bot's ProjectMembership to project admin — required for bots that must write project-admin-only resource types (ProjectMembership, Project, User), e.g. a group→AccessPolicy mapper writing `membership.access[]`. Reads reflect the live `membership.admin`, so out-of-band changes surface as drift; setting it back to `false` demotes the membership.

### Reliability

* **client:** add `FHIRUpdateIfMatch` (conditional PUT with `If-Match: W/"<versionId>"`) and `IsConflict` (HTTP 412/409 classification) supporting optimistic-concurrency read-modify-write flows.

# v0.4.0 (2026-07-20)

### Features

* **medplum_bot:** new resource managing the full bot lifecycle: creation via the project-admin endpoint (Bot + ProjectMembership together, with optional `access_policy` on the membership), live code deployment through `$deploy` on create and whenever the bundle changes (no server restart), `timeout`/`run_as_user`, and import. Bot code is supplied inline (`code`) or by file (`source_path`, recommended — the bundle stays out of Terraform state; only its SHA-256 `source_hash` is stored). Reads recompute `source_hash` from the deployed Binary, so out-of-band deploys surface as drift and are reverted on apply.
* **all typed resources:** new computed `ref` attribute carrying the full FHIR reference (`AccessPolicy/abc`, `Bot/xyz`, ...), so cross-resource wiring reads `access_policy = medplum_access_policy.x.ref` instead of manual string interpolation.
* **provider:** new `supported_bot_runtimes` setting (default `["vmcontext"]`). A `medplum_bot.runtime_version` outside the set — e.g. `"fission"` on a cluster without Fission — fails at plan time instead of at first bot execution.

# v0.3.2 (2026-07-16)

### Dependencies

* Bump `golang.org/x/crypto` to v0.54.0, resolving 13 open security advisories (6 critical, 2 high, 5 medium) against `golang.org/x/crypto < 0.52.0`. The companion `golang.org/x/{net,sys,text,mod,sync,tools}` modules move forward via `go mod tidy`. Indirect dependency only; no provider behaviour changes.

# v0.3.1 (2026-07-16)

### Dependencies

* Bump `terraform-plugin-framework` to 1.19.0, along with the companion `terraform-plugin-go`, `terraform-plugin-testing`, and `terraform-plugin-sdk/v2` libraries so the plugin stack builds against the updated `tfprotov5.ProviderServer` interface (which now requires `CloseEphemeralResource`). The `go` directive moves to 1.25.8 as required by the updated testing module. Build/dependency maintenance only; no provider behaviour changes.

# v0.3.0 (2026-07-09)

### Features

* **medplum_fhir_data_migration:** a `code_remap` whose `from.system` is the empty string now matches codings that carry no `system` (or an empty one) by `code` alone, and its `to` may add a system where there was none. Enables migrating data written without a code system (e.g. `Condition.severity` codings stored as a bare code). Purely additive: any remap with a non-empty `from.system` behaves exactly as before.

# v0.2.1 (2026-07-01)

### Documentation

* Publish registry documentation for the `medplum_fhir_data_migration` resource and `medplum_fhir_search` data source (schema, examples, usage notes), which were added in v0.2.0 but not yet in the generated docs. Also sync `medplum_fhir_resource` docs, which were missing the `validation` attribute added in v0.1.6. No functional changes.

# v0.2.0 (2026-07-01)

### Features

* **medplum_fhir_data_migration:** add a resource that performs idempotent, chunked, self-limiting bulk code-remaps over live FHIR resources via Medplum batch/transaction bundles. It rewrites matching `Coding`s (e.g. `QuestionnaireResponse` answers) as a coordinated step inside `terraform apply` — for keeping stored data consistent when a managed `ValueSet`/`CodeSystem` changes its codes. It is a task resource (records that a migration ran at a given transform hash, rather than tracking drift): Create/Update run a converging scan→remap→tag→write loop, Read is inert, and Delete is warn-only. Idempotency comes from a fixed-point transform, a `meta.tag` marker with a `_tag:not` self-limiting scan, and a `ModifyPlan` that keeps a no-op apply an empty plan. `batch` bundles are the default (per-entry, non-atomic; a failed page surfaces an error and re-`apply` resumes since migrated resources are skipped), with `transaction` available opt-in.

* **medplum_fhir_search:** add a data source that reports the count (`Bundle.total`) of resources matching a FHIR search (via `_summary=count`), for previewing migration scope.

### Reliability

* **client:** add `FHIRSearch` (raw-query search) and `FHIRBundle` (batch/transaction bundle POST) to the FHIR client, reusing the existing `Retry-After`-aware retry and `OperationOutcome` error handling.

# v0.1.7 (2026-06-29)

### Bug Fixes

* **fhir_resource:** stop pinning server-managed `version_id`/`last_updated` to their prior value when the FHIR body changes. The `UseStateForUnknown` modifier added in v0.1.6 was unconditional, so on an in-place update Terraform planned the old metadata values while Medplum assigned new ones on write, failing the apply with "Provider produced inconsistent result after apply". They are now held only when the body is semantically unchanged (no-op plan / post-import) and left "known after apply" when it changes. The no-op and import diff-suppression behaviour from v0.1.6 is preserved.

### CI

* Gate tag releases on the acceptance suite. The acceptance job is now a reusable workflow invoked by both `ci.yml` and `release.yml`, so a provider that fails acceptance can no longer be tagged and published (the update bug above shipped in v0.1.6 despite the acceptance test already covering it, because the release workflow did not run it).

# v0.1.6 (2026-06-09)

### Bug Fixes

* **fhir_resource:** compare FHIR arrays order-insensitively in the drift check — Medplum reorders array elements (e.g. `CodeSystem.concept`, `compose.include`, extensions) on write, which previously surfaced as spurious diffs. A genuinely changed/added/removed element is still detected.
* **fhir_resource:** make the new `validation` attribute Optional-only (no Computed default) so it does not surface as a spurious `+ validation` diff on every imported resource.

* **fhir_resource:** stabilize server-managed `version_id`/`last_updated` with `UseStateForUnknown` so `terraform import` (and any no-op plan) no longer shows them flipping to "(known after apply)" as a spurious in-place update.

### Features

* **fhir_resource:** add a `validation` attribute (`error` default | `warning` | `none`) controlling how FHIR R4 schema-validation results are reported, for resources that intentionally use Medplum-accepted constructs outside strict R4.

* **client:** treat a read that returns HTTP 200 with an error `OperationOutcome` (e.g. a Gravitee gateway intermittently answering with `200 + "Not found"` instead of the resource) as a transient error: retry it with backoff and, if it persists, surface an error rather than storing the OperationOutcome as the resource body. Mirrors Medplum's own retry philosophy; a success/information OperationOutcome (delete response) is unaffected.

# v0.1.5 (2026-06-09)

### Bug Fixes

* **fhir_resource:** suppress the plan diff when config is a subset of the stored body (use `Contains`, not `Equal`, in the body plan modifier). After `terraform import` the state holds the full server body (server-managed `meta`, narrative `text`, defaults), so comparing it for strict equality against the user's config subset marked every imported resource as needing an update. Now mirrors the Read drift check.

# v0.1.4 (2026-06-09)

### Bug Fixes

* **fhir_resource:** treat an empty array in config as equal to the server omitting that field. FHIR forbids empty arrays, so Medplum drops them on write; the drift check previously flagged config `header: []` (etc.) vs the server's omitted field as a spurious diff, marking nearly every imported resource as needing an update.

# v0.1.3 (2026-06-09)

### Bug Fixes

* **client:** auto-detect the OAuth client-credentials auth style (`AuthStyleAutoDetect`) instead of hardcoding body params. A Gravitee AM token endpoint fronting Medplum requires `client_secret_basic` and rejected params with `invalid_client: missing or unsupported authentication method`. Probes Basic first, falls back to params for direct Medplum.

# v0.1.2 (2026-06-09)

### Bug Fixes

* **client:** detach the OAuth token source from the Configure-time context. Terraform cancels that context once Configure returns, so client-credentials token fetches during CRUD failed with "context canceled" on larger plans/applies (only the first token succeeded). Token fetches now use a background context, matching the email/password path.

# v0.1.1 (2026-06-09)

### Security

* **deps:** bump `google.golang.org/grpc` (1.79.3), `golang.org/x/crypto` (0.46.0), `golang.org/x/net` (0.48.0), and `github.com/cloudflare/circl` (1.6.3) to patched versions, clearing 9 Dependabot advisories (2 critical, 1 high, 4 moderate, 2 low). Applies #11.

# v0.1.0 (2026-06-09)

### Features

* **medplum_fhir_resource:** manage any FHIR R4 resource stored in Medplum by resource type and ID
* **medplum_access_policy:** manage Medplum Access Policy resources controlling per-resource-type read/write permissions
* **medplum_client_application:** manage Medplum ClientApplication resources for machine-to-machine OAuth 2.0 clients
* **medplum_project_membership:** manage Medplum ProjectMembership resources binding profiles to projects
* **medplum_user:** manage Medplum User resources
* **medplum_project:** manage Medplum Project resources
* **medplum_fhir_profile:** manage FHIR StructureDefinition (profile) resources stored in Medplum

### Reliability

* **client:** retry transient `429`/`502`/`503`/`504` responses with `Retry-After`-aware exponential backoff, so large concurrent applies ride out Medplum throttling instead of failing
