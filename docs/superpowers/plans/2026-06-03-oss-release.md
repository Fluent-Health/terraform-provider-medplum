# Medplum Provider — OSS Release Readiness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax. This plan is **phased**; each phase (1–5) is an independent execution round with its own branch + PR + green-CI gate.

**Goal:** Make the merged Medplum provider publishable OSS, import-ready for `fhir-static-data`, version-accurate for the deployed Medplum (5.0.10), CI/CD-hardened (no warnings) with matching infra, and proven end-to-end via a FSH→SD→provider acceptance test.

**Architecture:** Five phases — (1) close 3 typed-resource import gaps; (2) version-keyed profile matrix + CI on 5.0.10; (3) OSS file set + generated docs/examples; (4) warning-free CI + GoReleaser release + infra pipeline stack; (5) SUSHI + IG-Publisher → `medplum_fhir_profile` e2e.

**Tech Stack:** Go 1.22+, terraform-plugin-framework, GoReleaser, tfplugindocs, GitHub Actions, Terramate (infra repo), SUSHI + HL7 IG Publisher.

**Spec:** `docs/superpowers/specs/2026-06-03-oss-release-design.md`.

**Conventions:** TDD where code; before each commit `gofmt -w . && go vet ./... && go test ./... -count=1`; Conventional Commits, footer `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>` via `git -c commit.gpgsign=false commit`. One branch per phase (`feat/oss-p<N>-<slug>`), PR, live-CI green, merge — same loop as Plans 1–3.

---

## Phase 1 — Import coverage (typed-resource fields)

**Branch:** `feat/oss-p1-import-fields`. Reference repo for the existing resources: `internal/provider/{client_application_resource,access_policy_resource}.go`.

### Task 1.1: `client_application.identity_provider` block

**Files:** Modify `internal/provider/client_application_resource.go`, `internal/provider/client_application_resource_test.go`.

- [ ] **Step 1: Add the model + schema block.** Add to `clientApplicationModel`:
```go
	IdentityProvider *identityProviderModel `tfsdk:"identity_provider"`
```
and the type + schema block:
```go
type identityProviderModel struct {
	AuthorizeURL types.String `tfsdk:"authorize_url"`
	TokenURL     types.String `tfsdk:"token_url"`
	UserInfoURL  types.String `tfsdk:"user_info_url"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	UseSubject   types.Bool   `tfsdk:"use_subject"`
}
```
In `Schema`, add to `Blocks` (add a `Blocks` map to the schema if not present):
```go
		Blocks: map[string]schema.Block{
			"identity_provider": schema.SingleNestedBlock{
				MarkdownDescription: "External OIDC identity provider for token exchange.",
				Attributes: map[string]schema.Attribute{
					"authorize_url":  schema.StringAttribute{Optional: true},
					"token_url":      schema.StringAttribute{Optional: true},
					"user_info_url":  schema.StringAttribute{Optional: true},
					"client_id":      schema.StringAttribute{Optional: true},
					"client_secret":  schema.StringAttribute{Optional: true, Sensitive: true},
					"use_subject":    schema.BoolAttribute{Optional: true},
				},
			},
		},
```

- [ ] **Step 2: Map in `toFHIR`.** When `m.IdentityProvider != nil`, add an `identityProvider` object (camelCase keys `authorizeUrl`/`tokenUrl`/`userInfoUrl`/`clientId`/`clientSecret`/`useSubject`), omitting empty strings; include `useSubject` only when set.

- [ ] **Step 3: Map in `fromFHIR`.** Decode `identityProvider` into the struct (null block when the server omits it; for each field use `optString`/bool-pointer handling consistent with the codebase).

- [ ] **Step 4: Unit test.** Add `TestClientApplication_toFHIR_IdentityProvider`: a model with an `identity_provider` whose `authorize_url`/`client_id`/`use_subject=true` are set; assert `toFHIR` emits `identityProvider.authorizeUrl`/`clientId`/`useSubject`. Run `go test ./internal/provider/... -run TestClientApplication -v`.

- [ ] **Step 5: gate + commit.** `gofmt -w . && go vet ./... && go build ./... && go test ./... -count=1`. Commit `feat(provider): client_application identity_provider block`.

### Task 1.2: `access_policy` interaction + top-level compartment

**Files:** Modify `internal/provider/access_policy_resource.go`, `internal/provider/access_policy_resource_test.go`.

- [ ] **Step 1: Model.** Add to `accessPolicyModel`: `Compartment types.String \`tfsdk:"compartment"\``. Add to `accessPolicyResourceRow`: `Interaction types.List \`tfsdk:"interaction"\``.

