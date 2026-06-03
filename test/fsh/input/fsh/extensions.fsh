// FluentTag extension — carries a short code label on a resource.
// Used in FluentPatient to tag patients with an internal classification code.
Extension: FluentTag
Id: fluent-tag
Title: "Fluent Tag"
Description: "A short internal classification code applied to a patient record."
* ^url = "http://example.com/fhir/StructureDefinition/fluent-tag"
* ^context[0].type = #element
* ^context[0].expression = "Patient"
* value[x] only code
