#!/usr/bin/env bash
# Shared config for multica-deploy scripts.
#
# Hard-coded for the host admin@100.69.248.97 (ssh alias `multica`). Sourcing
# this on any other machine will produce nonsense — every path/name/profile
# below was captured from that specific deployment.
#
# Source convention: every script does `source "$(dirname "$0")/config.sh"`
# before doing anything. Edit values here, not in the consumer scripts.

set -euo pipefail

# ---------------------------------------------------------------------------
# Repo / artifacts
# ---------------------------------------------------------------------------

# The git checkout the deploy scripts run from. All other paths are relative
# to this.
REPO_DIR="${REPO_DIR:-/home/admin/multica}"

# Where compiled / bundled artifacts land. The Dockerfiles COPY from here.
BACKEND_ARTIFACT_DIR="${REPO_DIR}/.deploy-artifacts/backend"
FRONTEND_ARTIFACT_DIR="${REPO_DIR}/.deploy-artifacts/web"

# ---------------------------------------------------------------------------
# Go toolchain
# ---------------------------------------------------------------------------

GO_BIN="${GO_BIN:-/home/admin/go/bin/go}"

# ---------------------------------------------------------------------------
# Node / pnpm. nvm puts pnpm under a node version dir; we don't bother
# sourcing nvm.sh — direct path is simpler and pinned.
# ---------------------------------------------------------------------------

PNPM_BIN="${PNPM_BIN:-/home/admin/.nvm/versions/node/v22.22.3/bin/pnpm}"
NODE_BIN="${NODE_BIN:-/home/admin/.nvm/versions/node/v22.22.3/bin/node}"

# ---------------------------------------------------------------------------
# Docker compose
# ---------------------------------------------------------------------------

COMPOSE_FILE="${REPO_DIR}/docker-compose.multica2.yml"
ENV_FILE="${REPO_DIR}/.env.multica2"
COMPOSE_CMD=(docker compose -f "${COMPOSE_FILE}" --env-file "${ENV_FILE}")

# ---------------------------------------------------------------------------
# Images & containers
# ---------------------------------------------------------------------------

BACKEND_IMAGE="multica2-backend"
FRONTEND_IMAGE="multica2-web"
LIVE_TAG="artifact"
ROLLBACK_TAG_PREFIX="rollback-pre"

BACKEND_CONTAINER="multica2-backend-1"
FRONTEND_CONTAINER="multica2-frontend-1"
POSTGRES_CONTAINER="multica2-postgres-1"

# ---------------------------------------------------------------------------
# Ports (host-side; matches the `ports:` mapping in compose)
# ---------------------------------------------------------------------------

BACKEND_PORT="${BACKEND_PORT:-18080}"
FRONTEND_PORT="${FRONTEND_PORT:-13000}"

# ---------------------------------------------------------------------------
# Build-time env that gets baked into the frontend bundle. REMOTE_API_URL
# must use the docker-compose service name (`backend`) because the frontend
# server.js makes server-side fetches FROM INSIDE its container — localhost
# there is itself, not the backend.
# ---------------------------------------------------------------------------

FRONTEND_BUILD_REMOTE_API_URL="${FRONTEND_BUILD_REMOTE_API_URL:-http://backend:8080}"

# ---------------------------------------------------------------------------
# CLI binary paths. The first is root-owned (needs sudo to overwrite); the
# second is admin-owned. `deploy-cli.sh` writes to both so every daemon
# profile picks up the new code regardless of which path it was started from.
# ---------------------------------------------------------------------------

CLI_PATH_ROOT="/usr/local/bin/multica"
CLI_PATH_ADMIN="/apsara/data1/home-admin-offload/multica/server/bin/multica"

# ---------------------------------------------------------------------------
# Daemon profiles. Captured verbatim from `/proc/<pid>/cmdline` at the time
# these scripts were written. Each entry is a single shell-quoted string —
# `deploy-cli.sh` evals it inside a nohup wrapper after stopping the existing
# process for that profile.
#
# When you add a profile here, also add a matching `multica daemon stop ...`
# line to STOP_DAEMON_CMDS below.
# ---------------------------------------------------------------------------

DAEMON_PROFILES=(
  # Default profile (no --profile flag). Device name comes from $(hostname).
  "${CLI_PATH_ROOT} daemon start --foreground"

  # multica2 profile — leader for THIS docker stack. Uses the admin-owned
  # binary path historically; doesn't matter functionally since the binary
  # content is the same after deploy-cli.sh.
  "${CLI_PATH_ADMIN} daemon start --foreground --device-name multica2-local --runtime-name multica2-local-runtime --profile multica2"

  # agentloop-2 profile (a different team's workspace).
  "${CLI_PATH_ROOT} daemon start --foreground --daemon-id 8a1aead4-18d4-4d88-9cbb-a8d3ef4fd3ee --device-name agentloop-dev-2 --runtime-name agentloop-dev-2-runtime --profile agentloop-2"

  # 376610 profile (a personal workspace).
  "${CLI_PATH_ROOT} daemon start --foreground --daemon-id af063197-ff4a-4dd3-9706-fb883c6dc219 --device-name 376610@alibaba-inc.com --runtime-name 376610@alibaba-inc.com --profile 376610"
)

