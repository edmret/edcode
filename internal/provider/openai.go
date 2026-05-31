package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type ModelConfig struct {
	ID         string
	Name       string
	ContextLen int
	OutputLen  int
}

type OpenAIProvider struct {
	name     string
	apiKey   string
	baseURL  string
	defaultModel string
	models   []ModelInfo
	client   *http.Client
}

func NewOpenAIProvider(name, apiKey, baseURL, defaultModel string, models []ModelConfig) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	info := make([]ModelInfo, len(models))
	for i, m := range models {
		info[i] = ModelInfo{
			ID: m.ID, Name: m.Name,
			ContextLen: m.ContextLen, OutputLen: m.OutputLen,
			Provider: name,
		}
	}
	if len(info) == 0 {
		info = append(info, ModelInfo{
			ID: defaultModel, Name: defaultModel,
			ContextLen: 128000, OutputLen: 16384,
			Provider: name,
		})
	}
	return &OpenAIProvider{
		name: name, apiKey: apiKey, baseURL: strings.TrimRight(baseURL, "/"),
		defaultModel: defaultModel, models: info,
		client: &http.Client{},
	}
}

func (p *OpenAIProvider) Name() string { return p.name }

func (p *OpenAIProvider) Models() []ModelInfo { return p.models }

type openAIChatReq struct {
	Model       string            `json:"model"`
	Messages    []openAIMessage   `json:"messages"`
	Tools       []openAITool      `json:"tools,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
	TopP        float64           `json:"top_p,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Stream      bool              `json:"stream,omitempty"`
}

