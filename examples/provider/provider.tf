terraform {
  required_providers {
    medplum = {
      source  = "Fluent-Health/medplum"
      version = "~> 1.0"
    }
  }
}

# Option 1 (recommended for automation): OAuth client credentials.
# Set MEDPLUM_CLIENT_ID and MEDPLUM_CLIENT_SECRET in the environment, or inline:
provider "medplum" {
  base_url = "https://medplum.example.com"

  client_id     = var.medplum_client_id
  client_secret = var.medplum_client_secret

  # Pin the Medplum server version so the profile-enforcement analysis
  # uses the correct support matrix (default: 5.0.10).
  medplum_version = "5.0.10"
}

# Option 2: pre-obtained bearer token (e.g. from a CI secrets store).
# provider "medplum" {
#   base_url     = "https://medplum.example.com"
#   access_token = var.medplum_access_token
# }

# Option 3: super-admin email + password (required for medplum_project).
# provider "medplum" {
#   base_url = "https://medplum.example.com"
#   email    = var.medplum_email
#   password = var.medplum_password
# }
