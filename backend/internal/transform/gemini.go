package transform

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"

	json "github.com/mydisha/keirouter/backend/internal/fastjson"

	"github.com/mydisha/keirouter/backend/internal/core"
)

// GeminiCodec handles Google's Gemini generateContent wire format. Gemini
// groups turns under "contents" with role "user"/"model", carries system text
// in a separate "systemInstruction", and nests tool calls as functionCall /
// functionResponse parts.
type GeminiCodec struct{}

func (GeminiCodec) Dialect() core.Dialect { return core.DialectGemini }

// ---- wire types -------------------------------------------------------------

type gemRequest struct {
	Contents          []gemContent    `json:"contents"`
	SystemInstruction *gemContent     `json:"systemInstruction,omitempty"`
	Tools             []gemTool       `json:"tools,omitempty"`
	ToolConfig        json.RawMessage `json:"toolConfig,omitempty"`
	GenerationConfig  *gemGenConfig   `json:"generationConfig,omitempty"`
}

type gemGenConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type gemContent struct {
	Role  string    `json:"role,omitempty"`
	Parts []gemPart `json:"parts"`
}

type gemPart struct {
	Text             string             `json:"text,omitempty"`
	FunctionCall     *gemFunctionCall   `json:"functionCall,omitempty"`
	FunctionResponse *gemFunctionResult `json:"functionResponse,omitempty"`
	InlineData       *gemInlineData     `json:"inlineData,omitempty"`
	FileData         *gemFileData       `json:"fileData,omitempty"`
	ThoughtSignature string             `json:"thoughtSignature,omitempty"`
	Thought          bool               `json:"thought,omitempty"`
}

type gemInlineData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type gemFileData struct {
	MIMEType string `json:"mimeType,omitempty"`
	FileURI  string `json:"fileUri"`
}

type gemFunctionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type gemFunctionResult struct {
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type gemTool struct {
	FunctionDeclarations []gemFuncDecl `json:"functionDeclarations"`
}

type gemFuncDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ---- request parsing --------------------------------------------------------

func (GeminiCodec) ParseRequest(body []byte) (*core.ChatRequest, error) {
	var raw gemRequest
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("gemini: parse request: %w", err)
	}

	req := &core.ChatRequest{}
	if raw.SystemInstruction != nil {
		for _, p := range raw.SystemInstruction.Parts {
			req.System += p.Text
		}
	}
	if raw.GenerationConfig != nil {
		req.Temperature = raw.GenerationConfig.Temperature
		req.TopP = raw.GenerationConfig.TopP
		req.MaxTokens = raw.GenerationConfig.MaxOutputTokens
		req.Stop = raw.GenerationConfig.StopSequences
	}
	for _, t := range raw.Tools {
		for _, fd := range t.FunctionDeclarations {
			req.Tools = append(req.Tools, core.Tool{Name: fd.Name, Description: fd.Description, Parameters: fd.Parameters})
		}
	}
	for _, c := range raw.Contents {
		req.Messages = append(req.Messages, parseGemContent(c))
	}
	return req, nil
}

