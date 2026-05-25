#!/usr/bin/env bash
# Build the Next.js standalone bundle, sync to .deploy-artifacts/web/,
# then `docker build` a candidate image. Critical: REMOTE_API_URL is set
# at build time because next.config.ts bakes it into the rewrite table
# (the value is evaluated when `next build` runs, NOT at container start).
# A missing/wrong value here is the bug that produced the 158c91371-pre-fix
# 500 on /auth/send-code — frontend tried to proxy to localhost:8080 from
# inside its container, where nothing answers.

source "$(dirname "$0")/config.sh"

show_help() {
  cat <<EOF
Usage: build-frontend.sh [--commit REV] [--dry-run]

Compiles apps/web with STANDALONE=true, refreshes the .deploy-artifacts/web/
tree (standalone + static + public), and builds a docker image tagged
candidate-<commit>-stamped. Does not promote to :artifact.

REMOTE_API_URL is forced to ${FRONTEND_BUILD_REMOTE_API_URL} via config.sh.
Override at your own risk via env.

Flags:
  --commit REV   tag the image after this ref instead of HEAD
  --dry-run      show what would happen
EOF
}

REMAINING_ARGS=()
parse_common_flags "$@"

cd "${REPO_DIR}"

COMMIT="${COMMIT_OVERRIDE:-$(current_commit)}"
VERSION="$(current_version)"
CANDIDATE_TAG="${FRONTEND_IMAGE}:candidate-${COMMIT}-stamped"

info "build-frontend: commit=${COMMIT} version=${VERSION} REMOTE_API_URL=${FRONTEND_BUILD_REMOTE_API_URL}"

# ---- Step 1: pnpm build ----
# pnpm picks up STANDALONE from the env at build time. NEXT_PUBLIC_APP_VERSION
# becomes part of every page bundle so server logs can tell which build a
# given client request came from.
info "pnpm build (Next.js standalone)"
run env STANDALONE=true \
  REMOTE_API_URL="${FRONTEND_BUILD_REMOTE_API_URL}" \
  NEXT_PUBLIC_APP_VERSION="${VERSION}" \
  PATH="$(dirname "${NODE_BIN}"):${PATH}" \
  "${PNPM_BIN}" --filter @multica/web build

# ---- Step 2: sync artifacts ----
# rm -rf first so stale files from a previous build don't ride along — the
# Dockerfile uses COPY (not COPY --no-cache or anything similar), so leftover
# files would land in the image even if pnpm build didn't produce them.
info "syncing standalone/static/public into ${FRONTEND_ARTIFACT_DIR}"
run rm -rf "${FRONTEND_ARTIFACT_DIR}/standalone" \
           "${FRONTEND_ARTIFACT_DIR}/static" \
           "${FRONTEND_ARTIFACT_DIR}/public"
run cp -a "${REPO_DIR}/apps/web/.next/standalone" "${FRONTEND_ARTIFACT_DIR}/standalone"
run cp -a "${REPO_DIR}/apps/web/.next/static"     "${FRONTEND_ARTIFACT_DIR}/static"
run cp -a "${REPO_DIR}/apps/web/public"           "${FRONTEND_ARTIFACT_DIR}/public"

# ---- Step 3: sanity-check that REMOTE_API_URL actually got baked in ----
# Bake-in failure was the bug we just fixed; this catches a regression
# before the image is built.
if [ "${DRY_RUN:-0}" != "1" ]; then
  baked=$(grep -oE 'http://[a-z][a-z0-9.-]*:8080' \
    "${FRONTEND_ARTIFACT_DIR}/standalone/apps/web/server.js" 2>/dev/null | head -1)
  if [ -z "$baked" ]; then
    warn "no http://*:8080 reference found in built server.js — check next.config.ts wasn't refactored"
  elif [ "$baked" != "${FRONTEND_BUILD_REMOTE_API_URL}" ]; then
    fail "baked-in API URL is ${baked}, expected ${FRONTEND_BUILD_REMOTE_API_URL}"
  else
    ok "baked-in API URL = ${baked}"
  fi
fi

# ---- Step 4: docker build ----
info "docker build → ${CANDIDATE_TAG}"
run docker build -t "${CANDIDATE_TAG}" \
  -f "${REPO_DIR}/Dockerfile.artifact.web" "${REPO_DIR}"

ok "built ${CANDIDATE_TAG}"
echo "$CANDIDATE_TAG"