- [ ] **Step 2: Schema.** Top-level attribute `"compartment": schema.StringAttribute{Optional: true, MarkdownDescription: "Top-level compartment reference, e.g. %profile."}`. In the `resource` nested block attributes add `"interaction": schema.ListAttribute{Optional: true, ElementType: types.StringType}` (plain Optional, omit-or-nonempty per the Plan-2 list convention).

- [ ] **Step 3: toFHIR.** When `strOrEmpty(m.Compartment) != ""` set top-level `doc["compartment"] = refObj(...)`. Per row, when `listToStrings(row.Interaction)` is non-empty set `entry["interaction"]`.

- [ ] **Step 4: fromFHIR.** Decode top-level `compartment.reference` → `optString`; per-row `interaction []string` → `stringsToList`.

- [ ] **Step 5: Unit test.** Extend a toFHIR test asserting top-level `compartment.reference` and a row's `interaction` array. Run it.

- [ ] **Step 6: gate + commit.** As above. Commit `feat(provider): access_policy interaction + top-level compartment`.

### Task 1.3: import-fidelity acceptance touch-ups

**Files:** Modify `internal/provider/client_application_resource_test.go`, `internal/provider/access_policy_resource_test.go`.

- [ ] **Step 1:** Add an `identity_provider {}` block to a step in `TestAccClientApplication_basic` (or a new `TestAccClientApplication_identityProvider`) asserting create + a no-op `PlanOnly` re-apply. Add a top-level `compartment` + a row `interaction` to a step in the access-policy acceptance test, asserting no-op plan. Keep TF_ACC-gated.
- [ ] **Step 2:** `go vet ./... && go test ./internal/provider/... -run 'TestAcc' -v` → SKIP locally, compiles. Commit `test(provider): acc coverage for identity_provider, compartment, interaction`.

---

## Phase 2 — Version-aware support matrix (default 5.0.10)

**Branch:** `feat/oss-p2-version-matrix`.

### Task 2.1: Verify the 5.0.10 matrix against source (investigation, no code)

- [ ] **Step 1:** From `/home/ivan/Developer/medplum`, read the v5.0.10 profile-validation source WITHOUT checkout:
```bash
git -C /home/ivan/Developer/medplum show v5.0.10:packages/core/src/typeschema/validation.ts > /tmp/v5010-validation.ts
git -C /home/ivan/Developer/medplum show v5.0.10:packages/core/src/typeschema/types.ts > /tmp/v5010-types.ts
git -C /home/ivan/Developer/medplum show v5.0.10:packages/core/src/typeschema/crawler.ts > /tmp/v5010-crawler.ts
git -C /home/ivan/Developer/medplum diff v5.0.10 v5.1.14 -- packages/core/src/typeschema/validation.ts packages/core/src/typeschema/types.ts packages/core/src/typeschema/crawler.ts > /tmp/v5010-v5114.diff
```
- [ ] **Step 2:** Re-confirm each rule in the spike matrix (`docs/superpowers/specs/2026-06-03-plan3-profile-spike-findings.md`) against the v5.0.10 files: the empty-snapshot throw (`types.ts`), discriminator-type whitelist + `enterSlice` throw, `getNestedProperty` path resolution (`crawler.ts`), extension url-fixed matching, slicing-rules-unread, required-binding `matchDiscriminant` true, mustSupport/targetProfile/constraint. Record, per rule, whether 5.0.10 behaves the SAME as 5.1.x or DIFFERENTLY, with v5.0.10 file:line. Write findings into a new section "## 5.0.10 verification" appended to the spike-findings doc. (No provider code yet.)
- [ ] **Step 3:** Commit the doc update: `docs: verify profile validator matrix against Medplum v5.0.10`.

