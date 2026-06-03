# Terraform Provider for Medplum — Milestone 1 Design

* Status: proposed
* Author: Ivan Kerin (ivan.kerin@fluentinhealth.com)
* Date: 2026-06-03
* Repo: https://github.com/Fluent-Health/terraform-provider-medplum

## Context and Problem Statement

Fluent Health configures its Medplum EMR through the [`fhir-static-data`](../../../../fhir-static-data)
repo, which uses the generic Mastercard `restapi` Terraform provider to push `*.fhir.json`
files to Medplum's FHIR R4 endpoint, plus an `admin` provider alias for non-RESTful operations
(e.g. `ClientApplication` creation via `POST /admin/projects/{id}/client`).

This works but has sharp edges:

* **No plan-time validation.** Malformed FHIR is only caught when the server rejects it at apply
  time. FHIR has a well-defined R4 JSON schema that can validate any resource at plan time.
* **Intricate, non-RESTful admin flows are awkward.** Creating client applications, projects,
  users, and project memberships does not map cleanly to REST. Today this is worked around with
  separate create/update paths, conditional payloads, and "create-only, then import" comments in
  modules. Some operations (super-admin Project creation) are done by hand.
* **Large CodeSystems are unmanageable.** Importing a CodeSystem with hundreds of thousands of
  concepts via the generic resource is not feasible. (Out of scope for milestone 1; see Future Phases.)

We will build a purpose-built Terraform provider for Medplum, in Go, using the
terraform-plugin-framework, published to the public Terraform Registry.

## Decisions (locked during brainstorming)

| Decision | Choice |
| --- | --- |
| Resource model | **Hybrid**: one generic FHIR resource + a few hand-written typed resources for intricate concepts |
| Implementation framework | **terraform-plugin-framework** (Go) with `terraform-plugin-testing` |
| Distribution | **Public Terraform Registry**, namespace `Fluent-Health/medplum` |
| Plan-time validation | **Base R4 JSON schema** only (no custom profiles in M1) |
| Auth | **Gateway-agnostic, native Medplum methods**: client-credentials, static bearer token, super-admin email+password login |
| CI | **GitHub Actions** against a live Medplum (docker-compose) |
| Sequencing | **One spec**, phased implementation plan |

## Goals (Milestone 1)

1. Provider scaffold + gateway-agnostic, multi-method auth.
2. Generic `medplum_fhir_resource` with plan-time base-R4 JSON-schema validation and stable
   drift handling (server-managed fields ignored). Reaches functional parity with current
   `restapi_object` usage in `fhir-static-data`.
3. Four typed abstraction resources: `medplum_project`, `medplum_project_membership`
   (incl. the admin invite flow), `medplum_client_application`, `medplum_access_policy`.
4. Acceptance tests (`TF_ACC`) run against a live Medplum in GitHub Actions; release pipeline
   publishes signed builds to the Terraform Registry.

## Non-Goals (deferred to later phases)

* Large / imported CodeSystem (100k+ concepts) resource.
* Validation against custom `StructureDefinition` profiles / FHIRPath constraints.
* Fully-typed resources for every R4 resource type (only the generic resource + the four typed
  abstractions in M1).
* Migrating `fhir-static-data` to consume this provider (downstream change, tracked separately).
* Subscriptions, Bots, and other concepts as typed resources (handled by the generic resource in M1).

## Architecture

### Component overview

```
internal/
  provider/       # provider definition, schema, configure (auth method selection)
  client/         # thin Medplum HTTP client: token acquisition + FHIR + admin endpoints
  fhirschema/     # embedded R4 fhir.schema.json + plan-time validator
  resources/      # generic resource + 4 typed resources, each with acceptance tests
  acctest/        # shared acceptance-test helpers (provider factory, env guards)
docs/             # tfplugindocs-generated provider docs
examples/         # registry examples per resource
.github/workflows/ ci.yml (lint+unit+acc), release.yml (goreleaser+gpg)
```

Each unit has one purpose and a clear interface:

* `client` knows how to authenticate and speak HTTP to Medplum; it knows nothing about Terraform.
* `fhirschema` validates a JSON document against R4; it knows nothing about Terraform or HTTP.
* `resources` map Terraform schema/state to `client` calls; they depend on `client` and (for the
  generic resource) `fhirschema`.

