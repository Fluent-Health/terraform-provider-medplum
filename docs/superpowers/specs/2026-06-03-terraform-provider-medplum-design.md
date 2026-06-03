# Terraform Provider for Medplum — Milestone 1 Design

* Status: proposed
* Author: Ivan Kerin (ivan.kerin@fluentinhealth.com)
* Date: 2026-06-03
* Repo: https://github.com/Fluent-Health/terraform-provider-medplum

## Context and Problem Statement

A common way to manage Medplum configuration today is the generic Mastercard `restapi` Terraform
provider: push `*.fhir.json` files to Medplum's FHIR R4 endpoint, plus a second provider alias for
the non-RESTful admin operations (e.g. `ClientApplication` creation via
`POST /admin/projects/{id}/client`).

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
| Client + access model | **Two symmetric FHIR resources** (`client_application` + `project_membership`); no admin `/client` or `/invite` endpoints |
| `project_membership` | **Pure FHIR CRUD** generic profile binder (clients, bots, users) — `accessPolicy` lives here, not on the client |
| Human users | **`medplum_user` via plain FHIR** (no invite email); optional write-only password via `/setpassword`. The `invite` flow is **out of scope** |
| Client secret | **Server-generated via `$rotate-secret`** by default; **optional explicit `secret`** override |

## Goals (Milestone 1)

1. Provider scaffold + gateway-agnostic, multi-method auth.
2. Generic `medplum_fhir_resource` with plan-time base-R4 JSON-schema validation and stable
   drift handling (server-managed fields ignored). Reaches functional parity with the generic
   `restapi`-provider approach.
3. Five typed abstraction resources, each a symmetric 1:1 wrapper over one underlying Medplum
   FHIR resource: `medplum_access_policy`, `medplum_client_application`, `medplum_project_membership`,
   `medplum_user`, `medplum_project`.
4. Acceptance tests (`TF_ACC`) run against a live Medplum in GitHub Actions; release pipeline
   publishes signed builds to the Terraform Registry.

## Non-Goals (deferred to later phases)

* Large / imported CodeSystem (100k+ concepts) resource.
* Validation against custom `StructureDefinition` profiles / FHIRPath constraints.
* Fully-typed resources for every R4 resource type (only the generic resource + the five typed
  abstractions in M1).
* The **`invite` flow** (`POST /admin/projects/:id/invite`): it is imperative and side-effectful
  (sends email, mints password-reset tokens, conditional upserts, SES error semantics) and does
  not fit a declarative resource. Human onboarding via invite is a deliberate later-phase concern;
  `medplum_user` covers IaC-provisioned (machine/externalId/managed-password) users without email.
* Migrating existing `restapi`-based configurations to this provider (downstream change).
* Subscriptions, Bots, and other concepts as typed resources (handled by the generic resource in M1).

## Architecture

### Component overview

