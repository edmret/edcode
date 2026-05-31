package middleware

import (
	"context"

	"github.com/edmundo/edcode/internal/provider"
)

type Context struct {
	AgentName string
	StepName  string
	Workdir   string
	SessionID string
	Messages  []provider.Message
}

type PreModelHook func(ctx context.Context, mwCtx *Context, req *provider.ChatRequest) (*provider.ChatRequest, error)
type PostModelHook func(ctx context.Context, mwCtx *Context, req *provider.ChatRequest, resp *provider.ChatResponse) (*provider.ChatResponse, error)
type PreToolHook func(ctx context.Context, mwCtx *Context, toolName string, args map[string]any) (map[string]any, error)
type PostToolHook func(ctx context.Context, mwCtx *Context, toolName string, args map[string]any, result string) (string, error)

type Chain struct {
	preModel  []PreModelHook
	postModel []PostModelHook
	preTool   []PreToolHook
	postTool  []PostToolHook
}

func NewChain() *Chain {
	return &Chain{}
}

func (c *Chain) AddPreModel(hook PreModelHook) {
	c.preModel = append(c.preModel, hook)
}

func (c *Chain) AddPostModel(hook PostModelHook) {
	c.postModel = append(c.postModel, hook)
}

func (c *Chain) AddPreTool(hook PreToolHook) {
	c.preTool = append(c.preTool, hook)
}

func (c *Chain) AddPostTool(hook PostToolHook) {
	c.postTool = append(c.postTool, hook)
}

func (c *Chain) RunPreModel(ctx context.Context, mwCtx *Context, req *provider.ChatRequest) (*provider.ChatRequest, error) {
	var err error
	for _, hook := range c.preModel {
		req, err = hook(ctx, mwCtx, req)
		if err != nil {
			return nil, err
		}
	}
	return req, nil
}

func (c *Chain) RunPostModel(ctx context.Context, mwCtx *Context, req *provider.ChatRequest, resp *provider.ChatResponse) (*provider.ChatResponse, error) {
	var err error
	for _, hook := range c.postModel {
		resp, err = hook(ctx, mwCtx, req, resp)
		if err != nil {
			return nil, err
		}
	}
	return resp, nil
}

func (c *Chain) RunPreTool(ctx context.Context, mwCtx *Context, toolName string, args map[string]any) (map[string]any, error) {
	var err error
	for _, hook := range c.preTool {
		args, err = hook(ctx, mwCtx, toolName, args)
		if err != nil {
			return nil, err
		}
	}
	return args, nil
}

func (c *Chain) RunPostTool(ctx context.Context, mwCtx *Context, toolName string, args map[string]any, result string) (string, error) {
	var err error
	for _, hook := range c.postTool {
		result, err = hook(ctx, mwCtx, toolName, args, result)
		if err != nil {
			return "", err
		}
	}
	return result, nil
}

func ModelRetry(maxRetries int) PreModelHook {
	return func(ctx context.Context, mwCtx *Context, req *provider.ChatRequest) (*provider.ChatRequest, error) {
		return req, nil
	}
}

func MaxTokensGuard(maxTokens int) PreModelHook {
	return func(ctx context.Context, mwCtx *Context, req *provider.ChatRequest) (*provider.ChatRequest, error) {
		totalTokens := 0
		for _, msg := range req.Messages {
			for _, c := range msg.Content {
				totalTokens += len(c.Text) / 4
			}
		}
		if req.System != "" {
			totalTokens += len(req.System) / 4
		}
		if totalTokens > maxTokens && len(req.Messages) > 2 {
			req.Messages = req.Messages[len(req.Messages)/2:]
		}
		return req, nil
	}
}

func ToolLogger(logFn func(string, ...any)) PostToolHook {
	return func(ctx context.Context, mwCtx *Context, toolName string, args map[string]any, result string) (string, error) {
		logFn("[tool] %s/%s: %d chars", mwCtx.AgentName, toolName, len(result))
		return result, nil
	}
}
