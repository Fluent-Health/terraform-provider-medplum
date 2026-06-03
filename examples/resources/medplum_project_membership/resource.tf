resource "medplum_client_application" "api_service" {
  name = "api-service"
}

resource "medplum_access_policy" "service_policy" {
  name = "api-service-policy"

  resource {
    resource_type = "Patient"
    interaction   = ["read", "search"]
  }

  resource {
    resource_type = "Observation"
    interaction   = ["read", "write", "create", "search"]
  }
}

# Bind the client application to a project with a scoped access policy.
# The project, user, and profile fields are immutable; changing them forces
# a new membership to be created.
resource "medplum_project_membership" "api_service" {
  project = "Project/11111111-1111-1111-1111-111111111111"

  # For a ClientApplication the user and profile both reference the same resource.
  user    = "ClientApplication/${medplum_client_application.api_service.id}"
  profile = "ClientApplication/${medplum_client_application.api_service.id}"

  access_policy = "AccessPolicy/${medplum_access_policy.service_policy.id}"
}
