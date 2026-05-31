package tool

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/edmundo/edcode/internal/provider"
)

type ReadTool struct{}

func NewReadTool() *ReadTool { return &ReadTool{} }

func (t *ReadTool) Name() string { return "read" }

func (t *ReadTool) Description() string {
	return "Read the contents of a file. Supports optional line offset and limit."
}

func (t *ReadTool) InputSchema() provider.InputSchema {
	return provider.InputSchema{
		Type: "object",
		Properties: map[string]any{
			"filePath": map[string]any{"type": "string", "description": "Absolute path to the file"},
			"offset":   map[string]any{"type": "integer", "description": "Starting line number (1-indexed)"},
			"limit":    map[string]any{"type": "integer", "description": "Max lines to read"},
		},
		Required: []string{"filePath"},
	}
}

func (t *ReadTool) Execute(ctx context.Context, args map[string]any, toolCtx *Context) *Result {
	fp, _ := args["filePath"].(string)
	if fp == "" {
		return &Result{Success: false, Error: "filePath is required"}
	}
	offset, _ := args["offset"].(int)
	limit, _ := args["limit"].(int)
	if offset <= 0 {
		offset = 1
	}

	resolved := fp
	if !filepath.IsAbs(fp) && toolCtx != nil {
		resolved = filepath.Join(toolCtx.Workdir, fp)
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return &Result{Success: false, Error: fmt.Sprintf("read file: %v", err)}
	}
	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)
	if offset > 0 {
		offset-- // 1-indexed
	}
	if offset >= totalLines {
		return &Result{Success: true, Data: "(empty - offset beyond file length)"}
	}
	lines = lines[offset:]
	if limit > 0 && limit < len(lines) {
		lines = lines[:limit]
	}
	out := strings.Join(lines, "\n")
	if limit > 0 && len(lines) >= limit {
		out += fmt.Sprintf("\n... (showing %d of %d lines)", limit, totalLines)
	}
	return &Result{Success: true, Data: out}
}

type GlobTool struct{}

func NewGlobTool() *GlobTool { return &GlobTool{} }

func (t *GlobTool) Name() string { return "glob" }

func (t *GlobTool) Description() string {
	return "Find files matching a glob pattern."
}

func (t *GlobTool) InputSchema() provider.InputSchema {
	return provider.InputSchema{
		Type: "object",
		Properties: map[string]any{
			"pattern": map[string]any{"type": "string", "description": "Glob pattern e.g. **/*.go"},
			"path":    map[string]any{"type": "string", "description": "Directory to search in"},
		},
		Required: []string{"pattern"},
	}
}

func (t *GlobTool) Execute(ctx context.Context, args map[string]any, toolCtx *Context) *Result {
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return &Result{Success: false, Error: "pattern is required"}
	}
	searchPath, _ := args["path"].(string)
	if searchPath == "" && toolCtx != nil {
		searchPath = toolCtx.Workdir
	}
	matches, err := filepath.Glob(filepath.Join(searchPath, pattern))
	if err != nil {
		return &Result{Success: false, Error: fmt.Sprintf("glob: %v", err)}
	}
	if len(matches) == 0 {
		return &Result{Success: true, Data: "(no matches)"}
	}
	return &Result{Success: true, Data: strings.Join(matches, "\n")}
}

type GrepTool struct{}

func NewGrepTool() *GrepTool { return &GrepTool{} }

func (t *GrepTool) Name() string { return "grep" }

func (t *GrepTool) Description() string {
	return "Search file contents using regular expressions."
}

func (t *GrepTool) InputSchema() provider.InputSchema {
	return provider.InputSchema{
		Type: "object",
		Properties: map[string]any{
			"pattern": map[string]any{"type": "string", "description": "Regex pattern to search"},
			"include": map[string]any{"type": "string", "description": "File glob pattern e.g. *.go"},
			"path":    map[string]any{"type": "string", "description": "Directory to search"},
		},
		Required: []string{"pattern"},
	}
}