STOP_DAEMON_CMDS=(
  "${CLI_PATH_ROOT} daemon stop"
  "${CLI_PATH_ROOT} daemon stop --profile multica2"
  "${CLI_PATH_ROOT} daemon stop --profile agentloop-2"
  "${CLI_PATH_ROOT} daemon stop --profile 376610"
)

# Per-profile log destinations.
DAEMON_LOGS=(
  "/home/admin/.multica/daemon.log"
  "/home/admin/.multica/profiles/multica2/daemon.log"
  "/home/admin/.multica/profiles/agentloop-2/daemon.log"
  "/home/admin/.multica/profiles/376610/daemon.log"
)

# ---------------------------------------------------------------------------
# Health-check helpers used by deploy scripts.
# ---------------------------------------------------------------------------

backend_health_url()  { echo "http://localhost:${BACKEND_PORT}/health"; }
frontend_health_url() { echo "http://localhost:${FRONTEND_PORT}/"; }

# ---------------------------------------------------------------------------
# Small helpers everyone uses. Defined as functions so scripts can rely on
# colored output / consistent error handling.
# ---------------------------------------------------------------------------

if [ -t 1 ] || [ -t 2 ]; then
  BOLD=$'\033[1m'; GREEN=$'\033[0;32m'; YELLOW=$'\033[0;33m'
  RED=$'\033[0;31m'; CYAN=$'\033[0;36m'; RESET=$'\033[0m'
else
  BOLD=''; GREEN=''; YELLOW=''; RED=''; CYAN=''; RESET=''
fi

info() { printf "${BOLD}${CYAN}==> %s${RESET}\n" "$*"; }
ok()   { printf "${BOLD}${GREEN}✓ %s${RESET}\n" "$*"; }
warn() { printf "${BOLD}${YELLOW}⚠ %s${RESET}\n" "$*" >&2; }
fail() { printf "${BOLD}${RED}✗ %s${RESET}\n" "$*" >&2; exit 1; }

# Returns the short commit hash of the working tree. Used to tag images and
# stamp build metadata. The fallback `unknown` lets the build keep going
# when invoked from a tarball extraction (we usually don't, but defensive).
current_commit() {
  git -C "${REPO_DIR}" rev-parse --short HEAD 2>/dev/null || echo unknown
}

current_version() {
  git -C "${REPO_DIR}" describe --tags --always --dirty 2>/dev/null || echo dev
}

current_date_iso() {
  date -u '+%Y-%m-%dT%H:%M:%SZ'
}

timestamp() {
  date '+%Y%m%d%H%M%S'
}

# Wait until URL returns HTTP 200 or `max_wait` seconds elapse. Used after
# every container recreate so callers can fail fast if the new image
# crash-loops.
wait_for_url() {
  local url="$1" max_wait="${2:-60}" elapsed=0
  while ! curl -sf "$url" > /dev/null 2>&1; do
    if [ "$elapsed" -ge "$max_wait" ]; then
      fail "URL ${url} never returned 200 in ${max_wait}s"
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  ok "${url} ready in ${elapsed}s"
}

# Wait for `fuser <path>` to find no process holding the file open. Required
# before `cp` over a running binary on Linux ('text file busy'). Used by
# deploy-cli.sh after stopping daemons.
wait_for_fuser_clear() {
  local path="$1" max_wait="${2:-15}" elapsed=0
  while sudo fuser "$path" >/dev/null 2>&1; do
    if [ "$elapsed" -ge "$max_wait" ]; then
      fail "${path} is still in use after ${max_wait}s; some process still holds it"
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
}

# Simple --dry-run / --help flag parsing used by every consumer script.
# Sets DRY_RUN=1 if --dry-run; calls show_help and exits if -h/--help.
parse_common_flags() {
  DRY_RUN="${DRY_RUN:-0}"
  while [ $# -gt 0 ]; do
    case "$1" in
      --dry-run) DRY_RUN=1; shift ;;
      -h|--help) show_help; exit 0 ;;
      --commit)  shift; COMMIT_OVERRIDE="$1"; shift ;;
      *)         REMAINING_ARGS+=("$1"); shift ;;
    esac
  done
}

# Runner that respects DRY_RUN. Use `run cmd args...` instead of bare cmd
# invocations in places where you want --dry-run to skip the real call.
run() {
  if [ "${DRY_RUN:-0}" = "1" ]; then
    printf "${YELLOW}[dry-run]${RESET} %s\n" "$*" >&2
  else
    "$@"
  fi
}
