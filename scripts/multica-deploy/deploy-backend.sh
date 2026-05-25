#!/usr/bin/env bash
# Promote a previously-built backend candidate image to `:artifact`, then
# force-recreate the backend container. Tags the currently-live image as
# `:rollback-pre-<commit>` first so rollback-backend.sh can swap back.
#
# Idempotent: re-running with the same --commit is a no-op (well, you get
# another container recreate). The candidate tag must already exist —
# build-backend.sh produces it.

source "$(dirname "$0")/config.sh"

show_help() {
  cat <<EOF
Usage: deploy-backend.sh --commit REV [--dry-run]

Steps:
  1. Verify ${BACKEND_IMAGE}:candidate-<commit>-stamped exists
  2. Tag current ${LIVE_TAG} image as :${ROLLBACK_TAG_PREFIX}-<commit>
  3. Promote candidate → ${LIVE_TAG}
  4. docker compose force-recreate backend
  5. Wait for /health to return 200 (90s max), or fail
EOF
}

REMAINING_ARGS=()
parse_common_flags "$@"

cd "${REPO_DIR}"

if [ -z "${COMMIT_OVERRIDE:-}" ]; then
  COMMIT_OVERRIDE="$(current_commit)"
fi
COMMIT="${COMMIT_OVERRIDE}"
CANDIDATE_TAG="${BACKEND_IMAGE}:candidate-${COMMIT}-stamped"
ROLLBACK_TAG="${BACKEND_IMAGE}:${ROLLBACK_TAG_PREFIX}-${COMMIT}"

info "deploy-backend: commit=${COMMIT}"

# ---- Step 1: candidate must exist ----
if ! docker image inspect "${CANDIDATE_TAG}" >/dev/null 2>&1; then
  fail "candidate image ${CANDIDATE_TAG} does not exist; run build-backend.sh --commit ${COMMIT} first"
fi

# ---- Step 2: snapshot live → rollback tag ----
# Doesn't move the image; just adds another tag pointing at the same ID.
# `docker tag` overwrites silently if the rollback tag already exists, so
# repeated runs with the same commit just clobber each other's snapshot
# (which is fine — the live image is the same).
if docker image inspect "${BACKEND_IMAGE}:${LIVE_TAG}" >/dev/null 2>&1; then
  info "tagging current :${LIVE_TAG} as :${ROLLBACK_TAG_PREFIX}-${COMMIT}"
  run docker tag "${BACKEND_IMAGE}:${LIVE_TAG}" "${ROLLBACK_TAG}"
else
  warn "no current :${LIVE_TAG} to snapshot for rollback (first deploy?)"
fi

# ---- Step 3: promote ----
info "promoting candidate → :${LIVE_TAG}"
run docker tag "${CANDIDATE_TAG}" "${BACKEND_IMAGE}:${LIVE_TAG}"

# ---- Step 4: recreate ----
info "force-recreating ${BACKEND_CONTAINER}"
run "${COMPOSE_CMD[@]}" up -d --force-recreate backend

# ---- Step 5: health check ----
if [ "${DRY_RUN:-0}" != "1" ]; then
  wait_for_url "$(backend_health_url)" 90
fi

ok "backend deploy complete (commit=${COMMIT})"
docker ps --format '  {{.Names}}\t{{.Image}}\t{{.Status}}' | grep "${BACKEND_CONTAINER}" || true
