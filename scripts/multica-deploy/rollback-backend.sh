#!/usr/bin/env bash
# Roll the backend image back to the most recent :rollback-pre-* tag and
# force-recreate the container.
#
# When you have multiple rollback tags (from several deploys), the script
# picks the one whose underlying image is NEWEST by CreatedAt. That's a
# heuristic — if you want a specific commit, pass --to <commit>.

source "$(dirname "$0")/config.sh"

show_help() {
  cat <<EOF
Usage: rollback-backend.sh [--to <commit>] [--dry-run]

Steps:
  1. Find the rollback tag to use (latest by created time, or --to)
  2. Promote that tag → :${LIVE_TAG}
  3. force-recreate backend
  4. Wait for /health
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

# Pick rollback tag. The format filter matches our convention; sort by
# CreatedAt and take the first one (newest).
if [ -n "$TARGET_COMMIT" ]; then
  ROLLBACK_TAG="${BACKEND_IMAGE}:${ROLLBACK_TAG_PREFIX}-${TARGET_COMMIT}"
  if ! docker image inspect "${ROLLBACK_TAG}" >/dev/null 2>&1; then
    fail "no such image: ${ROLLBACK_TAG}"
  fi
else
  ROLLBACK_TAG=$(docker images "${BACKEND_IMAGE}" \
    --format '{{.Tag}}\t{{.CreatedAt}}' \
    | grep "^${ROLLBACK_TAG_PREFIX}-" \
    | sort -k2 -r \
    | head -1 \
    | awk '{print $1}')
  if [ -z "$ROLLBACK_TAG" ]; then
    fail "no ${BACKEND_IMAGE}:${ROLLBACK_TAG_PREFIX}-* tag found"
  fi
  ROLLBACK_TAG="${BACKEND_IMAGE}:${ROLLBACK_TAG}"
fi

info "rolling backend back to ${ROLLBACK_TAG}"
run docker tag "${ROLLBACK_TAG}" "${BACKEND_IMAGE}:${LIVE_TAG}"

info "force-recreating ${BACKEND_CONTAINER}"
run "${COMPOSE_CMD[@]}" up -d --force-recreate backend

if [ "${DRY_RUN:-0}" != "1" ]; then
  wait_for_url "$(backend_health_url)" 90
fi

ok "backend rolled back to ${ROLLBACK_TAG}"
docker ps --format '  {{.Names}}\t{{.Image}}\t{{.Status}}' | grep "${BACKEND_CONTAINER}" || true
