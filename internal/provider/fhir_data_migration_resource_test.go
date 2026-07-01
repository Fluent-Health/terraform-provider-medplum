package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/client"
)

// newAccClient builds a client from the same env vars the provider uses.
func newAccClient(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.New(context.Background(), client.Config{
		BaseURL:      os.Getenv("MEDPLUM_BASE_URL"),
		FHIRPath:     os.Getenv("MEDPLUM_FHIR_PATH"),
		TokenURL:     os.Getenv("MEDPLUM_TOKEN_URL"),
		ClientID:     os.Getenv("MEDPLUM_CLIENT_ID"),
		ClientSecret: os.Getenv("MEDPLUM_CLIENT_SECRET"),
		AccessToken:  os.Getenv("MEDPLUM_ACCESS_TOKEN"),
		Email:        os.Getenv("MEDPLUM_EMAIL"),
		Password:     os.Getenv("MEDPLUM_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("build acc client: %v", err)
	}
	return c
}

func seedDietQR(t *testing.T, c *client.Client, questionnaireURL, code string) string {
	t.Helper()
	body := fmt.Sprintf(`{
		"resourceType":"QuestionnaireResponse",
		"status":"completed",
		"questionnaire":%q,
		"item":[{"linkId":"diet","answer":[{"valueCoding":{"system":"http://example.com/old-diet","code":%q}}]}]
	}`, questionnaireURL, code)
	out, err := c.FHIRCreate(context.Background(), "QuestionnaireResponse", []byte(body))
	if err != nil {
		t.Fatalf("seed QR: %v", err)
	}
	var doc struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &doc); err != nil || doc.ID == "" {
		t.Fatalf("seed QR: no id in %s", out)
	}
	return doc.ID
}

func readDietCode(t *testing.T, c *client.Client, id string) (system, code string, hasMarker bool) {
	t.Helper()
	out, err := c.FHIRRead(context.Background(), "QuestionnaireResponse", id)
	if err != nil {
		t.Fatalf("read QR %s: %v", id, err)
	}
	var doc struct {
		Meta struct {
			Tag []struct {
				System string `json:"system"`
			} `json:"tag"`
		} `json:"meta"`
		Item []struct {
			Answer []struct {
				ValueCoding struct {
					System string `json:"system"`
					Code   string `json:"code"`
				} `json:"valueCoding"`
			} `json:"answer"`
		} `json:"item"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("parse QR %s: %v", id, err)
	}
	for _, tg := range doc.Meta.Tag {
		if strings.HasPrefix(tg.System, "urn:terraform-provider-medplum:data-migration/") {
			hasMarker = true
		}
	}
	vc := doc.Item[0].Answer[0].ValueCoding
	return vc.System, vc.Code, hasMarker
}

func TestAccFHIRDataMigration_rewritesAndIsIdempotent(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Acceptance tests skipped unless env 'TF_ACC' set")
	}
	suffix := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	qURL := "http://example.com/fhir/Questionnaire/tf-acc-diet-" + suffix
	c := newAccClient(t)

	var id1, id2 string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Seed two QRs with the old code just before the first apply.
				PreConfig: func() {
					id1 = seedDietQR(t, c, qURL, "1001")
					id2 = seedDietQR(t, c, qURL, "1001")
					t.Cleanup(func() {
						_ = c.FHIRDelete(context.Background(), "QuestionnaireResponse", id1)
						_ = c.FHIRDelete(context.Background(), "QuestionnaireResponse", id2)
					})
				},
				Config: testAccDataMigrationConfig(suffix, "breakfast"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("medplum_fhir_data_migration.diet", "scanned_count", "2"),
					resource.TestCheckResourceAttr("medplum_fhir_data_migration.diet", "changed_count", "2"),
					resource.TestCheckResourceAttr("medplum_fhir_data_migration.diet", "failed_count", "0"),
				),
			},
			{
				// TF-level idempotency: unchanged config plans as a no-op.
				Config:   testAccDataMigrationConfig(suffix, "breakfast"),
				PlanOnly: true,
			},
			{
				// Spec change (new "to" code -> new hash) re-runs. Codes already
				// migrated to "breakfast" no longer match old|1001, so changed=0,
				// proving fixed-point convergence, while scanned=2 (re-tagged at new hash).
				Config: testAccDataMigrationConfig(suffix, "brunch"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("medplum_fhir_data_migration.diet", "scanned_count", "2"),
					resource.TestCheckResourceAttr("medplum_fhir_data_migration.diet", "changed_count", "0"),
				),
			},
		},
	})

	// After the run, assert server state directly.
	for _, id := range []string{id1, id2} {
		sys, code, marker := readDietCode(t, c, id)
		if sys != "http://example.com/new-diet" || code != "breakfast" {
			t.Fatalf("QR %s not rewritten: system=%s code=%s", id, sys, code)
		}
		if !marker {
			t.Fatalf("QR %s missing migration marker tag", id)
		}
	}
}

func testAccDataMigrationConfig(suffix, toCode string) string {
	return fmt.Sprintf(`
resource "medplum_fhir_data_migration" "diet" {
  name                 = "tf-acc-diet-%s"
  target_resource_type = "QuestionnaireResponse"
  search               = "questionnaire=http://example.com/fhir/Questionnaire/tf-acc-diet-%s"

  code_remap {
    from = { system = "http://example.com/old-diet", code = "1001" }
    to   = { system = "http://example.com/new-diet", code = %q }
  }
}
`, suffix, suffix, toCode)
}