### Provider configuration & auth

The provider is **gateway-agnostic**: `base_url` may point at the Gravitee gateway or directly at
Medplum. All Medplum-native auth methods are supported so it works without the gateway (local dev,
CI, non-gateway consumers).

```hcl
provider "medplum" {
  base_url  = "https://gateway.fluent.health/medplum" # gateway OR direct Medplum base
  fhir_path = "/fhir/R4"   # optional; default "/fhir/R4"
  token_url = "..."        # optional; default "${base_url}/oauth2/token"

  # Exactly one auth method (validated in Configure):
  # (a) OAuth2 client-credentials
  client_id     = "..."
  client_secret = "..."
  # (b) pre-obtained bearer token (e.g. a Gravitee-exchanged token, or CI)
  access_token  = "..."
  # (c) super-admin email + password login (required for Project creation)
  email    = "..."
  password = "..."
}
```

* Every attribute is also readable from env (`MEDPLUM_BASE_URL`, `MEDPLUM_CLIENT_ID`,
  `MEDPLUM_CLIENT_SECRET`, `MEDPLUM_ACCESS_TOKEN`, `MEDPLUM_EMAIL`, `MEDPLUM_PASSWORD`,
  `MEDPLUM_FHIR_PATH`, `MEDPLUM_TOKEN_URL`).
* All secret-bearing attributes are marked `Sensitive`.
* `Configure` validates that exactly one auth method is fully specified and constructs the
  `client`. The `client` handles token acquisition/refresh internally:
  * client-credentials → `POST {token_url}` grant `client_credentials`.
  * static token → used as-is as `Authorization: Bearer`.
  * email/password → `POST {base_url}/auth/login` then exchange for an access token.
* The `client` exposes method-agnostic helpers: `FHIRCreate/Read/Update/Delete`, `FHIRSearch`,
  and admin helpers (`AdminCreateClient`, `AdminInvite`, `AdminCreateProject`).

### Generic FHIR resource: `medplum_fhir_resource`

Holds an arbitrary R4 resource. Mirrors the current `.fhir.json` workflow while adding validation
and stable drift handling.

```hcl
resource "medplum_fhir_resource" "languages" {
  resource_type = "ValueSet"                  # required, validated against known R4 types
  body          = file("ValueSet/languages.fhir.json")  # or jsonencode({...})
}
```

Schema:

| Attribute | Type | Notes |
| --- | --- | --- |
| `resource_type` | string, required, ForceNew | Must match `body.resourceType`; used to build the path `/{fhir_path}/{resource_type}`. |
| `body` | string (JSON), required | The FHIR resource as JSON. Validated at plan time. |
| `id` | string, computed | Server-assigned id. |
| `version_id` | string, computed | From `meta.versionId`. |
| `last_updated` | string, computed | From `meta.lastUpdated`. |

Behavior:

* **Plan-time validation** (`ValidateConfig` / a plan modifier): parse `body`; assert it is a JSON
  object, that `resourceType` is present and equals `resource_type`, that `id` is **not** set
  (mirrors the current precondition — ids are server-assigned), and validate the document against
  the embedded R4 JSON schema. Validation errors are returned as Terraform diagnostics with the
  JSON path.
* **Create** → `POST /{fhir_path}/{resource_type}`; store `id`, `version_id`, `last_updated`.
* **Read** → `GET /{fhir_path}/{resource_type}/{id}`.
* **Update** → `PUT /{fhir_path}/{resource_type}/{id}` (re-inject the stored `id` into the body).
* **Delete** → `DELETE /{fhir_path}/{resource_type}/{id}`.
* **Drift handling**: comparison is on a **canonicalized** form of `body` with server-managed
  fields removed (`id`, `meta.versionId`, `meta.lastUpdated`, and `meta` if it becomes empty).
  A semantic JSON-equality plan modifier prevents perpetual diffs from key ordering or
  server-added metadata. The user's configured `body` is preserved in state verbatim; the
  normalized form is used only for diffing.
* **Import**: `terraform import medplum_fhir_resource.x ValueSet/{id}`.

