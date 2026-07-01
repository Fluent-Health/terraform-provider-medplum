# Count how many resources match a FHIR search (via `_summary=count`). Useful
# for previewing migration scope — combine with the migration's own `_tag:not`
# marker to see how many resources remain unmigrated.
data "medplum_fhir_search" "diet_intake" {
  target_resource_type = "QuestionnaireResponse"
  search               = "questionnaire=DietIntake"
}

output "diet_intake_total" {
  value = data.medplum_fhir_search.diet_intake.total
}
