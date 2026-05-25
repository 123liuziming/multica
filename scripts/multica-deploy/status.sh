#!/usr/bin/env bash
# Show the current state of the multica2 deployment: what's running, what
# image is live, what rollback / candidate tags exist, daemon health.
#
# Read-only. Safe to run anytime. Useful as a sanity check after a release
# or before deciding whether to roll back.

source "$(dirname "$0")/config.sh"

cd "${REPO_DIR}"

printf "${BOLD}==> git${RESET}\n"
printf "  branch  : %s\n" "$(git rev-parse --abbrev-ref HEAD)"
printf "  HEAD    : %s\n" "$(git log -1 --format='%h %s')"
printf "  version : %s\n" "$(current_version)"

printf "\n${BOLD}==> containers${RESET}\n"
docker ps --format '  {{.Names}}\t{{.Image}}\t{{.Status}}' | grep multica2 || warn "no multica2 containers running"

printf "\n${BOLD}==> backend image tags${RESET}\n"
docker images "${BACKEND_IMAGE}" \
  --format '  {{.Tag}}\t{{.ID}}\t{{.CreatedSince}}\t{{.Size}}' \
  | head -10

printf "\n${BOLD}==> frontend image tags${RESET}\n"
docker images "${FRONTEND_IMAGE}" \
  --format '  {{.Tag}}\t{{.ID}}\t{{.CreatedSince}}\t{{.Size}}' \
  | head -10

printf "\n${BOLD}==> daemons${RESET}\n"
# Status check per profile. Errors swallowed so a stopped daemon doesn't
# kill the whole script.
for status_args in "" "--profile multica2" "--profile agentloop-2" "--profile 376610"; do
  printf "  "
  "${CLI_PATH_ROOT}" daemon status $status_args 2>&1 | head -1 || true
done

printf "\n${BOLD}==> CLI binaries${RESET}\n"
for p in "${CLI_PATH_ROOT}" "${CLI_PATH_ADMIN}"; do
  if [ -x "$p" ]; then
    v="$("$p" --version 2>&1 | head -1)"
    printf "  %s\n    %s\n" "$p" "$v"
  else
    warn "  ${p} missing or not executable"
  fi
done

printf "\n${BOLD}==> health endpoints${RESET}\n"
# `|| true` so a temporarily-down service doesn't abort the rest of the
# status report. -w writes the status code; --max-time bounds the wait.
printf "  backend  : "
curl -s -o /dev/null -w '%{http_code}\n' --max-time 5 "$(backend_health_url)" || true
printf "  frontend : "
curl -s -o /dev/null -w '%{http_code}\n' --max-time 5 "$(frontend_health_url)" || true

printf "\n${BOLD}==> rollback options${RESET}\n"
printf "  backend  : "
docker images "${BACKEND_IMAGE}" --format '{{.Tag}}' | grep "^${ROLLBACK_TAG_PREFIX}-" | head -3 | tr '\n' ' ' || true
echo
printf "  frontend : "
docker images "${FRONTEND_IMAGE}" --format '{{.Tag}}' | grep "^${ROLLBACK_TAG_PREFIX}-" | head -3 | tr '\n' ' ' || true
echo
printf "  CLI      : "
ls -1t "${CLI_PATH_ROOT}".bak.* 2>/dev/null | head -3 | tr '\n' ' ' || true
echo
