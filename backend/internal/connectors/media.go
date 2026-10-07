package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mydisha/keirouter/backend/internal/core"
)

// This file implements the non-chat capability interfaces for the
// OpenAI-compatible connector: image generation (/images/generations),
// speech-to-text (/audio/transcriptions), and text-to-speech (/audio/speech).
// These follow the OpenAI wire format, so they serve OpenAI itself plus any
// provider that mirrors those endpoints (Groq STT, etc.).

// ---- Image generation -------------------------------------------------------

type oaiImageRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	Quality        string `json:"quality,omitempty"`
	Style          string `json:"style,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

type oaiImageResponse struct {
	Created int64 `json:"created"`
	Data    []struct {
		URL           string `json:"url"`
		B64JSON       string `json:"b64_json"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
}

// GenerateImage produces images via the OpenAI images endpoint.
func (c *OpenAICompatible) GenerateImage(ctx context.Context, req *core.ImageRequest, creds core.Credentials) (*core.ImageResponse, error) {
	n := req.N
	if n <= 0 {
		n = 1
	}
	body, err := json.Marshal(oaiImageRequest{
		Model: req.Model, Prompt: req.Prompt, N: n, Size: req.Size,
		Quality: req.Quality, Style: req.Style, ResponseFormat: req.ResponseFormat,
	})
	if err != nil {
		return nil, &core.ProviderError{Kind: core.ErrInternal, Provider: c.id, Model: req.Model, Message: err.Error(), Cause: err}
	}

	url := joinURL(c.baseURL(creds), "images/generations")
	respBody, err := doJSON(ctx, c.id, req.Model, url, body, c.headers(creds))
	if err != nil {
		return nil, err
	}

	var raw oaiImageResponse
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, &core.ProviderError{Kind: core.ErrUpstream, Provider: c.id, Model: req.Model, Message: "parse image: " + err.Error(), Cause: err}
	}
	out := &core.ImageResponse{Model: req.Model, Created: raw.Created}
	for _, d := range raw.Data {
		out.Data = append(out.Data, core.ImageData{URL: d.URL, B64JSON: d.B64JSON, RevisedPrompt: d.RevisedPrompt})
	}
	return out, nil
}

// ---- Speech-to-text ---------------------------------------------------------

type oaiTranscriptionResponse struct {
	Text     string  `json:"text"`
	Language string  `json:"language"`
	Duration float64 `json:"duration"`
}

// Transcribe converts audio to text via the OpenAI transcriptions endpoint.
func (c *OpenAICompatible) Transcribe(ctx context.Context, req *core.TranscriptionRequest, creds core.Credentials) (*core.TranscriptionResponse, error) {
	filename := req.Filename
	if filename == "" {
		filename = "audio.mp3"
	}
	fields := []multipartField{
		{Name: "model", Value: req.Model},
		{Name: "language", Value: req.Language},
		{Name: "prompt", Value: req.Prompt},
		{Name: "response_format", Value: req.ResponseFormat},
	}
	if req.Temperature != nil {
		fields = append(fields, multipartField{Name: "temperature", Value: strconv.FormatFloat(*req.Temperature, 'f', -1, 64)})
	}

	url := joinURL(c.baseURL(creds), "audio/transcriptions")
	respBody, err := doMultipart(ctx, c.id, req.Model, url, "file", filename, req.Audio, fields, c.headers(creds))
	if err != nil {
		return nil, err
	}

	// response_format=text returns a bare string, not JSON.
	if req.ResponseFormat == "text" {
		return &core.TranscriptionResponse{Text: string(respBody)}, nil
	}
	var raw oaiTranscriptionResponse
	if err := json.Unmarshal(respBody, &raw); err != nil {
		// Fall back to treating the body as plain text.
		return &core.TranscriptionResponse{Text: string(respBody)}, nil
	}
	return &core.TranscriptionResponse{Text: raw.Text, Language: raw.Language, Duration: raw.Duration}, nil
}

// ---- Text-to-speech ---------------------------------------------------------

type oaiSpeechRequest struct {
	Model          string   `json:"model"`
	Input          string   `json:"input"`
	Voice          string   `json:"voice"`
	ResponseFormat string   `json:"response_format,omitempty"`
	Speed          *float64 `json:"speed,omitempty"`
}

// Synthesize converts text to speech via the OpenAI speech endpoint, returning
// raw audio bytes.
func (c *OpenAICompatible) Synthesize(ctx context.Context, req *core.SpeechRequest, creds core.Credentials) (*core.SpeechResponse, error) {
	voice := req.Voice
	if voice == "" {
		voice = "alloy"
	}
	body, err := json.Marshal(oaiSpeechRequest{
		Model: req.Model, Input: req.Input, Voice: voice,
		ResponseFormat: req.ResponseFormat, Speed: req.Speed,
	})
	if err != nil {
		return nil, &core.ProviderError{Kind: core.ErrInternal, Provider: c.id, Model: req.Model, Message: err.Error(), Cause: err}
	}

	url := joinURL(c.baseURL(creds), "audio/speech")
	raw, err := doRaw(ctx, c.id, req.Model, url, body, c.headers(creds))
	if err != nil {
		return nil, err
	}
	ct := raw.ContentType
	if ct == "" {
		ct = "audio/mpeg"
	}
	return &core.SpeechResponse{Audio: raw.Body, ContentType: ct}, nil
}

// ---- Video generation -------------------------------------------------------

