package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
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

func TestAccFHIRResource_createUpdateImport(t *testing.T) {
	var firstVersionID string
	suffix := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFHIRResourceConfig("active", suffix),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_fhir_resource.test", "id"),
					resource.TestCheckResourceAttrWith("medplum_fhir_resource.test", "version_id", func(v string) error {
						firstVersionID = v
						return nil
					}),
					resource.TestCheckResourceAttr("medplum_fhir_resource.test", "resource_type", "ValueSet"),
				),
			},
			{
				// No-op re-apply must produce an empty plan (drift stability).
				Config:   testAccFHIRResourceConfig("active", suffix),
				PlanOnly: true,
			},
			{
				Config: testAccFHIRResourceConfig("draft", suffix),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("medplum_fhir_resource.test", "version_id", func(v string) error {
						if v == firstVersionID {
							return fmt.Errorf("version_id did not change after update: %s", v)
						}
						return nil
					}),
				),
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

func testAccFHIRResourceConfig(status string, urlSuffix string) string {
	return fmt.Sprintf(`
resource "medplum_fhir_resource" "test" {
  resource_type = "ValueSet"
  body = jsonencode({
    resourceType = "ValueSet"
    status       = %q
    url          = "http://example.com/fhir/ValueSet/tf-acc-test-%s"
  })
}
`, status, urlSuffix)
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
