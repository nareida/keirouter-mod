package pipeline

import (
	"strings"

	"github.com/mydisha/keirouter/backend/internal/capability"
	"github.com/mydisha/keirouter/backend/internal/core"
)

func optimisticCloudflareImageRequest(provider, model string, req *core.ChatRequest) bool {
	if provider != "cloudflare-ai" || capability.ImagePolicy(provider, model) != capability.ImageOptimistic || req == nil {
		return false
	}
	for _, message := range req.Messages {
		for _, part := range message.Content {
			if part.Type == core.PartImage {
				return true
			}
		}
	}
	return false
}

func isUnsupportedCloudflareImageError(err error, provider, model string, req *core.ChatRequest) bool {
	if !optimisticCloudflareImageRequest(provider, model, req) {
		return false
	}
	pe := core.AsProviderError(err)
	if pe.StatusCode != 400 && pe.StatusCode != 422 {
		return false
	}
	message := strings.ToLower(pe.Message)
	modality := strings.Contains(message, "image") || strings.Contains(message, "vision") || strings.Contains(message, "multimodal") || strings.Contains(message, "content part")
	rejection := strings.Contains(message, "unsupported") || strings.Contains(message, "not supported") || strings.Contains(message, "must be string") || strings.Contains(message, "invalid") || strings.Contains(message, "not allowed")
	return modality && rejection
}