// oaiVideoResponse captures the common async-job fields returned by
// video-generation endpoints without constraining provider-specific extras
// (those survive in VideoResponse.Raw). Different providers name the id field
// "request_id" or "id" and the output either "url" or "video_url"/"data[].url".
type oaiVideoResponse struct {
	RequestID string `json:"request_id"`
	ID        string `json:"id"`
	Status    string `json:"status"`
	State     string `json:"state"`
	URL       string `json:"url"`
	VideoURL  string `json:"video_url"`
	Output    struct {
		URL  string   `json:"url"`
		URLs []string `json:"urls"`
	} `json:"output"`
	Data []struct {
		URL string `json:"url"`
	} `json:"data"`
}

// decodeVideo maps the loosely-typed upstream body onto VideoResponse while
// preserving the raw bytes verbatim so no provider field is lost.
func decodeVideo(model string, body []byte) *core.VideoResponse {
	out := &core.VideoResponse{Model: model, Raw: body}
	var raw oaiVideoResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return out
	}
	out.RequestID = raw.RequestID
	if out.RequestID == "" {
		out.RequestID = raw.ID
	}
	out.Status = raw.Status
	if out.Status == "" {
		out.Status = raw.State
	}
	switch {
	case raw.URL != "":
		out.URL = raw.URL
	case raw.VideoURL != "":
		out.URL = raw.VideoURL
	case raw.Output.URL != "":
		out.URL = raw.Output.URL
	}
	out.URLs = raw.Output.URLs
	for _, d := range raw.Data {
		if d.URL != "" {
			out.URLs = append(out.URLs, d.URL)
		}
	}
	if out.URL == "" && len(out.URLs) > 0 {
		out.URL = out.URLs[0]
	}
	return out
}

// GenerateVideo submits an asynchronous video-generation job. Provider-specific
// JSON fields are preserved while the model is rewritten for the selected route.
// The pipeline never retries a submission after the request has been sent.
func (c *OpenAICompatible) GenerateVideo(ctx context.Context, req *core.VideoRequest, creds core.Credentials) (*core.VideoResponse, error) {
	body, err := videoRequestBody(req)
	if err != nil {
		return nil, &core.ProviderError{Kind: core.ErrInternal, Provider: c.id, Model: req.Model, Message: err.Error(), Cause: err}
	}
	url := joinURL(c.baseURL(creds), "videos/generations")
	respBody, err := doJSON(ctx, c.id, req.Model, url, body, c.headers(creds))
	if err != nil {
		return nil, err
	}
	return decodeVideo(req.Model, respBody), nil
}

func videoRequestBody(req *core.VideoRequest) ([]byte, error) {
	if len(req.Body) == 0 {
		return json.Marshal(map[string]string{"model": req.Model, "prompt": req.Prompt})
	}
	var payload map[string]any
	if err := json.Unmarshal(req.Body, &payload); err != nil {
		return nil, fmt.Errorf("video request must be a JSON object: %w", err)
	}
	payload["model"] = req.Model
	return json.Marshal(payload)
}

// PollVideo checks the status of an in-flight video job via GET /videos/{id}.
func (c *OpenAICompatible) PollVideo(ctx context.Context, req *core.VideoStatusRequest, creds core.Credentials) (*core.VideoResponse, error) {
	url := joinURL(c.baseURL(creds), "videos/"+req.RequestID)
	respBody, err := doJSONMethod(ctx, "GET", c.id, req.Model, url, nil, c.headers(creds))
	if err != nil {
		return nil, err
	}
	out := decodeVideo(req.Model, respBody)
	if out.RequestID == "" {
		out.RequestID = req.RequestID
	}
	return out, nil
}

// ---- Image understanding (image-to-text) ------------------------------------

// UnderstandImage answers a prompt about one or more input images by driving
// the standard chat-completions endpoint with multimodal content parts. Images
// are passed as URLs or base64 data URIs in image_url parts.
func (c *OpenAICompatible) UnderstandImage(ctx context.Context, req *core.ImageUnderstandingRequest, creds core.Credentials) (*core.ImageUnderstandingResponse, error) {
	type textPart struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type imageURL struct {
		URL string `json:"url"`
	}
	type imagePart struct {
		Type     string   `json:"type"`
		ImageURL imageURL `json:"image_url"`
	}
	content := make([]any, 0, len(req.Images)+1)
	content = append(content, textPart{Type: "text", Text: req.Prompt})
	for _, img := range req.Images {
		if strings.TrimSpace(img) == "" {
			continue
		}
		content = append(content, imagePart{Type: "image_url", ImageURL: imageURL{URL: img}})
	}
	payload := map[string]any{
		"model": req.Model,
		"messages": []map[string]any{
			{"role": "user", "content": content},
		},
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &core.ProviderError{Kind: core.ErrInternal, Provider: c.id, Model: req.Model, Message: err.Error(), Cause: err}
	}

	url := joinURL(c.baseURL(creds), "chat/completions")
	respBody, err := doJSON(ctx, c.id, req.Model, url, body, c.headers(creds))
	if err != nil {
		return nil, err
	}

	var raw struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, &core.ProviderError{Kind: core.ErrUpstream, Provider: c.id, Model: req.Model, Message: "parse understanding: " + err.Error(), Cause: err}
	}
	out := &core.ImageUnderstandingResponse{Model: req.Model}
	if len(raw.Choices) > 0 {
		out.Text = raw.Choices[0].Message.Content
	}
	out.Usage = core.Usage{
		PromptTokens:     raw.Usage.PromptTokens,
		CompletionTokens: raw.Usage.CompletionTokens,
		TotalTokens:      raw.Usage.TotalTokens,
	}
	return out, nil
}
