package provider

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
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

func TestClientApplication_toFHIR_IdentityProvider(t *testing.T) {
	m := clientApplicationModel{
		Name:        types.StringValue("test-app"),
		Description: types.StringNull(),
		RedirectURI: types.StringNull(),
		Secret:      types.StringNull(),
		IdentityProvider: &identityProviderModel{
			AuthorizeURL: types.StringValue("https://idp.example.com/auth"),
			TokenURL:     types.StringNull(),
			UserInfoURL:  types.StringNull(),
			ClientID:     types.StringValue("my-client-id"),
			ClientSecret: types.StringNull(),
			UseSubject:   types.BoolValue(true),
		},
	}
	b, err := m.toFHIR("", "")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	ipRaw, ok := doc["identityProvider"]
	if !ok {
		t.Fatalf("expected identityProvider in output; got: %s", b)
	}
	ip, ok := ipRaw.(map[string]any)
	if !ok {
		t.Fatalf("identityProvider is not an object: %T", ipRaw)
	}
	if ip["authorizeUrl"] != "https://idp.example.com/auth" {
		t.Errorf("authorizeUrl: got %v", ip["authorizeUrl"])
	}
	if ip["clientId"] != "my-client-id" {
		t.Errorf("clientId: got %v", ip["clientId"])
	}
	if ip["useSubject"] != true {
		t.Errorf("useSubject: got %v", ip["useSubject"])
	}
	// Fields with null/empty values must be omitted.
	if _, present := ip["tokenUrl"]; present {
		t.Errorf("tokenUrl should be omitted when null")
	}
	if _, present := ip["clientSecret"]; present {
		t.Errorf("clientSecret should be omitted when null")
	}
}

func TestClientApplication_fromFHIR_NilIdentityProvider(t *testing.T) {
	raw := []byte(`{"id":"abc","name":"test","secret":"s3cr3t"}`)
	var m clientApplicationModel
	if err := m.fromFHIR(raw); err != nil {
		t.Fatal(err)
	}
	if m.IdentityProvider != nil {
		t.Errorf("expected nil IdentityProvider when server omits it")
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