**Input format decision:** `body` is a JSON string (not a typed/`dynamic` object). This keeps
parity with the existing `.fhir.json` files, avoids hand-modeling every R4 type, and preserves
fidelity for arbitrary resources. The typed-object alternative was rejected for M1 (huge surface,
worse round-trip fidelity).

### Typed abstraction resources

Each is a thin, hand-modeled resource that hides a non-RESTful or multi-step Medplum flow behind a
clean Terraform schema. Full attribute schemas below.

#### `medplum_client_application`

Hides the create/update asymmetry seen today (`POST /admin/projects/{id}/client` to create —
returning a `secret` — vs `PUT /fhir/R4/ClientApplication/{id}` to update; `accessPolicy` sent only
on create).

```hcl
resource "medplum_client_application" "search" {
  project_id     = var.emr_project_id
  name           = "Search Service"
  description    = "Client application for search functionality"
  access_policy  = "AccessPolicy/${medplum_access_policy.search.id}"  # optional
  redirect_uri   = "..."   # optional
  # identity_provider { authorize_url, token_url, user_info_url, client_id, client_secret, use_subject }  # optional block
}
# computed: id, secret (sensitive)
```

* **Create** → `POST /admin/projects/{project_id}/client`; capture `id` + `secret`.
* **Read/Update** → `GET`/`PUT /{fhir_path}/ClientApplication/{id}`.
* **Delete** → `DELETE /{fhir_path}/ClientApplication/{id}`.
* `secret` is `Sensitive`, computed; consumers wire it to GCP Secret Manager themselves
  (the provider does not touch GCP).

#### `medplum_access_policy`

Typed model over the `AccessPolicy` resource (managed as raw JSON today).

```hcl
resource "medplum_access_policy" "patient" {
  name = "Patient Template"
  resource {
    resource_type = "Patient"
    criteria      = "Patient?_id=%patient.id"
    readonly      = false
    hidden_fields = ["..."]
    # readonly_fields, compartment, write_constraint as needed
  }
  resource { resource_type = "Observation" /* ... */ }
  # ip_access_rule { ... }  # optional
}
# computed: id
```

* CRUD via `/{fhir_path}/AccessPolicy`. The typed `resource` blocks compile to the AccessPolicy
  `resource[]` array; round-trips normalized like the generic resource.

#### `medplum_project`

Super-admin create/configure of a Medplum Project. Requires the super-admin login auth method.

```hcl
resource "medplum_project" "tenant" {
  name          = "..."
  description   = "..."
  features      = ["bots", "cron"]   # optional
  default_patient_access_policy = "AccessPolicy/..."  # optional
  # strict_mode, setting{}, system_setting{} as needed
}
# computed: id
```

* **Create** → `POST /admin/projects` (super-admin). **Read/Update/Delete** via super-admin
  FHIR `/{fhir_path}/Project/{id}`.
* Provider surfaces a clear error if a non-super-admin auth method is configured.

#### `medplum_project_membership`

The most intricate resource: links a User to a Project with an access-policy assignment and admin
flag. Supports two modes via a discriminating attribute, kept in one resource so the membership is
the single managed object:

```hcl
# Mode A: invite (user does not yet exist) — atomic User + profile + membership
resource "medplum_project_membership" "clinician" {
  project_id = var.emr_project_id
  invite {
    profile_resource_type = "Practitioner"   # or "Patient"
    first_name            = "Jane"
    last_name             = "Doe"
    email                 = "jane@example.com"
    send_email            = false
  }
  access_policy = "AccessPolicy/${medplum_access_policy.clinician.id}"
  admin         = false
}

# Mode B: bind an existing user/profile to the project
resource "medplum_project_membership" "bot_membership" {
  project_id    = var.emr_project_id
  user          = "User/..."       # existing user reference
  profile       = "Bot/..."        # existing profile reference
  access_policy = "AccessPolicy/..."
  admin         = false
}
```

* **invite mode** → `POST /admin/projects/{project_id}/invite` with the profile + membership
  payload; the response yields the created `ProjectMembership` (and `User`/profile). Store the
  membership `id`.
