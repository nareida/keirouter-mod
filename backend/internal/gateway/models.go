package gateway

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mydisha/keirouter/backend/internal/capability"
	"github.com/mydisha/keirouter/backend/internal/connectors"
	"github.com/mydisha/keirouter/backend/internal/core"
	"github.com/mydisha/keirouter/backend/internal/store"
)

// modelEntry is one entry in a /v1/models listing, in the OpenAI shape plus
// KeiRouter extensions (provider, kind, dimensions).
type modelEntry struct {
	ID               string                      `json:"id"`
	Object           string                      `json:"object"`
	OwnedBy          string                      `json:"owned_by"`
	Provider         string                      `json:"provider,omitempty"`
	Kind             string                      `json:"kind,omitempty"`
	Name             string                      `json:"name,omitempty"`
	Dimensions       int                         `json:"dimensions,omitempty"`
	Capabilities     *modelCapabilities          `json:"capabilities,omitempty"`
	CapabilitySource capability.CapabilitySource `json:"capability_source,omitempty"`
}

// handleListModels reports targetable models: the tenant's chains (as virtual
// models) plus every catalogued LLM model in provider/model form. This lets a
// client discover what it can pass in the `model` field.
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	key, _ := authedKey(r.Context())
	tenantID := tenantOf(key)

	data := make([]modelEntry, 0, 64)
	seen := make(map[string]struct{}, 64)

	// Per-key model visibility. A key carrying an allowlist must not see the
	// rest of the tenant's catalog in /v1/models: leaking the full list tells
	// the holder which models exist, which the request path already refuses to
	// route to them.
	allowedModels, err := s.identity.Keys().GetAllowedModels(r.Context(), key.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "model access check failed")
		return
	}
	keyRestricted := len(allowedModels) > 0

	usableProviders := s.usableModelProviders(r.Context(), tenantID)
	if s.log != nil {
		dyn := connectors.DynamicProviderModels()
		names := make([]string, 0, len(usableProviders))
		for name := range usableProviders {
			names = append(names, name)
		}
		s.log.Info("models listing",
			"usable_providers", names,
			"dynamic_model_count", len(dyn),
			"static_llm_count", len(connectors.ModelsByKind(core.ServiceLLM)),
		)
	}

	// Chains are exposed as "combo" models, matching the upstream convention:
	// a combo chains multiple providers with auto-fallback and is callable by
	// its bare name (and via the chain: prefix). owned_by:"combo" lets client
	// tools surface them distinctly from single-provider models.
	chains, err := s.chains.ListByTenant(r.Context(), tenantID)
	if err == nil {
		for _, c := range chains {
			chainMaxCtx := 0
			if s.accounts != nil {
				steps, serr := s.chains.ListSteps(r.Context(), c.ID)
				if serr == nil {
					for _, st := range steps {
						ctx := 0
						if spec, ok := connectors.FindModel(st.Provider, st.Model); ok && spec.ContextWindow > 0 {
							caps, _ := capabilityPayloadFor(st.Provider, st.Model, core.ServiceLLM, spec.ContextWindow)
							ctx = caps.ContextWindow
						}
						// Check custom_models repo for dynamic/custom providers
				if ctx == 0 && s.db != nil && s.db.CustomProviders() != nil {
					if cm, cerr := s.db.CustomProviders().GetModelByProviderAndID(r.Context(), st.Provider, st.Model); cerr == nil && cm.ContextWindow > 0 {
						ctx = cm.ContextWindow
					}
				}
						if ctx > chainMaxCtx {
							chainMaxCtx = ctx
						}
					}
				}
			}
			if chainMaxCtx == 0 {
				chainMaxCtx = 1048576 // 1M default for combo/chains
			}
			caps := modelCapabilities{ContextWindow: chainMaxCtx}
			if keyRestricted && !visibleToKey(c.Name, allowedModels) {
				continue
			}
			data = appendModelEntry(data, seen, modelEntry{
				ID:           c.Name, Object: "model", OwnedBy: "combo", Kind: string(core.ServiceLLM), Name: c.Name,
				Capabilities: &caps,
			})
		}
	}

	// Static catalog models for providers the tenant has connected. Without this
	// gate, discovery advertises provider/model ids that the dispatcher will later
	// reject with "no accounts configured". Custom providers are skipped here:
	// they are listed below under their user-chosen alias, and letting them
	// through would advertise the internal custom-openai-* id as well.
	for _, pm := range connectors.ModelsByKind(core.ServiceLLM) {
		if !usableProviders[pm.Provider] {
			continue
		}
		if spec, ok := connectors.SpecByID(pm.Provider); ok && spec.Custom {
			continue
		}
		caps, source := capabilityPayload(pm.Provider, pm.Model.ID, core.ServiceLLM)
		for _, prefix := range modelPrefixes(pm.Provider) {
			entryID := prefix + "/" + pm.Model.ID
			if keyRestricted && !visibleToKey(entryID, allowedModels) {
				continue
			}
			data = appendModelEntry(data, seen, modelEntry{
				ID:           entryID,
				Object:       "model",
				OwnedBy:      pm.Provider,
				Provider:     pm.Provider,
				Kind:         string(core.ServiceLLM),
				Name:         pm.Model.Name,
				Capabilities: &caps, CapabilitySource: source,
			})
		}
	}

	// Custom providers added through the dashboard. They have no static catalog
	// entry, so ModelsByKind above cannot see them; listing them here is what
	// makes /v1/models useful for clients that discover models before calling.
	// Both the provider id and the user-chosen alias are advertised, because
	// resolveTargets accepts either as the routing prefix.
	for _, pm := range connectors.DynamicProviderModels() {
		if !usableProviders[pm.Provider] {
			continue
		}
		caps, source := capabilityPayloadFor(pm.Provider, pm.Model.ID, core.ServiceLLM, pm.Model.ContextWindow)
		for _, prefix := range modelPrefixes(pm.Provider) {
			entryID := prefix + "/" + pm.Model.ID
			if keyRestricted && !visibleToKey(entryID, allowedModels) {
				continue
			}
			data = appendModelEntry(data, seen, modelEntry{
				ID:               entryID,
				Object:           "model",
				OwnedBy:          prefix,
				Provider:         pm.Provider,
				Kind:             string(core.ServiceLLM),
				Name:             pm.Model.Name,
				Capabilities:     &caps,
				CapabilitySource: source,
			})
		}
	}

	// Live model discovery: for providers with a LiveModelSource and connected
	// accounts, fetch the live catalog and merge (live models supplement, not
	// replace, the static catalog).
	liveModels := s.fetchLiveModels(r.Context(), tenantID)
	for provider, models := range liveModels {
		if !usableProviders[provider] {
			continue
		}
		for _, lm := range models {
			caps, source := capabilityPayload(provider, lm.ID, lm.Kind)
			entryID := provider + "/" + lm.ID
			if keyRestricted && !visibleToKey(entryID, allowedModels) {
				continue
			}
			data = appendModelEntry(data, seen, modelEntry{
				ID:           entryID,
				Object:       "model",
				OwnedBy:      provider,
				Provider:     provider,
				Kind:         string(lm.Kind),
				Name:         lm.Name,
				Capabilities: &caps, CapabilitySource: source,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

// handleListModelsByKind serves GET /v1/models/{kind}: it lists every model of
// the requested service kind (llm, embedding, image, stt, tts, search, fetch)
// across the provider catalog, plus a special "chains" view.
func (s *Server) handleListModelsByKind(w http.ResponseWriter, r *http.Request) {
	kindParam := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "kind")))

	// "chains" is a convenience view of the tenant's routing chains.
	if kindParam == "chains" {
		s.handleListModels(w, r)
		return
	}

	kind := core.ServiceKind(kindParam)
	if !core.ValidServiceKind(kind) {
		writeError(w, http.StatusBadRequest, "unknown model kind: "+kindParam)
		return
	}

	pairs := connectors.ModelsByKind(kind)
	data := make([]modelEntry, 0, len(pairs))
	seen := make(map[string]struct{}, len(pairs))
	key, _ := authedKey(r.Context())
	usableProviders := s.usableModelProviders(r.Context(), tenantOf(key))
	for _, pm := range pairs {
		if !usableProviders[pm.Provider] {
			continue
		}
		caps, source := capabilityPayload(pm.Provider, pm.Model.ID, pm.Model.Kind)
		data = appendModelEntry(data, seen, modelEntry{
			ID:           pm.Provider + "/" + pm.Model.ID,
			Object:       "model",
			OwnedBy:      pm.Provider,
			Provider:     pm.Provider,
			Kind:         string(pm.Model.Kind),
			Name:         pm.Model.Name,
			Dimensions:   pm.Model.Dimensions,
			Capabilities: &caps, CapabilitySource: source,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "kind": kindParam, "data": data})
}

// fetchLiveModels queries providers that support live model discovery, using
// the first connected account's credentials. Returns a map of provider →
// models. Errors are silently skipped (live discovery is best-effort).
func (s *Server) fetchLiveModels(ctx context.Context, tenantID string) map[string][]connectors.ModelSpec {
	if s.accounts == nil || s.vault == nil {
		return nil
	}
	result := map[string][]connectors.ModelSpec{}

	// Check each provider that has a live model source.
	for provider, src := range map[string]connectors.LiveModelSource{
		"kiro": connectors.GetLiveModelSource("kiro"),
	} {
		if src == nil {
			continue
		}
		accs, err := s.accounts.ListByProvider(ctx, tenantID, provider)
		if err != nil || len(accs) == 0 {
			continue
		}
		// Use the first non-disabled account.
		var acc store.Account
		for _, a := range accs {
			if !a.Disabled && !a.NeedsReconnect {
				acc = a
				break
			}
		}
		if acc.ID == "" {
			continue
		}
		creds, err := s.vault.Open(acc)
		if err != nil {
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		models, err := src.ListModels(probeCtx, creds)
		cancel()
		if err != nil || len(models) == 0 {
			continue
		}
		result[provider] = models
	}
	return result
}

func (s *Server) usableModelProviders(ctx context.Context, tenantID string) map[string]bool {
	usable := map[string]bool{}
	if s.accounts == nil {
		return usable
	}
	accs, err := s.accounts.ListByTenant(ctx, tenantID)
	if err != nil {
		return usable
	}
	for _, acc := range accs {
		if acc.Provider == "" || acc.Disabled || acc.NeedsReconnect {
			continue
		}
		usable[acc.Provider] = true
	}
	return usable
}

func appendModelEntry(data []modelEntry, seen map[string]struct{}, entry modelEntry) []modelEntry {
	if entry.ID == "" {
		return data
	}
	if _, ok := seen[entry.ID]; ok {
		return data
	}
	seen[entry.ID] = struct{}{}
	return append(data, entry)
}

// visibleToKey reports whether a listed model survives the API key's allowlist.
// Chains carry their own bare name as the id, and an allowlist naming a chain
// must keep it visible. Matching ignores the routing prefix, so an allowlist
// written as either "ds/deepseek-v4-flash" or the bare model id matches.
func visibleToKey(entryID string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	bare := entryID
	if _, rest, ok := strings.Cut(entryID, "/"); ok {
		bare = rest
	}
	return modelMatchesAny(entryID, allowed) || modelMatchesAny(bare, allowed)
}

// modelPrefixes returns the routing prefix for providerID that should appear
// in /v1/models. The alias is preferred because it is the user-facing form; the
// provider id is still accepted by resolveTargets but is omitted from the
// listing to avoid duplicating entries.
func modelPrefixes(providerID string) []string {
	if spec, ok := connectors.SpecByID(providerID); ok && spec.Alias != "" && spec.Alias != providerID {
		return []string{spec.Alias}
	}
	return []string{providerID}
}

// handleModelInfo serves GET /v1/models/info?id=<provider/model>: it returns
// metadata for a single model (kind, dimensions, provider, name).
func (s *Server) handleModelInfo(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "id query parameter is required")
		return
	}

	provider, model, ok := strings.Cut(id, "/")
	if !ok || provider == "" || model == "" {
		writeError(w, http.StatusBadRequest, "id must be in provider/model form")
		return
	}
	key, _ := authedKey(r.Context())
	if !s.usableModelProviders(r.Context(), tenantOf(key))[provider] {
		writeError(w, http.StatusNotFound, "unknown model: "+id)
		return
	}

	spec, found := connectors.FindModel(provider, model)
	if !found {
		writeError(w, http.StatusNotFound, "unknown model: "+id)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         id,
		"provider":   provider,
		"model":      spec.ID,
		"name":       spec.Name,
		"kind":       string(spec.Kind),
		"dimensions": spec.Dimensions,
	})
}
