#!/usr/bin/env bash
# End-to-end release: pull the requested ref, build all artifacts, deploy
# backend → frontend → CLI, with a health check after each phase.
#
# If any phase fails, the script stops — the rollback tag from the most
# recent deploy is still around, so an operator can roll back manually
# with rollback-backend.sh / rollback-frontend.sh.

source "$(dirname "$0")/config.sh"

show_help() {
  cat <<EOF
Usage: release.sh [--branch BRANCH] [--skip-cli] [--skip-frontend] [--dry-run]

End-to-end release flow:
  1. git fetch + pull --rebase from crimson on BRANCH (default: add-questions)
  2. build-backend.sh
  3. deploy-backend.sh (health-checked)
  4. build-frontend.sh
  5. deploy-frontend.sh (health-checked)
  6. deploy-cli.sh — replaces /usr/local/bin/multica + restarts daemons

Flags:
  --branch BRANCH    pull from crimson/BRANCH (default: add-questions)
  --skip-cli         don't touch CLI binary or daemons
  --skip-frontend    only backend + cli (and pull)
  --dry-run          print what would happen, do nothing
EOF
}

REMAINING_ARGS=()
BRANCH="add-questions"
SKIP_CLI=0
SKIP_FRONTEND=0
while [ $# -gt 0 ]; do
  case "$1" in
    --branch)        shift; BRANCH="$1"; shift ;;
    --skip-cli)      SKIP_CLI=1; shift ;;
    --skip-frontend) SKIP_FRONTEND=1; shift ;;
    --dry-run)       DRY_RUN=1; shift ;;
    -h|--help)       show_help; exit 0 ;;
    *)               REMAINING_ARGS+=("$1"); shift ;;
  esac
done

cd "${REPO_DIR}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
FLAGS=""
if [ "${DRY_RUN:-0}" = "1" ]; then FLAGS="--dry-run"; fi

# ---- Phase 0: pull ----
# We stash unrelated working-tree changes first (the aone_sync_scheduler
# tuning is a known long-lived dirty file on this checkout) so the pull
# can fast-forward. If the stash is empty git no-ops.
info "phase 0/6: git pull crimson ${BRANCH}"
if [ "${DRY_RUN:-0}" = "1" ]; then
  warn "[dry-run] skipping git operations"
else
  if [ -n "$(git status --porcelain | grep -v '^??')" ]; then
    git stash push -m "pre-release-$(timestamp) auto-stash" || true
    STASHED=1
  else
    STASHED=0
  fi
  git fetch crimson
  git pull --rebase crimson "${BRANCH}"
  if [ "$STASHED" = "1" ]; then
    git stash pop || warn "stash pop produced conflicts; resolve manually before next release"
  fi
fi

COMMIT="$(current_commit)"
VERSION="$(current_version)"
info "release: commit=${COMMIT} version=${VERSION}"

# ---- Phase 1: build backend ----
info "phase 1/6: build-backend"
bash "${SCRIPT_DIR}/build-backend.sh" --commit "${COMMIT}" ${FLAGS}

# ---- Phase 2: deploy backend ----
info "phase 2/6: deploy-backend"
bash "${SCRIPT_DIR}/deploy-backend.sh" --commit "${COMMIT}" ${FLAGS}

if [ "$SKIP_FRONTEND" = "0" ]; then
  # ---- Phase 3: build frontend ----
  info "phase 3/6: build-frontend"
  bash "${SCRIPT_DIR}/build-frontend.sh" --commit "${COMMIT}" ${FLAGS}

  # ---- Phase 4: deploy frontend ----
  info "phase 4/6: deploy-frontend"
  bash "${SCRIPT_DIR}/deploy-frontend.sh" --commit "${COMMIT}" ${FLAGS}
else
  warn "skipping frontend phases (--skip-frontend)"
fi

if [ "$SKIP_CLI" = "0" ]; then
  # ---- Phase 5: deploy CLI / restart daemons ----
  info "phase 5/6: deploy-cli"
  bash "${SCRIPT_DIR}/deploy-cli.sh" ${FLAGS}
else
  warn "skipping CLI / daemon restart (--skip-cli)"
fi

ok "release ${VERSION} (${COMMIT}) complete"
bash "${SCRIPT_DIR}/status.sh"
