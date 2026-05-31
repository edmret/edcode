package provider

import (
	"context"
	"io"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ContentType string

const (
	ContentText     ContentType = "text"
	ContentImage    ContentType = "image"
	ContentToolCall ContentType = "tool_call"
	ContentToolResult ContentType = "tool_result"
)

type Message struct {
	Role    Role        `json:"role"`
	Content []Content   `json:"content"`
	Name    string      `json:"name,omitempty"`
}

type Content struct {
	Type     ContentType `json:"type"`
	Text     string      `json:"text,omitempty"`
	ImageURL string      `json:"image_url,omitempty"`
	ToolCall *ToolCall   `json:"tool_call,omitempty"`
	ToolResult *ToolResult `json:"tool_result,omitempty"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolResult struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

type ToolDefinition struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema InputSchema `json:"input_schema"`
}

type InputSchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required,omitempty"`
}

type ChatRequest struct {
	Model       string          `json:"model"`
	Messages    []Message       `json:"messages"`
	System      string          `json:"system,omitempty"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	TopP        float64         `json:"top_p,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type ChatResponse struct {
	ID        string   `json:"id"`
	Model     string   `json:"model"`
	Message   Message  `json:"message"`
	Usage     Usage    `json:"usage"`
	FinishReason string `json:"finish_reason"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type ChatChunk struct {
	ID      string   `json:"id"`
	Model   string   `json:"model"`
	Content string   `json:"content,omitempty"`
	ToolCall *ToolCall `json:"tool_call,omitempty"`
	Usage   *Usage   `json:"usage,omitempty"`
	Done    bool     `json:"done"`
	Error   string   `json:"error,omitempty"`
}

type ModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContextLen  int    `json:"context_length"`
	OutputLen   int    `json:"output_length"`
	Provider    string `json:"provider"`
}

type StreamReader interface {
	Recv() (*ChatChunk, error)
	io.Closer
}

type Provider interface {
	Name() string
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	ChatStream(ctx context.Context, req *ChatRequest) (StreamReader, error)
	Models() []ModelInfo
	Supports(modelID string) bool
}
