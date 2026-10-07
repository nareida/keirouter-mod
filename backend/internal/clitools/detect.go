package clitools

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// detectTimeout caps how long a single tool's detection (config read + version
// probe) may run. Keeps one misbehaving CLI from stalling the whole batch.
const detectTimeout = 3 * time.Second

// versionProbeTimeout caps the `<cmd> --version` subprocess call.
const versionProbeTimeout = 1500 * time.Millisecond

// lookupBinary resolves name to an absolute executable path using an augmented
// PATH. On Windows, npm/cargo/pnpm global bin dirs are often absent from PATH
// for GUI-launched apps, so they are prepended explicitly. Returns ("", false)
// when the binary cannot be found.
//
// Unlike shelling out to `which`/`where`, this uses exec.LookPath directly —
// no subprocess, cross-platform, and respects PATHEXT on Windows.
func lookupBinary(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	// Retry with an augmented PATH covering common global bin locations that
	// may not be inherited when the gateway is launched by a desktop entry,
	// systemd, or a GUI session.
	for _, dir := range extraBinDirs() {
		candidate := filepath.Join(dir, exeName(name))
		if abs, err := filepath.Abs(candidate); err == nil {
			if isExecutable(abs) {
				return abs, true
			}
		}
	}
	return "", false
}

// exeName appends the .exe suffix on Windows when not already present.
func exeName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}

// extraBinDirs returns platform-specific global binary directories that are
// commonly missing from a server process's PATH.
func extraBinDirs() []string {
	home, _ := os.UserHomeDir()
	var dirs []string
	switch runtime.GOOS {
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			dirs = append(dirs,
				filepath.Join(appdata, "npm"),
				filepath.Join(appdata, "pnpm"),
			)
		}
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			dirs = append(dirs,
				filepath.Join(local, "pnpm"),
				filepath.Join(local, "Microsoft", "WinGet", "Links"),
			)
		}
		if home != "" {
			dirs = append(dirs, filepath.Join(home, ".cargo", "bin"),
				filepath.Join(home, ".opencode", "bin"))
		}
	case "darwin":
		if home != "" {
			dirs = append(dirs,
				"/opt/homebrew/bin",
				"/usr/local/bin",
				filepath.Join(home, ".cargo", "bin"),
				filepath.Join(home, ".local", "bin"),
				filepath.Join(home, ".bun", "bin"),
				filepath.Join(home, ".opencode", "bin"),
			)
		}
	default: // linux / *bsd
		if home != "" {
			dirs = append(dirs,
				filepath.Join(home, ".local", "bin"),
				filepath.Join(home, ".cargo", "bin"),
				filepath.Join(home, ".bun", "bin"),
				filepath.Join(home, ".opencode", "bin"),
				filepath.Join(home, "go", "bin"),
				"/usr/local/bin",
			)
		}
	}
	return dirs
}

// isExecutable reports whether path exists and is executable (regular file
// with any exec bit on POSIX, or any file on Windows).
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return !info.IsDir()
	}
	return !info.IsDir() && info.Mode().Perm()&0o111 != 0
}

// detectVersion runs `<bin> --version` (falling back to `-v` / `version`) with
// a short timeout and returns the first line of output. Returns "" on any
// failure — version is best-effort and must never block detection.
func detectVersion(binPath, name string) string {
	if binPath == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), versionProbeTimeout)
	defer cancel()

	for _, args := range [][]string{{"--version"}, {"-v"}, {"version"}, {"-V"}} {
		out, err := exec.CommandContext(ctx, binPath, args...).Output()
		if ctx.Err() == context.DeadlineExceeded {
			return ""
		}
		if err != nil {
			continue
		}
		line := strings.TrimSpace(string(out))
		if line == "" {
			continue
		}
		// Some CLIs print a banner before the version; take the first
		// non-empty line that contains a digit.
		sc := bufio.NewScanner(strings.NewReader(line))
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if l != "" && strings.ContainsAny(l, "0123456789") {
				return truncateVersion(l)
			}
		}
		if line != "" {
			return truncateVersion(line)
		}
	}
	return ""
}

// truncateVersion caps version strings so a chatty CLI cannot flood the UI.
func truncateVersion(s string) string {
	const max = 120
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// parseJSONC best-effort strips JSONC artifacts (// line comments and trailing
// commas) so that user-edited config files with JSONC syntax do not break JSON
// parsing and misreport a tool as "not configured".
func parseJSONC(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	sc := bufio.NewScanner(strings.NewReader(raw))
	// Allow long single-line JSONC files.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		// Strip // comments that are not inside a string. Simple heuristic:
		// cut at the first // that follows only whitespace/JSON tokens. This
		// mirrors the tolerance the frontend applies and is intentionally
		// conservative — quoted URLs rarely contain "//" preceded by a space.
		if idx := indexLineComment(line); idx >= 0 {
			line = line[:idx]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	// Strip trailing commas before } or ].
	s := b.String()
	s = stripTrailingCommas(s)
	return s
}

// indexLineComment returns the byte index of a `//` line comment start, or -1.
// It skips `//` inside double-quoted strings.
func indexLineComment(line string) int {
	inStr := false
	escaped := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			inStr = !inStr
			continue
		}
		if !inStr && c == '/' && i+1 < len(line) && line[i+1] == '/' {
			return i
		}
	}
	return -1
}

// stripTrailingCommas removes commas that immediately precede a closing } or ],
// allowing lenient parsing of hand-edited JSONC configs.
func stripTrailingCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ',' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// readJSONC is readJSON with JSONC tolerance: strips comments and trailing
// commas before unmarshalling. Used for user-edited config files (Claude,
// OpenCode, Cline, etc.) that commonly contain JSONC.
func readJSONC(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(parseJSONC(string(data))), v)
}