func parseGemContent(c gemContent) core.Message {
	msg := core.Message{Role: mapGemRole(c.Role)}
	for _, p := range c.Parts {
		switch {
		case p.FunctionCall != nil:
			// Preserve Gemini's native call id and thought signature when present;
			// otherwise derive a stable id from the function name so downstream
			// dialects can pair the matching result.
			msg.Content = append(msg.Content, core.ContentPart{
				Type:     core.PartToolCall,
				ToolCall: &core.ToolCall{ID: geminiEncodeCallID(p.FunctionCall, p.ThoughtSignature), Name: p.FunctionCall.Name, Arguments: p.FunctionCall.Args},
			})
		case p.FunctionResponse != nil:
			// Pair the result back to the native or name-derived call id.
			msg.Content = append(msg.Content, core.ContentPart{
				Type: core.PartToolResult,
				ToolResult: &core.ToolResult{
					CallID:  geminiEncodeResultID(p.FunctionResponse),
					Content: string(p.FunctionResponse.Response),
				},
			})
		case p.InlineData != nil:
			msg.Content = append(msg.Content, core.ContentPart{
				Type:  core.PartImage,
				Media: &core.MediaPayload{MIMEType: p.InlineData.MIMEType, Data: p.InlineData.Data},
			})
		case p.FileData != nil:
			msg.Content = append(msg.Content, core.ContentPart{
				Type:  core.PartImage,
				Media: &core.MediaPayload{MIMEType: p.FileData.MIMEType, URL: p.FileData.FileURI},
			})
		case p.Text != "":
			if p.Thought {
				msg.Content = append(msg.Content, core.ContentPart{Type: core.PartThinking, Text: p.Text})
			} else {
				msg.Content = append(msg.Content, core.ContentPart{Type: core.PartText, Text: p.Text})
			}
		}
	}
	return msg
}

func mapGemRole(role string) core.Role {
	if role == "model" {
		return core.RoleAssistant
	}
	return core.RoleUser
}

// ---- request rendering ------------------------------------------------------

func (GeminiCodec) RenderRequest(req *core.ChatRequest) ([]byte, error) {
	out := gemRequest{}
	if req.System != "" {
		out.SystemInstruction = &gemContent{Parts: []gemPart{{Text: req.System}}}
	}
	if req.Temperature != nil || req.TopP != nil || req.MaxTokens != nil || len(req.Stop) > 0 {
		out.GenerationConfig = &gemGenConfig{
			Temperature:     req.Temperature,
			TopP:            req.TopP,
			MaxOutputTokens: req.MaxTokens,
			StopSequences:   req.Stop,
		}
	}
	// Resolve a sanitized, collision-free name per declared tool. The same
	// mapping is reused when rendering functionCall parts so an assistant tool
	// call always references the exact name its declaration was sent under, and
	// when rendering functionResponse parts so the result name matches the call
	// (Gemini rejects a functionResponse whose name has no matching call).
	callIDToName := buildGeminiCallNameMap(req.Messages)
	if len(req.Tools) > 0 {
		var decls []gemFuncDecl
		for _, t := range req.Tools {
			decls = append(decls, gemFuncDecl{
				Name:        sanitizeGeminiName(t.Name),
				Description: t.Description,
				Parameters:  cleanGeminiToolSchema(t.Parameters),
			})
		}
		out.Tools = []gemTool{{FunctionDeclarations: decls}}

		// Render tool_choice as a functionCallingConfig only when tools are
		// declared; an allowed-name mode with no tools is rejected.
		if tc := openAIToolChoiceToGemini(req.ToolChoice, sanitizeGeminiName); tc != nil {
			if raw, err := json.Marshal(tc); err == nil {
				out.ToolConfig = raw
			}
		}
	}
	for _, m := range req.Messages {
		out.Contents = append(out.Contents, renderGemContent(m, callIDToName))
	}
	return json.Marshal(out)
}

// buildGeminiCallNameMap maps each tool-call id to its sanitized function name,
// gathered from every assistant tool call in the conversation. functionResponse
// parts use it to recover the name Gemini requires from the tool result's id.
func buildGeminiCallNameMap(messages []core.Message) map[string]string {
	m := map[string]string{}
	for _, msg := range messages {
		for _, p := range msg.Content {
			if p.Type == core.PartToolCall && p.ToolCall != nil && p.ToolCall.ID != "" {
				m[p.ToolCall.ID] = sanitizeGeminiName(p.ToolCall.Name)
			}
		}
	}
	return m
}

