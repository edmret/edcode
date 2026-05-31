package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/edmundo/edcode/internal/config"
	"github.com/edmundo/edcode/internal/ctxmgr"
	"github.com/edmundo/edcode/internal/enhance"
	"github.com/edmundo/edcode/internal/memory"
	"github.com/edmundo/edcode/internal/middleware"
	"github.com/edmundo/edcode/internal/provider"
	"github.com/edmundo/edcode/internal/session"
	"github.com/edmundo/edcode/internal/skills"
	"github.com/edmundo/edcode/internal/tool"
	"github.com/edmundo/edcode/internal/tool/mcp"
)

type EnhancedAgent struct {
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
	Memory      *memory.MemoryStore
	ContextMgr  *ctxmgr.Manager
	Enhancer    *enhance.Engine
	Skills      *skills.Manager
	StartTime   time.Time
}

type EnhancedStepResult struct {
	Messages    []provider.Message
	ToolResults []tool.Result
	Finished    bool
	TokensUsed  int
	Steps       int
	Errors      []string
}

func (a *EnhancedAgent) Execute(ctx context.Context, systemPrompt string, userPrompt string, maxSteps int) (*EnhancedStepResult, error) {
	a.StartTime = time.Now()
	sess := a.SessionMgr.Create(a.SessionID)
	a.SessionID = sess.ID
	a.Memory.SetSession(sess.ID)

	startTime := time.Now()

	systemPrompt = a.ContextMgr.EnrichWithMemory(systemPrompt)
	systemPrompt = a.Enhancer.AutoEnhanceSystemPrompt(systemPrompt, a.Name)

	if a.Skills != nil {
		available := a.Skills.SystemPrompt()
		if available != "" {
			systemPrompt += "\n\n" + available
		}
		loaded := a.Skills.LoadedInstructions()
		if loaded != "" {
			systemPrompt += "\n\n" + loaded
		}
	}

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

	result := &EnhancedStepResult{Finished: false}
	compactSince := time.Now()

	for i := 0; i < steps; i++ {
		messages := sess.Messages
		if time.Since(compactSince) > 2*time.Minute {
			cr := a.ContextMgr.AutoCompact(messages, false)
			if cr.Compacted {
				messages = cr.Messages
				sess.Messages = messages
				log.Printf("auto-compacted context: saved %d tokens (level %d)", cr.TokensSaved, cr.CompactionLevel)
				a.Memory.AddInsight(fmt.Sprintf("Auto-compacted context: saved ~%d tokens", cr.TokensSaved),
					[]string{"context", "compaction"})
			}
			compactSince = time.Now()
		}

		req := &provider.ChatRequest{
			Model:       a.ModelRef,
			System:      systemPrompt,
			Messages:    messages,
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
			Messages:  messages,
		}

		req, err := a.Middleware.RunPreModel(ctx, mwCtx, req)
		if err != nil {
			return nil, fmt.Errorf("pre-model hook: %w", err)
		}

		resp, err := a.Provider.Chat(ctx, req)
		if err != nil {
			a.Enhancer.RecordErrorPattern(a.Name, err.Error())
			result.Errors = append(result.Errors, err.Error())
			return nil, fmt.Errorf("chat: %w", err)
		}

		result.TokensUsed += resp.Usage.InputTokens + resp.Usage.OutputTokens
		result.Steps++

		resp, err = a.Middleware.RunPostModel(ctx, mwCtx, req, resp)
		if err != nil {
			return nil, fmt.Errorf("post-model hook: %w", err)
		}

		sess.Messages = append(sess.Messages, resp.Message)
		result.Messages = append(result.Messages, resp.Message)
		a.Memory.AddToolResult("model_response", "", resp.Message.Content[0].Text)

		hasToolCalls := false
		for _, c := range resp.Message.Content {
			if c.Type == provider.ContentToolCall {
				hasToolCalls = true
				tc := c.ToolCall

				args := make(map[string]any)
				if err := parseArgsJSON(tc.Arguments, &args); err != nil {
					log.Printf("parse args for %s: %v", tc.Name, err)
				}

				a.Enhancer.RecordToolUsage(a.Name, tc.Name)

				args, err = a.Middleware.RunPreTool(ctx, mwCtx, tc.Name, args)
				if err != nil {
					log.Printf("pre-tool hook: %v", err)
					continue
				}

				toolCtx := &tool.Context{
					Workdir: a.Workdir,
					Session: sess.ID,
					Agent:   a.Name,
					Skills:  a.Skills,
				}

				callStart := time.Now()
				toolResult := a.executeTool(ctx, tc.Name, args, toolCtx)
				callDuration := time.Since(callStart)

				if !toolResult.Success {
					a.Enhancer.RecordErrorPattern(a.Name, fmt.Sprintf("tool %s: %s", tc.Name, toolResult.Error))
				}

				result.ToolResults = append(result.ToolResults, *toolResult)

				a.Memory.AddToolResult(tc.Name,
					fmt.Sprintf("%v", args),
					fmt.Sprintf("success=%v, len=%d, duration=%v", toolResult.Success, len(toolResult.Data), callDuration))

				output, _ := a.Middleware.RunPostTool(ctx, mwCtx, tc.Name, args, toolResult.Data)

				resultMsg := provider.Message{
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
				}
				sess.Messages = append(sess.Messages, resultMsg)
				result.Messages = append(result.Messages, resultMsg)
			}
		}

		if !hasToolCalls {
			result.Finished = true
			break
		}
	}

	duration := time.Since(startTime)

	record := enhance.SessionRecord{
		ID: sess.ID, Agent: a.Name,
		Steps: result.Steps, ToolCalls: len(result.ToolResults),
		Success: result.Finished, Duration: duration,
		Tokens: result.TokensUsed, Errors: result.Errors,
		Outcome: extractOutcome(result.Messages),
	}
	a.Enhancer.RecordSession(record)

	return result, nil
}

func (a *EnhancedAgent) executeTool(ctx context.Context, name string, args map[string]any, toolCtx *tool.Context) *tool.Result {
	if t, ok := a.Tools.Get(name); ok {
		return t.Execute(ctx, args, toolCtx)
	}
	for _, mc := range a.MCPClients {
		for _, ti := range mc.Tools() {
			mcpName := fmt.Sprintf("%s_%s", mc.Definitions()[0].Name[:len(mc.Definitions()[0].Name)-len(mc.Tools()[0].Name)-1], ti.Name)
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

func parseArgsJSON(raw string, target *map[string]any) error {
	if raw == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw), target)
}

func extractOutcome(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		for _, c := range msgs[i].Content {
			if c.Type == provider.ContentText && len(c.Text) > 50 {
				text := c.Text
				if len(text) > 200 {
					text = text[:200]
				}
				return text
			}
		}
	}
	return "(no completion text)"
}

func (a *EnhancedAgent) SaveSessionOutcome() {
	summary := memory.SessionSummary{
		SessionID:   a.SessionID,
		StartedAt:   a.StartTime,
		EndedAt:     time.Now(),
		Goals:       []string{},
		Achieved:    []string{},
		KeyDecisions: []string{},
		Lessons:     []string{},
	}
	a.Memory.SaveSessionSummary(summary)
}
