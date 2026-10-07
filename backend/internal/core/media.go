package core

// This file defines the canonical request/response types for the non-chat
// service kinds: image generation, speech-to-text, text-to-speech, web search,
// and web fetch. Each maps to an OpenAI-style endpoint at the gateway edge and
// is served by a connector implementing the matching capability interface in
// connector.go.

// ---- Image generation -------------------------------------------------------

// ImageRequest is a canonical text-to-image request.
type ImageRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	// N is the number of images to generate (default 1).
	N int `json:"n,omitempty"`
	// Size is a WxH spec (e.g. "1024x1024").
	Size string `json:"size,omitempty"`
	// Quality, Style are provider-specific knobs (standard|hd, vivid|natural).
	Quality string `json:"quality,omitempty"`
	Style   string `json:"style,omitempty"`
	// ResponseFormat is "url" or "b64_json".
	ResponseFormat string `json:"response_format,omitempty"`
}

// ImageData is a single generated image, carrying either a URL or base64 data.
type ImageData struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// ImageResponse is the canonical image-generation result.
type ImageResponse struct {
	Model   string      `json:"model"`
	Created int64       `json:"created"`
	Data    []ImageData `json:"data"`
}

// ---- Speech-to-text ---------------------------------------------------------

// TranscriptionRequest is a canonical speech-to-text request. Audio is carried
// as raw bytes plus the original filename/content type for multipart upload.
type TranscriptionRequest struct {
	Model string `json:"model"`
	// Audio is the raw audio payload.
	Audio []byte `json:"-"`
	// Filename is the original file name (used for the multipart part).
	Filename string `json:"-"`
	// ContentType is the audio MIME type (e.g. "audio/mpeg").
	ContentType string `json:"-"`
	// Language is an optional ISO-639-1 hint.
	Language string `json:"language,omitempty"`
	// Prompt biases the transcription with context.
	Prompt string `json:"prompt,omitempty"`
	// ResponseFormat is "json", "text", "verbose_json", etc.
	ResponseFormat string `json:"response_format,omitempty"`
	// Temperature controls sampling for the transcription model.
	Temperature *float64 `json:"temperature,omitempty"`
}

// TranscriptionResponse is the canonical transcription result.
type TranscriptionResponse struct {
	Text     string  `json:"text"`
	Language string  `json:"language,omitempty"`
	Duration float64 `json:"duration,omitempty"`
}

// ---- Text-to-speech ---------------------------------------------------------

// SpeechRequest is a canonical text-to-speech request.
type SpeechRequest struct {
	Model string `json:"model"`
	// Input is the text to synthesize.
	Input string `json:"input"`
	// Voice selects the speaker (provider-specific id).
	Voice string `json:"voice,omitempty"`
	// ResponseFormat is the audio container (mp3, wav, opus, ...).
	ResponseFormat string `json:"response_format,omitempty"`
	// Speed adjusts playback rate where supported.
	Speed *float64 `json:"speed,omitempty"`
}

// SpeechResponse carries synthesized audio bytes and their content type.
type SpeechResponse struct {
	Audio       []byte
	ContentType string
}

// ---- Web search -------------------------------------------------------------

// SearchRequest is a canonical web-search request.
type SearchRequest struct {
	// Model identifies the search provider/model (provider/model form upstream).
	Model string `json:"model,omitempty"`
	// Query is the search query string.
	Query string `json:"query"`
	// MaxResults caps the number of results returned.
	MaxResults int `json:"max_results,omitempty"`
	// SearchType is "web", "news", etc. (provider-specific).
	SearchType string `json:"search_type,omitempty"`
}

// SearchResult is a single search hit.
type SearchResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Snippet string  `json:"snippet,omitempty"`
	Score   float64 `json:"score,omitempty"`
}

// SearchResponse is the canonical web-search result set.
type SearchResponse struct {
	Query   string         `json:"query"`
	Results []SearchResult `json:"results"`
}

// ---- Video generation -------------------------------------------------------

// VideoRequest is a canonical text-to-video request. Video generation is an
// asynchronous job upstream: submission returns a request id and a status, and
// the caller polls for completion. Body carries the raw client payload so
// provider-specific knobs (duration, aspect_ratio, resolution, ...) pass
// through untouched.
type VideoRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt,omitempty"`
	// Body carries provider-specific JSON fields that are not modeled above.
	// The connector rewrites its model field for each resolved routing attempt.
	Body []byte `json:"-"`
	// ContentType is the original JSON media type.
	ContentType string `json:"-"`
}

// VideoStatusRequest polls an in-flight video job by its upstream request id.
type VideoStatusRequest struct {
	Model     string `json:"model"`
	RequestID string `json:"request_id"`
}

// VideoResponse is the canonical video-generation result. It mirrors the async
// job shape: a request id to poll, a status, and (when done) the output url(s).
type VideoResponse struct {
	Model     string   `json:"model,omitempty"`
	RequestID string   `json:"request_id,omitempty"`
	Status    string   `json:"status,omitempty"`
	URL       string   `json:"url,omitempty"`
	URLs      []string `json:"urls,omitempty"`
	// Raw is the untouched upstream JSON, passed back verbatim so clients get
	// every provider-specific field without lossy re-shaping.
	Raw []byte `json:"-"`
	// AccountID pins subsequent job polling to the credential that submitted it.
	AccountID string `json:"-"`
}

// ---- Image understanding (image-to-text) ------------------------------------

// ImageUnderstandingRequest asks a vision model to describe or answer questions
// about an input image. Images are carried as URLs or base64 data URIs.
type ImageUnderstandingRequest struct {
	Model string `json:"model"`
	// Prompt is the question/instruction about the image(s).
	Prompt string `json:"prompt"`
	// Images holds image URLs or base64 data URIs (data:image/...;base64,...).
	Images []string `json:"images"`
	// MaxTokens caps the generated description length.
	MaxTokens int `json:"max_tokens,omitempty"`
}

// ImageUnderstandingResponse carries the model's textual answer.
type ImageUnderstandingResponse struct {
	Model string `json:"model"`
	Text  string `json:"text"`
	Usage Usage  `json:"usage"`
}

// ---- Web fetch --------------------------------------------------------------

// FetchRequest is a canonical URL-content-extraction request.
type FetchRequest struct {
	Model string `json:"model,omitempty"`
	// URL is the page to fetch and extract.
	URL string `json:"url"`
	// Format is the desired output format ("markdown", "text", "html").
	Format string `json:"format,omitempty"`
	// MaxCharacters caps the extracted content length.
	MaxCharacters int `json:"max_characters,omitempty"`
}

// FetchResponse is the canonical extracted-content result.
type FetchResponse struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content"`
	Format  string `json:"format,omitempty"`
}
