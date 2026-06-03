# A project-scoped user with an initial password.
# Requires email + project_id when password is set.
resource "medplum_user" "clinician" {
  first_name = "Jane"
  last_name  = "Smith"
  email      = "jane.smith@example.com"
  project_id = "11111111-1111-1111-1111-111111111111"

  # The password is write-only; it is applied via /setpassword and never read back.
  password = var.clinician_initial_password
}

# A server-scoped user (no project_id) — suitable for admin accounts.
resource "medplum_user" "admin_user" {
  first_name = "Platform"
  last_name  = "Admin"
  email      = "platform-admin@example.com"
  admin      = true
}
