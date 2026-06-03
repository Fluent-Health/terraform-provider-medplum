# Create a new Medplum project.
# Project creation calls Project/$init and requires super-admin credentials.
# Configure the provider with email/password or a super-admin access_token.
resource "medplum_project" "patient_portal" {
  name        = "patient-portal"
  description = "Patient-facing portal for appointment scheduling and records access."

  # Optional list of feature flags to enable on the project.
  features = ["smart-app-launch"]
}
