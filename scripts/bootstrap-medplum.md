# Medplum Bootstrap for Acceptance Tests

## Overview

The `docker-compose.test.yml` stack starts a self-contained Medplum server (image
`medplum/medplum-server:5.1.14`) backed by Postgres 16 and Redis 7.

Acceptance tests (`TF_ACC=1`) authenticate against this server using a super-admin
account. This note documents the mechanism for obtaining credentials and flags an
open follow-up for CI.

## Default admin credentials (dev image)

On first boot, the Medplum dev/server image auto-initialises a default project and a
super-admin user. The documented defaults for the dev image are:

| Field    | Value             |
|----------|-------------------|
| Email    | `admin@example.com` |
| Password | `medplum_admin`   |

These are surfaced in the container startup logs and are consistent with the Medplum
open-source development environment documentation.

## How CI obtains credentials

1. Start the stack: `docker compose -f docker-compose.test.yml up -d`
2. Wait for the server: `./scripts/wait-for-medplum.sh`
3. Set environment variables for the acceptance tests:
   ```bash
   export MEDPLUM_SERVER_URL="http://localhost:8103/"
   export MEDPLUM_CLIENT_ID="<client-id>"
   export MEDPLUM_CLIENT_SECRET="<client-secret>"
   ```
   A client application must be created (or already exists) in the default project.
   The super-admin credentials above can be used to authenticate and retrieve or
   create a client app via the Medplum REST API before running tests.

## Follow-up (acceptance-test task)

> **TODO:** Confirm the exact default-admin mechanism for pinned image
> `medplum/medplum-server:5.1.14`. Specifically:
> - Verify that `admin@example.com` / `medplum_admin` are valid on first boot.
> - Determine whether `MEDPLUM_ADMIN_*` environment variables override these defaults.
> - Document the exact API call to exchange admin credentials for a client app
>   `clientId`/`clientSecret` suitable for the Terraform provider's OAuth2 client
>   credentials flow.
> - Update this file with the verified steps once confirmed.
