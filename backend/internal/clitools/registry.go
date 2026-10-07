// Package clitools implements auto-configuration for coding CLI tools. Each
// tool knows how to detect its installation status, write KeiRouter-specific
// config keys into the tool's native config file, and remove them again.
//
// The common strategy is merge-not-overwrite: existing settings are preserved;
// only KeiRouter-specific keys are added or removed.
package clitools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

// Tool describes a CLI tool that can be auto-configured to use KeiRouter.
type Tool interface {
	// ID returns the tool's short identifier (e.g. "claude", "codex").
	ID() string
	// Name returns the human-readable display name.
	Name() string
	// Command returns the CLI binary name used to verify installation
	// (e.g. "claude", "codex"). Return "" for tools that ship only as IDE
	// extensions (cline, copilot, kilo) — those are detected via their config
	// files instead of a binary lookup.
	Command() string
	// DetectStatus checks whether the tool is installed and whether KeiRouter
	// is already configured. configPath is the path that was checked.
	DetectStatus(homeDir string) (installed, configured bool, configPath string, err error)
	// Configure writes KeiRouter settings into the tool's config file(s).
	Configure(homeDir, baseURL, apiKey string, models []string) error
	// Remove strips KeiRouter settings from the tool's config file(s).
	Remove(homeDir string) error
}

// Status holds the result of DetectStatus for one tool.
type Status struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Installed  bool   `json:"installed"`
	Configured bool   `json:"configured"`
	ConfigPath string `json:"config_path"`
	// BinaryPath is the resolved executable path, or "" when no binary was
	// found (e.g. IDE-only tools or uninstalled CLIs).
	BinaryPath string `json:"binary_path,omitempty"`
	// Version is the best-effort version string reported by the binary, or ""
	// when unavailable.
	Version string `json:"version,omitempty"`
	// Error holds a detection error message when detection failed for reasons
	// other than the tool simply being absent.
	Error string `json:"error,omitempty"`
}

// Registry holds all known CLI tools.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry builds a registry with all built-in tool implementations.
func NewRegistry() *Registry {
	r := &Registry{tools: make(map[string]Tool)}
	for _, t := range []Tool{
		&ClaudeTool{},
		&CodexTool{},
		&ClineTool{},
		&CopilotTool{},
		&DroidTool{},
		&OpenClawTool{},
		&OpenCodeTool{},
		&KiloTool{},
		&HermesTool{},
		&DeepSeekTool{},
		&JcodeTool{},
	} {
		r.tools[t.ID()] = t
	}
	return r
}

// Get returns a tool by id, or nil if unknown.
func (r *Registry) Get(id string) Tool { return r.tools[id] }

// All returns all registered tools.
func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// DetectAll returns the status of every registered tool. Detection runs in
// parallel with a per-tool timeout so one slow or misbehaving CLI cannot stall
// the batch. For each tool, detection combines:
//   - a binary lookup (exec.LookPath over an augmented PATH) — so a stale
//     config file left behind after uninstall no longer false-positives as
//     "installed";
//   - the tool's own config-file check; and
//   - a best-effort `<cmd> --version` probe.
//
// A tool counts as installed if EITHER its binary is on PATH OR its config
// file exists (the config file is the only signal for IDE-only extensions).
func (r *Registry) DetectAll(homeDir string) []Status {
	tools := r.All()
	out := make([]Status, len(tools))
	idx := make(map[string]int, len(tools))
	for i, t := range tools {
		idx[t.ID()] = i
		out[i] = Status{ID: t.ID(), Name: t.Name()}
	}

	var mu sync.Mutex
	g, gctx := errgroup.WithContext(context.Background())
	for i, t := range tools {
		i, t := i, t
		g.Go(func() error {
			ctx, cancel := context.WithTimeout(gctx, detectTimeout)
			defer cancel()

			st := detectOne(ctx, t, homeDir)
			mu.Lock()
			out[i] = st
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return out
}

// detectOne runs the full detection pipeline for a single tool. It never
// panics on errors — failures are surfaced via Status.Error so the UI can
// distinguish "not installed" from "detection broke".
func detectOne(ctx context.Context, t Tool, homeDir string) Status {
	st := Status{ID: t.ID(), Name: t.Name()}

	// Binary lookup (no timeout needed — exec.LookPath is fast).
	if bin, ok := lookupBinary(t.Command()); ok {
		st.BinaryPath = bin
	}

	// Tool-specific config check. Wrapped so a panic/long read cannot kill the
	// goroutine; the ctx timeout bounds the whole pipeline.
	done := make(chan struct{})
	go func() {
		defer close(done)
		inst, conf, path, err := t.DetectStatus(homeDir)
		if err != nil {
			st.Error = err.Error()
		}
		st.ConfigPath = path
		// A tool is installed if the binary is on PATH OR its config file
		// exists. This avoids false negatives when the binary is shadowed by
		// a shell alias/function, and avoids false positives being the only
		// signal: a stale config alone still counts, because the user may have
		// a non-standard install location.
		st.Installed = inst || st.BinaryPath != ""
		st.Configured = conf
	}()

	select {
	case <-done:
	case <-ctx.Done():
		if st.Error == "" {
			st.Error = "detection timed out"
		}
		// If we already found a binary, keep "installed" true even on timeout.
		if st.BinaryPath != "" {
			st.Installed = true
		}
	}

	// Best-effort version probe (skipped on timeout). Only probe real CLIs;
	// IDE extensions have no binary.
	if st.BinaryPath != "" && ctx.Err() == nil {
		vctx, vcancel := context.WithTimeout(context.Background(), versionProbeTimeout)
		v := make(chan string, 1)
		go func() {
			v <- detectVersion(st.BinaryPath, t.Command())
		}()
		select {
		case ver := <-v:
			st.Version = ver
		case <-vctx.Done():
		}
		vcancel()
	}

	return st
}

// ---- common helpers --------------------------------------------------------

// expandHome resolves ~ to homeDir.
func expandHome(homeDir, path string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, path[2:])
	}
	return path
}

// fileExists reports whether path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readJSON reads a JSON file into v. Returns os.ErrNotExist if missing.
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// writeJSON writes v as indented JSON to path, creating dirs as needed.
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

// readString reads a file into a string.
func readString(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// writeString writes content to path, creating dirs as needed.
func writeString(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// ensureSuffix ensures baseURL ends with suffix.
func ensureSuffix(baseURL, suffix string) string {
	if !strings.HasSuffix(baseURL, suffix) {
		return strings.TrimRight(baseURL, "/") + suffix
	}
	return baseURL
}

// stripSuffix removes suffix from the end of baseURL if present.
func stripSuffix(baseURL, suffix string) string {
	return strings.TrimSuffix(strings.TrimRight(baseURL, "/"), suffix)
}

// platformConfigPath returns the platform-specific VS Code config path.
func platformConfigPath(homeDir, suffix string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(homeDir, "Library", "Application Support", "Code", "User", suffix)
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "Code", "User", suffix)
		}
		return filepath.Join(homeDir, "AppData", "Roaming", "Code", "User", suffix)
	default: // linux
		return filepath.Join(homeDir, ".config", "Code", "User", suffix)
	}
}
