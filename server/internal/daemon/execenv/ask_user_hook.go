package execenv

import (
	"fmt"
	"os"
	"path/filepath"
)

// askUserHookScript is the bash hook installed at .claude/hooks/ask-user-question.sh
// when an agent has allow_ask_user_question enabled. It runs as a Claude Code
// PreToolUse hook matching the AskUserQuestion tool.
//
// Flow:
//  1. Reads the PreToolUse JSON payload from stdin.
//  2. If the tool isn't AskUserQuestion, returns {} (no-op, tool proceeds).
//  3. Otherwise POSTs the tool_input verbatim to the local daemon's
//     /question/ask endpoint. The daemon blocks until the user answers
//     (or fails the question) and returns {"formatted": "..."}.
//  4. Emits a PreToolUse hookSpecificOutput with permissionDecision=deny
//     and permissionDecisionReason set to the formatted answer. Claude
//     surfaces this reason to the model as the AskUserQuestion result, so
//     the model continues reasoning as if the tool returned the answer.
//
// We use deny + reason because in --permission-mode bypassPermissions the
// AskUserQuestion tool would otherwise execute without any UI prompt (this
// is a headless CLI), and there's no separate mechanism to synthesize a
// tool result. The deny-reason path is what Claude Code's hook docs call
// out for "agent-side tool veto with explanation"; it's the only seam that
// works for us.
const askUserHookScript = `#!/usr/bin/env bash
set -e

INPUT=$(cat)
TOOL_NAME=$(printf '%s' "$INPUT" | jq -r '.tool_name // ""')

if [ "$TOOL_NAME" != "AskUserQuestion" ]; then
  echo '{}'
  exit 0
fi

TOOL_INPUT=$(printf '%s' "$INPUT" | jq -c '.tool_input // {}')
PORT="${MULTICA_DAEMON_PORT:-19514}"
TASK_ID="${MULTICA_TASK_ID:-}"

if [ -z "$TASK_ID" ]; then
  jq -n '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"deny",permissionDecisionReason:"multica: MULTICA_TASK_ID not set; cannot route AskUserQuestion"}}'
  exit 0
fi

RESP=$(curl -sS -X POST "http://127.0.0.1:${PORT}/question/ask" \
  -H "Content-Type: application/json" \
  -H "X-Multica-Task-Id: ${TASK_ID}" \
  --data "$TOOL_INPUT" \
  --max-time 86400 || echo "")

if [ -z "$RESP" ]; then
  jq -n '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"deny",permissionDecisionReason:"multica: failed to reach daemon to forward AskUserQuestion"}}'
  exit 0
fi

REASON=$(printf '%s' "$RESP" | jq -r '.formatted // empty')
if [ -z "$REASON" ]; then
  REASON=$(printf '%s' "$RESP" | jq -r '.error // "multica: empty response from daemon"')
fi

jq -n --arg r "$REASON" '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"deny",permissionDecisionReason:$r}}'
`

// writeAskUserHookScript drops the script into the task workdir and returns
// its absolute path.
func writeAskUserHookScript(workDir string) (string, error) {
	dir := filepath.Join(workDir, ".claude", "hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "ask-user-question.sh")
	if err := os.WriteFile(p, []byte(askUserHookScript), 0o755); err != nil {
		return "", err
	}
	return filepath.Abs(p)
}

// mergeAskUserHook installs the PreToolUse hook entry into the existing
// settings map. Preserves user-authored PreToolUse hooks for other matchers.
// If the same matcher already has an AskUserQuestion entry pointing
// elsewhere we still append ours — Claude executes hook matchers
// independently, and ours is the one routing answers back to multica.
func mergeAskUserHook(settings map[string]any, workDir string, daemonPort int) error {
	scriptPath, err := writeAskUserHookScript(workDir)
	if err != nil {
		return fmt.Errorf("write ask-user hook script: %w", err)
	}
	_ = daemonPort // currently unused — script reads MULTICA_DAEMON_PORT at runtime

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	pre, _ := hooks["PreToolUse"].([]any)

	entry := map[string]any{
		"matcher": "AskUserQuestion",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": scriptPath,
			},
		},
	}
	pre = append(pre, entry)
	hooks["PreToolUse"] = pre
	settings["hooks"] = hooks
	return nil
}
