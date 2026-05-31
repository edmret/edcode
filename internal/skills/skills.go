package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	Files       []string `json:"files,omitempty"`
	Loaded      bool   `json:"loaded"`
}

type Manager struct {
	mu       sync.RWMutex
	skills   map[string]*Skill
	loaded   map[string]bool
	searchPaths []string
}

func NewManager(searchPaths []string) *Manager {
	m := &Manager{
		skills: make(map[string]*Skill),
		loaded: make(map[string]bool),
	}
	if len(searchPaths) == 0 {
		home, _ := os.UserHomeDir()
		searchPaths = []string{
			filepath.Join(home, ".edcode", "skills"),
			".skills",
			"skills",
		}
	}
	m.searchPaths = searchPaths
	m.Discover()
	return m
}

func (m *Manager) Discover() {
	for _, path := range m.searchPaths {
		m.discoverInPath(path)
	}
}

func (m *Manager) discoverInPath(path string) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return
	}
	m.walkSkillsDir(path)
}

func (m *Manager) walkSkillsDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		fullPath := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			m.walkSkillsDir(fullPath)
			continue
		}
		if entry.Name() == "SKILL.md" {
			m.loadSkillFromFile(fullPath)
		}
	}
}

func (m *Manager) loadSkillFromFile(filePath string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}

	skill, name := parseSkillFile(filePath, string(data))
	if skill == nil {
		return
	}

	m.mu.Lock()
	m.skills[name] = skill
	m.mu.Unlock()
}

func parseSkillFile(filePath, content string) (*Skill, string) {
	lines := strings.Split(content, "\n")
	var name, desc string
	var allowedTools []string
	var files []string
	inAllowedTools := false
	inFiles := false
	inContent := false
	var contentLines []string

	for _, line := range lines {
		if inContent {
			contentLines = append(contentLines, line)
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			name = strings.TrimPrefix(trimmed, "# ")
		} else if strings.HasPrefix(trimmed, "## Description") || strings.HasPrefix(trimmed, "##description") {
			// next non-empty line is description
		} else if strings.HasPrefix(trimmed, "- ") {
			if desc == "" {
				desc = strings.TrimPrefix(trimmed, "- ")
			}
		} else if strings.HasPrefix(trimmed, "## Allowed Tools") || strings.HasPrefix(trimmed, "##allowed-tools") {
			inAllowedTools = true
			inFiles = false
		} else if strings.HasPrefix(trimmed, "## Files") || strings.HasPrefix(trimmed, "##files") {
			inFiles = true
			inAllowedTools = false
		} else if strings.HasPrefix(trimmed, "- ") && (inAllowedTools || inFiles) {
			item := strings.TrimPrefix(trimmed, "- ")
			if inAllowedTools {
				allowedTools = append(allowedTools, item)
			} else if inFiles {
				files = append(files, item)
			}
		} else if trimmed == "" && !inContent {
			inContent = true
		}
	}

	if name == "" {
		name = filepath.Base(filepath.Dir(filePath))
	}

	if desc == "" {
		desc = "A reusable skill for specialized tasks"
	}

	return &Skill{
		Name:         name,
		Description:  desc,
		AllowedTools: allowedTools,
		Files:        files,
		Content:      strings.Join(contentLines, "\n"),
	}, name
}

func (m *Manager) Get(name string) *Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.skills[name]
}

func (m *Manager) Load(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	skill, ok := m.skills[name]
	if !ok {
		return fmt.Sprintf("Skill %q not found. Run 'skills list' to see available skills.", name)
	}
	skill.Loaded = true
	m.loaded[name] = true
	return skill.Content
}

func (m *Manager) Unload(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	skill, ok := m.skills[name]
	if ok {
		skill.Loaded = false
	}
	delete(m.loaded, name)
}

func (m *Manager) IsLoaded(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loaded[name]
}

func (m *Manager) List() []Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []Skill
	for _, s := range m.skills {
		result = append(result, *s)
	}
	return result
}

func (m *Manager) Search(query string) []Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()
	query = strings.ToLower(query)
	var result []Skill
	for _, s := range m.skills {
		if strings.Contains(strings.ToLower(s.Name), query) ||
			strings.Contains(strings.ToLower(s.Description), query) {
			result = append(result, *s)
		}
	}
	return result
}

func (m *Manager) SystemPrompt() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var parts []string
	for _, s := range m.skills {
		parts = append(parts, fmt.Sprintf("- **%s**: %s", s.Name, s.Description))
	}
	if len(parts) == 0 {
		return ""
	}
	return "## Available Skills\nYou can load skills for specialized tasks using the `skill` tool. Each skill provides reusable instructions and procedures.\n\n" + strings.Join(parts, "\n")
}

func (m *Manager) LoadedInstructions() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var parts []string
	for name := range m.loaded {
		if s, ok := m.skills[name]; ok {
			parts = append(parts, s.Content)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "## Active Skills\n" + strings.Join(parts, "\n\n---\n\n")
}

var _ = filepath.Join
var _ = os.UserHomeDir
var _ = os.ReadDir
var _ = os.ReadFile
var _ = os.Stat
var _ = fmt.Sprintf
var _ = strings.Split
var _ = strings.TrimSpace
var _ = strings.HasPrefix
var _ = strings.TrimPrefix
var _ = strings.Join
var _ = strings.ToLower
var _ = strings.Contains
var _ = filepath.Base
var _ = filepath.Dir
