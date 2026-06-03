# Medplum Bootstrap for Acceptance Tests

## Overview

The `docker-compose.test.yml` stack starts a self-contained Medplum server (image
`medplum/medplum-server:5.1.14`) backed by Postgres 16 and Redis 7.

The server `ENTRYPOINT` is `node ... packages/server/dist/index.js`; the config source
is passed as the `command:` argument. We use **`env`**, which makes the server read its
configuration from `MEDPLUM_*` environment variables (see `src/config/loader.ts`, the
`case 'env'` branch; without an argument the server defaults to
`file:medplum.config.json` and fails to boot).

## Default admin credentials (confirmed against v5.1.14 source)

On startup the server calls `seedDatabase(config)` (`src/app.ts`), which — if the database
is not already seeded — creates the first project and a super-admin user
(`src/seed.ts`):

| Field    | Value               | Override (env)                         |
|----------|---------------------|----------------------------------------|
| Email    | `admin@example.com` | `MEDPLUM_DEFAULTSUPERADMINEMAIL`       |
| Password | `medplum_admin`     | `MEDPLUM_DEFAULTSUPERADMINPASSWORD`    |

Seeding also rebuilds R4 StructureDefinitions, SearchParameters, and ValueSets, so the
**first boot is slow** (the healthcheck `start_period` and `wait-for-medplum.sh` timeout
account for this).

## How CI authenticates

The Terraform provider's super-admin login mode is the simplest path for CI (no
pre-existing client app needed). The acceptance job sets:

```bash
export MEDPLUM_BASE_URL="http://localhost:8103"
export MEDPLUM_EMAIL="admin@example.com"
export MEDPLUM_PASSWORD="medplum_admin"
export TF_ACC=1
```

The provider's `login()` POSTs to `${MEDPLUM_BASE_URL}/auth/login` and reads `accessToken`
from the response.

## Open items to verify on first live CI run (Task 10)

These could not be validated without booting the image; confirm them when CI first runs:

1. **`/auth/login` response shape.** Confirm Medplum returns a usable `accessToken`
   directly for the seeded super admin. If it instead returns a `login`/`code` requiring a
   project-selection / token-exchange step (`/auth/profile`, `/oauth2/token`), the
   provider's `login()` will need that second step, or CI should instead create a
   `ClientApplication` and use client-credentials.
2. **JWT signing / other required config.** Verify the server boots and issues tokens with
   only the env vars in `docker-compose.test.yml` (e.g. whether a signing key must be
   provided rather than auto-generated).
3. **Numeric env coercion** (`MEDPLUM_DATABASE_PORT`, `MEDPLUM_PORT`) is handled by the
   loader.
4. Update this file with the verified, exact steps once the first CI run is green.
