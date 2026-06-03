# OSS Release Readiness — Design

* Status: proposed
* Author: Ivan Kerin (ivan.kerin@fluentinhealth.com)
* Date: 2026-06-03
* Builds on: merged Plans 1–3 (provider + typed resources + profile validator).
* References: `terraform-provider-typesense` (OSS/CI template), `infra/stacks/pipelines` (pipeline pattern), `fhir-static-data` (import target), local `medplum` checkout (version matrices).

## Context

Plans 1–3 delivered a working, live-CI-green Medplum provider (generic + 6 typed resources + the
profile validator). This effort makes it **publishable OSS, import-ready for our real config,
version-accurate for the Medplum we actually run, CI/CD-hardened (no warnings) with matching infra,
and demonstrably end-to-end** via a FSH→SD→provider acceptance test.

Investigation established (all cited in the brainstorm):
- **Deployed Medplum = `5.0.10`** (`infra-service-projects/env/*/main.tf`); the validator was verified
  against `v5.1.14`. Medplum's `@medplum/core` profile-validation changed between these versions, so
  the matrix must be version-keyed.
- The generic `medplum_fhir_resource` already covers ~99% of `fhir-static-data` importably; only **3
  typed-resource field gaps** remain.
- `terraform-provider-typesense` is a complete OSS/CI template; Node-20 CI warnings are removed by
  bumping action majors.
- No Go-provider pipeline exists in `infra`; the `github_actions`-deployment stack pattern is the fit.

## Decisions (locked during brainstorming)

| Decision | Choice |
| --- | --- |
| License | **Apache-2.0** (matches Medplum; net-new code, single Fluent Health copyright) |
| Version matrix | **Version-keyed** in `fhirprofile`; provider `medplum_version` config **defaults to `5.0.10`**; verify the 5.0.10 matrix against `v5.0.10` source; align CI image to `5.0.10` |
| Import gaps | **Add all 3 typed fields**: `client_application.identity_provider`, `access_policy.interaction`, `access_policy` top-level `compartment` |
| Release/CI | **GitHub Actions + GoReleaser** → public Terraform Registry; infra provides WIF via a `github_actions` pipeline stack |
| FSH e2e | **SUSHI + HL7 IG Publisher** (snapshot-bearing SD) → `medplum_fhir_profile`, live in CI acceptance |
| Internal planning docs | **`docs/superpowers/` excluded from the OSS repo** (process artifacts referencing internal repos) |

## Goals

Five workstreams, sequenced as phases (one spec, one phased plan):

1. **Import coverage** — close the 3 typed-resource gaps so every `fhir-static-data` resource is
   manageable + importable into a typed resource with a no-op plan.
2. **Version-aware matrix** — `fhirprofile` selects a support matrix by Medplum version (default
   `5.0.10`); CI tests against `5.0.10`.
3. **OSS readiness** — Apache-2.0 + the full OSS file set, generated docs/examples, no internals.
4. **CI/CD hardening + infra** — warning-free workflows, GoReleaser release pipeline, and the
   `infra` pipeline stack.
5. **FSH→SD e2e** — a complex FSH profile compiled (SUSHI + IG Publisher) and applied via the
   provider in CI acceptance.

## Non-Goals

- Publishing to the Registry itself (a one-time manual registry registration + GPG key upload, and
  flipping the repo public — gated by `fh:publish-opensource` approval). We prepare everything; the
  flip is a separate human step.
- Migrating `fhir-static-data` to consume the provider (downstream change; this effort only proves
  importability).
- The deferred resources/attributes from earlier plans (bot, invite, large CodeSystem, etc.).

## Phase 1 — Import coverage (3 typed-resource fields)

Add the fields `fhir-static-data` uses so its resources import cleanly into typed resources:

- **`medplum_client_application.identity_provider`** (nested block): `authorize_url`, `token_url`,
  `user_info_url`, `client_id`, `client_secret` (sensitive), `use_subject` (bool) → FHIR
  `ClientApplication.identityProvider`. Used by the token-exchange client.
- **`medplum_access_policy.resource[].interaction`** (list of strings) → `AccessPolicyResource.interaction`.
  Used by `BoomUserTemplate`.
- **`medplum_access_policy.compartment`** (top-level string reference) → `AccessPolicy.compartment`
  (distinct from the existing per-resource-row `compartment`). Used by 5 templates (e.g. `%profile`).

