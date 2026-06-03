package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccAccessPolicy_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "medplum_access_policy" "test" {
  name = "tf-acc-policy"
  resource {
    resource_type = "Patient"
    criteria      = "Patient?_id=%patient.id"
    readonly      = true
    hidden_fields = ["telecom"]
  }
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_access_policy.test", "id"),
					resource.TestCheckResourceAttr("medplum_access_policy.test", "resource.0.resource_type", "Patient"),
					resource.TestCheckResourceAttr("medplum_access_policy.test", "resource.0.readonly", "true"),
				),
			},
			{Config: `
resource "medplum_access_policy" "test" {
  name = "tf-acc-policy"
  resource {
    resource_type = "Patient"
    criteria      = "Patient?_id=%patient.id"
    readonly      = true
    hidden_fields = ["telecom"]
  }
}`, PlanOnly: true},
			{
				ResourceName:      "medplum_access_policy.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
