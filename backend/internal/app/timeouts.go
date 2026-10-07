package app

import (
	"context"
	"encoding/json"
	"time"
)

const endpointSettingsStoreKey = "endpoint_settings"

type timeoutSettingsReader interface {
	Get(context.Context, string) (string, error)
}

// initialTimeoutValues overlays persisted dashboard timeouts on the configured
// fallbacks. Missing, malformed, or partial settings remain safe and preserve
// the corresponding fallback value.
func initialTimeoutValues(
	ctx context.Context,
	settings timeoutSettingsReader,
	fallbackStall, fallbackResponseHeader, fallbackRequest time.Duration,
) (stall, responseHeader, request time.Duration) {
	stall = fallbackStall
	responseHeader = fallbackResponseHeader
	request = fallbackRequest
	if settings == nil {
		return
	}

	raw, err := settings.Get(ctx, endpointSettingsStoreKey)
	if err != nil || raw == "" {
		return
	}
	var stored struct {
		StreamStallTimeoutMs    int `json:"stream_stall_timeout_ms"`
		ResponseHeaderTimeoutMs int `json:"response_header_timeout_ms"`
		RequestTimeoutMs        int `json:"request_timeout_ms"`
	}
	if json.Unmarshal([]byte(raw), &stored) != nil {
		return
	}
	if stored.StreamStallTimeoutMs > 0 {
		stall = time.Duration(stored.StreamStallTimeoutMs) * time.Millisecond
	}
	if stored.ResponseHeaderTimeoutMs > 0 {
		responseHeader = time.Duration(stored.ResponseHeaderTimeoutMs) * time.Millisecond
	}
	if stored.RequestTimeoutMs > 0 {
		request = time.Duration(stored.RequestTimeoutMs) * time.Millisecond
	}
	return
}