func (t *GrepTool) Execute(ctx context.Context, args map[string]any, toolCtx *Context) *Result {
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return &Result{Success: false, Error: "pattern is required"}
	}
	include, _ := args["include"].(string)
	searchPath, _ := args["path"].(string)
	if searchPath == "" && toolCtx != nil {
		searchPath = toolCtx.Workdir
	}
	cmd := exec.CommandContext(ctx, "rg", "-n", pattern)
	if include != "" {
		cmd.Args = append(cmd.Args, "-g", include)
	}
	cmd.Dir = searchPath
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return &Result{Success: true, Data: "(no matches)"}
		}
		return &Result{Success: false, Error: fmt.Sprintf("grep: %v", err)}
	}
	return &Result{Success: true, Data: string(out)}
}

type BashTool struct{}

func NewBashTool() *BashTool { return &BashTool{} }

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Description() string {
	return "Execute a shell command. Returns stdout and stderr."
}

func (t *BashTool) InputSchema() provider.InputSchema {
	return provider.InputSchema{
		Type: "object",
		Properties: map[string]any{
			"command": map[string]any{"type": "string", "description": "Shell command to execute"},
			"workdir": map[string]any{"type": "string", "description": "Working directory"},
			"timeout": map[string]any{"type": "integer", "description": "Timeout in seconds"},
		},
		Required: []string{"command"},
	}
}

func (t *BashTool) Execute(ctx context.Context, args map[string]any, toolCtx *Context) *Result {
	cmdStr, _ := args["command"].(string)
	if cmdStr == "" {
		return &Result{Success: false, Error: "command is required"}
	}
	workdir, _ := args["workdir"].(string)
	if workdir == "" && toolCtx != nil {
		workdir = toolCtx.Workdir
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &Result{
			Success: false,
			Data:    string(out),
			Error:   fmt.Sprintf("exit: %v", err),
		}
	}
	return &Result{Success: true, Data: string(out)}
}

type WriteTool struct{}

func NewWriteTool() *WriteTool { return &WriteTool{} }

func (t *WriteTool) Name() string { return "write" }

func (t *WriteTool) Description() string {
	return "Create a new file or overwrite an existing one."
}

func (t *WriteTool) InputSchema() provider.InputSchema {
	return provider.InputSchema{
		Type: "object",
		Properties: map[string]any{
			"filePath": map[string]any{"type": "string", "description": "Absolute path to the file"},
			"content":  map[string]any{"type": "string", "description": "File content"},
		},
		Required: []string{"filePath", "content"},
	}
}

func (t *WriteTool) Execute(ctx context.Context, args map[string]any, toolCtx *Context) *Result {
	fp, _ := args["filePath"].(string)
	content, _ := args["content"].(string)
	if fp == "" {
		return &Result{Success: false, Error: "filePath is required"}
	}
	resolved := fp
	if !filepath.IsAbs(fp) && toolCtx != nil {
		resolved = filepath.Join(toolCtx.Workdir, fp)
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return &Result{Success: false, Error: fmt.Sprintf("mkdir: %v", err)}
	}
	if err := os.WriteFile(resolved, []byte(content), 0644); err != nil {
		return &Result{Success: false, Error: fmt.Sprintf("write: %v", err)}
	}
	return &Result{Success: true, Data: fmt.Sprintf("wrote %s (%d bytes)", fp, len(content))}
}

type EditTool struct{}

func NewEditTool() *EditTool { return &EditTool{} }

func (t *EditTool) Name() string { return "edit" }

func (t *EditTool) Description() string {
	return "Edit a file by replacing exact text with new text."
}

func (t *EditTool) InputSchema() provider.InputSchema {
	return provider.InputSchema{
		Type: "object",
		Properties: map[string]any{
			"filePath":  map[string]any{"type": "string", "description": "Absolute path to the file"},
			"oldString": map[string]any{"type": "string", "description": "Text to replace"},
			"newString": map[string]any{"type": "string", "description": "Replacement text"},
		},
		Required: []string{"filePath", "oldString", "newString"},
	}
}

