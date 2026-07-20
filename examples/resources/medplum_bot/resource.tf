# The access policy the bot runs under.
resource "medplum_access_policy" "bot_policy" {
  name = "bot-policy"
  resource {
    resource_type = "Patient"
    readonly      = true
  }
}

# A bot whose bundled code lives next to the Terraform module. Editing the
# bundle and running `terraform apply` deploys the new code live.
resource "medplum_bot" "subscription_handler" {
  name          = "subscription-handler"
  description   = "Processes Patient change notifications."
  source_path   = "${path.module}/dist/subscription-handler.js"
  timeout       = 30
  access_policy = "AccessPolicy/${medplum_access_policy.bot_policy.id}"
}

# A trivial inline bot. Prefer source_path for anything real: inline code is
# stored in Terraform state.
resource "medplum_bot" "ping" {
  name = "ping"
  code = "exports.handler = async () => \"pong\";"
}
