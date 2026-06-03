#!/usr/bin/env bash
# build-fsh.sh — compile FSH fixtures into a snapshot-bearing StructureDefinition
#
# 1. Ensures fsh-sushi and Java are available.
# 2. Downloads the HL7 IG Publisher jar (cached at ~/.fhir-ig-publisher/publisher.jar).
# 3. Runs the publisher on test/fsh/ with -tx n/a (no external terminology server).
# 4. Locates the snapshot-bearing SD for the fluent-patient profile.
# 5. Copies it to test/fsh/generated-sd.json and prints MEDPLUM_TEST_PROFILE_SD=<path>.
#    If GITHUB_ENV is set the variable is also appended there for subsequent steps.
#
# Usage: ./scripts/build-fsh.sh
set -euo pipefail

# ── Pinned versions ────────────────────────────────────────────────────────────
SUSHI_VERSION="fsh-sushi@3.16.3"
IG_PUBLISHER_VERSION="v1.8.5"
IG_PUBLISHER_JAR_URL="https://github.com/HL7/fhir-ig-publisher/releases/download/${IG_PUBLISHER_VERSION}/publisher.jar"
IG_PUBLISHER_JAR_FALLBACK="https://github.com/HL7/fhir-ig-publisher/releases/latest/download/publisher.jar"

PUBLISHER_CACHE_DIR="${HOME}/.fhir-ig-publisher"
PUBLISHER_JAR="${PUBLISHER_CACHE_DIR}/publisher.jar"

FSH_DIR="test/fsh"
GENERATED_SD_DEST="${FSH_DIR}/generated-sd.json"
SD_ID="fluent-patient"

# ── helpers ───────────────────────────────────────────────────────────────────
log()  { echo "[build-fsh] $*"; }
die()  { echo "[build-fsh] ERROR: $*" >&2; exit 1; }

# ── 1. Install SUSHI ──────────────────────────────────────────────────────────
if ! command -v sushi &>/dev/null; then
  log "Installing ${SUSHI_VERSION}..."
  npm install -g "${SUSHI_VERSION}"
else
  log "sushi already installed: $(sushi --version 2>/dev/null || true)"
fi

# ── 2. Download IG Publisher jar (skip if already cached) ─────────────────────
mkdir -p "${PUBLISHER_CACHE_DIR}"
if [[ ! -f "${PUBLISHER_JAR}" ]]; then
  log "Downloading IG Publisher ${IG_PUBLISHER_VERSION}..."
  if curl -fsSL --retry 3 "${IG_PUBLISHER_JAR_URL}" -o "${PUBLISHER_JAR}"; then
    log "Downloaded from pinned release: ${IG_PUBLISHER_JAR_URL}"
  else
    log "Pinned release failed, trying latest fallback..."
    curl -fsSL --retry 3 "${IG_PUBLISHER_JAR_FALLBACK}" -o "${PUBLISHER_JAR}" \
      || die "Failed to download IG Publisher jar from both pinned and fallback URLs"
    log "Downloaded from fallback: ${IG_PUBLISHER_JAR_FALLBACK}"
  fi
else
  log "IG Publisher jar already cached at ${PUBLISHER_JAR}"
fi

# ── 3. Run the IG Publisher ────────────────────────────────────────────────────
# The publisher runs SUSHI internally; -tx n/a disables external terminology
# server lookups so the build works without network access to tx.fhir.org.
log "Running IG Publisher on ${FSH_DIR}/ ..."
java -jar "${PUBLISHER_JAR}" \
  -ig "${FSH_DIR}" \
  -tx n/a \
  2>&1 | tee /tmp/ig-publisher-output.txt || {
  log "IG Publisher exited non-zero. Dumping last 50 lines of output:"
  tail -50 /tmp/ig-publisher-output.txt >&2
  # Also dump qa.txt if it exists
  if [[ -f "${FSH_DIR}/output/qa.txt" ]]; then
    log "qa.txt contents:"
    cat "${FSH_DIR}/output/qa.txt" >&2
  fi
  die "IG Publisher failed. See output above."
}

# ── 4. Locate the snapshot-bearing StructureDefinition ────────────────────────
# The publisher writes output to test/fsh/output/; the file name matches the SD id.
CANDIDATE_PATHS=(
  "${FSH_DIR}/output/StructureDefinition-${SD_ID}.json"
  "${FSH_DIR}/fsh-generated/resources/StructureDefinition-${SD_ID}.json"
)

SD_PATH=""
for p in "${CANDIDATE_PATHS[@]}"; do
  if [[ -f "${p}" ]]; then
    log "Checking candidate: ${p}"
    # Verify it has a non-empty snapshot.element array
    element_count=$(jq '.snapshot.element | length' "${p}" 2>/dev/null || echo "0")
    if [[ "${element_count}" -gt 0 ]]; then
      SD_PATH="${p}"
      log "Found snapshot-bearing SD at: ${SD_PATH} (${element_count} elements)"
      break
    else
      log "Candidate ${p} has no snapshot.element — skipping"
    fi
  fi
done

if [[ -z "${SD_PATH}" ]]; then
  log "Searched paths:"
  for p in "${CANDIDATE_PATHS[@]}"; do
    log "  ${p} (exists: $(test -f "${p}" && echo yes || echo no))"
  done
  if [[ -f "${FSH_DIR}/output/qa.txt" ]]; then
    log "qa.txt:"
    cat "${FSH_DIR}/output/qa.txt" >&2
  fi
  die "No snapshot-bearing StructureDefinition found for '${SD_ID}'. The IG Publisher must produce a snapshot in output/StructureDefinition-${SD_ID}.json."
fi

# ── 5. Copy to well-known path and export ─────────────────────────────────────
cp "${SD_PATH}" "${GENERATED_SD_DEST}"
log "Copied SD to: ${GENERATED_SD_DEST}"

EXPORT_VAL="MEDPLUM_TEST_PROFILE_SD=${PWD}/${GENERATED_SD_DEST}"
echo "${EXPORT_VAL}"

# Append to GITHUB_ENV when running inside GitHub Actions
if [[ -n "${GITHUB_ENV:-}" ]]; then
  echo "${EXPORT_VAL}" >> "${GITHUB_ENV}"
  log "Exported to GITHUB_ENV"
fi
