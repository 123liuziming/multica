package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// InjectProviderConfig writes provider-specific configuration files into the
// task's execution environment and returns the absolute path of the written
// file. The path is used by the caller to pass --settings (Claude) or
// --profile (Codex) to the agent CLI.
//
// Provider semantics:
//   - claude: writes .claude/settings.json in workDir (JSON)
//   - codex:  writes config.toml in codexHome (JSON→TOML conversion)
//   - others: writes .provider-config.json in workDir as a generic fallback
func InjectProviderConfig(workDir, codexHome, provider string, raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}

	switch provider {
	case "claude":
		dir := filepath.Join(workDir, ".claude")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		p := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(p, raw, 0o644); err != nil {
			return "", err
		}
		return abs(p)
	case "codex":
		if codexHome == "" {
			return "", nil
		}
		data, err := jsonToTOML(raw)
		if err != nil {
			return "", err
		}
		p := filepath.Join(codexHome, "config.toml")
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return "", err
		}
		return abs(p)
	default:
		p := filepath.Join(workDir, ".provider-config.json")
		if err := os.WriteFile(p, raw, 0o644); err != nil {
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
