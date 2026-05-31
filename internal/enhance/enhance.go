package enhance

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type SessionRecord struct {
	ID        string
	Agent     string
	Steps     int
	ToolCalls int
	Success   bool
	Duration  time.Duration
	Tokens    int
	Errors    []string
	Outcome   string
}

type AgentProfile struct {
	Name          string
	AvgSteps      float64
	ToolFrequency map[string]int
	SuccessRate   float64
	CommonFailures []string
}

type Engine struct {
	mu          sync.Mutex
	sessions    []SessionRecord
	insights    []string
	agentConfig map[string]*AgentProfile
}

func NewEngine(dataDir string) *Engine {
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".edcode", "enhance")
	}
	os.MkdirAll(dataDir, 0755)
	return &Engine{
		agentConfig: make(map[string]*AgentProfile),
	}
}

func (e *Engine) RecordSession(s SessionRecord) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sessions = append(e.sessions, s)
	p := e.agentConfig[s.Agent]
	if p == nil {
		p = &AgentProfile{Name: s.Agent, ToolFrequency: make(map[string]int)}
		e.agentConfig[s.Agent] = p
	}
	n := len(e.sessionsByAgent(s.Agent))
	p.AvgSteps = ((p.AvgSteps * float64(n-1)) + float64(s.Steps)) / float64(n)
	p.ToolFrequency["total_calls"] += s.ToolCalls
	if s.Success {
		p.SuccessRate = ((p.SuccessRate * float64(n-1)) + 1) / float64(n)
	} else {
		p.SuccessRate = ((p.SuccessRate * float64(n-1)) + 0) / float64(n)
	}
}

func (e *Engine) sessionsByAgent(agent string) []SessionRecord {
	var out []SessionRecord
	for _, s := range e.sessions {
		if s.Agent == agent {
			out = append(out, s)
		}
	}
	return out
}

func (e *Engine) AddInsight(content string, tags []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.insights = append(e.insights, fmt.Sprintf("[%s] %s", strings.Join(tags, ","), content))
}

func (e *Engine) RecordErrorPattern(agentName, errorMsg string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.agentConfig[agentName]
	if p == nil {
		p = &AgentProfile{Name: agentName, ToolFrequency: make(map[string]int)}
		e.agentConfig[agentName] = p
	}
	if len(p.CommonFailures) > 10 {
		p.CommonFailures = p.CommonFailures[1:]
	}
	p.CommonFailures = append(p.CommonFailures, fmt.Sprintf("[%s] %s", time.Now().Format(time.RFC3339), errorMsg))
}

func (e *Engine) RecordToolUsage(agentName, toolName string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.agentConfig[agentName]
	if p == nil {
		p = &AgentProfile{Name: agentName, ToolFrequency: make(map[string]int)}
		e.agentConfig[agentName] = p
	}
	p.ToolFrequency[toolName]++
}

func (e *Engine) GenerateAgentInstructions(agentName string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.agentConfig[agentName]
	if p == nil && len(e.insights) == 0 {
		return ""
	}
	var tips []string
	if p != nil {
		if p.AvgSteps > 15 {
			tips = append(tips, "You tend to use many steps. Try to batch operations where possible.")
		}
		if p.SuccessRate < 0.5 && len(e.sessionsByAgent(agentName)) > 3 {
			tips = append(tips, "Previous sessions had issues. Verify results carefully.")
		}
	}
	for _, ins := range e.insights {
		tips = append(tips, ins)
	}
	if len(tips) == 0 {
		return ""
	}
	return "## Auto-Optimization Tips\n" + strings.Join(tips, "\n")
}

func (e *Engine) AutoEnhanceSystemPrompt(basePrompt, agentName string) string {
	instructions := e.GenerateAgentInstructions(agentName)
	if instructions == "" {
		return basePrompt
	}
	return basePrompt + "\n\n" + instructions
}

func (e *Engine) GetOptimizations(agentName string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var opts []string
	p := e.agentConfig[agentName]
	if p != nil {
		if p.SuccessRate < 0.6 && len(e.sessionsByAgent(agentName)) > 3 {
			opts = append(opts, fmt.Sprintf("Agent %q success rate is %.0f%%. Consider switching model.", agentName, p.SuccessRate*100))
		}
		if p.AvgSteps > 20 {
			opts = append(opts, fmt.Sprintf("Agent %q averages %.1f steps. Break tasks into smaller pieces.", agentName, p.AvgSteps))
		}
	}
	for _, ins := range e.insights {
		opts = append(opts, "Tip: "+ins)
	}
	if len(opts) == 0 {
		opts = append(opts, "System is still learning. More sessions needed for optimization insights.")
	}
	sort.Strings(opts)
	return opts
}
