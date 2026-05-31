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

type AnthropicProvider struct {
	name     string
	apiKey   string
	baseURL  string
	defaultModel string
	models   []ModelInfo
	client   *http.Client
}

func NewAnthropicProvider(name, apiKey, baseURL, defaultModel string, models []ModelConfig) *AnthropicProvider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
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
			ContextLen: 200000, OutputLen: 8192,
			Provider: name,
		})
	}
	return &AnthropicProvider{
		name: name, apiKey: apiKey, baseURL: strings.TrimRight(baseURL, "/"),
		defaultModel: defaultModel, models: info,
		client: &http.Client{},
	}
}

func (p *AnthropicProvider) Name() string { return p.name }

func (p *AnthropicProvider) Models() []ModelInfo { return p.models }

func (p *AnthropicProvider) Supports(modelID string) bool {
	for _, m := range p.models {
		if m.ID == modelID {
			return true
		}
	}
	return false
}

type anthropicReq struct {
	Model       string              `json:"model"`
	Messages    []anthropicMsg      `json:"messages"`
	System      string              `json:"system,omitempty"`
	MaxTokens   int                 `json:"max_tokens"`
	Temperature float64             `json:"temperature,omitempty"`
	TopP        float64             `json:"top_p,omitempty"`
	Tools       []anthropicTool     `json:"tools,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
}

type anthropicMsg struct {
	Role    string            `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type   string          `json:"type"`
	Text   string          `json:"text,omitempty"`
	Source *anthropicSource `json:"source,omitempty"`
	ID     string          `json:"id,omitempty"`
	Name   string          `json:"name,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	ToolUseID string       `json:"tool_use_id,omitempty"`
	Content string         `json:"content,omitempty"`
	IsError bool           `json:"is_error,omitempty"`
}

type anthropicSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

type anthropicResp struct {
	ID           string              `json:"id"`
	Model        string              `json:"model"`
	Role         string              `json:"role"`
	Content      []anthropicContent  `json:"content"`
	StopReason   string              `json:"stop_reason"`
	Usage        *anthropicUsage     `json:"usage,omitempty"`
	Error        *anthropicRespError `json:"error,omitempty"`
	Type         string              `json:"type"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicRespError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

type anthropicStreamEvent struct {
	Type   string          `json:"type"`
	Delta  *anthropicDelta `json:"delta,omitempty"`
	Index  int             `json:"index,omitempty"`
	Name   string          `json:"name,omitempty"`
	ID     string          `json:"id,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	Usage  *anthropicUsage `json:"usage,omitempty"`
	Message *anthropicResp `json:"message,omitempty"`
	ContentBlock *anthropicContent `json:"content_block,omitempty"`
	Error  *anthropicRespError `json:"error,omitempty"`
}

type anthropicDelta struct {
	Text        string `json:"text,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
	StopString  string `json:"stop_string,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Type        string `json:"type,omitempty"`
}

func (p *AnthropicProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
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

	var aResp anthropicResp
	if err := json.NewDecoder(resp.Body).Decode(&aResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if aResp.Error != nil {
		return nil, fmt.Errorf("api error: %s - %s", aResp.Error.Type, aResp.Error.Message)
	}
	return p.toChatResponse(&aResp, model), nil
}

func (p *AnthropicProvider) ChatStream(ctx context.Context, req *ChatRequest) (StreamReader, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}
	body := p.buildRequest(req, model, true)
	resp, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	return &anthropicStreamReader{
		scanner: bufio.NewScanner(resp.Body),
		closer:  resp.Body,
		model:   model,
		buffer:  &anthropicStreamAccumulator{},
	}, nil
}

func (p *AnthropicProvider) buildRequest(req *ChatRequest, model string, stream bool) *anthropicReq {
	aReq := &anthropicReq{
		Model:       model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      stream,
	}
	if req.MaxTokens <= 0 {
		aReq.MaxTokens = 8192
	}
	if req.System != "" {
		aReq.System = req.System
	}

	systemMsgs := 0
	for _, msg := range req.Messages {
		if msg.Role == RoleSystem {
			systemMsgs++
		}
	}
	msgs := make([]Message, 0, len(req.Messages))
	for _, msg := range req.Messages {
		if msg.Role != RoleSystem {
			msgs = append(msgs, msg)
		} else if systemMsgs > 1 {
			continue
		}
	}
	if systemMsgs > 1 && req.System == "" {
		for _, msg := range req.Messages {
			if msg.Role == RoleSystem {
				for _, c := range msg.Content {
					if c.Type == ContentText {
						aReq.System += c.Text + "\n"
					}
				}
			}
		}
	}

	for _, msg := range msgs {
		aReq.Messages = append(aReq.Messages, p.toAnthropicMessage(&msg))
	}
	for _, t := range req.Tools {
		aReq.Tools = append(aReq.Tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	return aReq
}

func (p *AnthropicProvider) toAnthropicMessage(msg *Message) anthropicMsg {
	aMsg := anthropicMsg{Role: string(msg.Role)}
	for _, c := range msg.Content {
		switch c.Type {
		case ContentText:
			aMsg.Content = append(aMsg.Content, anthropicContent{
				Type: "text", Text: c.Text,
			})
		case ContentImage:
			aMsg.Content = append(aMsg.Content, anthropicContent{
				Type: "image",
				Source: &anthropicSource{
					Type: "base64", MediaType: "image/png", Data: c.ImageURL,
				},
			})
		case ContentToolCall:
			aMsg.Content = append(aMsg.Content, anthropicContent{
				Type: "tool_use", ID: c.ToolCall.ID,
				Name: c.ToolCall.Name, Input: json.RawMessage(c.ToolCall.Arguments),
			})
		case ContentToolResult:
			aMsg.Content = append(aMsg.Content, anthropicContent{
				Type: "tool_result", ToolUseID: c.ToolResult.ID,
				Content: c.ToolResult.Result,
				IsError: c.ToolResult.Error != "",
			})
		}
	}
	if len(aMsg.Content) == 0 {
		aMsg.Content = append(aMsg.Content, anthropicContent{Type: "text", Text: ""})
	}
	return aMsg
}

func (p *AnthropicProvider) doRequest(ctx context.Context, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
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

func (p *AnthropicProvider) toChatResponse(aResp *anthropicResp, model string) *ChatResponse {
	resp := &ChatResponse{
		ID:    aResp.ID,
		Model: model,
		Message: Message{Role: RoleAssistant},
	}
	switch aResp.StopReason {
	case "end_turn", "stop_sequence":
		resp.FinishReason = "stop"
	case "tool_use":
		resp.FinishReason = "tool_calls"
	case "max_tokens":
		resp.FinishReason = "length"
	}
	for _, c := range aResp.Content {
		switch c.Type {
		case "text":
			resp.Message.Content = append(resp.Message.Content, Content{
				Type: ContentText, Text: c.Text,
			})
		case "tool_use":
			resp.Message.Content = append(resp.Message.Content, Content{
				Type: ContentToolCall,
				ToolCall: &ToolCall{
					ID: c.ID, Name: c.Name, Arguments: string(c.Input),
				},
			})
		}
	}
	if aResp.Usage != nil {
		resp.Usage = Usage{
			InputTokens: aResp.Usage.InputTokens,
			OutputTokens: aResp.Usage.OutputTokens,
		}
	}
	return resp
}

type anthropicStreamAccumulator struct {
	contentIndex int
	textBuffer   string
}

type anthropicStreamReader struct {
	scanner *bufio.Scanner
	closer  io.Closer
	model   string
	buffer  *anthropicStreamAccumulator
}

func (s *anthropicStreamReader) Recv() (*ChatChunk, error) {
	for s.scanner.Scan() {
		line := strings.TrimSpace(s.scanner.Text())
		if line == "" || !strings.HasPrefix(line, "event: ") {
			continue
		}
		eventType := strings.TrimPrefix(line, "event: ")

		if !s.scanner.Scan() {
			return nil, s.scanner.Err()
		}
		data := strings.TrimSpace(s.scanner.Text())
		if !strings.HasPrefix(data, "data: ") {
			continue
		}
		data = strings.TrimPrefix(data, "data: ")

		switch eventType {
		case "message_start":
			var msg anthropicResp
			if err := json.Unmarshal([]byte(data), &msg); err != nil {
				continue
			}
			return &ChatChunk{ID: msg.ID, Model: msg.Model}, nil

		case "content_block_start":
			var block struct {
				Index int              `json:"index"`
				Block anthropicContent `json:"content_block"`
			}
			json.Unmarshal([]byte(data), &block)
			s.buffer.contentIndex = block.Index
			if block.Block.Type == "tool_use" {
				return &ChatChunk{
					ToolCall: &ToolCall{
						ID: block.Block.ID, Name: block.Block.Name,
						Arguments: "",
					},
				}, nil
			}
			s.buffer.textBuffer = block.Block.Text
			if block.Block.Text != "" {
				return &ChatChunk{Content: block.Block.Text}, nil
			}

		case "content_block_delta":
			var delta struct {
				Index int             `json:"index"`
				Delta anthropicDelta `json:"delta"`
			}
			json.Unmarshal([]byte(data), &delta)
			s.buffer.textBuffer += delta.Delta.Text
			if delta.Delta.Text != "" {
				return &ChatChunk{Content: delta.Delta.Text}, nil
			}
			if delta.Delta.PartialJSON != "" {
				return &ChatChunk{
					ToolCall: &ToolCall{Arguments: delta.Delta.PartialJSON},
				}, nil
			}

		case "message_delta":
			var msgDelta struct {
				Delta     anthropicDelta  `json:"delta"`
				Usage     *anthropicUsage `json:"usage,omitempty"`
			}
			json.Unmarshal([]byte(data), &msgDelta)
			chunk := &ChatChunk{Done: true}
			if msgDelta.Usage != nil {
				chunk.Usage = &Usage{
					InputTokens:  msgDelta.Usage.InputTokens,
					OutputTokens: msgDelta.Usage.OutputTokens,
				}
			}
			return chunk, nil

		case "message_stop":
			return &ChatChunk{Done: true}, nil

		case "error":
			var errResp struct {
				Error anthropicRespError `json:"error"`
			}
			json.Unmarshal([]byte(data), &errResp)
			return &ChatChunk{Error: errResp.Error.Message, Done: true}, nil
		}
	}
	if err := s.scanner.Err(); err != nil {
		return nil, err
	}
	return &ChatChunk{Done: true}, nil
}

func (s *anthropicStreamReader) Close() error {
	return s.closer.Close()
}
