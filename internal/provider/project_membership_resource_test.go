package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/client"
)

var discoveredProjectID string

func testAccProjectID(t *testing.T) string {
	if discoveredProjectID != "" {
		return discoveredProjectID
	}
	cfg := client.Config{
		BaseURL:      os.Getenv("MEDPLUM_BASE_URL"),
		Email:        os.Getenv("MEDPLUM_EMAIL"),
		Password:     os.Getenv("MEDPLUM_PASSWORD"),
		ClientID:     os.Getenv("MEDPLUM_CLIENT_ID"),
		ClientSecret: os.Getenv("MEDPLUM_CLIENT_SECRET"),
		AccessToken:  os.Getenv("MEDPLUM_ACCESS_TOKEN"),
	}
	c, err := client.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("discover project id: client.New: %v", err)
	}
	pid, err := c.CurrentProjectID(context.Background())
	if err != nil {
		t.Fatalf("discover project id: %v", err)
	}
	discoveredProjectID = pid
	return pid
}

func TestAccProjectMembership_bindsClient(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance test")
	}
	suffix := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "medplum_access_policy" "p" {
  name = "tf-acc-mbr-policy-%[1]s"
  resource { resource_type = "Patient" }
}
resource "medplum_client_application" "c" {
  name = "tf-acc-mbr-client-%[1]s"
}
resource "medplum_project_membership" "m" {
  project       = "Project/%[2]s"
  user          = medplum_client_application.c.id
  profile       = medplum_client_application.c.id
  access_policy = medplum_access_policy.p.id
}
`, suffix, testAccProjectID(t)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_project_membership.m", "id"),
					resource.TestCheckResourceAttrSet("medplum_project_membership.m", "access_policy"),
				),
			},
		},
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
