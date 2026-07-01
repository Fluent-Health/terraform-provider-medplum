# Unreleased

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
