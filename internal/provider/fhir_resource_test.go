package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccPreCheck(t *testing.T) {
	if os.Getenv("MEDPLUM_BASE_URL") == "" {
		t.Fatal("MEDPLUM_BASE_URL must be set for acceptance tests")
	}
	hasCreds := os.Getenv("MEDPLUM_CLIENT_ID") != "" || os.Getenv("MEDPLUM_ACCESS_TOKEN") != "" || os.Getenv("MEDPLUM_EMAIL") != ""
	if !hasCreds {
		t.Fatal("a Medplum auth method env var must be set for acceptance tests")
	}
}

func TestAccFHIRResource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFHIRResourceConfig("active"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_fhir_resource.test", "id"),
					resource.TestCheckResourceAttrSet("medplum_fhir_resource.test", "version_id"),
					resource.TestCheckResourceAttr("medplum_fhir_resource.test", "resource_type", "ValueSet"),
				),
			},
			{
				// No-op re-apply must produce an empty plan (drift stability).
				Config:   testAccFHIRResourceConfig("active"),
				PlanOnly: true,
			},
			{
				Config: testAccFHIRResourceConfig("draft"),
				Check:  resource.TestCheckResourceAttrSet("medplum_fhir_resource.test", "version_id"),
			},
			{
				ResourceName:      "medplum_fhir_resource.test",
				ImportState:       true,
				ImportStateIdFunc: importIDFunc("medplum_fhir_resource.test"),
				ImportStateVerify: true,
				// body is re-read from server; ignore exact string match on import verify.
				ImportStateVerifyIgnore: []string{"body"},
			},
		},
	})
}

func testAccFHIRResourceConfig(status string) string {
	return fmt.Sprintf(`
resource "medplum_fhir_resource" "test" {
  resource_type = "ValueSet"
  body = jsonencode({
    resourceType = "ValueSet"
    status       = %q
    url          = "http://example.com/fhir/ValueSet/tf-acc-test"
  })
}
`, status)
}

func importIDFunc(name string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return "", fmt.Errorf("resource %s not found", name)
		}
		return "ValueSet/" + rs.Primary.Attributes["id"], nil
	}
}
