#!/usr/bin/env bash
# Replace the multica CLI binary at both paths (/usr/local/bin/multica and
# /apsara/.../multica) and restart every daemon profile so they pick up
# the new code.
#
# Why two paths: history. The default and three named profiles all start
# from /usr/local/bin/multica, but the `multica2` profile was started with
# the /apsara/... path. After deploy-cli.sh they're byte-identical, so it
# doesn't matter going forward — but the profile config files remember the
# launcher path so we keep both in sync.
#
# Why we can't `cp` while daemons run: Linux refuses to overwrite a binary
# whose text is mapped into a running process ("Text file busy"). We stop
# every daemon, wait for the fuser count on the path to drop to zero,
# then cp.

source "$(dirname "$0")/config.sh"

show_help() {
  cat <<EOF
Usage: deploy-cli.sh [--dry-run]

Reads the freshly-built CLI at ${BACKEND_ARTIFACT_DIR}/multica
(so build-backend.sh must have run first), then for each daemon profile
in config.sh DAEMON_PROFILES:

  1. Backup current binaries → *.bak.<timestamp>
  2. Stop all daemon profiles
  3. Wait until no process holds /usr/local/bin/multica open
  4. sudo cp new binary to both paths
  5. nohup-restart every profile with its original cmdline
  6. Verify each profile reports running via 'multica daemon status'

Flags:
  --dry-run  print what would happen, do nothing
EOF
}

REMAINING_ARGS=()
parse_common_flags "$@"

NEW_BIN="${BACKEND_ARTIFACT_DIR}/multica"
if [ ! -x "$NEW_BIN" ]; then
  fail "no executable at ${NEW_BIN}; run build-backend.sh first"
fi

NEW_VERSION="$(${NEW_BIN} --version 2>&1 | head -1 || true)"
info "deploy-cli: new binary = ${NEW_VERSION}"

STAMP="$(timestamp)"

# ---- Step 1: backup current binaries ----
info "backing up current CLI binaries (stamp=${STAMP})"
if [ -f "${CLI_PATH_ROOT}" ]; then
  run sudo cp "${CLI_PATH_ROOT}" "${CLI_PATH_ROOT}.bak.${STAMP}"
fi
if [ -f "${CLI_PATH_ADMIN}" ]; then
  run cp "${CLI_PATH_ADMIN}" "${CLI_PATH_ADMIN}.bak.${STAMP}"
fi

# ---- Step 2: stop all profiles ----
# `daemon stop` exits 0 even if not running, so we ignore errors. We use
# the LIVE_TAG binary (current) to send the stop signal; the signaller is
# short-lived so the bin-busy lock from running this script doesn't matter.
info "stopping all daemon profiles"
for cmd in "${STOP_DAEMON_CMDS[@]}"; do
  run bash -c "${cmd} 2>&1 || true"
done

# ---- Step 3: wait for fuser ----
# Stop is async — the daemon catches SIGTERM, finishes its current task
# message, then exits. fuser gives us the authoritative "is anyone still
# executing this binary" answer.
if [ "${DRY_RUN:-0}" != "1" ]; then
  info "waiting for ${CLI_PATH_ROOT} to be unused…"
  wait_for_fuser_clear "${CLI_PATH_ROOT}" 15
fi

# ---- Step 4: install new binary ----
info "installing new CLI binary to ${CLI_PATH_ROOT} and ${CLI_PATH_ADMIN}"
run sudo cp "${NEW_BIN}" "${CLI_PATH_ROOT}"
run sudo chmod +x "${CLI_PATH_ROOT}"
run cp "${NEW_BIN}" "${CLI_PATH_ADMIN}"
run chmod +x "${CLI_PATH_ADMIN}"

# ---- Step 5: restart each profile with its original cmdline ----
# We background each restart with `nohup ... & disown` so the orchestrator
# ssh session (if any) doesn't hold the child after it exits.
info "restarting daemon profiles"
for i in "${!DAEMON_PROFILES[@]}"; do
  cmd="${DAEMON_PROFILES[$i]}"
  log="${DAEMON_LOGS[$i]}"
  info "  → ${cmd}"
  # We need both nohup AND disown so the daemon survives the ssh that
  # invoked the script. Without disown, OpenSSH waits for the orphaned
  # process group on logout.
  if [ "${DRY_RUN:-0}" = "1" ]; then
    printf "${YELLOW}[dry-run]${RESET} nohup %s >> %s 2>&1 & disown\n" "$cmd" "$log" >&2
  else
    nohup bash -c "${cmd} >> ${log} 2>&1" >/dev/null 2>&1 &
    disown
  fi
done

# ---- Step 6: verify ----
if [ "${DRY_RUN:-0}" != "1" ]; then
  sleep 5
  info "checking daemon status (5s after restart)"
  for status_args in "" "--profile multica2" "--profile agentloop-2" "--profile 376610"; do
    printf "  "
    "${CLI_PATH_ROOT}" daemon status $status_args 2>&1 | head -1 || true
  done
fi

ok "CLI deploy complete"
