package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestGenerateClientSecret_UniqueAndLong(t *testing.T) {
	a, err := generateClientSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, err := generateClientSecret()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("expected unique secrets")
	}
	if len(a) < 40 {
		t.Fatalf("secret too short: %d", len(a))
	}
}

func TestAccClientApplication_basic(t *testing.T) {
	suffix := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	cfg := func() string {
		return fmt.Sprintf(`
resource "medplum_client_application" "test" {
  name        = "tf-acc-client-%s"
  description = "acc test"
}`, suffix)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_client_application.test", "id"),
					resource.TestCheckResourceAttrSet("medplum_client_application.test", "secret"),
				),
			},
			{Config: cfg(), PlanOnly: true},
			{
				Config: cfg(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_client_application.test", "secret"),
				),
			},
			{
				ResourceName:            "medplum_client_application.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"secret"},
			},
		},
	})
}
