package tool

import (
	"context"

	"github.com/edmundo/edcode/internal/provider"
	"github.com/edmundo/edcode/internal/skills"
)

type Permission string

const (
	PermAllow Permission = "allow"
	PermAsk   Permission = "ask"
	PermDeny  Permission = "deny"
)

type Result struct {
	Success bool   `json:"success"`
	Data    string `json:"data"`
	Error   string `json:"error,omitempty"`
}

type Context struct {
	Workdir  string
	Session  string
	Agent    string
	Message  string
	Skills   *skills.Manager
}

type Tool interface {
	Name() string
	Description() string
	InputSchema() provider.InputSchema
	Execute(ctx context.Context, args map[string]any, toolCtx *Context) *Result
}

type PermissionChecker func(toolName string, args map[string]any) Permission

type Manager struct {
	tools     map[string]Tool
	checkPerm PermissionChecker
}

func NewManager(checkPerm PermissionChecker) *Manager {
	return &Manager{
		tools:     make(map[string]Tool),
		checkPerm: checkPerm,
	}
}

func (m *Manager) Register(t Tool) {
	m.tools[t.Name()] = t
}

func (m *Manager) Get(name string) (Tool, bool) {
	t, ok := m.tools[name]
	return t, ok
}

func (m *Manager) All() []Tool {
	var tools []Tool
	for _, t := range m.tools {
		tools = append(tools, t)
	}
	return tools
}

func (m *Manager) Definitions() []provider.ToolDefinition {
	var defs []provider.ToolDefinition
	for _, t := range m.tools {
		defs = append(defs, provider.ToolDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}
	return defs
}

func (m *Manager) CheckPermission(toolName string, args map[string]any) Permission {
	if m.checkPerm != nil {
		return m.checkPerm(toolName, args)
	}
	return PermAllow
}

func (m *Manager) Execute(ctx context.Context, name string, args map[string]any, toolCtx *Context) *Result {
	t, ok := m.tools[name]
	if !ok {
		return &Result{Success: false, Error: "tool not found: " + name}
	}
	perm := m.CheckPermission(name, args)
	if perm == PermDeny {
		return &Result{Success: false, Error: "tool denied by policy: " + name}
	}
	return t.Execute(ctx, args, toolCtx)
}
