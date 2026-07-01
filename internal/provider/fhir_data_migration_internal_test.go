package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func testMigrationModel() dataMigrationModel {
	return dataMigrationModel{
		Name:               types.StringValue("diet-1001-to-semantic"),
		TargetResourceType: types.StringValue("QuestionnaireResponse"),
		Search:             types.StringValue("questionnaire=X"),
		MarkerSystem:       types.StringValue("urn:terraform-provider-medplum:data-migration"),
		BundleType:         types.StringValue("batch"),
		PageSize:           types.Int64Value(50),
		CodeRemap: []codeRemapBlock{{
			From: codingObj{System: types.StringValue("http://old"), Code: types.StringValue("1001")},
			To:   codingObj{System: types.StringValue("http://new"), Code: types.StringValue("breakfast"), Display: types.StringValue("Breakfast")},
		}},
	}
}

func TestToSpec_mapsRemaps(t *testing.T) {
	s := testMigrationModel().toSpec()
	if s.TargetResourceType != "QuestionnaireResponse" || s.Search != "questionnaire=X" {
		t.Fatalf("bad spec scalars: %+v", s)
	}
	if len(s.Remaps) != 1 || s.Remaps[0].From.Code != "1001" || s.Remaps[0].To.Code != "breakfast" || s.Remaps[0].To.Display != "Breakfast" {
		t.Fatalf("bad remap mapping: %+v", s.Remaps)
	}
}

func TestConfigUnchanged(t *testing.T) {
	a := testMigrationModel()
	b := testMigrationModel()
	if !configUnchanged(a, b) {
		t.Fatal("identical configs must compare equal")
	}
	b.Search = types.StringValue("questionnaire=Y")
	if configUnchanged(a, b) {
		t.Fatal("changed search must compare unequal")
	}
	c := testMigrationModel()
	c.CodeRemap[0].To.Code = types.StringValue("dinner")
	if configUnchanged(a, c) {
		t.Fatal("changed remap must compare unequal")
	}
}
