package provider

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccUser_serverScoped(t *testing.T) {
	suffix := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	email := "tf-acc-" + suffix + "@example.com"
	cfg := func(last string) string {
		return fmt.Sprintf(`resource "medplum_user" "test" {
  first_name = "Acc"
  last_name  = %q
  email      = %q
}`, last, email)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg("One"), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet("medplum_user.test", "id"),
				resource.TestCheckResourceAttr("medplum_user.test", "last_name", "One"),
			)},
			{Config: cfg("One"), PlanOnly: true},
			{Config: cfg("Two"), Check: resource.TestCheckResourceAttr("medplum_user.test", "last_name", "Two")},
			{ResourceName: "medplum_user.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"password"}},
		},
	})
}

func TestUser_toFHIR_ProjectScope(t *testing.T) {
	m := userModel{
		FirstName: typesStr("Jane"),
		LastName:  typesStr("Doe"),
		Email:     typesStr("jane@example.com"),
		ProjectID: typesStr("p1"),
	}
	b, err := m.toFHIR("")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	proj, _ := doc["project"].(map[string]any)
	if proj["reference"] != "Project/p1" {
		t.Fatalf("bad project ref: %v", doc["project"])
	}
}
