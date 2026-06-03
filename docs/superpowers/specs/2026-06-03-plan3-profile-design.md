# Plan 3 — `medplum_fhir_profile` — Design

* Status: proposed
* Author: Ivan Kerin (ivan.kerin@fluentinhealth.com)
* Date: 2026-06-03
* Inputs: issue #1; `2026-06-03-plan3-profile-spike-findings.md`; builds on merged Plan 1 + Plan 2.

## Context

We manage FHIR R4 profiles (`StructureDefinition`s) as infrastructure-as-code. A profile can be
perfectly valid FHIR yet have most of its constraints **silently unenforced by Medplum** — it looks
correct in review but constrains nothing. This resource manages an SD's lifecycle against Medplum
**and** gates it at `plan` time on whether Medplum will actually enforce what it expresses.

The spike resolved the two empirical unknowns:
- **Drift:** reuse the existing subset-containment model (`fhirjson.Contains`) unchanged — Medplum
  adds only server-managed `meta` and never reorders/drops user SD content.
- **Validator matrix:** all reject/warn/enforced claims verified against `@medplum/core` v5.1.14
  (with refinements). See the spike findings doc.

**FSH→SD compilation and the IG-Publisher pipeline are out of the provider** — the provider consumes
compiled SD JSON only.

## Decisions (locked during brainstorming)

| Decision | Choice |
| --- | --- |
| Drift model | **Reuse `fhirjson.Contains`** (subset-containment); no SD-specific normalization |
| Strict mode | **Warn by default; `strict = true` escalates WARN/decorative-only to a plan error** |
| Extension SDs | **Managed by the same resource** (they are StructureDefinitions); validator notes Medplum won't deep-validate them |
| FSH→SD + IG Publisher | **Out of the provider** (Phase 3 is CI, documented only) |
| Input | Raw compiled SD **JSON string** (`structure_definition`), mirroring the generic resource |

## Goals

1. `medplum_fhir_profile` — apply a `StructureDefinition` to Medplum with drift detection (reusing
   `Contains`), reject an empty `snapshot.element` at plan, and support `terraform import`.
2. A **Medplum-context useful-profile validator** (`internal/fhirprofile`) that statically classifies
   every construct as REJECT / WARN / ENFORCED per the spike matrix, runs at `plan`, emits
   diagnostics + a per-profile report (enforced vs decorative), and escalates under `strict`.

## Non-Goals

* FSH parsing/compilation; snapshot *generation*; IG-Publisher rendering (Phase 3, CI-only).
* Snapshot portability to non-Medplum tooling.
* Enforcing constraints Medplum itself doesn't (the validator's whole point is to flag those).

## Architecture

### Resource `medplum_fhir_profile`

A thin resource over `StructureDefinition`, reusing Plan-1/2 machinery (client, `Contains` drift,
embedded R4 schema validator) plus the new profile validator.

```hcl
resource "medplum_fhir_profile" "patient_fluent" {
  structure_definition = file("build/sds/Patient-fluent.json")  # compiled SD JSON
  strict               = false                                   # optional; escalate WARN -> error
}
# computed: id, url (StructureDefinition.url), version_id, last_updated
```

| Attribute | Type | Notes |
| --- | --- | --- |
| `structure_definition` | string (JSON), required | The compiled SD. Validated at plan. |
| `strict` | bool, optional (default false) | Escalate WARN + decorative-only to a plan error. |
| `id`/`url`/`version_id`/`last_updated` | computed | From the server / SD. |

CRUD: `POST/PUT/GET/DELETE /{fhir_path}/StructureDefinition`. Drift via `Contains(config, server)`
(unchanged from the generic resource); `IsNotFound` already covers 404 + 410. Import via
`StructureDefinition/{id}` or by `url` (decide in plan; default by id).

### Plan-time validation (`ModifyPlan`)

Runs where provider data is available (same pattern as `medplum_fhir_resource`). Steps:
1. Base R4 JSON-schema validation (reuse `internal/fhirschema`).
2. **`internal/fhirprofile.Analyze(sdJSON) (Report, error)`** — the centerpiece. Static analysis of
   `snapshot.element` + slicing/discriminators producing classified findings.
