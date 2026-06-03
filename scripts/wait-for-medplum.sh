#!/usr/bin/env bash
# Waits for the Medplum server healthcheck endpoint to respond.
set -euo pipefail

URL="${1:-http://localhost:8103/healthcheck}"
for i in $(seq 1 60); do
  if curl -fsS "$URL" >/dev/null 2>&1; then
    echo "Medplum is up"
    exit 0
  fi
  echo "waiting for Medplum ($i)..."
  sleep 5
done
echo "Medplum did not become healthy in time" >&2
exit 1
