package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/edmundo/edcode/internal/agent"
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
	"github.com/edmundo/edcode/internal/workspace"
)

type App struct {
	Config      *config.Config
	ProviderReg *provider.Registry
	SessionMgr  *session.Manager
	Workdir     string
	Agents      map[string]*agent.EnhancedAgent
	MCPClients  []*mcp.Client
	GlobalMW    *middleware.Chain
	Memory      *memory.MemoryStore
	ContextMgr  *ctxmgr.Manager
	Enhancer    *enhance.Engine
	Skills      *skills.Manager
	Workspace   *workspace.WorkspaceManager
	WorkspaceInfo *workspace.Info
	AgentsMD    string // AGENTS.md content loaded from workspace
}

type Option func(*App)

func WithConfig(cfg *config.Config) Option {
	return func(h *App) { h.Config = cfg }
}

func WithWorkdir(wd string) Option {
	return func(h *App) { h.Workdir = wd }
}

func New(opts ...Option) *App {
	h := &App{
		Config:      config.Default(),
		ProviderReg: provider.NewRegistry(),
		SessionMgr:  session.NewManager(),
		Agents:      make(map[string]*agent.EnhancedAgent),
		GlobalMW:    middleware.NewChain(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *App) Init() error {
	if wd := h.Workdir; wd == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
		h.Workdir = cwd
	}

	cfgPath := config.DefaultPath()
	if cfg, err := config.Load(cfgPath); err == nil {
		h.Config = cfg
		log.Printf("loaded config from %s", cfgPath)
	}

	reg, err := provider.ProvidersFromConfig(h.Config)
	if err != nil {
		return fmt.Errorf("init providers: %w", err)
	}
	h.ProviderReg = reg

	// Initialize workspace manager
	h.Workspace = workspace.New(h.Workdir)
	h.WorkspaceInfo = h.Workspace.LoadInfo()

	// Ensure workspace directory exists
	if !h.WorkspaceInfo.HasWorkspace {
		if err := h.Workspace.EnsureWorkspace(); err != nil {
			log.Printf("warn: ensure workspace: %v", err)
		} else {
			h.WorkspaceInfo.HasWorkspace = true
			h.WorkspaceInfo.WorkspaceDir = filepath.Join(h.Workdir, workspace.WorkspaceDirName)
			h.WorkspaceInfo.EnhanceDir = filepath.Join(h.WorkspaceInfo.WorkspaceDir, "enhance")
			h.WorkspaceInfo.MemoryDir = filepath.Join(h.WorkspaceInfo.WorkspaceDir, "memory")
		}
	}

	// Load workspace config overrides
	wsCfg, err := config.LoadWorkspaceConfig(h.Workdir)
	if err != nil {
		log.Printf("warn: load workspace config: %v", err)
	}
	h.Config = config.MergeWorkspace(h.Config, wsCfg)

	// Two-tier memory: workspace dir + global dir
	h.Memory = memory.NewMemoryStore(h.WorkspaceInfo.MemoryDir, h.WorkspaceInfo.GlobalMemory)
	h.Memory.LoadAllSessions()
	h.ContextMgr = ctxmgr.NewManager(h.Config.Session.MaxTokens, h.Memory)

	// Two-tier enhance: workspace sessions + global insights
	h.Enhancer = enhance.NewEngine(h.WorkspaceInfo.EnhanceDir, h.WorkspaceInfo.GlobalEnhance)

	// Skills: workspace + global paths
	h.Skills = skills.NewManager([]string{
		filepath.Join(h.Workdir, ".skills"),
		filepath.Join(h.Workdir, "skills"),
		filepath.Join(os.Getenv("HOME"), ".edcode", "skills"),
	})

	// Load AGENTS.md content for system prompt
	h.AgentsMD = h.WorkspaceInfo.AgentsMD

	for _, pc := range h.Config.MCP {
		if pc.Enabled {
			c := mcp.NewClient("mcp", pc)
			if err := c.Start(context.Background()); err != nil {
				log.Printf("warn: mcp %q: %v", "mcp", err)
				continue
			}
			h.MCPClients = append(h.MCPClients, c)
		}
	}

	h.GlobalMW.AddPreModel(middleware.MaxTokensGuard(h.Config.Session.MaxTokens))

	for name, ac := range h.Config.Agents {
		p, modelRef, err := h.ProviderReg.GetByModel(ac.Model)
		if err != nil {
			log.Printf("warn: agent %q: %v, using default", name, err)
			for _, p2 := range h.ProviderReg.ListProviders() {
				p, _ = h.ProviderReg.Get(p2)
				modelRef = ac.Model
				break
			}
			if p == nil {
				continue
			}
		}

		toolMgr := tool.NewManager(func(toolName string, args map[string]any) tool.Permission {
			perm := ac.Permission
			m := map[string]string{
				"read": perm.Read, "write": perm.Write, "edit": perm.Edit,
				"bash": perm.Bash, "glob": perm.Glob, "grep": perm.Grep,
				"web":  perm.Web,
			}
			if p, ok := m[toolName]; ok {
				switch p {
				case "allow":
					return tool.PermAllow
				case "ask":
					return tool.PermAsk
				case "deny":
					return tool.PermDeny
				}
			}
			if perm.Custom != nil {
				if p, ok := perm.Custom[toolName]; ok {
					switch p {
					case "allow":
						return tool.PermAllow
					case "deny":
						return tool.PermDeny
					}
				}
			}
			return tool.PermAllow
		})

		toolMgr.Register(tool.NewReadTool())
		toolMgr.Register(tool.NewGlobTool())
		toolMgr.Register(tool.NewGrepTool())
		toolMgr.Register(tool.NewBashTool())
		toolMgr.Register(tool.NewWriteTool())
		toolMgr.Register(tool.NewEditTool())
		toolMgr.Register(tool.NewWebFetchTool())
		toolMgr.Register(tool.NewSkillTool())

		mw := middleware.NewChain()
		mw.AddPreModel(h.GlobalMW.RunPreModel)
		mw.AddPostModel(h.GlobalMW.RunPostModel)

		h.Agents[name] = &agent.EnhancedAgent{
			Name:           name,
			Config:         ac,
			Provider:       p,
			ModelRef:       modelRef,
			Tools:          toolMgr,
			MCPClients:     h.MCPClients,
			Middleware:     mw,
			SessionMgr:     h.SessionMgr,
			Workdir:        h.Workdir,
			Memory:         h.Memory,
			ContextMgr:     h.ContextMgr,
			Enhancer:       h.Enhancer,
			Skills:         h.Skills,
			WorkspaceAgentsMD: h.AgentsMD,
		}
	}

	return nil
}

func (h *App) GetAgent(name string) (*agent.EnhancedAgent, error) {
	a, ok := h.Agents[name]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", name)
	}
	return a, nil
}

