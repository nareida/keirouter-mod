// Package persona injects a caller-specific system prompt into chat requests.
//
// Each KeiRouter API key can carry its own persona. A request arriving on key
// K gets K's persona prepended as a system message, so every consumer of this
// gateway can present a different identity without changing the gateway or the
// client. Keys with no persona are left untouched.
//
// Personas live in a JSON file so they can be edited without a rebuild or a
// restart: the file is re-read whenever its mtime changes.
package persona

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultFile is the persona store path. Override with PERSONA_FILE.
const DefaultFile = "/opt/data/keirouter/personas.json"

// store is the on-disk shape:
//
//	{ "keys": { "<api-key-id>": { "name": "...", "prompt": "..." } } }
type store struct {
	Keys map[string]entry `json:"keys"`
}

type entry struct {
	Name   string `json:"name,omitempty"`
	Prompt string `json:"prompt"`
}

// Registry resolves a persona for an API key id.
type Registry struct {
	path string

	mu       sync.RWMutex
	byKey    map[string]entry
	mtime    time.Time
	size     int64
	loadedAt time.Time
}

// New builds a Registry. An empty path uses DefaultFile.
func New(path string) *Registry {
	if path == "" {
		path = DefaultFile
	}
	r := &Registry{path: path}
	r.reload()
	return r
}

// Path reports the file backing this registry.
func (r *Registry) Path() string { return r.path }

func (r *Registry) reload() {
	st, err := os.Stat(r.path)
	if err != nil {
		r.mu.Lock()
		r.byKey = nil
		r.mtime = time.Time{}
		r.size = 0
		r.mu.Unlock()
		return
	}
	// Skip the read when the file is unchanged since the last load.
	if st.ModTime().Equal(r.mtime) && st.Size() == r.size {
		return
	}
	raw, err := os.ReadFile(r.path)
	if err != nil {
		return
	}
	var s store
	if err := json.Unmarshal(raw, &s); err != nil {
		// A malformed file must not wipe a working persona set; keep the last
		// good copy and let the operator fix the file.
		return
	}
	byKey := make(map[string]entry, len(s.Keys))
	for id, e := range s.Keys {
		p := strings.TrimSpace(e.Prompt)
		if p == "" {
			continue
		}
		byKey[id] = entry{Name: e.Name, Prompt: p}
	}
	r.mu.Lock()
	r.byKey = byKey
	r.mtime = st.ModTime()
	r.size = st.Size()
	r.loadedAt = time.Now()
	r.mu.Unlock()
}

// For returns the persona for an API key id, refreshing from disk when the
// backing file changed. The boolean reports whether a persona exists.
func (r *Registry) For(apiKeyID string) (string, bool) {
	if r == nil || apiKeyID == "" {
		return "", false
	}
	r.reload()
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.byKey[apiKeyID]
	if !ok {
		return "", false
	}
	return e.Prompt, true
}

// Set writes or replaces the persona for an API key id and persists the file.
func (r *Registry) Set(apiKeyID, name, prompt string) error {
	if r == nil {
		return nil
	}
	if strings.TrimSpace(apiKeyID) == "" {
		return errEmptyKey
	}
	r.reload()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byKey == nil {
		r.byKey = map[string]entry{}
	}
	if strings.TrimSpace(prompt) == "" {
		delete(r.byKey, apiKeyID)
	} else {
		r.byKey[apiKeyID] = entry{Name: name, Prompt: prompt}
	}
	return r.persistLocked()
}

// Delete removes a persona.
func (r *Registry) Delete(apiKeyID string) error {
	if r == nil {
		return nil
	}
	r.reload()
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byKey, apiKeyID)
	return r.persistLocked()
}

// List returns every persona, sorted by key id, for the admin API.
func (r *Registry) List() []Persona {
	if r == nil {
		return nil
	}
	r.reload()
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Persona, 0, len(r.byKey))
	for id, e := range r.byKey {
		out = append(out, Persona{APIKeyID: id, Name: e.Name, Prompt: e.Prompt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].APIKeyID < out[j].APIKeyID })
	return out
}

// Persona is one key's configured identity.
type Persona struct {
	APIKeyID string `json:"api_key_id"`
	Name     string `json:"name,omitempty"`
	Prompt   string `json:"prompt"`
}

func (r *Registry) persistLocked() error {
	s := store{Keys: make(map[string]entry, len(r.byKey))}
	for id, e := range r.byKey {
		s.Keys[id] = e
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// Write to a sibling temp file and rename so a reader never observes a
	// half-written store.
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, r.path); err != nil {
		return err
	}
	// Force the next read to pick up the new file rather than trusting mtime.
	r.mtime = time.Time{}
	r.size = 0
	return nil
}