* **bind mode** → `POST /{fhir_path}/ProjectMembership`.
* **Read/Update/Delete** → `/{fhir_path}/ProjectMembership/{id}`. Updating `access_policy`/`admin`
  is a `PUT`. `invite` block attributes are `ForceNew` (changing identity recreates).
* Validation: exactly one of (`invite` block) or (`user`+`profile`) must be set.

## Testing strategy

* **Unit tests** (always run, no network):
  * `fhirschema`: valid/invalid documents per resource type, missing `resourceType`, wrong type,
    `id`-present rejection.
  * `client`: auth-method selection, token acquisition (httptest server), request shaping for
    admin vs FHIR endpoints.
  * Body normalization/diff suppression: key reordering and server metadata produce no diff.
* **Acceptance tests** (`TF_ACC=1`, live Medplum): per resource — create/read/update/delete,
  `ImportState`, and `ExpectNonEmptyPlan(false)` on no-op re-apply (drift stability). Project and
  super-admin paths gated on super-admin credentials being present.

## Live-Medplum CI (GitHub Actions)

* `ci.yml` on PR/push:
  * `lint` job: `golangci-lint`, `terraform fmt` check on examples, `tfplugindocs` drift check.
  * `unit` job: `go test ./...` (no `TF_ACC`).
  * `acc` job: start Medplum via `docker-compose` (Postgres + Redis + `medplum-server`) as
    services; wait for health; seed/register a super-admin and a project; export
    `MEDPLUM_*` env; run `TF_ACC=1 go test ./...`.
* `release.yml` on tag `v*`: GoReleaser builds cross-platform archives, signs with the GPG key
  (repo secret), publishes the GitHub release. Registry publishing uses the standard
  Terraform Registry GitHub App for `Fluent-Health/terraform-provider-medplum`.
* Pin the Medplum server image version explicitly (consistent with the `fh:upgrades` discipline);
  document the supported Medplum version range in the README.

## Distribution

* Public GitHub repo `Fluent-Health/terraform-provider-medplum`; registry address
  `registry.terraform.io/Fluent-Health/medplum`.
* `LICENSE` (e.g. MPL-2.0 or Apache-2.0 — confirm during release), `docs/` generated via
  `tfplugindocs`, `examples/` per resource. Follow the `fh:publish-opensource` process for the
  public release (secret scan, CTO approval issue, SECURITY.md/CONTRIBUTING.md).

## Implementation phasing (for the plan)

One spec, but the plan should land these independently and in order:

1. **Foundation**: repo scaffold, provider skeleton, `client` with all three auth methods + tests,
   CI skeleton (lint + unit). Dev-override consumption documented.
2. **Generic resource**: `fhirschema` validator (embed R4 schema) + `medplum_fhir_resource` with
   drift handling; acceptance test + live-Medplum CI job.
3. **Typed resources**: `medplum_access_policy`, then `medplum_client_application`, then
   `medplum_project_membership` (invite + bind), then `medplum_project` (super-admin).
4. **Release**: `tfplugindocs`, examples, GoReleaser + signing, registry publish.

## Risks & open questions

* **Medplum API specifics** (exact admin payload/response shapes for invite, client creation,
  super-admin project creation) must be verified against the pinned Medplum version during
  implementation — treat the endpoint paths above as the current best understanding from
  `fhir-static-data` + Medplum docs, to be confirmed by acceptance tests.
* **R4 schema embedding**: the existing `fhir.schema.json` (~61k lines) will be embedded via
  `go:embed`; validator choice (e.g. `santhosh-tekuri/jsonschema`) and startup cost to be
  validated.
* **Super-admin in CI**: seeding a super-admin against a fresh docker-compose Medplum needs a
  reproducible bootstrap; confirm the supported mechanism for the pinned version.
* **`medplum_project_membership` modes** in one resource vs two resources — revisit if the
  discriminator proves awkward in practice.

## Future phases (not in this spec)

* `medplum_codesystem_import` / "large CodeSystem" resource for 100k+ concepts via Medplum's
  CodeSystem import operation (out-of-band upload, async import, state tracking).
* Custom `StructureDefinition` profile validation at plan time.
* Additional typed resources (Subscription, Bot, etc.) as demand warrants.
* Migrating `fhir-static-data` to consume this provider.
