package provider

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testAccMembershipConfig = `
resource "medplum_access_policy" "p" {
  name = "tf-acc-mbr-policy"
  resource { resource_type = "Patient" }
}

resource "medplum_client_application" "c" {
  name = "tf-acc-mbr-client"
}

resource "medplum_project_membership" "m" {
  project       = "Project/${var_project_id}"
  user          = medplum_client_application.c.id
  profile       = medplum_client_application.c.id
  access_policy = medplum_access_policy.p.id
}
`

func TestAccProjectMembership_bindsClient(t *testing.T) {
	t.Skip("requires a known project id; enable once project bootstrap exposes MEDPLUM_TEST_PROJECT_ID")
	// Implementation note for the executor: replace ${var_project_id} with the
	// CI project id (from MEDPLUM_TEST_PROJECT_ID) and unskip. medplum_client_application.c.id
	// is "ClientApplication/<uuid>"; user and profile both reference the client.
	_ = testAccMembershipConfig
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    []resource.TestStep{},
	})
}

func TestProjectMembership_toFHIR(t *testing.T) {
	m := projectMembershipModel{
		Project:      typesStr("Project/p1"),
		User:         typesStr("ClientApplication/c1"),
		Profile:      typesStr("ClientApplication/c1"),
		AccessPolicy: typesStr("AccessPolicy/a1"),
	}
	b, err := m.toFHIR("")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	if doc["resourceType"] != "ProjectMembership" {
		t.Fatalf("bad resourceType: %v", doc["resourceType"])
	}
	ap, _ := doc["accessPolicy"].(map[string]any)
	if ap["reference"] != "AccessPolicy/a1" {
		t.Fatalf("bad accessPolicy ref: %v", doc["accessPolicy"])
	}
}

func typesStr(s string) types.String { return types.StringValue(s) }