### Task 2.2: Version-key the validator

**Files:** Modify `internal/fhirprofile/analyze.go`, `internal/fhirprofile/analyze_test.go`.

- [ ] **Step 1: Failing test.** Add `TestAnalyzeForVersion_5010` and `TestAnalyzeForVersion_UnknownWarns`:
```go
func TestAnalyzeForVersion_Selects(t *testing.T) {
	// A rule that holds in all supported versions still classifies correctly.
	r, err := AnalyzeForVersion(sdWith(`[{"id":"Patient","path":"Patient"},{"id":"Patient.active","path":"Patient.active","min":1,"max":"1"}]`), "5.0.10")
	if err != nil { t.Fatal(err) }
	if r.EnforcedCount != 1 { t.Fatalf("got %d", r.EnforcedCount) }
}

func TestAnalyzeForVersion_UnknownWarns(t *testing.T) {
	r, err := AnalyzeForVersion(sdWith(`[{"id":"Patient","path":"Patient"}]`), "9.9.9")
	if err != nil { t.Fatal(err) }
	found := false
	for _, f := range r.Warns() {
		if strings.Contains(f.Message, "unrecognized Medplum version") { found = true }
	}
	if !found { t.Fatal("want a warning for an unrecognized version") }
}
```
(Add `"strings"` to the test imports if needed.)

- [ ] **Step 2: Implement.** Add to `analyze.go`:
```go
// supportedVersions enumerates Medplum versions whose profile-validation behavior
// the matrix has been verified against. Keep newest last.
var supportedVersions = []string{"5.0.10", "5.1.14"}

// AnalyzeForVersion classifies an SD using the support matrix for the given
// Medplum version. Unknown versions fall back to the newest verified matrix and
// add a WARN finding. (Per the 5.0.10 verification, the classification rules are
// identical across the supported versions; this function is the seam for any
// future divergence — branch on `version` inside the relevant rule when needed.)
func AnalyzeForVersion(sdJSON []byte, version string) (Report, error) {
	rep, err := Analyze(sdJSON)
	if err != nil {
		return rep, err
	}
	known := false
	for _, v := range supportedVersions {
		if v == version {
			known = true
		}
	}
	if !known {
		rep.warn("provider", fmt.Sprintf("unrecognized Medplum version %q; using the matrix verified for %s (re-verify on upgrade)", version, supportedVersions[len(supportedVersions)-1]))
	}
	return rep, nil
}
```
(If Task 2.1 found a genuine behavioral divergence at 5.0.10, thread `version` into `Analyze` and branch that specific rule; otherwise the shared matrix above is correct and `version` only gates the unknown-version warning. Keep the existing `Analyze` as-is for callers/tests.)

- [ ] **Step 3:** Run `go test ./internal/fhirprofile/... -v`. Commit `feat(fhirprofile): version-keyed AnalyzeForVersion (default matrix verified 5.0.10–5.1.x)`.

### Task 2.3: Provider `medplum_version` config + wire into the profile resource

**Files:** Modify `internal/provider/provider.go`, `internal/provider/fhir_profile_resource.go`.

- [ ] **Step 1:** Add a `medplum_version` string attribute to the provider schema (Optional, MarkdownDescription "Medplum server version used to select the profile support matrix. Default 5.0.10."), read via the existing `firstNonEmpty(m.MedplumVersion, "MEDPLUM_VERSION")`, defaulting to `"5.0.10"` when empty. Store it on `providerData` as `MedplumVersion string`.
- [ ] **Step 2:** In `fhir_profile_resource.go` `ModifyPlan`, replace `fhirprofile.Analyze(body)` with `fhirprofile.AnalyzeForVersion(body, r.data.MedplumVersion)` (default to `"5.0.10"` if `r.data` nil-guarded path).
- [ ] **Step 3:** `go build ./... && go test ./... -count=1`. Commit `feat(provider): medplum_version config selects profile matrix`.

### Task 2.4: Align CI to Medplum 5.0.10

**Files:** Modify `docker-compose.test.yml`, `scripts/bootstrap-medplum.md`.

