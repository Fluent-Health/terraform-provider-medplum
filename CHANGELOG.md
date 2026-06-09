# Unreleased

### Bug Fixes

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