```
internal/
  provider/       # provider definition, schema, configure (auth method selection)
  client/         # thin Medplum HTTP client: token acquisition + FHIR + admin endpoints
  fhirschema/     # embedded R4 fhir.schema.json + plan-time validator
  resources/      # generic resource + 5 typed resources, each with acceptance tests
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

The provider is **gateway-agnostic**: `base_url` may point at an API gateway that fronts Medplum or
directly at Medplum. All Medplum-native auth methods are supported so it works without a gateway
(local dev, CI, non-gateway consumers).

```hcl
provider "medplum" {
  base_url  = "https://medplum.example.com" # gateway in front of Medplum, OR direct Medplum base
  fhir_path = "/fhir/R4"   # optional; default "/fhir/R4"
  token_url = "..."        # optional; default "${base_url}/oauth2/token"

  # Exactly one auth method (validated in Configure):
  # (a) OAuth2 client-credentials
  client_id     = "..."
  client_secret = "..."
  # (b) pre-obtained bearer token (e.g. a gateway-exchanged token, or CI)
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
  FHIR operation invocation (`$rotate-secret`, `Project/$init`), and the `/setpassword` admin call.

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

### Medplum source findings (validated against v5.1.14)

These behaviors were confirmed by reading the Medplum server source and drive the design below:

* **`accessPolicy` lives on `ProjectMembership`, not `ClientApplication`.** `createClient`
  (`packages/server/src/admin/client.ts`) creates a `ClientApplication` with a generated `secret`
  **and** a `ProjectMembership` whose `user` and `profile` both reference the client, carrying the
  `accessPolicy`. The admin `/client` endpoint is just that bundle. So one logical "client" is two
  server objects, and access control is set on the membership.
* **The `invite` flow is heavy and imperative** (`packages/server/src/admin/invite.ts`):
  upserts a `User` (bcrypt password hash or password-reset token for new users), upserts a profile
  (Patient/Practitioner/RelatedPerson), upserts a membership, and sends an email — returning
  HTTP 200 + an error `OperationOutcome` when SES is unconfigured. Deliberately **excluded** (see Non-Goals).
* **A plain `POST /fhir/R4/ClientApplication` does not auto-generate a `secret`** — only the admin
  path calls `generateSecret(32)`. But `secret` is settable via FHIR, and there is a
  `POST /fhir/R4/ClientApplication/:id/$rotate-secret` operation (`fhir/routes.ts:394`).
* **User** (`packages/fhirtypes/dist/User.d.ts`) fields: `firstName`, `lastName`, `email`,
  `externalId`, `admin`, `passwordHash`, `mfaRequired`, `project` (presence ⇒ project-scoped;
  absence ⇒ server-scoped). Passwords are set via `POST /admin/projects/:projectId/setpassword`
  (`admin/project.ts:26`) — server-side hash, **no email sent**.
* **Project creation** is the `POST /fhir/R4/Project/$init` operation (`fhir/routes.ts:241`,
  `fhir/operations/projectinit.ts`), which creates the Project and the owner membership.

### Typed abstraction resources

Each is a thin, hand-modeled resource that maps **1:1 to one underlying Medplum FHIR resource**,
with fully symmetric create/update (no admin convenience endpoints, no hidden second object).

#### `medplum_client_application`

Manages only the `ClientApplication` FHIR resource. Access control is attached separately via
`medplum_project_membership` (because `accessPolicy` lives on the membership — see findings).

```hcl
resource "medplum_client_application" "search" {
  project_id   = var.project_id
  name         = "Search Service"
  description  = "Client application for search functionality"
  redirect_uri = "..."   # optional
  secret       = "..."   # optional, sensitive — explicit override; omit to have the server generate one
  # identity_provider { authorize_url, token_url, user_info_url, client_id, client_secret, use_subject }  # optional block
}
# computed: id, secret (sensitive — populated by $rotate-secret when not set explicitly)
```

* **Create** → `POST /{fhir_path}/ClientApplication` (sets `meta.project`). If `secret` is set,
  it is sent in the body; otherwise the provider calls `POST .../ClientApplication/{id}/$rotate-secret`
  so Medplum generates one, then reads it into sensitive computed state.
* **Read/Update** → `GET`/`PUT /{fhir_path}/ClientApplication/{id}`. Changing `secret` (set→unset or
  value change) reconciles via `$rotate-secret` or a `PUT`.
* **Delete** → `DELETE /{fhir_path}/ClientApplication/{id}`.
* `secret` is `Sensitive`; consumers wire it into their own secret store as needed (the provider
  never touches any external secret manager).

#### `medplum_project_membership`

Pure FHIR CRUD over `ProjectMembership` — the **generic profile binder** that attaches any profile
(ClientApplication, Bot, User/Practitioner/Patient) to a project with an access policy and admin
flag. No `invite`, no email, no implicit user/profile creation.

```hcl
# Bind a client application (client is its own user + profile)
resource "medplum_project_membership" "search" {
  project_id    = var.project_id
  user          = medplum_client_application.search.id   # "ClientApplication/xxx"
  profile       = medplum_client_application.search.id
  access_policy = medplum_access_policy.search.id        # "AccessPolicy/xxx"
  admin         = false
}

# Bind an existing user to a project with a profile
resource "medplum_project_membership" "clinician" {
  project_id    = var.project_id
  user          = medplum_user.jane.id
  profile       = "Practitioner/..."
  access_policy = medplum_access_policy.clinician.id
  # access = [{ policy = "...", parameter = [...] }]   # optional parameterized policies
}
```

* CRUD via `/{fhir_path}/ProjectMembership`. `project`, `user`, `profile` are `ForceNew`
  (changing the binding identity recreates); `access_policy`, `access[]`, `admin` update in place.
* Open question (see Risks): whether plain FHIR delete is sufficient or the admin
  `DELETE /admin/projects/:id/members/:membershipId` cleanup is needed.

#### `medplum_user`

Plain FHIR CRUD over the `User` resource — for IaC-provisioned users (machine, externalId/IdP, or
managed-password), **without** the invite email flow.

```hcl
resource "medplum_user" "jane" {
  first_name   = "Jane"
  last_name    = "Doe"
  email        = "jane@example.com"   # optional
  external_id  = "..."                # optional (external IdP)
  scope        = "project"            # "project" (sets meta.project) or "server" (default per Medplum)
  project_id   = var.project_id   # required when scope = "project"
  admin        = false
  mfa_required = false
  password     = "..."                # optional, write-only, sensitive — applied via /setpassword (no email)
}
# computed: id
```

* **Create/Read/Update/Delete** → `/{fhir_path}/User`. When `password` is set, after the
  create/update the provider calls `POST /admin/projects/{project_id}/setpassword` (server-side
  hash, no email). `password` is write-only and never read back; drift on it is not tracked.
* `password` requires `email` + a project scope (constraint of `/setpassword`); validated at plan time.

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

Create/configure a Medplum Project. Requires the super-admin auth method.

```hcl
resource "medplum_project" "tenant" {
  name        = "..."
  description = "..."
  features    = ["bots", "cron"]   # optional
  default_patient_access_policy = "AccessPolicy/..."  # optional
  # strict_mode, setting{}, system_setting{} as needed
}
# computed: id, owner (membership)
```

* **Create** → `POST /{fhir_path}/Project/$init` (creates Project + owner membership).
* **Read/Update/Delete** → `/{fhir_path}/Project/{id}` (super-admin).
* Provider surfaces a clear error if a non-super-admin auth method is configured.

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
* Pin the Medplum server image version explicitly; document the supported Medplum version range
  in the README.

## Distribution

* Public GitHub repo `Fluent-Health/terraform-provider-medplum`; registry address
  `registry.terraform.io/Fluent-Health/medplum`.
* `LICENSE` (Apache-2.0, matching Medplum — confirm during release), `docs/` generated via
  `tfplugindocs`, `examples/` per resource, plus `README.md`, `SECURITY.md`, and `CONTRIBUTING.md`.
* Standard pre-publication hygiene before going public: secret scan of the full history and a
  review that no internal-only references remain in the repo.

## Implementation phasing (for the plan)

One spec, but the plan should land these independently and in order:

1. **Foundation**: repo scaffold, provider skeleton, `client` with all three auth methods + tests,
   CI skeleton (lint + unit). Dev-override consumption documented.
2. **Generic resource**: `fhirschema` validator (embed R4 schema) + `medplum_fhir_resource` with
   drift handling; acceptance test + live-Medplum CI job.
3. **Typed resources** (each symmetric FHIR CRUD, in order): `medplum_access_policy`,
   `medplum_client_application` (+ `$rotate-secret`), `medplum_project_membership`, `medplum_user`
   (+ `/setpassword`), `medplum_project` (super-admin `$init`).
4. **Release**: `tfplugindocs`, examples (incl. the composed client+membership pattern), GoReleaser
   + signing, registry publish.

## Risks & open questions

* **Medplum API specifics confirmed against v5.1.14** (see Medplum source findings) — but the exact
  request/response shapes for `$rotate-secret`, `$init`, `/setpassword`, and ProjectMembership
  delete semantics must still be pinned by acceptance tests against the CI Medplum version.
* **ProjectMembership delete**: confirm plain FHIR `DELETE /ProjectMembership/{id}` fully removes
  access, or whether the admin `DELETE /admin/projects/:id/members/:membershipId` cleanup is required.
* **`medplum_user` password**: `/setpassword` is project-scoped and email-keyed; server-scoped or
  externalId-only users can't use it. Plan-time validation must enforce the `email` + project-scope
  precondition, and treat `password` as write-only (no drift detection).
* **Client secret rotation drift**: when `secret` is unset (server-generated), ensure re-reads do not
  produce perpetual diffs; the stored computed secret must be treated as authoritative until an
  explicit change.
* **R4 schema embedding**: the existing `fhir.schema.json` (~61k lines) will be embedded via
  `go:embed`; validator choice (e.g. `santhosh-tekuri/jsonschema`) and startup cost to be validated.
* **Super-admin in CI**: seeding a super-admin against a fresh docker-compose Medplum needs a
  reproducible bootstrap; confirm the supported mechanism for the pinned version.

## Future phases (not in this spec)

* `medplum_codesystem_import` / "large CodeSystem" resource for 100k+ concepts via Medplum's
  CodeSystem import operation (out-of-band upload, async import, state tracking).
* Custom `StructureDefinition` profile validation at plan time.
* Human onboarding via the `invite` flow (email + password-reset) — likely a dedicated
  `medplum_invite` resource (or a non-resource action) that owns the side effects explicitly.
* Additional typed resources (Subscription, Bot, etc.) as demand warrants.
* Migrating existing `restapi`-based Medplum configurations to consume this provider.
