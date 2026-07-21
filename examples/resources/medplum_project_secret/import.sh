# medplum_project_secret is imported by the secret's name (the entry key in
# the session project's Project.secret[] array).
terraform import medplum_project_secret.webhook_token WEBHOOK_TOKEN
