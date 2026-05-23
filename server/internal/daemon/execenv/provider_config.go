package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// ProviderConfigOptions captures the per-task knobs that influence what we
// write into the agent's `.claude/settings.json` (or codex config.toml). The
// raw payload is the agent's user-configured provider settings; the bool
// triggers our own PreToolUse hook injection for AskUserQuestion.
type ProviderConfigOptions struct {
	// Raw is the user-authored provider config blob (passed through from the
	// agent's runtime_config). Optional; may be empty.
	Raw json.RawMessage
	// AllowAskUserQuestion enables the PreToolUse AskUserQuestion hook
	// injection on top of Raw. Only honored for the claude provider today.
	AllowAskUserQuestion bool
	// DaemonPort is the local daemon HTTP port the hook script needs to call
	// back into. Required when AllowAskUserQuestion is true; the hook script
	// reads MULTICA_DAEMON_PORT at runtime, but the absolute hook command
	// path lives in workDir so we still bake the dir at write time.
	DaemonPort int
}

// InjectProviderConfig writes provider-specific configuration files into the
// task's execution environment and returns the absolute path of the written
// file. The path is used by the caller to pass --settings (Claude) or
// --profile (Codex) to the agent CLI.
//
// Provider semantics:
//   - claude: writes .claude/settings.json in workDir (JSON), deep-merging
//     the AskUserQuestion PreToolUse hook when opts.AllowAskUserQuestion.
//   - codex:  writes config.toml in codexHome (JSON→TOML conversion)
//   - others: writes .provider-config.json in workDir as a generic fallback
func InjectProviderConfig(workDir, codexHome, provider string, opts ProviderConfigOptions) (string, error) {
	switch provider {
	case "claude":
		// Even when Raw is empty we may still need to write settings.json to
		// carry our PreToolUse hook — bail only when there's literally
		// nothing to write.
		if len(opts.Raw) == 0 && !opts.AllowAskUserQuestion {
			return "", nil
		}
		merged := map[string]any{}
		if len(opts.Raw) > 0 {
			if err := json.Unmarshal(opts.Raw, &merged); err != nil {
				return "", err
			}
		}
		if opts.AllowAskUserQuestion {
			if err := mergeAskUserHook(merged, workDir, opts.DaemonPort); err != nil {
				return "", err
			}
		}
		out, err := json.MarshalIndent(merged, "", "  ")
		if err != nil {
			return "", err
		}
		dir := filepath.Join(workDir, ".claude")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		p := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(p, out, 0o644); err != nil {
			return "", err
		}
		return abs(p)
	case "codex":
		if codexHome == "" || len(opts.Raw) == 0 {
			return "", nil
		}
		data, err := jsonToTOML(opts.Raw)
		if err != nil {
			return "", err
		}
		p := filepath.Join(codexHome, "config.toml")
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return "", err
		}
		return abs(p)
	default:
		if len(opts.Raw) == 0 {
			return "", nil
		}
		p := filepath.Join(workDir, ".provider-config.json")
		if err := os.WriteFile(p, opts.Raw, 0o644); err != nil {
			return "", err
		}
		return abs(p)
	}
}

func jsonToTOML(raw json.RawMessage) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return toml.Marshal(m)
}

func abs(p string) (string, error) {
	return filepath.Abs(p)
}
