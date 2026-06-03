package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// testAccFHIRProfileConfig returns HCL for a minimal StructureDefinition with
// exactly one enforced constraint (Patient.active min=1) so the gate passes
// and a no-op re-apply produces an empty plan.
func testAccFHIRProfileConfig(url string) string {
	return fmt.Sprintf(`
resource "medplum_fhir_profile" "test" {
  structure_definition = jsonencode({
    resourceType   = "StructureDefinition"
    url            = %q
    name           = "TfAccProfile"
    status         = "active"
    kind           = "resource"
    abstract       = false
    type           = "Patient"
    baseDefinition = "http://hl7.org/fhir/StructureDefinition/Patient"
    derivation     = "constraint"
    snapshot = {
      element = [
        {
          id         = "Patient"
          path       = "Patient"
          definition = "A patient."
          min        = 0
          max        = "*"
          base       = { path = "Patient", min = 0, max = "*" }
        },
        {
          id         = "Patient.active"
          path       = "Patient.active"
          definition = "Whether this patient record is in active use."
          min        = 1
          max        = "1"
          base       = { path = "Patient.active", min = 0, max = "1" }
          type       = [{ code = "boolean" }]
        }
      ]
    }
  })
}
`, url)
}

func TestAccFHIRProfile_basic(t *testing.T) {
	url := "http://example.com/fhir/StructureDefinition/tf-acc-" + acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	cfg := testAccFHIRProfileConfig(url)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_fhir_profile.test", "id"),
					resource.TestCheckResourceAttr("medplum_fhir_profile.test", "url", url),
				),
			},
			{Config: cfg, PlanOnly: true},
			{
				ResourceName:            "medplum_fhir_profile.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"structure_definition", "strict"},
			},
		},
	})
}

func TestAccFHIRProfile_rejectsEmptySnapshot(t *testing.T) {
	cfg := `
resource "medplum_fhir_profile" "test" {
  structure_definition = jsonencode({
    resourceType   = "StructureDefinition"
    url            = "http://example.com/fhir/StructureDefinition/tf-acc-empty"
    name           = "Empty"
    status         = "active"
    kind           = "resource"
    abstract       = false
    type           = "Patient"
    baseDefinition = "http://hl7.org/fhir/StructureDefinition/Patient"
    derivation     = "constraint"
  })
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, ExpectError: regexp.MustCompile("snapshot")},
		},
	})
}
