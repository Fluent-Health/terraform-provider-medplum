# Unreleased

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
