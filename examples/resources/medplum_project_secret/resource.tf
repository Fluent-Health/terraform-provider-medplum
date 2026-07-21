# A named secret in the session project's Project.secret[] settings. Bots
# receive every project secret at execution time as event.secrets.
resource "medplum_project_secret" "webhook_token" {
  name         = "WEBHOOK_TOKEN"
  value_string = var.webhook_token
}

# Each entry is its own resource: many secrets can be applied in the same run
# (writes to the shared Project resource are serialized with optimistic
# concurrency), and entries not managed by Terraform are left untouched.
resource "medplum_project_secret" "smtp_password" {
  name         = "SMTP_PASSWORD"
  value_string = var.smtp_password
}

# A bot reading the secrets:
#   exports.handler = async (medplum, event) => {
#     const token = event.secrets["WEBHOOK_TOKEN"].valueString;
#     ...
#   };