Each maps via the existing `toFHIR`/`fromFHIR` pattern (string → `{reference}` for compartment;
list/optional handling consistent with Plan-2 conventions). Unit tests per field; the acceptance
tests gain a case exercising `identity_provider` and top-level `compartment`. After this, a documented
`terraform import` of a representative `fhir-static-data` resource of each type yields a no-op plan
(verified by an acceptance import test where feasible; the full migration remains downstream).

## Phase 2 — Version-aware support matrix

`internal/fhirprofile` becomes version-keyed:
- Introduce `Analyze(sdJSON []byte, version string) (Report, error)` (keep the current `Analyze` as a
  thin wrapper defaulting to the latest, or update call sites). A small internal `matrix` describes
  the version-dependent classification; a `matrixFor(version)` resolver picks `5.0.10` vs latest
  (`5.1.x`), falling back to the latest with a WARN finding for unknown versions.
- **Verify the `5.0.10` matrix against `v5.0.10` source** (`git show v5.0.10:packages/core/src/typeschema/{validation,types,crawler}.ts`).
  The diff vs HEAD touches `validation.ts` (+67/-19), `types.ts`, `crawler.ts` — re-confirm each
  reject/warn/enforced rule at 5.0.10 and record any divergence in the matrix + the spike-findings doc.
  If a rule is identical across versions, both keys share it.
- Provider config gains **`medplum_version` (string, optional, default `"5.0.10"`)**; the
  `medplum_fhir_profile` resource passes it to `Analyze`. Document the supported set.
- **Align CI**: `docker-compose.test.yml` Medplum image → `5.0.10`; the `bootstrap-medplum.md`
  default-admin/version notes updated. (Re-confirm the auth/rate-limit/setpassword behaviors still
  hold at 5.0.10 on the first CI run — same iteration loop as before.)

## Phase 3 — OSS readiness

Add (mirroring typesense, adapted; **no `.copywrite.hcl`** — HashiCorp-internal):
- **`LICENSE`** (Apache-2.0, `Copyright 2026 Fluent Health`).
- **`CONTRIBUTING.md`** (dev prereqs, `docker compose -f docker-compose.test.yml up -d`, unit/acc test
  + `make doc` flow, PR checklist, release-by-tag, no-CLA).
- **`SECURITY.md`** (GitHub private vulnerability reporting, 5-business-day SLA).
- **`CHANGELOG.md`** (`# Unreleased` + conventional-commit sections).
- **`terraform-registry-manifest.json`** (`{"version":1,"metadata":{"protocol_versions":["6.0"]}}`).
- **`tools/tools.go`** (`//go:build tools` blank import of `tfplugindocs`) + `go get` the dep.
- **`main.go`** `//go:generate` directives: `terraform fmt -recursive ./examples/` and `tfplugindocs
  generate -provider-name medplum`.
- **`Makefile`** `doc` target (`go generate ./...`).
- **`templates/`** (`index.md.tmpl` + one per resource) + generated **`docs/`** (committed).
- **`examples/`** — `provider/provider.tf` + `resources/<resource>/resource.tf` for every resource
  (the usage examples requested), all using generic placeholders (`https://medplum.example.com`, no
  internal hostnames/secret names/project ids).
- **`catalog-info.yaml`** (Backstage component, `Fluent-Health/terraform-provider-medplum`,
  `type: library`, owner devops, `fluentinhealth.com/oss: pending`).
- **`README.md`** — description, resources table, requirements (Terraform / Go / Medplum version),
  quick-start HCL, build/test, releasing, contributing, license.
