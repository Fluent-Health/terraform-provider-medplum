# Manage a FHIR ValueSet as a generic FHIR resource.
# Any FHIR R4 resource type is supported via the body attribute.
resource "medplum_fhir_resource" "blood_types" {
  resource_type = "ValueSet"

  body = jsonencode({
    resourceType = "ValueSet"
    url          = "https://example.com/fhir/ValueSet/blood-types"
    name         = "BloodTypes"
    title        = "Blood Types"
    status       = "active"
    description  = "ABO blood group system codes from SNOMED CT."

    compose = {
      include = [
        {
          system = "http://snomed.info/sct"
          concept = [
            { code = "112144000", display = "Blood group A" },
            { code = "165743006", display = "Blood group B" },
            { code = "46251000", display = "Blood group AB" },
            { code = "58460004", display = "Blood group O" },
          ]
        }
      ]
    }
  })
}
