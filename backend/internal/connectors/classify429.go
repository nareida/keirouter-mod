package connectors

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mydisha/keirouter/backend/internal/core"
)

// quotaPatterns matches provider error text indicating a hard quota/cap has
// been reached (as opposed to a transient rate limit). These warrant a much
// longer cooldown than a per-minute rate limit.
var quotaPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)daily.*(?:limit|quota|allocation)`),
	regexp.MustCompile(`(?i)monthly.*(?:limit|quota)`),
	regexp.MustCompile(`(?i)per.?day.*limit`),
	regexp.MustCompile(`(?i)per.?month.*limit`),
	regexp.MustCompile(`(?i)insufficient.*quota`),
	regexp.MustCompile(`(?i)billing.*cap`),
	regexp.MustCompile(`(?i)credit.*exhaust`),
	regexp.MustCompile(`(?i)out of credits`),
	regexp.MustCompile(`(?i)hard.?limit`),
	regexp.MustCompile(`(?i)plan.*limit`),
	regexp.MustCompile(`(?i)subscription.*(?:limit|quota|cap)`),
	regexp.MustCompile(`(?i)weekly.*(?:limit|quota)`),
	regexp.MustCompile(`(?i)session.*(?:limit|quota)`),
	regexp.MustCompile(`(?i)daily free allocation`),
}

// creditsPatterns matches provider error text indicating the account's paid
// balance is depleted. Unlike calendar quotas these never recover on their
// own — the user must top up or renew — so they warrant parking the account
// rather than scheduling a quota-window cooldown.
var creditsPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)insufficient[ _]?(?:credit|balance|fund)`),
	regexp.MustCompile(`(?i)insufficient[ _]?quota`),
	regexp.MustCompile(`(?i)out of credits`),
	regexp.MustCompile(`(?i)no credits? remaining`),
	regexp.MustCompile(`(?i)credit.{0,40}(?:exhaust|too low|deplet)`),
	regexp.MustCompile(`(?i)balance.{0,40}(?:too low|insufficient|exhaust|deplet)`),
	regexp.MustCompile(`(?i)(?:purchase|buy|add).{0,20}credits`),
	regexp.MustCompile(`(?i)\btop[- ]?up\b`),
	regexp.MustCompile(`(?i)payment required`),
}

// looksLikeCreditsExhausted reports whether the provider error body indicates
// a depleted paid balance rather than a resettable quota or transient rate
// limit. Used to classify errors into a terminal credits-exhausted state.
func looksLikeCreditsExhausted(body string) bool {
	if body == "" {
		return false
	}
	for _, re := range creditsPatterns {
		if re.MatchString(body) {
			return true
		}
	}
	return false
}