type openAIMessage struct {
	Role       string        `json:"role"`
	Content    any           `json:"content"`
	Name       string        `json:"name,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAITool struct {
	Type     string          `json:"type"`
	Function openAIFunction  `json:"function"`
}

type openAIFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type openAIToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function openAIToolFunc   `json:"function"`
}

type openAIToolFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIChatResp struct {
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Choices []openAIChoice  `json:"choices"`
	Usage   *openAIUsage    `json:"usage,omitempty"`
	Error   *openAIError    `json:"error,omitempty"`
}

type openAIChoice struct {
	Index        int             `json:"index"`
	Message      openAIMessage   `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

type openAIStreamChunk struct {
	ID                string              `json:"id"`
	Model             string              `json:"model"`
	Choices           []openAIStreamChoice `json:"choices"`
	Usage             *openAIUsage        `json:"usage,omitempty"`
}

type openAIStreamChoice struct {
	Delta        openAIDelta      `json:"delta"`
	FinishReason string           `json:"finish_reason"`
	Index        int              `json:"index"`
}

type openAIDelta struct {
	Role      string           `json:"role,omitempty"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
}

func (p *OpenAIProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}
	body := p.buildRequest(req, model, false)
	resp, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var oaResp openAIChatResp
	if err := json.NewDecoder(resp.Body).Decode(&oaResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if oaResp.Error != nil {
		return nil, fmt.Errorf("api error: %s - %s", oaResp.Error.Type, oaResp.Error.Message)
	}
	return p.toChatResponse(&oaResp, model), nil
}

func (p *OpenAIProvider) ChatStream(ctx context.Context, req *ChatRequest) (StreamReader, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}
	body := p.buildRequest(req, model, true)
	resp, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	return &openAIStreamReader{
		scanner: bufio.NewScanner(resp.Body),
		closer:  resp.Body,
		model:   model,
	}, nil
}

func (p *OpenAIProvider) buildRequest(req *ChatRequest, model string, stream bool) *openAIChatReq {
	oaiReq := &openAIChatReq{
		Model:       model,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		MaxTokens:   req.MaxTokens,
		Stream:      stream,
	}
	if req.System != "" {
		oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
			Role: "system", Content: req.System,
		})
	}
	for _, msg := range req.Messages {
		oaiReq.Messages = append(oaiReq.Messages, p.toOpenAIMessage(&msg))
	}
	for _, t := range req.Tools {
		oaiReq.Tools = append(oaiReq.Tools, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return oaiReq
}

func (p *OpenAIProvider) toOpenAIMessage(msg *Message) openAIMessage {
	if len(msg.Content) == 1 && msg.Content[0].Type == ContentText {
		oai := openAIMessage{Role: string(msg.Role), Content: msg.Content[0].Text}
		if msg.Role == RoleTool {
			oai.ToolCallID = msg.Content[0].ToolResult.ID
		}
		return oai
	}
	var parts []map[string]any
	for _, c := range msg.Content {
		switch c.Type {
		case ContentText:
			parts = append(parts, map[string]any{"type": "text", "text": c.Text})
		case ContentImage:
			parts = append(parts, map[string]any{
				"type": "image_url",
				"image_url": map[string]string{"url": c.ImageURL},
			})
		case ContentToolCall:
			parts = append(parts, map[string]any{
				"type": "function",
				"id":   c.ToolCall.ID,
				"function": map[string]string{
					"name":      c.ToolCall.Name,
					"arguments": c.ToolCall.Arguments,
				},
			})
		case ContentToolResult:
			oai := openAIMessage{
				Role: "tool", ToolCallID: c.ToolResult.ID,
				Content: c.ToolResult.Result,
			}
			if c.ToolResult.Error != "" {
				oai.Content = fmt.Sprintf("Error: %s", c.ToolResult.Error)
			}
			return oai
		}
	}
	data, _ := json.Marshal(parts)
	return openAIMessage{Role: string(msg.Role), Content: json.RawMessage(data)}
}

func (p *OpenAIProvider) doRequest(ctx context.Context, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}
	return resp, nil
}

func (p *OpenAIProvider) toChatResponse(oaResp *openAIChatResp, model string) *ChatResponse {
	resp := &ChatResponse{
		ID:    oaResp.ID,
		Model: model,
		Message: Message{
			Role: RoleAssistant,
		},
	}
	if len(oaResp.Choices) > 0 {
		c := oaResp.Choices[0]
		resp.FinishReason = c.FinishReason
		msg := p.fromOpenAIMessage(&c.Message)
		resp.Message = *msg
	}
	if oaResp.Usage != nil {
		resp.Usage = Usage{
			InputTokens:  oaResp.Usage.PromptTokens,
			OutputTokens: oaResp.Usage.CompletionTokens,
		}
	}
	return resp
}

func (p *OpenAIProvider) fromOpenAIMessage(msg *openAIMessage) *Message {
	m := &Message{Role: Role(msg.Role)}
	switch c := msg.Content.(type) {
	case string:
		m.Content = append(m.Content, Content{Type: ContentText, Text: c})
	case []any:
		for _, part := range c {
			if p, ok := part.(map[string]any); ok {
				if t, _ := p["type"].(string); t == "text" {
					if txt, _ := p["text"].(string); txt != "" {
						m.Content = append(m.Content, Content{Type: ContentText, Text: txt})
					}
				}
			}
		}
	}
	for _, tc := range msg.ToolCalls {
		m.Content = append(m.Content, Content{
			Type: ContentToolCall,
			ToolCall: &ToolCall{
				ID: tc.ID, Name: tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	return m
}

type openAIStreamReader struct {
	scanner *bufio.Scanner
	closer  io.Closer
	model   string
}

func (s *openAIStreamReader) Recv() (*ChatChunk, error) {
	for s.scanner.Scan() {
		line := strings.TrimSpace(s.scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return &ChatChunk{Done: true}, nil
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		cc := &ChatChunk{ID: chunk.ID, Model: chunk.Model}
		if len(chunk.Choices) > 0 {
			d := chunk.Choices[0].Delta
			cc.Content = d.Content
			if d.ToolCalls != nil {
				for _, tc := range d.ToolCalls {
					cc.ToolCall = &ToolCall{
						ID: tc.ID, Name: tc.Function.Name,
						Arguments: tc.Function.Arguments,
					}
				}
			}
			if chunk.Choices[0].FinishReason != "" {
				cc.Done = true
			}
		}
		if chunk.Usage != nil {
			cc.Usage = &Usage{
				InputTokens: chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
			}
		}
		return cc, nil
	}
	if err := s.scanner.Err(); err != nil {
		return nil, err
	}
	return &ChatChunk{Done: true}, nil
}

func (s *openAIStreamReader) Close() error {
	return s.closer.Close()
}

func (p *OpenAIProvider) Supports(modelRef string) bool {
	for _, m := range p.models {
		if m.ID == modelRef {
			return true
		}
	}
	return false
}