func (h *App) RunPipeline(ctx context.Context, userPrompt string) error {
	parentSess := h.SessionMgr.Create("")

	for _, step := range h.Config.Pipeline.Steps {
		a, err := h.GetAgent(step.Agent)
		if err != nil {
			return fmt.Errorf("pipeline step %q: %w", step.Name, err)
		}

		systemPrompt := step.Prompt
		if step.Prompt == "" && a.Config.Prompt != "" {
			systemPrompt = a.Config.Prompt
		}
		if systemPrompt == "" {
			systemPrompt = fmt.Sprintf("You are a %s agent. Complete the following task.", step.Name)
		}

		agCtx := context.WithValue(ctx, "step", step.Name)
		ag := *a
		ag.SessionID = parentSess.ID

		if step.Model != "" {
			p, modelRef, err := h.ProviderReg.GetByModel(step.Model)
			if err == nil {
				ag.Provider = p
				ag.ModelRef = modelRef
			}
		}

		prompt := userPrompt
		if step.Name != "plan" {
			prompt = fmt.Sprintf("Continue with the %s step: %s", step.Name, userPrompt)
		}

		log.Printf("running pipeline step: %s (agent: %s, model: %s/%s)",
			step.Name, a.Name, ag.Provider.Name(), ag.ModelRef)

		result, err := ag.Execute(agCtx, systemPrompt, prompt, step.MaxSteps)
		if err != nil {
			return fmt.Errorf("step %q failed: %w", step.Name, err)
		}

		if !result.Finished {
			log.Printf("step %q hit max steps (%d), continuing", step.Name, step.MaxSteps)
		}

		fmt.Printf("\n=== Step: %s ===\n", step.Name)
		for _, msg := range result.Messages {
			for _, c := range msg.Content {
				if c.Type == provider.ContentText && c.Text != "" {
					fmt.Printf("%s\n", c.Text)
				}
			}
		}

		ag.SaveSessionOutcome()
	}

	return nil
}

func (h *App) SwitchModel(agentName, modelRef string) error {
	a, ok := h.Agents[agentName]
	if !ok {
		return fmt.Errorf("agent %q not found", agentName)
	}
	p, ref, err := h.ProviderReg.GetByModel(modelRef)
	if err != nil {
		return fmt.Errorf("model %q: %w", modelRef, err)
	}
	a.Provider = p
	a.ModelRef = ref
	a.Config.Model = modelRef
	log.Printf("agent %q switched to model: %s (resolved: %s/%s)", agentName, modelRef, p.Name(), ref)
	return nil
}

func (h *App) ListModels() []string {
	return h.ProviderReg.AvailableModels()
}

func (h *App) ListOptimizations(agentName string) []string {
	return h.Enhancer.GetOptimizations(agentName)
}

func (h *App) ListWorkspaces() string {
	return h.Workspace.ListText()
}

func (h *App) PruneWorkspaces() []string {
	return h.Workspace.Prune()
}

func (h *App) Close() {
	for _, mc := range h.MCPClients {
		mc.Close()
	}
}

func RunOnce(prompt string, opts ...Option) error {
	h := New(opts...)
	if err := h.Init(); err != nil {
		return fmt.Errorf("init: %w", err)
	}
	defer h.Close()
	return h.RunPipeline(context.Background(), prompt)
}