// rateLimitPatterns matches transient rate-limit text. These are short-term
// backoffs, not long-term quota exhaustion.
var rateLimitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)rate.?limit`),
	regexp.MustCompile(`(?i)too many requests`),
	regexp.MustCompile(`(?i)requests? per (?:minute|second|hour)`),
	regexp.MustCompile(`(?i)RPM`),
	regexp.MustCompile(`(?i)TPM`),
	regexp.MustCompile(`(?i)concurrent`),
	regexp.MustCompile(`(?i)throttl`),
}

// looksLikeQuotaExhausted reports whether the provider error body indicates
// a hard quota/cap rather than a transient rate limit. Used to classify 429s
// into ErrQuotaExhausted (long cooldown) vs ErrRateLimit (short backoff).
func looksLikeQuotaExhausted(body string) bool {
	if body == "" {
		return false
	}
	// Per-second/minute/hour quota wording is a transient throttle even when
	// the provider also uses the phrase "quota exceeded".
	if looksLikeRateLimit(body) {
		return false
	}
	for _, re := range quotaPatterns {
		if re.MatchString(body) {
			return true
		}
	}
	return false
}

// looksLikeRateLimit reports whether the provider error body indicates a
// transient rate limit. Used as a tie-breaker when quota patterns don't match.
func looksLikeRateLimit(body string) bool {
	if body == "" {
		return false
	}
	for _, re := range rateLimitPatterns {
		if re.MatchString(body) {
			return true
		}
	}
	return false
}

// parseRetryAfterHeader parses the Retry-After header value. It handles both
// integer seconds and HTTP-date formats, returning the duration to wait.
func parseRetryAfterHeader(ra string) time.Duration {
	if ra == "" {
		return 0
	}
	// Integer seconds.
	if secs, err := strconv.Atoi(ra); err == nil {
		return time.Duration(secs) * time.Second
	}
	// HTTP-date.
	if retryAt, err := http.ParseTime(ra); err == nil {
		if wait := time.Until(retryAt); wait > 0 {
			return wait
		}
	}
	return 0
}

// parseResetFromHeaders extracts rate-limit reset hints from common headers.
// Returns the duration until the limit resets, or 0 if no hint is present.
func parseResetFromHeaders(resp *http.Response) time.Duration {
	// Retry-After is the most authoritative.
	if d := parseRetryAfterHeader(resp.Header.Get("Retry-After")); d > 0 {
		return d
	}
	// X-RateLimit-Reset: unix timestamp in seconds or milliseconds.
	if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
		if ts, err := strconv.ParseInt(reset, 10, 64); err == nil {
			// Distinguish seconds vs milliseconds: > 1e10 is likely ms.
			if ts > 10000000000 {
				if wait := time.Duration(ts-time.Now().UnixMilli()) * time.Millisecond; wait > 0 {
					return wait
				}
				return 0
			}
			if wait := time.Duration(ts-time.Now().Unix()) * time.Second; wait > 0 {
				return wait
			}
			return 0
		}
	}
	return 0
}

// parseResetFromBody extracts common retry/reset hints from nested JSON error
// bodies. Providers vary between duration fields and absolute reset timestamps.
func parseResetFromBody(body []byte) time.Duration {
	var value any
	if len(body) == 0 || json.Unmarshal(body, &value) != nil {
		return 0
	}
	return findResetHint(value, 0)
}

func findResetHint(value any, depth int) time.Duration {
	if depth > 5 {
		return 0
	}
	switch v := value.(type) {
	case map[string]any:
		for key, raw := range v {
			normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
			compact := strings.ReplaceAll(normalized, "_", "")
			if strings.Contains(compact, "retryafter") ||
				strings.Contains(compact, "resetat") ||
				strings.Contains(compact, "ratelimitreset") {
				if wait := resetHintDuration(normalized, raw); wait > 0 {
					return wait
				}
			}
		}
		for _, raw := range v {
			if wait := findResetHint(raw, depth+1); wait > 0 {
				return wait
			}
		}
	case []any:
		for _, raw := range v {
			if wait := findResetHint(raw, depth+1); wait > 0 {
				return wait
			}
		}
	}
	return 0
}

func resetHintDuration(key string, value any) time.Duration {
	var number float64
	switch v := value.(type) {
	case float64:
		number = v
	case string:
		s := strings.TrimSpace(v)
		if parsed, err := strconv.ParseFloat(s, 64); err == nil {
			number = parsed
			break
		}
		for _, layout := range []string{time.RFC3339, http.TimeFormat} {
			if at, err := time.Parse(layout, s); err == nil {
				if wait := time.Until(at); wait > 0 {
					return wait
				}
				return 0
			}
		}
		if wait, err := time.ParseDuration(s); err == nil && wait > 0 {
			return wait
		}
	default:
		return 0
	}

	if number <= 0 {
		return 0
	}
	// Reset fields generally carry an epoch. Retry-after fields carry seconds.
	if strings.Contains(key, "reset") {
		if number > 1e12 {
			return positiveDuration(time.UnixMilli(int64(number)))
		}
		if number > 1e9 {
			return positiveDuration(time.Unix(int64(number), 0))
		}
	}
	return time.Duration(number * float64(time.Second))
}

func positiveDuration(at time.Time) time.Duration {
	if wait := time.Until(at); wait > 0 {
		return wait
	}
	return 0
}

// classify429 determines whether a 429 response is a transient rate limit, a
// hard quota exhaustion, or a depleted paid balance, and extracts any
// upstream-provided retry hint. Returns (kind, retryAfter, creditsExhausted).
func classify429(resp *http.Response, body []byte) (kind core.ErrorKind, retryAfter time.Duration, creditsExhausted bool) {
	// Extract retry hints from headers first.
	retryAfter = parseResetFromHeaders(resp)
	if retryAfter <= 0 {
		retryAfter = parseResetFromBody(body)
	}

	bodyStr := string(body)
	// A depleted balance takes precedence over everything: it never recovers
	// on its own, so the dispatcher parks the account instead of scheduling a
	// quota-window cooldown.
	if looksLikeCreditsExhausted(bodyStr) {
		return core.ErrQuotaExhausted, retryAfter, true
	}

	// Hard quota exhaustion next: long cooldown, no point retrying before the
	// calendar/quota window rolls over.
	if looksLikeQuotaExhausted(bodyStr) {
		// If no explicit retry hint, use a conservative long default.
		if retryAfter <= 0 {
			retryAfter = 30 * time.Minute
		}
		return core.ErrQuotaExhausted, retryAfter, false
	}

	// Transient rate limit: short exponential backoff. If no header hint,
	// use a short default.
	if retryAfter <= 0 {
		retryAfter = 5 * time.Second
	}
	return core.ErrRateLimit, retryAfter, false
}