// geminiEncodeCallID derives a deterministic tool-call id, preserving id and thoughtSignature if present.
func geminiEncodeCallID(call *gemFunctionCall, thoughtSig string) string {
	id := call.ID
	if id == "" {
		id = call.Name
	}
	if id == "" {
		id = "unknown"
	}
	encoded := id
	encoded = strings.ReplaceAll(encoded, ":", "_")
	if !strings.HasPrefix(encoded, "call_") {
		encoded = "call_" + encoded
	}
	if thoughtSig != "" && !strings.Contains(encoded, "__sig__") {
		encoded += "__sig__" + base64.RawURLEncoding.EncodeToString([]byte(thoughtSig))
	}
	return encoded
}

func geminiEncodeResultID(res *gemFunctionResult) string {
	id := res.ID
	if id == "" {
		id = res.Name
	}
	if id == "" {
		id = "unknown"
	}
	if strings.HasPrefix(id, "call_") {
		return id
	}
	return "call_" + id
}

func renderGemContent(m core.Message, callIDToName map[string]string) gemContent {
	role := "user"
	if m.Role == core.RoleAssistant {
		role = "model"
	}
	c := gemContent{Role: role}
	for _, p := range m.Content {
		switch p.Type {
		case core.PartThinking:
			c.Parts = append(c.Parts, gemPart{Text: p.Text, Thought: true})
		case core.PartText:
			c.Parts = append(c.Parts, gemPart{Text: p.Text})
		case core.PartToolCall:
			name := sanitizeGeminiName(p.ToolCall.Name)
			idStr := p.ToolCall.ID
			idStr = strings.TrimPrefix(idStr, "call_")

			var thoughtSig string
			if idx := strings.Index(idStr, "__sig__"); idx >= 0 {
				sigB, _ := base64.RawURLEncoding.DecodeString(idStr[idx+7:])
				thoughtSig = string(sigB)
				idStr = idStr[:idx]
			}

			idToSend := idStr
			if idToSend == name || idToSend == strings.ReplaceAll(name, ":", "_") {
				idToSend = ""
			}

			c.Parts = append(c.Parts, gemPart{
				ThoughtSignature: thoughtSig,
				FunctionCall: &gemFunctionCall{
					ID:   idToSend,
					Name: name,
					Args: normalizeGeminiArgs(p.ToolCall.Arguments),
				},
			})
		case core.PartToolResult:
			// Recover the function name from the call id; Gemini requires the
			// response name to match the originating functionCall name.
			name := callIDToName[p.ToolResult.CallID]
			if name == "" {
				name = "tool"
			}

			idStr := p.ToolResult.CallID
			idStr = strings.TrimPrefix(idStr, "call_")
			if idx := strings.Index(idStr, "__sig__"); idx >= 0 {
				idStr = idStr[:idx]
			}
			idToSend := idStr
			if idToSend == name || idToSend == strings.ReplaceAll(name, ":", "_") {
				idToSend = ""
			}

			c.Parts = append(c.Parts, gemPart{FunctionResponse: &gemFunctionResult{
				ID:       idToSend,
				Name:     name,
				Response: json.RawMessage(quoteIfNotJSON(p.ToolResult.Content)),
			}})
		case core.PartImage:
			if p.Media != nil {
				if p.Media.Data != "" {
					c.Parts = append(c.Parts, gemPart{InlineData: &gemInlineData{MIMEType: p.Media.MIMEType, Data: p.Media.Data}})
				} else if p.Media.URL != "" {
					c.Parts = append(c.Parts, gemPart{FileData: &gemFileData{MIMEType: p.Media.MIMEType, FileURI: p.Media.URL}})
				}
			}
		}
	}
	if len(c.Parts) == 0 {
		c.Parts = append(c.Parts, gemPart{Text: ""})
	}
	return c
}

// normalizeGeminiArgs guarantees functionCall.args is a JSON object; Gemini
// rejects a null or non-object args value.
func normalizeGeminiArgs(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || !json.Valid(trimmed) || trimmed[0] != '{' {
		return json.RawMessage("{}")
	}
	return trimmed
}

