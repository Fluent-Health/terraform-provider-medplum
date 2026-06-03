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

// TestAccAccessPolicy_emptyList verifies that an explicit empty list ([]) in config
// does not produce an "inconsistent result after apply" error. The emptyListAsNull
// plan modifier normalises [] → null at plan time, matching what fromFHIR returns
// (Medplum strips empty arrays on the server side).
func TestAccAccessPolicy_emptyList(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with explicit empty lists – must not crash on apply.
			{
				Config: `
resource "medplum_access_policy" "empty_list" {
  name = "tf-acc-policy-empty-list"
  resource {
    resource_type   = "Observation"
    hidden_fields   = []
    readonly_fields = []
  }
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_access_policy.empty_list", "id"),
					resource.TestCheckResourceAttr("medplum_access_policy.empty_list", "resource.0.resource_type", "Observation"),
				),
			},
			// Re-plan with the same config – must be a no-op (no churn).
			{
				Config: `
resource "medplum_access_policy" "empty_list" {
  name = "tf-acc-policy-empty-list"
  resource {
    resource_type   = "Observation"
    hidden_fields   = []
    readonly_fields = []
  }
}`,
				PlanOnly: true,
			},
		},
	})
}