3. Emit diagnostics: REJECT → errors; WARN → warnings; plus a **summary** ("N enforced, M decorative
   constraints"). If `strict`, WARN and decorative-only become errors.

### `internal/fhirprofile` validator

Encodes the spike's **version-pinned** support matrix. Pure function over the SD (no network), unit-
testable in isolation.

- **REJECT:** empty `snapshot.element`; discriminator type ∉ {value,pattern,type}; discriminator
  `path` with FHIRPath functions (only dotted paths + `$this` resolve); extension slice lacking a
  fixed-`url` child.
- **WARN (decorative — Medplum silently ignores):** `closed`/`openAtEnd`/`ordered` slicing rules;
  slices discriminated only by a required ValueSet binding; deep extension value typing / slice
  `type.profile`; required-binding **code membership**; `mustSupport`/`isModifier`/summary flags;
  Reference `targetProfile` (warn-not-error in Medplum); FHIRPath `constraint.expression` (coverage
  unverified — treat as WARN).
- **ENFORCED (counts toward "useful"):** cardinality + required presence; `fixed[x]`/`pattern[x]`;
  choice-type narrowing / unknown-property rejection; per-slice cardinality with value/pattern/type
  discriminators on dotted/`$this` paths; extension presence + cardinality keyed by URL.
- **Output:** `Report{ Rejects []Finding; Warns []Finding; EnforcedCount int }`, each `Finding`
  carrying an element path + message. A profile with `EnforcedCount == 0` warns loudly (errors under
  `strict`).
- **Maintenance:** the matrix is pinned to a Medplum version; re-verify on upgrade (a documented
  task). Ideal future: generate/verify the matrix from `@medplum/core` source.

## Phasing (for the plan)

1. **Phase 1 — SD-consuming resource:** `medplum_fhir_profile` (CRUD + `Contains` drift + reject
   empty `snapshot.element`), import, acceptance test. Unblocks IaC management of profiles.
2. **Phase 2 — useful-profile validator:** `internal/fhirprofile` classifier + per-profile plan
   report + `strict`. The centerpiece. Heavily unit-tested (table-driven over crafted SDs).
3. **Phase 3 — docs/IG pipeline (out of provider):** FSH→SD compile in CI (generated SDs git-ignored)
   + IG Publisher render + optional `ImplementationGuide` manifest. Documented, not built in the
   provider.

Plan 3 implementation = Phases 1 + 2. Phase 3 is a separate CI concern.

## Testing & CI

- **Unit:** `fhirprofile.Analyze` table-driven tests — crafted SDs hitting each REJECT/WARN/ENFORCED
  case (empty snapshot, `exists` discriminator, FHIRPath path, extension w/ and w/o fixed url,
  closed slicing, binding-only slice, enforced cardinality/fixed/pattern, extension presence). Assert
  the classification + `EnforcedCount`.
- **Acceptance (live Medplum):** apply a real enforced profile (create → empty no-op plan via
  `Contains` → import); a `plan`-fails case (empty snapshot) via `ExpectError`; a `strict`-fails case
  (decorative-only). Confirms drift stability + the gate end-to-end.
- Reuse the green Plan-1/2 CI (docker Medplum, `TF_ACC_TERRAFORM_PATH`, disabled rate limiter).

## Risks & open questions

- **Matrix drift across Medplum versions** — pin to the targeted version; re-verify on upgrade.
- **FHIRPath `constraint` coverage** — classified WARN pending a separate trace of supported FHIRPath
  features in `@medplum/core` (a Phase-2 sub-task; safe default is WARN).
- **Empty-array round-trip** — confirm live (spike low-risk item) that a config SD with no empty
  values round-trips cleanly under `Contains` (shared with the generic resource).
- **Slicing depth in our real profiles** — drives how much slicing nuance the validator must classify
  precisely; verify against our actual compiled SDs during Phase 2.
