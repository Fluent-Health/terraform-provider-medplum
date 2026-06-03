# A FHIR R4 StructureDefinition (profile) for Patient that requires
# the active element (min = 1). The snapshot must be non-empty.
#
# At plan time the provider analyses the profile against the Medplum
# enforcement matrix and warns about constructs that Medplum does not enforce.
# Set strict = true to turn those warnings into errors.
resource "medplum_fhir_profile" "active_patient" {
  structure_definition = jsonencode({
    resourceType   = "StructureDefinition"
    url            = "https://example.com/fhir/StructureDefinition/ActivePatient"
    name           = "ActivePatient"
    title          = "Active Patient"
    status         = "active"
    kind           = "resource"
    abstract       = false
    type           = "Patient"
    baseDefinition = "http://hl7.org/fhir/StructureDefinition/Patient"
    derivation     = "constraint"

    snapshot = {
      element = [
        {
          id   = "Patient"
          path = "Patient"
        },
        {
          id   = "Patient.active"
          path = "Patient.active"
          min  = 1
          max  = "1"
        }
      ]
    }
  })

  # Promote unenforced-construct warnings to errors.
  strict = false
}
