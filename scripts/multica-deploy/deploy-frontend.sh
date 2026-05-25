#!/usr/bin/env bash
# Frontend variant of deploy-backend.sh. Same pattern: snapshot live →
# rollback, promote candidate, recreate, health check.

source "$(dirname "$0")/config.sh"

show_help() {
  cat <<EOF
Usage: deploy-frontend.sh --commit REV [--dry-run]

Steps:
  1. Verify ${FRONTEND_IMAGE}:candidate-<commit>-stamped exists
  2. Tag current ${LIVE_TAG} image as :${ROLLBACK_TAG_PREFIX}-<commit>
  3. Promote candidate → ${LIVE_TAG}
  4. docker compose force-recreate frontend
  5. Wait for http://localhost:${FRONTEND_PORT}/ to return 200 (90s max)
EOF
}

REMAINING_ARGS=()
parse_common_flags "$@"

cd "${REPO_DIR}"

if [ -z "${COMMIT_OVERRIDE:-}" ]; then
  COMMIT_OVERRIDE="$(current_commit)"
fi
COMMIT="${COMMIT_OVERRIDE}"
CANDIDATE_TAG="${FRONTEND_IMAGE}:candidate-${COMMIT}-stamped"
ROLLBACK_TAG="${FRONTEND_IMAGE}:${ROLLBACK_TAG_PREFIX}-${COMMIT}"

info "deploy-frontend: commit=${COMMIT}"

if ! docker image inspect "${CANDIDATE_TAG}" >/dev/null 2>&1; then
  fail "candidate image ${CANDIDATE_TAG} does not exist; run build-frontend.sh --commit ${COMMIT} first"
fi

if docker image inspect "${FRONTEND_IMAGE}:${LIVE_TAG}" >/dev/null 2>&1; then
  info "tagging current :${LIVE_TAG} as :${ROLLBACK_TAG_PREFIX}-${COMMIT}"
  run docker tag "${FRONTEND_IMAGE}:${LIVE_TAG}" "${ROLLBACK_TAG}"
else
  warn "no current :${LIVE_TAG} to snapshot for rollback (first deploy?)"
fi

info "promoting candidate → :${LIVE_TAG}"
run docker tag "${CANDIDATE_TAG}" "${FRONTEND_IMAGE}:${LIVE_TAG}"

info "force-recreating ${FRONTEND_CONTAINER}"
run "${COMPOSE_CMD[@]}" up -d --force-recreate frontend

if [ "${DRY_RUN:-0}" != "1" ]; then
  wait_for_url "$(frontend_health_url)" 90
fi

ok "frontend deploy complete (commit=${COMMIT})"
docker ps --format '  {{.Names}}\t{{.Image}}\t{{.Status}}' | grep "${FRONTEND_CONTAINER}" || true
