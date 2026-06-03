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

// TestAccAccessPolicy_omittedLists verifies that a resource block with hidden_fields
// and readonly_fields entirely omitted (null) creates without error and re-plans
// as a no-op. FHIR does not allow empty arrays, so the correct approach is to omit
// these attributes entirely rather than setting them to [].
func TestAccAccessPolicy_omittedLists(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with hidden_fields and readonly_fields omitted – must apply cleanly.
			{
				Config: `
resource "medplum_access_policy" "omitted_lists" {
  name = "tf-acc-policy-omitted-lists"
  resource {
    resource_type = "Observation"
    criteria      = "Observation?status=final"
  }
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_access_policy.omitted_lists", "id"),
					resource.TestCheckResourceAttr("medplum_access_policy.omitted_lists", "resource.0.resource_type", "Observation"),
				),
			},
			// Re-plan with the same config – must be a no-op (no churn).
			{
				Config: `
resource "medplum_access_policy" "omitted_lists" {
  name = "tf-acc-policy-omitted-lists"
  resource {
    resource_type = "Observation"
    criteria      = "Observation?status=final"
  }
}`,
				PlanOnly: true,
			},
		},
	})
}
