# Rewrite stored QuestionnaireResponse answers from old numeric diet codes to
# new semantic codes. Run this after the ValueSet/CodeSystem that defines the
# codes has been updated, so the stored data stays consistent with it.
#
# The migration is idempotent: every scanned resource is marked with a meta.tag,
# the scan skips already-marked resources, and re-running converges. An
# unchanged configuration plans as a no-op.
resource "medplum_fhir_data_migration" "diet_codes" {
  name                 = "diet-numeric-to-semantic"
  target_resource_type = "QuestionnaireResponse"

  # Narrow the scan. FHIR has no search parameter for a coded answer value, so
  # the provider searches by an indexable field and matches codings in memory.
  search = "questionnaire=DietIntake"

  code_remap {
    from = { system = "http://example.com/diet", code = "1001" }
    to   = { system = "http://example.com/diet", code = "breakfast" }
  }

  code_remap {
    from = { system = "http://example.com/diet", code = "1002" }
    to   = { system = "http://example.com/diet", code = "lunch" }
  }

  # One bundle per page; default is 50.
  page_size = 100

  # Tip: use `depends_on` to order the migration after the terminology resource
  # that introduced the new codes, e.g.
  #   depends_on = [medplum_fhir_resource.valueset_diet]
}

# Fix a Condition.severity coding stored with no system at all: match by code
# alone (empty from.system) and add the correct SNOMED system + code.
resource "medplum_fhir_data_migration" "condition_severity_moderate" {
  name                 = "severity-moderate-snomed-6736007-condition"
  target_resource_type = "Condition"
  search               = "severity=1255665007"

  code_remap {
    from = { system = "", code = "1255665007" }
    to   = { system = "http://snomed.info/sct", code = "6736007", display = "Moderate" }
  }
}