- **`.tool-versions`** (`terraform <pinned>`).
- **Scrub internals:** exclude `docs/superpowers/` from the published repo (move under a top-level
  `.gitignore` entry or relocate to an internal location — keep locally, don't ship); grep the repo
  for internal hostnames/repo names/secret ids and ensure none ship in code/docs/examples/README.

## Phase 4 — CI/CD hardening + infra pipeline

**Workflows (`.github/workflows/`):**
- Bump to warning-free majors: `actions/checkout@v6`, `actions/setup-go@v6`,
  `golangci/golangci-lint-action@v9` (drop the hard `version: v1.61` pin), `hashicorp/setup-terraform@v4`.
- Add **`release.yml`** (tag `v[0-9]+.[0-9]+.[0-9]+`, env `release`): `fetch-depth: 0`,
  `setup-go` from `go.mod`, `crazy-max/ghaction-import-gpg@v7`, `goreleaser/goreleaser-action@v7`
  `release --clean`, `GPG_FINGERPRINT` wired.
- **`.goreleaser.yml`** (the typesense content verbatim — multi-OS/arch, `_v{{.Version}}` binary,
  zip archives, checksum + manifest extra_files, GPG sign checksum).
- Upgrade **`.golangci.yml`** to v2 schema (formatters block, exclusions presets,
  `goimports.local-prefixes = github.com/Fluent-Health/terraform-provider-medplum`).
- Add **`.github/CODEOWNERS`** (`* @Fluent-Health/devops`) and **`dependabot.yml`** (gomod +
  github-actions, monthly).
- Document the **manual** one-time registry setup (GPG key upload, `release` environment secrets
  `GPG_PRIVATE_KEY`/`PASSPHRASE`, registry registration) in `CONTRIBUTING.md`/README — not automatable here.

**Infra (`infra/stacks/pipelines/terraform-provider-medplum/`):**
- New Terramate stack mirroring the `github_actions`-deployment pattern (`testing-android` shape):
  `pipeline.tm.hcl`, `module/{main,variables,provider}.tf`, `nonprod/` + `prod/` substacks with
  `stack.tm.hcl` + `main.tf`. `module/main.tf` uses `modules/pipeline` with `trigger = { none = true }`
  and `deployment = { github_actions = {} }`, exposing the WIF provider for the release workflow's
  `google-github-actions/auth`. Single nonprod target is sufficient (no per-env deploy artifact).
  Run `terramate generate` to emit the `_terramate_generated_*` files. (Authoring is in the `infra`
  repo, not this one.)

## Phase 5 — FSH → SD → provider end-to-end

A complex profile proven through the real toolchain in CI acceptance:
- **Fixture:** `test/fsh/` (excluded from the provider build) — `sushi-config.yaml` + a complex
  `.fsh` profile (slicing on a value/pattern discriminator, a required extension with a fixed `url`,
  cardinality + fixed/pattern constraints — i.e. constructs the validator classifies as ENFORCED,
  plus at least one decorative one to exercise the report).
- **CI step (acceptance job):** install Node + `fsh-sushi`, run SUSHI; then run the **HL7 IG
  Publisher** (`_genonce`/`sushi`+publisher) to generate the **snapshot**; the snapshot-bearing
  `StructureDefinition-*.json` is written to a known path.
- **Acceptance test:** `TestAccFHIRProfile_fromFSH` reads the generated SD (via an env var pointing at
  the path, gated on TF_ACC + the file existing) and applies it through `medplum_fhir_profile` — asserting
  create succeeds, the validator reports enforced constraints (no rejects), and a no-op re-plan. This
  proves FSH→SUSHI→IG-Publisher→SD→provider→Medplum end-to-end.
- Heavier CI (Java + publisher); cache the publisher and FHIR packages. If the publisher proves too
  slow/flaky, fall back to committing the generated SD as a fixture (documented), but the default is
  the live chain.

## Testing & CI summary

- Phases 1–2: Go unit tests + live acceptance (existing pipeline, image → 5.0.10).
- Phase 5: a new acceptance test gated on the SUSHI/publisher artifact, in the same `acceptance` job
  (or a dedicated job that produces the SD then runs the test).
- All workflows warning-free; `make doc` keeps `docs/` in sync (a CI drift check optional).

## Risks & open questions

- **5.0.10 matrix divergence:** the `@medplum/core` diff vs 5.1.x must be read rule-by-rule; if a
  REJECT/WARN/ENFORCED classification changed, the matrix encodes both. Verified during Phase 2.
- **5.0.10 server behaviors:** auth/PKCE, rate limit, `/setpassword`, `$init`, SD `sdf` invariants —
  re-confirm on the first CI run against the 5.0.10 image (the prior fixes were found against 5.1.14;
  most are version-stable, but the first 5.0.10 run is the gate).
- **IG Publisher in CI:** Java + network downloads (FHIR packages, publisher jar); cache aggressively;
  documented fallback to a committed SD fixture.
- **Import no-op fidelity:** Phase 1 makes typed import clean for the gap cases; the full
  `fhir-static-data` migration (and importing the admin-endpoint-created ClientApplication
  memberships) remains a downstream exercise, not part of this effort.
