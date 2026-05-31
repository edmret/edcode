package agent

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/edmundo/edcode/internal/config"
	"github.com/edmundo/edcode/internal/middleware"
	"github.com/edmundo/edcode/internal/provider"
	"github.com/edmundo/edcode/internal/session"
	"github.com/edmundo/edcode/internal/tool"
	"github.com/edmundo/edcode/internal/tool/mcp"
)

type Agent struct {
	Name        string
	Config      config.AgentConfig
	Provider    provider.Provider
	ModelRef    string
	Tools       *tool.Manager
	MCPClients  []*mcp.Client
	Middleware  *middleware.Chain
	SessionMgr  *session.Manager
	SessionID   string
	Workdir     string
}

type StepResult struct {
	Messages    []provider.Message
	ToolResults []tool.Result
	Finished    bool
}

func (a *Agent) Execute(ctx context.Context, systemPrompt string, userPrompt string, maxSteps int) (*StepResult, error) {
	sess := a.SessionMgr.Create(a.SessionID)
	a.SessionID = sess.ID

	if userPrompt != "" {
		sess.Messages = append(sess.Messages, provider.Message{
			Role:    provider.RoleUser,
			Content: []provider.Content{{Type: provider.ContentText, Text: userPrompt}},
		})
	}

	steps := maxSteps
	if steps <= 0 {
		steps = a.Config.MaxSteps
	}
	if steps <= 0 {
		steps = 25
	}

	result := &StepResult{Finished: false}

	for i := 0; i < steps; i++ {
		req := &provider.ChatRequest{
			Model:       a.ModelRef,
			System:      systemPrompt,
			Messages:    sess.Messages,
			Tools:       a.Tools.Definitions(),
			Temperature: a.Config.Temperature,
			TopP:        a.Config.TopP,
		}

		for _, mc := range a.MCPClients {
			req.Tools = append(req.Tools, mc.Definitions()...)
		}

		mwCtx := &middleware.Context{
			AgentName: a.Name,
			Workdir:   a.Workdir,
			SessionID: sess.ID,
			Messages:  sess.Messages,
		}

		req, err := a.Middleware.RunPreModel(ctx, mwCtx, req)
		if err != nil {
			return nil, fmt.Errorf("pre-model hook: %w", err)
		}

		resp, err := a.Provider.Chat(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("chat: %w", err)
		}

		resp, err = a.Middleware.RunPostModel(ctx, mwCtx, req, resp)
		if err != nil {
			return nil, fmt.Errorf("post-model hook: %w", err)
		}

		sess.Messages = append(sess.Messages, resp.Message)
		result.Messages = append(result.Messages, resp.Message)

		hasToolCalls := false
		for _, c := range resp.Message.Content {
			if c.Type == provider.ContentToolCall {
				hasToolCalls = true
				tc := c.ToolCall
				args := make(map[string]any)
				if err := parseArgs(tc.Arguments, &args); err != nil {
					log.Printf("parse args for %s: %v", tc.Name, err)
				}

				args, err = a.Middleware.RunPreTool(ctx, mwCtx, tc.Name, args)
				if err != nil {
					log.Printf("pre-tool hook: %v", err)
					continue
				}

				toolCtx := &tool.Context{
					Workdir: a.Workdir,
					Session: sess.ID,
					Agent:   a.Name,
				}

				toolResult := a.executeTool(ctx, tc.Name, args, toolCtx)
				result.ToolResults = append(result.ToolResults, *toolResult)

				output, _ := a.Middleware.RunPostTool(ctx, mwCtx, tc.Name, args, toolResult.Data)

				sess.Messages = append(sess.Messages, provider.Message{
					Role: provider.RoleTool,
					Content: []provider.Content{{
						Type: provider.ContentToolResult,
						ToolResult: &provider.ToolResult{
							ID:     tc.ID,
							Name:   tc.Name,
							Result: output,
							Error:  toolResult.Error,
						},
					}},
				})
			}
		}

		if !hasToolCalls {
			result.Finished = true
			break
		}
	}

	return result, nil
}

func (a *Agent) executeTool(ctx context.Context, name string, args map[string]any, toolCtx *tool.Context) *tool.Result {
	if t, ok := a.Tools.Get(name); ok {
		return t.Execute(ctx, args, toolCtx)
	}
	for _, mc := range a.MCPClients {
		for _, ti := range mc.Tools() {
			mcpName := fmt.Sprintf("%s_%s", mc.Definitions()[0].Name[:strings.Index(mc.Definitions()[0].Name, "_")], ti.Name)
			if mcpName == name {
				out, err := mc.Call(ctx, name, args)
				if err != nil {
					return &tool.Result{Success: false, Error: err.Error()}
				}
				return &tool.Result{Success: true, Data: out}
			}
		}
	}
	return &tool.Result{Success: false, Error: fmt.Sprintf("unknown tool: %s", name)}
}

func parseArgs(raw string, target *map[string]any) error {
	if raw == "" {
		return nil
	}
	dec := strings.NewReader(raw)
	_, err := fmt.Fscanf(dec, "%v", target)
	return err
}
