// FluentPatient — a complex Patient profile that exercises multiple
// constraint categories as classified by the Medplum profile validator.
//
// ENFORCED constraints (Medplum will reject non-conformant resources):
//   1. active 1..1         — required cardinality (min=1)
//   2. active = true       — fixed value on active
//   3. gender 1..1 MS      — required cardinality on gender (MS is decorative)
//   4. identifier:mrn 1..1 — per-slice cardinality (value discriminator on system)
//
// DECORATIVE constraints (Medplum does not enforce at runtime):
//   5. name MS             — mustSupport flag on name
//   6. birthDate MS        — mustSupport flag on birthDate
//
// NOTE: we deliberately do NOT add an `* extension contains ...` slice here.
// SUSHI/IG-Publisher generate an extension slice typed only via `type.profile`
// (a canonical reference), with NO inline fixed `url` child element. Medplum
// matches extension slices by `slice.elements['url'].fixed`, so such a slice
// never matches and the constraint is silently inert — which the provider's
// profile validator correctly REJECTS at plan time. Keeping it out lets this
// fixture exercise the happy path (a profile Medplum genuinely enforces); the
// validator's rejection of the inert extension form is covered by unit tests.

Profile: FluentPatient
Parent: Patient
Id: fluent-patient
Title: "Fluent Patient"
Description: "A complex Patient profile used to test the FSH → SD → Terraform pipeline."
* ^url = "http://example.com/fhir/StructureDefinition/fluent-patient"
* ^status = #active

// ── ENFORCED: required active flag fixed to true ──────────────────────────
* active 1..1
* active = true

// ── ENFORCED + DECORATIVE: required gender (MS is decorative) ────────────
* gender 1..1 MS

// ── ENFORCED: value-discriminated slice on identifier.system ─────────────
* identifier ^slicing.discriminator[0].type = #value
* identifier ^slicing.discriminator[0].path = "system"
* identifier ^slicing.rules = #open

* identifier contains mrn 1..1

* identifier[mrn].system 1..1
* identifier[mrn].system = "http://example.com/fhir/identifier/mrn"
* identifier[mrn].value 1..1

// ── DECORATIVE: mustSupport on name and birthDate ─────────────────────────
* name MS
* birthDate MS
