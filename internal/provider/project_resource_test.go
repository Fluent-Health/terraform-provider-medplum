package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccProject_basic(t *testing.T) {
	suffix := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	name := "tf-acc-project-" + suffix
	basic := fmt.Sprintf("resource \"medplum_project\" \"test\" {\n  name = %q\n}\n", name)
	withDesc := fmt.Sprintf("resource \"medplum_project\" \"test\" {\n  name        = %q\n  description = \"updated\"\n}\n", name)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: basic,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_project.test", "id"),
					resource.TestCheckResourceAttr("medplum_project.test", "name", name),
				),
			},
			{Config: basic, PlanOnly: true},
			{
				Config: withDesc,
				Check:  resource.TestCheckResourceAttr("medplum_project.test", "description", "updated"),
			},
			{ResourceName: "medplum_project.test", ImportState: true, ImportStateVerify: true},
		},
	})
}

func TestProject_toFHIR(t *testing.T) {
	m := projectModel{
		Name:        typesStr("Acme"),
		Description: typesStr("d"),
		Features:    stringsToList([]string{"bots"}),
	}
	b, err := m.toFHIR("p1")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(b), `"id":"p1"`) || !contains(string(b), `"bots"`) {
		t.Fatalf("unexpected body: %s", b)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
