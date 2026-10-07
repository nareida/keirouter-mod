package connectors

import (
	"regexp"
	"strings"

	"github.com/mydisha/keirouter/backend/internal/core"
)

// codexClientVersion mirrors the Codex CLI release the ChatGPT backend expects.
// The /codex/models endpoint gates entries by minimal_client_version and the
// responses endpoint favors requests that look like the official CLI, so keep
// this reasonably current.
const codexClientVersion = "0.144.0"

// codexEffortLevels are the reasoning-effort suffixes accepted on Codex model
// ids (e.g. "gpt-5.3-codex-high"). The ChatGPT Codex backend only knows base
// model ids, so the suffix is stripped and folded into reasoning.effort.
// "xhigh" is listed before "high" so the longer suffix wins. "max"/"ultra" are
// deliberately absent: they collide with real model ids (gpt-5.1-codex-max)
// and are only valid as gpt-5.6 alias suffixes (see codexGPT56AliasSuffix).
var codexEffortLevels = []string{"xhigh", "high", "medium", "low", "minimal", "none"}

// codexGPT56AliasSuffix matches the gpt-5.6 family's -max/-ultra effort alias
// ids (e.g. "gpt-5.6-sol-ultra"), tolerating the dash-for-dot version typo.
var codexGPT56AliasSuffix = regexp.MustCompile(`^(gpt-5[.-]6-(?:sol|terra|luna))-(max|ultra)$`)

// codexModelAliases maps convenience ids to real ChatGPT Codex backend ids.
// The backend has no bare "gpt-5.6"; the 5.6 family ships as sol/terra/luna.
var codexModelAliases = map[string]string{
	"gpt-5.6": "gpt-5.6-sol",
}

// codexDashVersion matches a dashed version segment right after the "gpt"
// prefix ("gpt-5-6"), the common misspelling of the dotted version ids
// ("gpt-5.6") Codex actually uses — sending the dashed form upstream fails
// with 400 "model is not supported". Anchoring to the prefix keeps later
// digit-dash-digit runs intact, e.g. a date-suffixed snapshot id like
// "gpt-5.1-codex-max-2025-11-13".
var codexDashVersion = regexp.MustCompile(`^(gpt-\d+)-(\d+)`)

// codexModelKnown reports whether id is in the curated Codex catalog.
func codexModelKnown(id string) bool {
	for _, spec := range providerModels["codex"] {
		if spec.ID == id {
			return true
		}
	}
	return false
}

// resolveCodexModel normalizes a requested model id to the id the ChatGPT
// Codex backend understands. It strips a reasoning-effort suffix (returned
// separately), tolerates dash-for-dot version typos, and resolves aliases.
// Unknown ids are passed through (the catalog may lag upstream releases);
// the upstream 400 is then classified as model-unavailable for fallback.
func resolveCodexModel(model string) (string, string) {
	effort := ""
	base := model
	if m := codexGPT56AliasSuffix.FindStringSubmatch(base); m != nil {
		base, effort = m[1], m[2]
	} else {
		for _, level := range codexEffortLevels {
			if strings.HasSuffix(base, "-"+level) {
				base = strings.TrimSuffix(base, "-"+level)
				effort = level
				break
			}
		}
	}
	if codexModelKnown(base) {
		return base, effort
	}
	norm := codexDashVersion.ReplaceAllString(base, "$1.$2")
	if alias, ok := codexModelAliases[norm]; ok {
		return alias, effort
	}
	return norm, effort
}

// codexEffortsGPT56 is the wire enum the gpt-5.6 family accepts, taken from
// the backend's own 400 message: "Supported values are: 'none', 'minimal',
// 'low', 'medium', 'high', and 'max'." — note "max" replaced "xhigh".
var codexEffortsGPT56 = map[string]bool{
	"none": true, "minimal": true, "low": true, "medium": true, "high": true, "max": true,
}

// codexEffortsLegacy is the wire enum pre-5.6 Codex models accept (the
// original codex CLI set, topping out at "xhigh").
var codexEffortsLegacy = map[string]bool{
	"none": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true,
}

// normalizeCodexEffort maps any client-supplied effort onto the wire enum the
// target model actually accepts. Clients speak many dialects here: Claude Code
// sends "adaptive", Responses clients send "xhigh"/"max"/"ultra", and the
// backend hard-rejects values outside its per-family enum, so nothing may pass
// through unvalidated.
func normalizeCodexEffort(model, effort string) string {
	e := strings.ToLower(strings.TrimSpace(effort))
	switch e {
	case "", "auto", "adaptive", "default":
		// "Model decides" modes have no Codex wire value; use the CLI default.
		e = "medium"
	case "off", "disabled":
		e = "none"
	}
	if strings.HasPrefix(model, "gpt-5.6") {
		if e == "xhigh" || e == "ultra" {
			// Ultra is a client-side delegation concept; the wire effort is max.
			e = "max"
		}
		if !codexEffortsGPT56[e] {
			e = "medium"
		}
		return e
	}
	if e == "max" || e == "ultra" {
		e = "xhigh"
	}
	if !codexEffortsLegacy[e] {
		e = "medium"
	}
	return e
}

// prepareCodexRequest normalizes the model id and reasoning config for the
// ChatGPT Codex backend. It returns a shallow copy so a fallback to another
// provider still dispatches with the caller's original request.
func prepareCodexRequest(req *core.ChatRequest) *core.ChatRequest {
	model, effort := resolveCodexModel(req.Model)
	out := *req
	out.Model = model
	// An effort suffix is an explicit selection and overrides any
	// client-injected reasoning default (mirrors the codex CLI); otherwise
	// honor the client's effort. Either way the value is normalized onto the
	// model's wire enum — all Codex models are reasoning models and the
	// backend rejects unknown values (e.g. Claude Code's "adaptive").
	if effort == "" && out.Reasoning != nil {
		effort = out.Reasoning.Effort
	}
	out.Reasoning = &core.ReasoningConfig{Effort: normalizeCodexEffort(model, effort)}
	return &out
}

// applyCodexHeaders mirrors the identity headers the official Codex CLI sends
// on inference calls. chatgpt-account-id is the critical one: without it,
// tokens for accounts with multiple workspaces can bind to the wrong ChatGPT
// account and surface auth or model-access errors.
func applyCodexHeaders(h map[string]string, creds core.Credentials) {
	h["OpenAI-Beta"] = "responses=experimental"
	h["originator"] = "codex_cli_rs"
	h["version"] = codexClientVersion
	h["User-Agent"] = "codex_cli_rs/" + codexClientVersion
	// Stable per-account session id gives the backend prompt-cache affinity.
	if creds.AccountID != "" {
		h["session_id"] = creds.AccountID
	}
	// keirouter's own OAuth flow stores chatgpt_account_id; imported accounts
	// may carry the CLI/9router-style keys instead.
	for _, key := range []string{"chatgpt_account_id", "workspaceId", "accountId", "chatgptAccountId"} {
		if v := creds.Extra[key]; v != "" {
			h["chatgpt-account-id"] = v
			break
		}
	}
}
