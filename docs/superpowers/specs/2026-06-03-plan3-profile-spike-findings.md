# Plan 3 Spike Findings — `medplum_fhir_profile`

* Status: spike complete (input to the Plan 3 spec)
* Date: 2026-06-03
* Source investigated: `/home/ivan/Developer/medplum` (v5.1.14)
* Feeds: issue #1 (`medplum_fhir_profile`) and its eventual spec/plan

Two empirical questions blocked a detailed Plan 3 spec (issue #1 open questions). Both are now
answered from the Medplum source. A live round-trip remains a cheap confirmation during Plan 3
implementation but is **not** required to write the spec.

## Spike A — SD-vs-server drift equivalence

**Conclusion: reuse the existing subset-containment drift model (`fhirjson.Contains`) as-is. No
SD-specific normalization needed.**

Medplum applies **no StructureDefinition-specific write transformation** — SDs go through the same
generic `createResource`/`updateResource` path as any resource. Confirmed behaviors:

- **Adds only server-managed `meta`** (`versionId`, `lastUpdated`, `author`, `meta.project`,
  `meta.compartment`) — `repo.ts:825-844`. Additions are safe: `Contains(config, server)` ignores
  server-only keys.
- **No element/array reordering** of `snapshot.element` / `differential.element` / `context` — no
  sort on the SD write path. Containment's order-sensitivity is therefore safe.
- **Does NOT drop `differential`** — the `differential: undefined` behavior is **only** in the
  seeder caller (`seeds/structuredefinitions.ts:33-43`), not the general write path. A user-supplied
  `differential` is stored and round-trips.
- **Read returns stored content verbatim** (`repo.ts:507`); `removeHiddenFields` only blanks
  server-managed `meta.*` — removals of additions, never user body.
- **Only content mutation = empty-value stripping** (`""`, `[]`, `{}`, `null`) by `stringify` →
  `removeEmptyFromUnknown` (`core/src/utils.ts:535-656`). FHIR JSON forbids empty arrays/strings,
  so valid SDs are unaffected. This is the **same caveat as the generic `medplum_fhir_resource`**:
  config must not contain empty values.

**Implication for the resource:** Phase 1 (SD-consuming resource) is essentially the generic
resource specialized to `StructureDefinition` — `body` (JSON) + `Contains`-based drift +
`$rotate`-free CRUD. The "SD-vs-server equivalence" the issue flagged as the hard part is already
handled by Plan 1's `Contains`.

**Live confirmation to do in Plan 3 (low risk):** submit an SD with a deliberately empty array and
confirm it round-trips without that key; confirm no project AccessPolicy touches SD fields.

## Spike B — Validator support matrix (verified against `@medplum/core` v5.1.14)

All 10 issue-#1 claims **CONFIRMED**, with refinements. Files: `core/src/typeschema/types.ts`,
`validation.ts`, `crawler.ts`, `fhirpath/utils.ts`.

| Construct | Class | Evidence |
| --- | --- | --- |
| Missing/empty `snapshot.element` | **REJECT** (throws "No snapshot defined") | types.ts:257-258 |
| Discriminator type ≠ value/pattern/type (`exists`/`profile`/`position`) | **REJECT** (throws) | types.ts:386-388 |
| Discriminator `path` with FHIRPath funcs (`where()`/`resolve()`/`ofType()`/`extension(url)`) | **REJECT** (silently never matches → reject at plan) | crawler.ts:241-247; validation.ts:696-699,738 |
| Extension slice lacking fixed-`url` child | **REJECT** (never matches) | validation.ts:699,706,712-713 |
| Slicing `rules` `closed`/`openAtEnd`; `ordered` | **WARN** (parsed, never read) | types.ts:395-396 |
| Slice discriminated only by required ValueSet binding | **WARN** (matches unconditionally) | validation.ts:716-721 |
| Deep extension value typing / sub-extensions / slice `type.profile` | **WARN** (only cardinality counted; SD not resolved) | validation.ts:239-251,283-304; crawler.ts:103-107 |
| Element cardinality (min/max), required presence | **ENFORCED** | validation.ts:221-230,260-263 |
| `fixed[x]` / `pattern[x]` | **ENFORCED** | validation.ts:675-679,232-233 |
| Choice-type narrowing / unknown-property rejection | **ENFORCED** | validation.ts:320-321,363-364,621-642 |
| Per-slice cardinality w/ value/pattern/type discriminators on dotted/`$this` paths | **ENFORCED** | validation.ts:704-745,283-304 |
| Extension presence + cardinality keyed by URL (fixed-`url` present) | **ENFORCED** | types.ts:609-617; validation.ts:699,712-713,293 |

**Refinements / corrections to issue #1:**
- **Required-binding ValueSet *code membership* is NOT enforced** in the sync path — codes are only
  *collected* for out-of-band/async terminology validation (`validation.ts:454-457`). So a required
  binding is effectively **WARN** for membership (the slice-discriminator case is already WARN).
- **`mustSupport` / `isModifier` / summary flags are parsed but not enforced** (only `min>0` drives a
  presence error) → **WARN** if a profile relies on them.
- **Reference `targetProfile`** is matched only by HL7/Medplum resource-type URL; custom/US-Core
  profiles are "cannot validate → skip", and even a recognized mismatch is a **warning, not error**
  (`validation.ts:408-444`) → classify as **WARN/partial**, not enforced.
- **FHIRPath `constraint` (invariant) expressions** are evaluated (`validation.ts:369,482`) but the
  supported FHIRPath feature set was not traced; treat custom `constraint.expression` as **WARN**
  until separately verified (a Plan 3 sub-task).

## Recommendations for the Plan 3 spec

1. **Drift:** reuse `fhirjson.Contains`; no SD-specific equivalence code. Phase 1 = a thin
   StructureDefinition CRUD resource (JSON `body`) + reject empty `snapshot.element` at plan.
2. **Validator (Phase 2):** encode the matrix above, **version-pinned to the Medplum the provider
   targets**, emitting a per-profile plan report (enforced vs decorative). Add the two refinements
   (binding membership, mustSupport, reference targetProfile, constraints) as WARN classes.
3. **Open design decisions still needed from the user** (for the spec/brainstorm): strict-mode
   (decorative-only profile = hard fail vs warn); whether the resource also manages Extension
   `StructureDefinition`s; how much non-extension slicing our real profiles use (drives validator
   depth). FSH→SD compilation and the IG-Publisher pipeline remain out of the provider (Phase 3).