func (t *EditTool) Execute(ctx context.Context, args map[string]any, toolCtx *Context) *Result {
	fp, _ := args["filePath"].(string)
	oldStr, _ := args["oldString"].(string)
	newStr, _ := args["newString"].(string)
	if fp == "" || oldStr == "" {
		return &Result{Success: false, Error: "filePath and oldString are required"}
	}
	resolved := fp
	if !filepath.IsAbs(fp) && toolCtx != nil {
		resolved = filepath.Join(toolCtx.Workdir, fp)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return &Result{Success: false, Error: fmt.Sprintf("read: %v", err)}
	}
	content := string(data)
	if !strings.Contains(content, oldStr) {
		return &Result{Success: false, Error: "oldString not found in file"}
	}
	content = strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(resolved, []byte(content), 0644); err != nil {
		return &Result{Success: false, Error: fmt.Sprintf("write: %v", err)}
	}
	return &Result{Success: true, Data: fmt.Sprintf("edited %s", fp)}
}

type WebFetchTool struct{}

func NewWebFetchTool() *WebFetchTool { return &WebFetchTool{} }

func (t *WebFetchTool) Name() string { return "web" }

func (t *WebFetchTool) Description() string {
	return "Fetch content from a URL."
}

func (t *WebFetchTool) InputSchema() provider.InputSchema {
	return provider.InputSchema{
		Type: "object",
		Properties: map[string]any{
			"url":    map[string]any{"type": "string", "description": "URL to fetch"},
			"format": map[string]any{"type": "string", "description": "text, markdown, or html"},
		},
		Required: []string{"url"},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, args map[string]any, toolCtx *Context) *Result {
	url, _ := args["url"].(string)
	if url == "" {
		return &Result{Success: false, Error: "url is required"}
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return &Result{Success: false, Error: fmt.Sprintf("request: %v", err)}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return &Result{Success: false, Error: fmt.Sprintf("fetch: %v", err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &Result{Success: false, Error: fmt.Sprintf("read: %v", err)}
	}
	return &Result{Success: true, Data: string(body)}
}

type SkillTool struct{}

func NewSkillTool() *SkillTool { return &SkillTool{} }

func (t *SkillTool) Name() string { return "skill" }

func (t *SkillTool) Description() string {
	return "Load a skill for specialized tasks. Skills are reusable instruction sets. Use 'skill list' to see available skills, 'skill load <name>' to load one, 'skill unload <name>' to unload."
}

func (t *SkillTool) InputSchema() provider.InputSchema {
	return provider.InputSchema{
		Type: "object",
		Properties: map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action: list, load, unload, search",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Skill name (required for load/unload)",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Search query (for search action)",
			},
		},
		Required: []string{"action"},
	}
}

func (t *SkillTool) Execute(ctx context.Context, args map[string]any, toolCtx *Context) *Result {
	action, _ := args["action"].(string)
	name, _ := args["name"].(string)
	query, _ := args["query"].(string)

	if toolCtx.Skills == nil {
		return &Result{Success: false, Error: "skills not initialized"}
	}

	switch action {
	case "list":
		skills := toolCtx.Skills.List()
		if len(skills) == 0 {
			return &Result{Success: true, Data: "No skills found. Place SKILL.md files in .skills/, skills/, or ~/.edcode/skills/"}
		}
		var lines []string
		for _, s := range skills {
			status := "unloaded"
			if toolCtx.Skills.IsLoaded(s.Name) {
				status = "loaded"
			}
			lines = append(lines, fmt.Sprintf("- **%s** [%s]: %s", s.Name, status, s.Description))
		}
		return &Result{Success: true, Data: strings.Join(lines, "\n")}

	case "load":
		if name == "" {
			return &Result{Success: false, Error: "name is required for load action"}
		}
		content := toolCtx.Skills.Load(name)
		return &Result{Success: true, Data: content}

	case "unload":
		if name == "" {
			return &Result{Success: false, Error: "name is required for unload action"}
		}
		toolCtx.Skills.Unload(name)
		return &Result{Success: true, Data: fmt.Sprintf("Skill %q unloaded", name)}

	case "search":
		if query == "" {
			return &Result{Success: false, Error: "query is required for search action"}
		}
		results := toolCtx.Skills.Search(query)
		if len(results) == 0 {
			return &Result{Success: true, Data: fmt.Sprintf("No skills found matching %q", query)}
		}
		var lines []string
		for _, s := range results {
			lines = append(lines, fmt.Sprintf("- **%s**: %s", s.Name, s.Description))
		}
		return &Result{Success: true, Data: strings.Join(lines, "\n")}

	default:
		return &Result{Success: false, Error: fmt.Sprintf("unknown action %q. Use: list, load, unload, search", action)}
	}
}

