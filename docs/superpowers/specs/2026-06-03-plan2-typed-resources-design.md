# Plan 2 — Typed Resources + Auth Refresh — Design

* Status: proposed
* Author: Ivan Kerin (ivan.kerin@fluentinhealth.com)
* Date: 2026-06-03
* Builds on: `2026-06-03-terraform-provider-medplum-design.md` (milestone-1 spec) and the merged
  Plan 1 foundation.

## Context

Plan 1 delivered the provider foundation: gateway-agnostic auth, the Medplum FHIR client, the
embedded R4 validator, JSON drift utilities (subset-containment `Contains`, `semanticJSONBody` plan
modifier), and the generic `medplum_fhir_resource` — all green in live-Medplum CI.

Plan 2 adds the typed abstraction resources over Medplum's intricate surface, plus one auth
hardening item surfaced by Plan 1. The `medplum_fhir_profile` feature (issue #1) is **explicitly
deferred to Plan 3**, preceded by an empirical spike (see Roadmap).

## Decisions (locked during brainstorming)

| Decision | Choice |
| --- | --- |
| Decomposition | **Two sequential plans**: Plan 2 = auth refresh + 5 typed resources; Plan 3 = `medplum_fhir_profile` after a spike |
| Resources in Plan 2 | `access_policy`, `client_application`, `project_membership`, `user`, `project` |
| `medplum_bot` | **Deferred** (not in Plan 2) |
| Auth | Add **refreshable login token source** |
| Profile resource | **Spike first**, then its own spec→plan (Plan 3) |
| Build pattern | Each resource is a thin 1:1 FHIR-CRUD wrapper reusing Plan-1 client + drift mechanics |

## Goals (Plan 2)

1. Make super-admin login produce a **refreshing** token (long applies must not fail mid-run).
2. Implement five typed resources, each with unit tests + a live-Medplum acceptance test.
3. Reuse the Plan-1 drift model (`Contains` + `semanticJSONBody`) and client throughout.

## Non-Goals (Plan 2)

* `medplum_bot`, `medplum_fhir_profile`, the `invite` flow, large-CodeSystem import, custom-profile
  validation (Plan 3+ / future).
* Bot `$deploy`/`$execute`, AccessPolicy terminology-aware validation.

## Component 1 — Refreshable login token source

**Problem (Plan-1 handoff):** `Config.login()` returns a non-refreshing `oauth2.StaticTokenSource`.
A long `terraform apply` (e.g. `project` + several `project_membership` + `user`) can outlive the
token; client-credentials already auto-refreshes, login does not.

**Design:**
* Capture `expires_in` from the `/oauth2/token` response into `oauth2.Token.Expiry`
  (`time.Now().Add(expiresIn * time.Second)`), with a sensible default if absent.
* In `tokenSource()`, for the login method return
  `oauth2.ReuseTokenSource(nil, &loginTokenSource{cfg: c, ctx: ctx})` where
  `loginTokenSource.Token()` runs the full PKCE login flow. `ReuseTokenSource` caches until expiry,
  then re-invokes `Token()` — transparently re-logging-in.
* Keep the PKCE/profile-selection logic factored so both the first call and refresh reuse it.
* Note: each refresh is one `/auth/login`; Medplum throttles login to 5/min. CI already disables
  the limiter (`MEDPLUM_DEFAULT_RATE_LIMIT=-1`); document that production callers with very long
  applies should prefer client-credentials.

**Tests:** httptest with a short `expires_in`; assert a second `Token()` after expiry triggers a
fresh `/auth/login` (count requests), and that a non-expired second call does not.

## Component 2 — Typed resources

Full attribute schemas are in the milestone-1 spec; this section records the build order and the
Plan-1 learnings applied. Each resource: `Configure` pulls `providerData`; CRUD via the FHIR client;
import via `Type/id`; drift via `Contains` + `semanticJSONBody`; unit tests + acceptance test.

Build order (each lands independently):

1. **`medplum_access_policy`** — typed `resource {}` blocks (resource_type, criteria, readonly,
   hidden_fields, readonly_fields, compartment, write_constraint) + optional `ip_access_rule`,
   compiled to the `AccessPolicy.resource[]` array. FHIR CRUD on `AccessPolicy`. No server-side
   secrets. Built first because client/membership reference it.
2. **`medplum_client_application`** — FHIR CRUD on `ClientApplication` (name, description,
   redirect_uri, optional `identity_provider {}` block). `secret` is sensitive: default
   server-generated via `POST /ClientApplication/{id}/$rotate-secret` after create; optional
   explicit `secret` input override. No `access_policy` field (lives on the membership).
3. **`medplum_project_membership`** — generic profile binder. `project`/`user`/`profile` are
   ForceNew; `access_policy`/`access[]`/`admin` update in place. **Plain FHIR `DELETE`** (not the
   admin cascade) + a **project-owner guard** (refuse to delete/manage the owner membership).
   Surfaces `Contains` errors (Plan-1 hardening).
4. **`medplum_user`** — FHIR CRUD on `User` (first_name, last_name, email, external_id,
   scope→`project` reference, admin, mfa_required) + optional **write-only `password`** applied via
   `POST /admin/projects/{project_id}/setpassword` (no email). Plan-time validation: `password`
   requires `email` + project scope.
5. **`medplum_project`** — create via `POST /{fhir_path}/Project/$init`; read/update/delete via
   `/{fhir_path}/Project/{id}`. **Requires super-admin auth**; emit a clear diagnostic if a
   non-super-admin method is configured. Computed `owner` (the auto-created owner membership).

Wiring: register all five in the provider `Resources()` alongside `medplum_fhir_resource`.

## Component 3 — Testing & CI

* Unit tests per resource (httptest-backed client): CRUD request shaping, drift suppression,
  resource-specific logic (secret rotation, owner guard, setpassword precondition, `$init`).
* Acceptance tests reuse the green Plan-1 pipeline. A typical composition test creates an
  `access_policy`, a `client_application`, and binds them via `project_membership`, asserting the
  client can be read back. `project` acceptance is gated on super-admin creds (the CI super-admin
  login provides them).
* No CI structural changes expected beyond possibly seeding/needs; the docker-compose Medplum +
  `TF_ACC_TERRAFORM_PATH` + disabled rate limiter remain.

## Roadmap — Plan 3: `medplum_fhir_profile` (issue #1)

Deferred. Before a detailed plan, run a focused **empirical spike** (its findings become the Plan 3
spec inputs):

* **Spike A — SD-vs-server drift.** Apply a representative `StructureDefinition` to our docker
  Medplum, read it back, and diff input vs server representation to characterize normalization
  (element ordering, defaulted fields, snapshot handling). Output: the equivalence/normalization
  rule for profile drift.
* **Spike B — validator support matrix.** Verify issue #1's reject/warn/enforced classification
  against the pinned `@medplum/core` (snapshot-only validation; slicing discriminators value/
  pattern/type only; `getNestedProperty` paths; extension `url`-fixed matching; unread slicing
  rules). Output: a version-pinned support matrix the validator encodes.

Then plan: Phase 1 (SD-consuming resource + reject-empty-snapshot + drift), Phase 2 (the
Medplum-context useful-profile validator + per-profile `plan` report). Phase 3 (FSH→SD CI + IG
Publisher) is mostly outside the provider. Open design questions to resolve during Plan 3:
strict-mode (decorative-only = fail vs warn), and whether Extension `StructureDefinition`s are
managed by this resource.

## Risks & open questions

* **Login refresh vs rate limit:** frequent re-login could approach Medplum's 5/min login throttle
  for pathologically long applies; mitigated in CI, documented for prod (prefer client-credentials).
* **`medplum_user` password path:** `/setpassword` is project-scoped + email-keyed; server-scoped or
  externalId-only users can't use it — enforced at plan time.
* **`medplum_project` super-admin:** the acceptance test needs super-admin; confirm the seeded
  super-admin can create projects via `$init` against the CI instance.
* **`client_application` secret rotation drift:** ensure re-reads with a server-generated secret
  don't produce perpetual diffs (the secret is computed/sensitive and authoritative until changed).
