package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"

	"github.com/edmundo/edcode/internal/config"
	"github.com/edmundo/edcode/internal/provider"
)

type jsonRPCReq struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type Client struct {
	name        string
	cfg         config.MCPConfig
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      *bufio.Scanner
	stderr      io.ReadCloser
	mu          sync.Mutex
	msgID       int
	tools       []ToolInfo
	initialized bool
}

func NewClient(name string, cfg config.MCPConfig) *Client {
	return &Client{name: name, cfg: cfg, msgID: 1}
}

func (c *Client) Start(ctx context.Context) error {
	if c.initialized {
		return nil
	}
	if c.cfg.Type == "remote" {
		return c.connectRemote(ctx)
	}
	return c.connectLocal(ctx)
}

func (c *Client) connectLocal(ctx context.Context) error {
	if c.cfg.Command == "" {
		return fmt.Errorf("mcp %q: no command specified", c.name)
	}
	c.cmd = exec.CommandContext(ctx, c.cfg.Command, c.cfg.Args...)
	for k, v := range c.cfg.Environment {
		c.cmd.Env = append(c.cmd.Environ(), fmt.Sprintf("%s=%s", k, v))
	}
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	c.stdin = stdin
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	c.stdout = bufio.NewScanner(stdout)
	stderr, err := c.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	c.stderr = stderr
	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("start mcp: %w", err)
	}
	if err := c.initialize(); err != nil {
		c.cmd.Process.Kill()
		return fmt.Errorf("initialize mcp %q: %w", c.name, err)
	}
	return nil
}

func (c *Client) connectRemote(ctx context.Context) error {
	body, _ := json.Marshal(jsonRPCReq{
		JSONRPC: "2.0", ID: c.nextID(), Method: "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "edcode", "version": "0.1.0"},
		},
	})
	resp, err := http.Post(c.cfg.URL+"/mcp", "application/json", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("remote connect: %w", err)
	}
	defer resp.Body.Close()
	c.initialized = true
	return nil
}

func (c *Client) initialize() error {
	c.send(jsonRPCReq{
		JSONRPC: "2.0", ID: c.nextID(), Method: "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "edcode", "version": "0.1.0"},
		},
	})
	if _, err := c.recv(); err != nil {
		return err
	}
	c.send(jsonRPCReq{
		JSONRPC: "2.0", ID: c.nextID(), Method: "notifications/initialized",
	})
	if err := c.discoverTools(); err != nil {
		return err
	}
	c.initialized = true
	return nil
}

func (c *Client) discoverTools() error {
	c.send(jsonRPCReq{
		JSONRPC: "2.0", ID: c.nextID(), Method: "tools/list",
	})
	resp, err := c.recv()
	if err != nil {
		return err
	}
	var listResult struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &listResult); err != nil {
		return fmt.Errorf("parse tools list: %w", err)
	}
	c.tools = listResult.Tools
	return nil
}

func (c *Client) Tools() []ToolInfo { return c.tools }

func (c *Client) Definitions() []provider.ToolDefinition {
	var defs []provider.ToolDefinition
	for _, t := range c.tools {
		schema := provider.InputSchema{Type: "object"}
		if t.InputSchema != nil {
			var parsed struct {
				Type       string         `json:"type"`
				Properties map[string]any `json:"properties"`
				Required   []string       `json:"required"`
			}
			if err := json.Unmarshal(t.InputSchema, &parsed); err == nil {
				schema.Type = parsed.Type
				schema.Properties = parsed.Properties
				schema.Required = parsed.Required
			}
		}
		defs = append(defs, provider.ToolDefinition{
			Name:        fmt.Sprintf("%s_%s", c.name, t.Name),
			Description: t.Description,
			InputSchema: schema,
		})
	}
	return defs
}

func (c *Client) Call(ctx context.Context, toolName string, args map[string]any) (string, error) {
	strippedName := strings.TrimPrefix(toolName, c.name+"_")
	c.send(jsonRPCReq{
		JSONRPC: "2.0", ID: c.nextID(), Method: "tools/call",
		Params: map[string]any{
			"name":      strippedName,
			"arguments": args,
		},
	})
	resp, err := c.recv()
	if err != nil {
		return "", err
	}
	var callResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &callResult); err != nil {
		return string(resp.Result), nil
	}
	var out string
	for _, c := range callResult.Content {
		out += c.Text
	}
	if callResult.IsError {
		return out, fmt.Errorf("mcp tool error: %s", out)
	}
	return out, nil
}

func (c *Client) send(req jsonRPCReq) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin == nil {
		return
	}
	data, _ := json.Marshal(req)
	fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n%s", len(data), data)
}

func (c *Client) recv() (*jsonRPCResp, error) {
	for c.stdout.Scan() {
		line := c.stdout.Text()
		if strings.HasPrefix(line, "Content-Length:") {
			continue
		}
		if line == "" {
			if !c.stdout.Scan() {
				break
			}
			line = c.stdout.Text()
		}
		var resp jsonRPCResp
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return &resp, nil
	}
	if err := c.stdout.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

func (c *Client) nextID() int {
	id := c.msgID
	c.msgID++
	return id
}

func (c *Client) Close() error {
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}
	return nil
}
