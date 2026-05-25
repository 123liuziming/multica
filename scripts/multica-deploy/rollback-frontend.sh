#!/usr/bin/env bash
# Mirror of rollback-backend.sh for the web container.

source "$(dirname "$0")/config.sh"

show_help() {
  cat <<EOF
Usage: rollback-frontend.sh [--to <commit>] [--dry-run]

Steps:
  1. Find the rollback tag to use (latest by created time, or --to)
  2. Promote that tag → :${LIVE_TAG}
  3. force-recreate frontend
  4. Wait for http://localhost:${FRONTEND_PORT}/
EOF
}

REMAINING_ARGS=()
TARGET_COMMIT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --to)      shift; TARGET_COMMIT="$1"; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) show_help; exit 0 ;;
    *)         REMAINING_ARGS+=("$1"); shift ;;
  esac
done

cd "${REPO_DIR}"

if [ -n "$TARGET_COMMIT" ]; then
  ROLLBACK_TAG="${FRONTEND_IMAGE}:${ROLLBACK_TAG_PREFIX}-${TARGET_COMMIT}"
  if ! docker image inspect "${ROLLBACK_TAG}" >/dev/null 2>&1; then
    fail "no such image: ${ROLLBACK_TAG}"
  fi
else
  ROLLBACK_TAG=$(docker images "${FRONTEND_IMAGE}" \
    --format '{{.Tag}}\t{{.CreatedAt}}' \
    | grep "^${ROLLBACK_TAG_PREFIX}-" \
    | sort -k2 -r \
    | head -1 \
    | awk '{print $1}')
  if [ -z "$ROLLBACK_TAG" ]; then
    fail "no ${FRONTEND_IMAGE}:${ROLLBACK_TAG_PREFIX}-* tag found"
  fi
  ROLLBACK_TAG="${FRONTEND_IMAGE}:${ROLLBACK_TAG}"
fi

info "rolling frontend back to ${ROLLBACK_TAG}"
run docker tag "${ROLLBACK_TAG}" "${FRONTEND_IMAGE}:${LIVE_TAG}"

info "force-recreating ${FRONTEND_CONTAINER}"
run "${COMPOSE_CMD[@]}" up -d --force-recreate frontend

if [ "${DRY_RUN:-0}" != "1" ]; then
  wait_for_url "$(frontend_health_url)" 90
fi

ok "frontend rolled back to ${ROLLBACK_TAG}"
docker ps --format '  {{.Names}}\t{{.Image}}\t{{.Status}}' | grep "${FRONTEND_CONTAINER}" || true
