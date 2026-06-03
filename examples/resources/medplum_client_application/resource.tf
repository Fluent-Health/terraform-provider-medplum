# A basic client application — the provider generates a secret if none is set.
resource "medplum_client_application" "api_service" {
  name        = "api-service"
  description = "Backend service for the patient portal API."
}

# A client application with an external OIDC identity provider for token exchange.
resource "medplum_client_application" "sso_app" {
  name         = "sso-app"
  description  = "Application that delegates authentication to an external IdP."
  redirect_uri = "https://app.example.com/auth/callback"

  identity_provider {
    authorize_url = "https://idp.example.com/oauth2/authorize"
    token_url     = "https://idp.example.com/oauth2/token"
    user_info_url = "https://idp.example.com/oauth2/userinfo"
    client_id     = var.idp_client_id
    client_secret = var.idp_client_secret
    use_subject   = true
  }
}