- [ ] **Step 1:** Change the medplum service image to `medplum/medplum-server:5.0.10`. Update `bootstrap-medplum.md` to say the pinned version is 5.0.10 (the deployed version) and note the first 5.0.10 CI run re-confirms auth/rate-limit/`$init`/`sdf` behaviors.
- [ ] **Step 2:** Validate YAML; commit `test: pin CI Medplum image to deployed 5.0.10`.
- [ ] **Step 3 (CI gate):** On the Phase-2 PR, watch CI; if any acceptance test regresses under 5.0.10 (auth shape, rate limit, `$init`, SD invariants), triage with systematic-debugging and fix (same loop as before).

---

## Phase 3 — OSS readiness

**Branch:** `feat/oss-p3-files`. Reference (read, don't copy internals): `/home/ivan/Developer/terraform-provider-typesense`.

### Task 3.1: Core OSS docs + manifest + license

**Files:** Create `LICENSE`, `CONTRIBUTING.md`, `SECURITY.md`, `CHANGELOG.md`, `terraform-registry-manifest.json`, `catalog-info.yaml`, `.tool-versions`; rewrite `README.md`.

- [ ] **Step 1: LICENSE** — the standard Apache License 2.0 full text with `Copyright 2026 Fluent Health` in the appendix boilerplate. (Fetch the canonical text; do not hand-paraphrase.)
- [ ] **Step 2: terraform-registry-manifest.json** verbatim:
```json
{
    "version": 1,
    "metadata": {
        "protocol_versions": ["6.0"]
    }
}
```
- [ ] **Step 3: SECURITY.md** — GitHub private vulnerability reporting link for `Fluent-Health/terraform-provider-medplum`, 5-business-day ack SLA.
- [ ] **Step 4: CONTRIBUTING.md** — prereqs (Go, Docker, Terraform CLI); local Medplum via `docker compose -f docker-compose.test.yml up -d` + `./scripts/wait-for-medplum.sh`; `go test ./...` (unit) and `make testacc` (acceptance, env vars `MEDPLUM_BASE_URL`/`MEDPLUM_EMAIL`/`MEDPLUM_PASSWORD`); `make doc` after schema changes; PR checklist; release-by-tag; "no CLA". One-time maintainer setup: `release` GitHub environment with `GPG_PRIVATE_KEY`/`PASSPHRASE`, registry registration + GPG public key upload.
- [ ] **Step 5: CHANGELOG.md** — `# Unreleased` with `### Features` listing the resources.
- [ ] **Step 6: catalog-info.yaml** — Backstage `Component`, name `terraform-provider-medplum`, slug `Fluent-Health/terraform-provider-medplum`, `type: library`, `owner: group:default/devops`, `fluentinhealth.com/oss: pending`, tags `[terraform, medplum, opensource]`.
- [ ] **Step 7: .tool-versions** — `terraform 1.12.1`.
- [ ] **Step 8: README.md** — description; resources table (`medplum_fhir_resource`, `medplum_access_policy`, `medplum_client_application`, `medplum_project_membership`, `medplum_user`, `medplum_project`, `medplum_fhir_profile`); Requirements (Terraform ≥1.0, Go 1.22+, Medplum 5.0.x); quick-start HCL using `https://medplum.example.com` + client-credentials; Building/Testing; Releasing (tag); Contributing + License links. No internal hostnames/repos.
- [ ] **Step 9:** `go build ./...` (sanity), commit `docs: Apache-2.0 license + OSS docs (contributing/security/changelog/manifest/readme/catalog)`.

### Task 3.2: tfplugindocs wiring + templates + generated docs

**Files:** Create `tools/tools.go`, `templates/index.md.tmpl`, `templates/resources/<name>.md.tmpl` (×7); modify `main.go`, `Makefile`, `go.mod`; generate `docs/`.

- [ ] **Step 1:** `go get github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs` and create `tools/tools.go`:
```go
//go:build tools
package tools
import _ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
```
- [ ] **Step 2:** Add to `main.go` (above `func main`):
```go
//go:generate terraform fmt -recursive ./examples/
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate -provider-name medplum
```
- [ ] **Step 3:** Add a `Makefile` target `doc:` running `go generate ./...`.
- [ ] **Step 4:** `templates/index.md.tmpl` — provider overview prose + `{{ .SchemaMarkdown | trimspace }}` + `{{ if .HasExample }}{{ tffile .ExampleFile }}{{ end }}`. One `templates/resources/<name>.md.tmpl` per resource (title/description/example/`{{ .SchemaMarkdown }}`).
- [ ] **Step 5:** Run `make doc` (needs the examples from Task 3.3 to be present for example injection — run after 3.3, or run twice). Verify `docs/index.md` + `docs/resources/*.md` generated.
- [ ] **Step 6:** `go mod tidy`; commit `docs: tfplugindocs wiring + generated provider docs`.

### Task 3.3: examples/ (usage examples)

**Files:** Create `examples/provider/provider.tf` and `examples/resources/<full-resource-name>/resource.tf` for each resource.

- [ ] **Step 1:** `examples/provider/provider.tf` — provider block with `base_url = "https://medplum.example.com"` + commented client-credentials/`access_token`/super-admin options + `medplum_version`.
- [ ] **Step 2:** One `resource.tf` per resource with a realistic, generic example:
  - `medplum_fhir_resource` (a ValueSet via `jsonencode`),
  - `medplum_access_policy` (resource rules + `interaction` + top-level `compartment`),
  - `medplum_client_application` (+ `identity_provider`), `medplum_project_membership` (binding the client), `medplum_user`, `medplum_project`, `medplum_fhir_profile` (a small SD).
  All generic — no internal URLs/ids.
- [ ] **Step 3:** `terraform fmt -recursive ./examples/` (or `make doc` which fmt's). Re-run `make doc` so examples are injected into `docs/`. Commit `docs: usage examples for all resources`.

### Task 3.4: Exclude internal planning docs + scrub

**Files:** Modify `.gitignore`; remove `docs/superpowers/` from the repo (keep locally/in history note).

- [ ] **Step 1:** Decide: the `docs/superpowers/` specs/plans reference internal repos (`infra`, `fhir-static-data`, local paths). For OSS cleanliness, `git rm -r --cached docs/superpowers` and add `docs/superpowers/` to `.gitignore` (the files remain on disk for our use; they leave the tracked tree). NOTE: this removes them from future commits but they remain in git history — acceptable for an INTERNAL-visibility repo; the public-flip step (out of scope) would squash/rewrite if required by `fh:publish-opensource`.
- [ ] **Step 2: Scrub.** `git grep -nIE "fhir-static-data|infra-service-projects|fh-(dev|test|stage|prod)-svc|gateway\.fluent|secret_id|/home/ivan" -- . ':(exclude)docs/superpowers'` over tracked files; ensure no internal hostnames/secret ids/project ids/local paths remain in code, README, examples, or generated docs. Fix any hit.
- [ ] **Step 3:** `go build ./... && go test ./... -count=1`; commit `chore: exclude internal planning docs and scrub internal references for OSS`.

---

## Phase 4 — CI/CD hardening + release + infra

**Branch (provider):** `feat/oss-p4-cicd`. **Branch (infra):** separate, in `/home/ivan/Developer/infra`.

### Task 4.1: Warning-free CI + golangci v2 + repo meta

**Files:** Modify `.github/workflows/ci.yml`, `.golangci.yml`; create `.github/CODEOWNERS`, `.github/dependabot.yml`.

- [ ] **Step 1:** In `ci.yml` bump: `actions/checkout@v6`, `actions/setup-go@v6` (all jobs), `golangci/golangci-lint-action@v9` (remove `version: v1.61`), `hashicorp/setup-terraform@v4`. Keep the lint/unit/acceptance structure + `needs: [lint, unit]` on acceptance + `TF_ACC_TERRAFORM_PATH` + the disabled-rate-limit env.
- [ ] **Step 2:** Rewrite `.golangci.yml` to v2 schema: `version: "2"`; `linters.enable` (govet, errcheck, staticcheck, ineffassign, unused, misspell, revive, unconvert); `formatters` (gofmt, goimports) with `settings.goimports.local-prefixes: github.com/Fluent-Health/terraform-provider-medplum`; `exclusions.presets` (comments, common-false-positives, legacy, std-error-handling); `run.tests: true`.
- [ ] **Step 3:** `.github/CODEOWNERS` = `* @Fluent-Health/devops`. `.github/dependabot.yml` = monthly gomod + github-actions, open-PR-limit 1 each.
- [ ] **Step 4:** Validate YAML (`python3 -c "import yaml;..."`); commit `ci: warning-free action majors, golangci v2, CODEOWNERS, dependabot`.

### Task 4.2: GoReleaser release workflow

**Files:** Create `.goreleaser.yml`, `.github/workflows/release.yml`.

- [ ] **Step 1: `.goreleaser.yml`** — the verified content: `before.hooks: [go mod tidy]`; `builds` (CGO off, `-trimpath`, ldflags `-s -w -X main.version={{.Version}} -X main.commit={{.Commit}}`, goos freebsd/windows/linux/darwin, goarch amd64/386/arm/arm64, ignore darwin/386, binary `{{ .ProjectName }}_v{{ .Version }}`); `archives` zip `{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}`; `checksum` with `extra_files` manifest + `_SHA256SUMS` sha256; `signs` (checksum, `--local-user {{ .Env.GPG_FINGERPRINT }}` detach-sign); `release.extra_files` manifest; `changelog.disable: true`.
- [ ] **Step 2: `release.yml`** — trigger `push: tags: ['v[0-9]+.[0-9]+.[0-9]+']`; `permissions: contents: write`; `environment: release`; job `goreleaser`: `actions/checkout@v6` (`fetch-depth: 0`), `actions/setup-go@v6` (`go-version-file: go.mod`, `cache: true`), `crazy-max/ghaction-import-gpg@v7` (`gpg_private_key: ${{ secrets.GPG_PRIVATE_KEY }}`, `passphrase: ${{ secrets.PASSPHRASE }}`, id `import_gpg`), `goreleaser/goreleaser-action@v7` (`args: release --clean`, env `GITHUB_TOKEN`, `GPG_FINGERPRINT: ${{ steps.import_gpg.outputs.fingerprint }}`).
- [ ] **Step 3:** Validate YAML; `go build ./...`. Commit `ci: GoReleaser release workflow + config for Terraform Registry`.
- [ ] **Step 4 (verify, no real release):** Optionally run `goreleaser check` locally if available (`go run github.com/goreleaser/goreleaser/v2@latest check`), expecting "configuration is valid". If goreleaser isn't installable offline, skip and rely on the release job's first dry behavior.

### Task 4.3: Infra pipeline stack (in the `infra` repo)

**Files (in `/home/ivan/Developer/infra`):** Create `stacks/pipelines/terraform-provider-medplum/{pipeline.tm.hcl, module/{main,variables,provider}.tf, nonprod/{stack.tm.hcl,main.tf}, prod/{stack.tm.hcl,main.tf}}`; then `terramate generate`.

- [ ] **Step 1:** Read `stacks/pipelines/testing-android/` (the `github_actions`-deployment template) and one full `nonprod/main.tf` to copy the `custom_role_ids`, `target_context`, `pipeline_context`, and `data.google_iam_workload_identity_pool_provider` boilerplate exactly.
- [ ] **Step 2:** `pipeline.tm.hcl` — `globals "pipeline" { name = "terraform-provider-medplum"; module = "pipeline-terraform-provider-medplum"; targets = { nonprod = ["fh-dev-svc"]; prod = [] } }`; `globals { state_key = "pipelines/terraform-provider-medplum" }`. (Single nonprod target; no prod deploy artifact.)
- [ ] **Step 3:** `nonprod/stack.tm.hcl` (`name = "pipelines/terraform-provider-medplum/nonprod"`, tags `["pipeline","tier-nonprod"]`, `tier=nonprod`, `gcp_project_id="fh-nonprod-host"`, `targets=["fh-dev-svc"]`, `needs_kubernetes=false`). `module/main.tf`:
```hcl
module "pipeline" {
  source     = "../../../../modules/pipeline"
  context    = var.pipeline_context
  name       = "terraform-provider-medplum"
  trigger    = { none = true }
  deployment = { github_actions = {} }
}
```
plus `module/variables.tf` (`pipeline_context`) and `nonprod/main.tf` instantiating it (mirroring testing-android, single `fh-dev-svc` target). Expose the module's `github_actions_wif_provider` output.
- [ ] **Step 4:** From the infra repo root, run `terramate generate` to emit `_terramate_generated_*`. Run `terraform -chdir=... init`/`validate` or the repo's lint (`terramate run -- tflint` per repo conventions) as available; otherwise `terraform validate` the generated stack.
- [ ] **Step 5:** Commit in the infra repo on its own branch: `feat(pipelines): add terraform-provider-medplum github_actions pipeline`. (Open that PR per infra repo conventions; the WIF provider output feeds the provider repo's `release.yml` if/when releases impersonate GCP — document the WIF resource name in CONTRIBUTING.)

NOTE: Phase-4 infra is in a DIFFERENT repo with its own review/CI; treat it as a separate PR. The provider's GitHub Actions release does not strictly need GCP (GoReleaser publishes to GitHub + Registry), so the infra stack is for any GCP-touching CI (e.g. acceptance against a managed Medplum) — keep it minimal (single nonprod WIF) per the spec.

---

## Phase 5 — FSH → SD → provider end-to-end

**Branch:** `feat/oss-p5-fsh-e2e`.

### Task 5.1: Complex FSH fixture

**Files:** Create `test/fsh/sushi-config.yaml`, `test/fsh/input/fsh/complex-profile.fsh`, `test/fsh/.gitignore` (ignore `fsh-generated/`, `output/`, `input-cache/`).

- [ ] **Step 1:** `sushi-config.yaml` — minimal IG config (id, canonical `http://example.com/fhir`, fhirVersion `4.0.1`, a name/title, `FSHOnly: false` so the publisher runs, dependencies none).
- [ ] **Step 2:** `complex-profile.fsh` — a Patient (or Observation) profile exercising ENFORCED constructs: a required element (`* active 1..1`), a fixed/pattern value, a required **extension with a fixed url** (sliced), and a value/pattern-discriminated **slice** with per-slice cardinality — plus one decorative construct (a `MS` mustSupport flag) so the validator report shows enforced + decorative. Keep it valid FSH that SUSHI compiles.
- [ ] **Step 3:** Commit `test(fsh): complex profile FSH fixture + sushi config`.

### Task 5.2: CI compile (SUSHI + IG Publisher) producing a snapshot SD

**Files:** Modify `.github/workflows/ci.yml` (add a step/job), create `scripts/build-fsh.sh`.

- [ ] **Step 1: `scripts/build-fsh.sh`** — `set -euo pipefail`; install SUSHI (`npm install -g fsh-sushi`); run `sushi build test/fsh` (or `cd test/fsh && sushi .`); download + run the HL7 IG Publisher (`curl -L https://github.com/HL7/fhir-ig-publisher/releases/latest/download/publisher.jar -o /tmp/publisher.jar` then `java -jar /tmp/publisher.jar -ig test/fsh -no-sushi` — SUSHI already ran; the publisher generates snapshots); locate the snapshot-bearing `StructureDefinition-*.json` under `test/fsh/output/` (or `fsh-generated`) and copy it to a stable path `test/fsh/generated-sd.json`; print its path. The script must fail loudly if no snapshot SD is produced.
- [ ] **Step 2: CI job** — add an `e2e-profile` job (or extend `acceptance`): `needs: [unit]`; checkout@v6; setup-go@v6; `actions/setup-node@v4`; `actions/setup-java@v4` (temurin 17); cache `~/.fhir` + the publisher jar (`actions/cache@v4` keyed on a publisher version); start Medplum (`docker compose -f docker-compose.test.yml up -d`) + wait; setup-terraform@v4 + `TF_ACC_TERRAFORM_PATH`; run `./scripts/build-fsh.sh`; then `TF_ACC=1 MEDPLUM_* ... MEDPLUM_TEST_PROFILE_SD=$PWD/test/fsh/generated-sd.json go test ./internal/provider/... -run TestAccFHIRProfile_fromFSH -v -timeout 30m`; dump Medplum logs + the FSH build log on failure.
- [ ] **Step 3:** Validate YAML; commit `ci: FSH→SUSHI→IG-Publisher build step for the e2e profile`.

### Task 5.3: e2e acceptance test

**Files:** Modify `internal/provider/fhir_profile_resource_test.go`.

- [ ] **Step 1:** Add `TestAccFHIRProfile_fromFSH`:
```go
func TestAccFHIRProfile_fromFSH(t *testing.T) {
	sdPath := os.Getenv("MEDPLUM_TEST_PROFILE_SD")
	if sdPath == "" {
		t.Skip("MEDPLUM_TEST_PROFILE_SD not set (FSH→SD build step did not run)")
	}
	raw, err := os.ReadFile(sdPath)
	if err != nil { t.Fatalf("read generated SD: %v", err) }
	// jsonencode(jsondecode(<file>)) keeps the SD intact in HCL.
	cfg := fmt.Sprintf(`
resource "medplum_fhir_profile" "fsh" {
  structure_definition = jsonencode(jsondecode(%q))
}
`, string(raw))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_fhir_profile.fsh", "id"),
					resource.TestCheckResourceAttrSet("medplum_fhir_profile.fsh", "url"),
				),
			},
			{Config: cfg, PlanOnly: true},
		},
	})
}
```
Add `"os"` import if missing.
- [ ] **Step 2:** `go vet ./... && go test ./internal/provider/... -run TestAccFHIRProfile_fromFSH -v` → SKIP locally (no env). Commit `test(provider): e2e acceptance applying the FSH-generated profile`.
- [ ] **Step 3 (CI gate):** On the Phase-5 PR, watch the `e2e-profile` job. Expect first-run friction (publisher download/cache, SD path, the SD's snapshot satisfying Medplum's `sdf` invariants — like the Plan-3 minimal-snapshot fix but now publisher-generated, so it should be complete). Triage with systematic-debugging; if the IG Publisher proves too flaky in CI, fall back to committing `test/fsh/generated-sd.json` as a fixture (documented in the spec) and have the test read it directly.

---

## Self-Review (completed during plan authoring)

**Spec coverage:** Phase 1 → Tasks 1.1–1.3 (3 fields + acc). Phase 2 → 2.1 (verify 5.0.10), 2.2 (version-key), 2.3 (provider config), 2.4 (CI image). Phase 3 → 3.1 (license/docs/manifest), 3.2 (tfplugindocs), 3.3 (examples), 3.4 (scrub/exclude). Phase 4 → 4.1 (warning-free + golangci v2 + meta), 4.2 (GoReleaser), 4.3 (infra stack). Phase 5 → 5.1 (FSH fixture), 5.2 (SUSHI+publisher CI), 5.3 (e2e test). ✓

**Placeholder scan:** The infra (4.3) and FSH-publisher (5.2) tasks reference environment-specific commands (terramate generate, publisher jar URL) that are concrete but will need CI iteration — flagged explicitly, not hidden TODOs. Task 2.2 explicitly conditionalizes on the 2.1 verification outcome (shared matrix vs branch) — that's a real decision point, documented, not a placeholder. The Apache LICENSE text is "fetch canonical" (it's a standard 11KB legal text — do not hand-write).

**Type/consistency:** `fhirprofile.AnalyzeForVersion`, `providerData.MedplumVersion`, `identityProviderModel`, `accessPolicyModel.Compartment`, `accessPolicyResourceRow.Interaction` are introduced once and referenced consistently. Action versions (checkout@v6/setup-go@v6/golangci@v9/setup-terraform@v4/goreleaser@v7/import-gpg@v7) are uniform across Phase 4.

**Execution note:** Phases are independent rounds; recommend executing 1→2→3→4→5, each as its own branch+PR+green-CI before the next, because Phase 2 changes the CI image (affecting all later acceptance runs) and Phase 3/4 change CI/docs that Phase 5 builds on.
