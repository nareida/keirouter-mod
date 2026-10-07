package health

// RecommendationFor returns an actionable recommendation for a given main issue
// / error type. The dashboard surfaces this so a degraded/unhealthy status
// always comes with a next step.
func RecommendationFor(errType ProviderErrorType) string {
	switch errType {
	case ProviderErrorAuth:
		return "Re-check the API key or OAuth connection for this provider account."
	case ProviderErrorRateLimited:
		return "Lower concurrency, reduce chain priority, or add another account/provider fallback."
	case ProviderErrorQuotaExceeded:
		return "Check provider billing/quota or disable this account temporarily."
	case ProviderErrorTimeout:
		return "Increase the timeout if acceptable, or move this model lower in the fallback chain."
	case ProviderErrorProvider5xx:
		return "Likely upstream instability. Keep fallback enabled and monitor recovery."
	case ProviderErrorUnsupported:
		return "Verify the model name, refresh the model list, or check capability support."
	case ProviderErrorNetwork:
		return "Check network connectivity, DNS, proxy, or the provider base URL."
	case ProviderErrorBadRequest:
		return "Review the request shape; a 4xx indicates a client-side issue."
	case ProviderErrorUnknown:
		return "Check provider logs and run a manual probe."
	default:
		return ""
	}
}

// RecommendationForIssue maps a derived main_issue label (e.g. high_latency) to
// a recommendation when there is no single dominant error type.
func RecommendationForIssue(issue string) string {
	switch issue {
	case "high_latency":
		return "Move a lower-latency model earlier in the chain or reduce request size."
	case "fallback_spike":
		return "Fallback is absorbing failures; investigate the primary provider or add capacity."
	case "":
		return ""
	default:
		return RecommendationFor(ProviderErrorType(issue))
	}
}
