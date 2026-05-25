#!/usr/bin/env bash
# Build server + migrate + multica (CLI) Go binaries with ldflags, drop them
# into .deploy-artifacts/backend/, then `docker build` an image tagged
# `multica2-backend:candidate-<commit>`. Does NOT promote to `:artifact` —
# that's deploy-backend.sh's job.
#
# Runs on admin@100.69.248.97 (ssh alias `multica`). Uses /home/admin/go/bin/go.

source "$(dirname "$0")/config.sh"

show_help() {
  cat <<EOF
Usage: build-backend.sh [--commit REV] [--dry-run]

Compiles server + migrate + cli with -ldflags 'main.version=<git describe>
main.commit=<short> main.date=<utc-iso>', refreshes the .deploy-artifacts/
backend/ tree from the working copy, and produces an image tagged with the
commit so a later deploy step can promote it atomically.

Flags:
  --commit REV   tag the image after this ref instead of HEAD
  --dry-run      show what would happen, skip actual build/docker
EOF
}

REMAINING_ARGS=()
parse_common_flags "$@"

cd "${REPO_DIR}"

COMMIT="${COMMIT_OVERRIDE:-$(current_commit)}"
VERSION="$(current_version)"
DATE="$(current_date_iso)"
CANDIDATE_TAG="${BACKEND_IMAGE}:candidate-${COMMIT}-stamped"

info "build-backend: commit=${COMMIT} version=${VERSION}"

# ---- Step 1: compile Go binaries to /tmp ----
# We use /tmp rather than writing straight into .deploy-artifacts/ so a
# half-finished build doesn't poison the artifact tree (the dockerfile COPYs
# from there unconditionally).
LDFLAGS_SERVER="-X main.version=${VERSION} -X main.commit=${COMMIT}"
LDFLAGS_CLI="-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}"

info "go build server (cmd/server)"
run env CGO_ENABLED=0 "${GO_BIN}" -C server build \
  -ldflags "${LDFLAGS_SERVER}" \
  -o /tmp/multica-server-new ./cmd/server

info "go build migrate (cmd/migrate)"
run env CGO_ENABLED=0 "${GO_BIN}" -C server build \
  -o /tmp/multica-migrate-new ./cmd/migrate

info "go build cli (cmd/multica)"
run env CGO_ENABLED=0 "${GO_BIN}" -C server build \
  -ldflags "${LDFLAGS_CLI}" \
  -o /tmp/multica-cli-new ./cmd/multica

# ---- Step 2: install into .deploy-artifacts/backend/ ----
# Migrations is a directory; the dockerfile COPYs the whole tree so we need
# it kept in sync with the source migrations/ on disk.
info "installing artifacts into ${BACKEND_ARTIFACT_DIR}"
run mv /tmp/multica-server-new "${BACKEND_ARTIFACT_DIR}/server"
run mv /tmp/multica-migrate-new "${BACKEND_ARTIFACT_DIR}/migrate"
run mv /tmp/multica-cli-new "${BACKEND_ARTIFACT_DIR}/multica"
run chmod +x "${BACKEND_ARTIFACT_DIR}"/{server,migrate,multica}
run rm -rf "${BACKEND_ARTIFACT_DIR}/migrations"
run cp -a "${REPO_DIR}/server/migrations" "${BACKEND_ARTIFACT_DIR}/migrations"

# ---- Step 3: docker build into a candidate tag ----
# We never tag :artifact here — promotion is a separate deploy step so an
# operator can rebuild repeatedly without changing what's live.
info "docker build → ${CANDIDATE_TAG}"
run docker build -t "${CANDIDATE_TAG}" \
  -f "${REPO_DIR}/Dockerfile.artifact.backend" "${REPO_DIR}"

ok "built ${CANDIDATE_TAG}"
echo "$CANDIDATE_TAG"
