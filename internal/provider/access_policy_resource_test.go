package provider

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccessPolicy_toFHIR_CompartmentAndInteraction(t *testing.T) {
	m := accessPolicyModel{
		Name:        types.StringValue("test-policy"),
		Compartment: types.StringValue("%profile"),
		Resource: []accessPolicyResourceRow{
			{
				ResourceType:   types.StringValue("Patient"),
				Criteria:       types.StringNull(),
				Readonly:       types.BoolNull(),
				HiddenFields:   types.ListNull(types.StringType),
				ReadonlyFields: types.ListNull(types.StringType),
				Compartment:    types.StringNull(),
				Interaction:    stringsToList([]string{"read", "search-type"}),
			},
		},
	}
	b, err := m.toFHIR("")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}

	// Assert top-level compartment.reference.
	cmpRaw, ok := doc["compartment"]
	if !ok {
		t.Fatalf("expected top-level compartment; got: %s", b)
	}
	cmp, ok := cmpRaw.(map[string]any)
	if !ok {
		t.Fatalf("compartment is not an object: %T", cmpRaw)
	}
	if cmp["reference"] != "%profile" {
		t.Errorf("compartment.reference: got %v", cmp["reference"])
	}

	// Assert per-row interaction array.
	resRaw, ok := doc["resource"]
	if !ok {
		t.Fatal("expected resource array")
	}
	resArr, ok := resRaw.([]any)
	if !ok || len(resArr) == 0 {
		t.Fatal("resource must be a non-empty array")
	}
	row, ok := resArr[0].(map[string]any)
	if !ok {
		t.Fatalf("resource[0] is not an object: %T", resArr[0])
	}
	interactionRaw, ok := row["interaction"]
	if !ok {
		t.Fatalf("expected interaction in resource[0]; got: %v", row)
	}
	interaction, ok := interactionRaw.([]any)
	if !ok {
		t.Fatalf("interaction is not an array: %T", interactionRaw)
	}
	if len(interaction) != 2 || interaction[0] != "read" || interaction[1] != "search-type" {
		t.Errorf("interaction: got %v", interaction)
	}
}

func TestAccessPolicy_toFHIR_NoCompartment(t *testing.T) {
	m := accessPolicyModel{
		Name:        types.StringValue("no-compartment"),
		Compartment: types.StringNull(),
		Resource:    nil,
	}
	b, err := m.toFHIR("")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["compartment"]; ok {
		t.Errorf("compartment should be omitted when null; got: %s", b)
	}
}

func TestAccessPolicy_fromFHIR_CompartmentAndInteraction(t *testing.T) {
	raw := []byte(`{
		"id": "pol1",
		"name": "test",
		"compartment": {"reference": "%profile"},
		"resource": [
			{"resourceType": "Patient", "interaction": ["read", "write"]}
		]
	}`)
	var m accessPolicyModel
	if err := m.fromFHIR(raw); err != nil {
		t.Fatal(err)
	}
	if m.Compartment.ValueString() != "%profile" {
		t.Errorf("Compartment: got %v", m.Compartment)
	}
	if m.Ref.ValueString() != "AccessPolicy/pol1" {
		t.Errorf("Ref: got %v", m.Ref)
	}
	if len(m.Resource) != 1 {
		t.Fatalf("expected 1 resource row, got %d", len(m.Resource))
	}
	ia := listToStrings(m.Resource[0].Interaction)
	if len(ia) != 2 || ia[0] != "read" || ia[1] != "write" {
		t.Errorf("Interaction: got %v", ia)
	}
}

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

// TestAccAccessPolicy_withCompartmentAndInteraction verifies that a top-level compartment
// and per-row interaction values round-trip without drift (no-op re-plan).
func TestAccAccessPolicy_withCompartmentAndInteraction(t *testing.T) {
	suffix := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	cfg := func() string {
		return fmt.Sprintf(`
resource "medplum_access_policy" "compartment_test" {
  name        = "tf-acc-policy-cmp-%s"
  compartment = "%%profile"

  resource {
    resource_type = "Observation"
    interaction   = ["read", "search-type"]
  }
}`, suffix)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_access_policy.compartment_test", "id"),
					resource.TestCheckResourceAttr("medplum_access_policy.compartment_test", "compartment", "%profile"),
					resource.TestCheckResourceAttr("medplum_access_policy.compartment_test", "resource.0.resource_type", "Observation"),
					resource.TestCheckResourceAttr("medplum_access_policy.compartment_test", "resource.0.interaction.0", "read"),
					resource.TestCheckResourceAttr("medplum_access_policy.compartment_test", "resource.0.interaction.1", "search-type"),
				),
			},
			{Config: cfg(), PlanOnly: true},
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
