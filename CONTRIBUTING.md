# Contributing

Thanks for your interest in contributing.

## Reporting bugs

Open an issue describing the problem, steps to reproduce, the provider version, and the Medplum server version.

## Proposing changes

1. Fork the repository.
2. Create a branch: `git checkout -b my-fix`.
3. Make your changes and commit with a clear message.
4. Open a pull request against `main`.

## Development setup

### Prerequisites

- Go 1.22+
- Docker (for running Medplum locally during acceptance tests)
- Terraform CLI

### Build

```sh
make build
```

### Run a local Medplum

```sh
docker compose -f docker-compose.test.yml up -d
./scripts/wait-for-medplum.sh
```

### Run tests

```sh
# Unit tests
go test ./...

# Acceptance tests (needs Medplum running)
make testacc
```

The acceptance tests require the following environment variables (defaults shown match the local docker-compose stack):

| Variable | Default |
|---|---|
| `MEDPLUM_BASE_URL` | `http://localhost:8103` |
| `MEDPLUM_EMAIL` | `admin@example.com` |
| `MEDPLUM_PASSWORD` | `medplum_admin` |
| `TF_ACC` | `1` |

### Regenerate documentation

Run the following after any schema changes:

```sh
make doc
```

## Pull request checklist

- [ ] `make build` passes
- [ ] Unit tests pass (`go test ./...`)
- [ ] `make testacc` passes for any changed or added resources
- [ ] New resources / attributes have acceptance tests covering create, read, update, and import
- [ ] `make doc` was run if schemas changed
- [ ] PR description explains the change and links any related issues

## Release process

Releases are cut manually by a maintainer:

1. Update `CHANGELOG.md`.
2. Tag the commit: `git tag vX.Y.Z && git push origin vX.Y.Z`.
3. The [release workflow](.github/workflows/release-go.yml) builds, GPG-signs, and publishes the GitHub release via GoReleaser.
4. The Terraform Registry picks up the new version via its GitHub webhook.

## No CLA required

You do not need to sign a Contributor License Agreement. By submitting a pull request, you agree to license your contribution under the repository's existing [LICENSE](./LICENSE).

## Maintainer one-time setup

Before the release workflow can publish signed GitHub releases, a maintainer must complete the following steps once:

### GitHub Environment

Create a GitHub Environment named `release` in the repository settings and add the following secrets:

| Secret | Description |
|---|---|
| `GPG_PRIVATE_KEY` | ASCII-armoured GPG private key used to sign `SHA256SUMS` |
| `PASSPHRASE` | Passphrase protecting the GPG private key |

### Terraform Registry registration

1. Sign in to [registry.terraform.io](https://registry.terraform.io) with the GitHub account that owns the repository.
2. Click **Publish → Provider** and select `Fluent-Health/terraform-provider-medplum`.
3. Upload the GPG public key (corresponding to `GPG_PRIVATE_KEY` above) under **GPG Keys** in your namespace settings so the Registry can verify release signatures.