// quoteIfNotJSON wraps a tool-result string as a JSON value if it isn't already
// a valid JSON object, since Gemini's functionResponse.response expects a JSON object.
func quoteIfNotJSON(s string) string {
	var probe map[string]any
	if err := json.Unmarshal([]byte(s), &probe); err == nil {
		return s
	}

	var generic any
	if err := json.Unmarshal([]byte(s), &generic); err == nil {
		b, _ := json.Marshal(map[string]any{"result": generic})
		return string(b)
	}

	b, _ := json.Marshal(map[string]string{"result": s})
	return string(b)
}

// ---- response parsing -------------------------------------------------------

type gemResponse struct {
	Candidates []struct {
		Content      gemContent `json:"content"`
		FinishReason string     `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount        int `json:"promptTokenCount"`
		CandidatesTokenCount    int `json:"candidatesTokenCount"`
		ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
		TotalTokenCount         int `json:"totalTokenCount"`
		CachedContentTokenCount int `json:"cachedContentTokenCount"`
	} `json:"usageMetadata"`
}

func (GeminiCodec) ParseResponse(body []byte, model string) (*core.ChatResponse, error) {
	var raw gemResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("gemini: parse response: %w", err)
	}
	if len(raw.Candidates) == 0 {
		return nil, fmt.Errorf("gemini: response has no candidates")
	}
	cand := raw.Candidates[0]
	msg := parseGemContent(cand.Content)
	msg.Role = core.RoleAssistant

	completionTokens := raw.UsageMetadata.CandidatesTokenCount + raw.UsageMetadata.ThoughtsTokenCount
	if completionTokens == 0 && raw.UsageMetadata.TotalTokenCount > raw.UsageMetadata.PromptTokenCount {
		completionTokens = raw.UsageMetadata.TotalTokenCount - raw.UsageMetadata.PromptTokenCount
	}
	return &core.ChatResponse{
		Model:        model,
		Message:      msg,
		FinishReason: mapGemCandidateFinish(cand.Content, cand.FinishReason),
		Usage: core.Usage{
			PromptTokens:     raw.UsageMetadata.PromptTokenCount,
			CompletionTokens: completionTokens,
			TotalTokens:      raw.UsageMetadata.TotalTokenCount,
			CachedTokens:     raw.UsageMetadata.CachedContentTokenCount,
			ReasoningTokens:  raw.UsageMetadata.ThoughtsTokenCount,
			Source:           core.UsageSourceProvider,
		},
	}, nil
}

func (GeminiCodec) RenderResponse(resp *core.ChatResponse) ([]byte, error) {
	content := renderGemContent(resp.Message, nil)
	content.Role = "model"
	out := map[string]any{
		"candidates": []map[string]any{{
			"content":      content,
			"finishReason": renderGemFinish(resp.FinishReason),
			"index":        0,
		}},
		"usageMetadata": map[string]int{
			"promptTokenCount":     resp.Usage.PromptTokens,
			"candidatesTokenCount": resp.Usage.CompletionTokens,
			"totalTokenCount":      resp.Usage.TotalTokens,
		},
	}
	return json.Marshal(out)
}

// Gemini uses STOP for both a completed text response and a completed
// function-call turn. Canonical/OpenAI-compatible clients need the latter to
// be reported as tool_calls so their agent loop records and resumes the turn
// with the correct protocol state.
func mapGemCandidateFinish(content gemContent, reason string) core.FinishReason {
	finish := mapGemFinish(reason)
	if finish != core.FinishStop {
		return finish
	}
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			return core.FinishToolCalls
		}
	}
	return finish
}

func mapGemFinish(r string) core.FinishReason {
	switch r {
	case "STOP":
		return core.FinishStop
	case "MAX_TOKENS":
		return core.FinishLength
	case "SAFETY", "RECITATION":
		return core.FinishFilter
	default:
		return core.FinishStop
	}
}

func renderGemFinish(r core.FinishReason) string {
	switch r {
	case core.FinishLength:
		return "MAX_TOKENS"
	case core.FinishFilter:
		return "SAFETY"
	default:
		return "STOP"
	}
}
