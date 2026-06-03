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

## Login flow (verified against live Medplum in CI)

The seeded super admin's `/auth/login` does **not** return an access token directly. The
provider's `login()` implements the full native flow:

1. Generate a PKCE pair (`code_verifier` + S256 `code_challenge`).
2. `POST /auth/login` `{email, password, codeChallenge, codeChallengeMethod: "S256"}` →
   `{login, code}` (or `{login, memberships}`, handled via `POST /auth/profile`).
3. `POST /oauth2/token` (form) `grant_type=authorization_code&code=<code>&code_verifier=<verifier>`
   → `{access_token}`.

PKCE is **required**: a clientless authorization_code exchange without a `code_challenge` is
rejected with `invalid_request: "Missing verification context"` (server `oauth/token.ts`).

## Resolved follow-ups (first green CI run, 2026-06-03)

1. **`/auth/login` response shape** — resolved: it's the PKCE login → `/oauth2/token`
   exchange documented above (not a direct `accessToken`).
2. **JWT signing / required config** — the server boots and issues tokens with only the env
   vars in `docker-compose.test.yml`; no explicit signing key needed.
3. **Numeric env coercion** (`MEDPLUM_DATABASE_PORT`, `MEDPLUM_PORT`) — handled by the loader.
4. **Rate limiting** — Medplum throttles `/auth/login` to 5/min (`defaultLoginRateLimit`); the
   acceptance suite's many terraform subprocess logins tripped HTTP 429. Disabled in the test
   instance via `MEDPLUM_DEFAULT_RATE_LIMIT: "-1"` (Medplum's own test-config approach).
