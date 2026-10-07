package clitools

import (
	"os"
	"path/filepath"
)

// OpenCodeTool auto-configures OpenCode (~/.config/opencode/opencode.json).
type OpenCodeTool struct{}

func (t *OpenCodeTool) ID() string      { return "opencode" }
func (t *OpenCodeTool) Name() string    { return "OpenCode" }
func (t *OpenCodeTool) Command() string { return "opencode" }

// configPath resolves the OpenCode config path. OpenCode honors XDG_CONFIG_HOME
// (and the opencode.json file is JSONC-tolerant). Falls back to ~/.config.
func (t *OpenCodeTool) configPath(homeDir string) string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		if !filepath.IsAbs(xdg) {
			xdg = filepath.Join(homeDir, xdg)
		}
		return filepath.Join(xdg, "opencode", "opencode.json")
	}
	return expandHome(homeDir, "~/.config/opencode/opencode.json")
}

func (t *OpenCodeTool) DetectStatus(homeDir string) (bool, bool, string, error) {
	path := t.configPath(homeDir)
	if !fileExists(path) {
		return false, false, path, nil
	}
	raw, err := readString(path)
	if err != nil {
		return true, false, path, nil
	}
	configured := containsKeiRouter(raw)
	return true, configured, path, nil
}

func (t *OpenCodeTool) Configure(homeDir, baseURL, apiKey string, models []string) error {
	path := t.configPath(homeDir)
	cfg := make(map[string]any)
	_ = readJSON(path, &cfg)

	modelsMap := make(map[string]any)
	for _, m := range models {
		modelsMap[m] = map[string]any{"name": m}
	}
	if len(modelsMap) == 0 {
		modelsMap["gpt-4o"] = map[string]any{"name": "gpt-4o"}
	}

	provider, _ := cfg["provider"].(map[string]any)
	if provider == nil {
		provider = make(map[string]any)
	}
	provider["keirouter"] = map[string]any{
		"npm":     "@ai-sdk/openai-compatible",
		"options": map[string]any{"baseURL": ensureSuffix(baseURL, "/v1"), "apiKey": apiKey},
		"models":  modelsMap,
	}
	cfg["provider"] = provider

	if len(models) > 0 {
		cfg["model"] = "keirouter/" + models[0]
	}

	return writeJSON(path, cfg)
}

func (t *OpenCodeTool) Remove(homeDir string) error {
	path := t.configPath(homeDir)
	if !fileExists(path) {
		return nil
	}
	var cfg map[string]any
	if err := readJSON(path, &cfg); err != nil {
		return err
	}
	if provider, ok := cfg["provider"].(map[string]any); ok {
		delete(provider, "keirouter")
	}
	if model, ok := cfg["model"].(string); ok && containsKeiRouter(model) {
		delete(cfg, "model")
	}
	if len(cfg) == 0 {
		return os.Remove(path)
	}
	return writeJSON(path, cfg)
}
