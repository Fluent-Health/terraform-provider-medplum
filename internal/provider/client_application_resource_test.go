package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestGenerateClientSecret_UniqueAndLong(t *testing.T) {
	a, err := generateClientSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := generateClientSecret()
	if a == b {
		t.Fatal("expected unique secrets")
	}
	if len(a) < 40 {
		t.Fatalf("secret too short: %d", len(a))
	}
}

func TestAccClientApplication_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "medplum_client_application" "test" {
  name        = "tf-acc-client"
  description = "acc test"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_client_application.test", "id"),
					resource.TestCheckResourceAttrSet("medplum_client_application.test", "secret"),
				),
			},
			{Config: `
resource "medplum_client_application" "test" {
  name        = "tf-acc-client"
  description = "acc test"
}`, PlanOnly: true},
			{
				ResourceName:            "medplum_client_application.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"secret"},
			},
		},
	})
}
