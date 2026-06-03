// FluentPatient — a complex Patient profile that exercises multiple
// constraint categories as classified by the Medplum profile validator.
//
// ENFORCED constraints (Medplum will reject non-conformant resources):
//   1. active 1..1         — required cardinality (min=1)
//   2. active = true       — fixed value on active
//   3. gender 1..1 MS      — required cardinality on gender (MS is decorative)
//   4. FluentTag extension 1..1 — required extension by URL
//   5. identifier:mrn 1..1 — per-slice cardinality (value discriminator on system)
//
// DECORATIVE constraints (Medplum does not enforce at runtime):
//   6. name MS             — mustSupport flag on name
//   7. birthDate MS        — mustSupport flag on birthDate

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

// ── ENFORCED: required extension by URL ──────────────────────────────────
* extension contains FluentTag named fluentTag 1..1

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
