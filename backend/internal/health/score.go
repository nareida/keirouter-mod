package health

import "sort"

// Health status values used across the dashboard, store, and API.
const (
	StatusHealthy   = "healthy"
	StatusDegraded  = "degraded"
	StatusUnhealthy = "unhealthy"
	StatusUnknown   = "unknown"
	StatusDisabled  = "disabled"
)

// ScoreInput captures the raw signals needed to compute a 0-100 health score.
type ScoreInput struct {
	// SuccessRate is the fraction of successful requests in the window (0-1).
	SuccessRate float64
	// P95LatencyMs is the 95th percentile latency, or 0 if no latency samples.
	P95LatencyMs int
	// LatencyThresholdMs is the capability-specific p95 threshold. 0 disables
	// the latency component (treated as a perfect score).
	LatencyThresholdMs int
	// DominantErrorType is the most frequent error type in the window, or
	// ProviderErrorNone when there were no failures.
	DominantErrorType ProviderErrorType
	// ConsecutiveFailures is the current streak of failures.
	ConsecutiveFailures int
}

// ComputeScore returns a 0-100 health score from the weighted sum of success,
// latency, error-quality, and stability sub-scores.
//
//	success_score      0.40  = success_rate * 100
//	latency_score      0.25  = 100 if p95 <= threshold else linear decay
//	error_quality      0.20  = severity score of the dominant error type
//	stability_score    0.15  = based on consecutive failures
func ComputeScore(in ScoreInput) int {
	successScore := in.SuccessRate * 100

	latencyScore := 100.0
	if in.LatencyThresholdMs > 0 && in.P95LatencyMs > in.LatencyThresholdMs {
		over := float64(in.P95LatencyMs - in.LatencyThresholdMs)
		latencyScore = 100 - (over/float64(in.LatencyThresholdMs))*100
		if latencyScore < 0 {
			latencyScore = 0
		}
	}

	errorQuality := float64(ErrorSeverityScore(in.DominantErrorType))

	stability := stabilityScore(in.ConsecutiveFailures)

	score := successScore*0.40 + latencyScore*0.25 + errorQuality*0.20 + stability*0.15
	return clampInt(int(score))
}

func stabilityScore(consecutiveFailures int) float64 {
	switch {
	case consecutiveFailures <= 0:
		return 100
	case consecutiveFailures == 1:
		return 80
	case consecutiveFailures == 2:
		return 60
	case consecutiveFailures == 3:
		return 30
	default:
		return 0
	}
}

// StatusFromScore derives a health status from a score. When there is no data
// (hasData false) the status is unknown; disabled overrides everything.
func StatusFromScore(score int, hasData, disabled bool) string {
	if disabled {
		return StatusDisabled
	}
	if !hasData {
		return StatusUnknown
	}
	switch {
	case score >= 90:
		return StatusHealthy
	case score >= 65:
		return StatusDegraded
	default:
		return StatusUnhealthy
	}
}

// MainIssue derives a short human-readable main issue string from the dominant
// error type and signals, so a degraded/unhealthy row always explains itself.
func MainIssue(errType ProviderErrorType, p95LatencyMs, latencyThresholdMs int, fallbackCount int64) string {
	switch errType {
	case ProviderErrorAuth:
		return "auth_error"
	case ProviderErrorRateLimited:
		return "rate_limited"
	case ProviderErrorQuotaExceeded:
		return "quota_exceeded"
	case ProviderErrorTimeout:
		return "timeout"
	case ProviderErrorProvider5xx:
		return "provider_5xx"
	case ProviderErrorNetwork:
		return "network_error"
	case ProviderErrorUnsupported:
		return "unsupported_model_or_capability"
	case ProviderErrorBadRequest:
		return "bad_request"
	case ProviderErrorUnknown:
		return "unknown_error"
	}
	// No dominant error: check latency then fallbacks.
	if latencyThresholdMs > 0 && p95LatencyMs > latencyThresholdMs {
		return "high_latency"
	}
	if fallbackCount > 0 {
		return "fallback_spike"
	}
	return ""
}

// Percentile returns the q-th percentile (0-100) of a sorted-ascending sample
// slice. For small per-key windows exact percentiles are cheap and accurate.
func Percentile(sorted []int, q float64) int {
	if len(sorted) == 0 {
		return 0
	}
	if q <= 0 {
		return sorted[0]
	}
	if q >= 100 {
		return sorted[len(sorted)-1]
	}
	// Nearest-rank method.
	idx := int(float64(len(sorted)-1) * q / 100)
	return sorted[idx]
}

// SortInts returns a sorted copy of in ascending. Used before Percentile.
func SortInts(in []int) []int {
	out := make([]int, len(in))
	copy(out, in)
	sort.Ints(out)
	return out
}

func clampInt(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
