# Terraform Provider for Medplum

A Terraform provider for managing [Medplum](https://www.medplum.com) FHIR resources, access policies, projects, client applications, project memberships, users, and FHIR profiles.

## Resources

| Resource | Purpose |
|---|---|
| `medplum_fhir_resource` | Manage any arbitrary FHIR R4 resource stored in Medplum by resource type and ID |
| `medplum_access_policy` | Manage Access Policy resources controlling per-resource-type read/write permissions |
| `medplum_client_application` | Manage ClientApplication resources for machine-to-machine OAuth 2.0 clients |
| `medplum_project_membership` | Manage ProjectMembership resources that bind user/client profiles to a project |
| `medplum_user` | Manage Medplum User resources |
| `medplum_project` | Manage Medplum Project resources |
| `medplum_fhir_profile` | Manage FHIR StructureDefinition (profile) resources stored in Medplum |

Full per-resource docs in [`docs/resources/`](./docs/resources/).

## Requirements

- [Terraform](https://www.terraform.io/downloads.html) >= 1.0
- [Go](https://golang.org/doc/install) 1.22+ (for building and testing the provider from source; `make doc` requires Go 1.25+ for the tfplugindocs tool)
- [Medplum](https://www.medplum.com) server 5.0.x

## Provider configuration

```hcl
terraform {
  required_providers {
    medplum = {
      source  = "Fluent-Health/medplum"
      version = "~> 0.1"
    }
  }
}

provider "medplum" {
  base_url = "https://medplum.example.com"

  # Authenticate with client credentials (recommended for automation)
  client_id     = var.medplum_client_id
  client_secret = var.medplum_client_secret

  # Alternatively, authenticate with email + password
  # email    = var.medplum_email
  # password = var.medplum_password

  # Or supply a pre-obtained access token
  # access_token = var.medplum_access_token

  # Medplum server version — used to select the correct API behaviour.
  # Defaults to 5.0.10 if omitted.
  medplum_version = "5.0.10"
}
```

### Example resource

```hcl
resource "medplum_access_policy" "read_only_observations" {
  resource_json = jsonencode({
    resourceType = "AccessPolicy"
    name         = "ReadOnlyObservations"
    resource = [
      {
        resourceType = "Observation"
        readonly     = true
      }
    ]
  })
}
```

## Building and testing

### Build

```sh
git clone https://github.com/Fluent-Health/terraform-provider-medplum
cd terraform-provider-medplum
make build
```

### Unit tests

```sh
go test ./...
```

### Acceptance tests

Acceptance tests require a running Medplum instance. The included docker-compose file provides one:

```sh
docker compose -f docker-compose.test.yml up -d
./scripts/wait-for-medplum.sh

MEDPLUM_BASE_URL=http://localhost:8103 \
MEDPLUM_EMAIL=admin@example.com \
MEDPLUM_PASSWORD=medplum_admin \
TF_ACC=1 \
make testacc

docker compose -f docker-compose.test.yml down
```

## Releasing

Releases are cut by pushing a `v*` tag. [GoReleaser](https://goreleaser.com) builds the per-platform archives, signs `SHA256SUMS` with GPG, and publishes a GitHub Release that the Terraform Registry ingests automatically.

```sh
git tag vX.Y.Z
git push origin vX.Y.Z
```

The release workflow lives in [`.github/workflows/release-go.yml`](.github/workflows/release-go.yml) and requires the `GPG_PRIVATE_KEY` and `PASSPHRASE` secrets to be configured in the `release` GitHub Environment (see [CONTRIBUTING.md](./CONTRIBUTING.md) for one-time setup instructions).

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md). Bug reports and pull requests are welcome.

## License

[Apache 2.0](./LICENSE)
