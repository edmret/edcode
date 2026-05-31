package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WorkspaceManager manages workspace detection, indexing, and AGENTS.md loading.
type WorkspaceManager struct {
	homeDir   string
	workdir   string
	index     []WorkspaceEntry
	indexPath string
}

// WorkspaceEntry represents a single workspace in the index.
type WorkspaceEntry struct {
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	Added    time.Time `json:"added"`
	LastUsed time.Time `json:"last_used"`
}

// Info holds the resolved workspace information.
type Info struct {
	Path         string
	Name         string
	AgentsMD     string // content of AGENTS.md if found
	AgentsMDPath string // path to AGENTS.md
	HasWorkspace bool   // whether .edcode-workspace/ exists
	WorkspaceDir string // path to .edcode-workspace/
	EnhanceDir   string // path to .edcode-workspace/enhance/
	MemoryDir    string // path to .edcode-workspace/memory/
	GlobalMemory string // path to ~/.edcode/memory/
	GlobalEnhance string // path to ~/.edcode/enhance/
}

const indexFileName = "workspaces.json"
const WorkspaceDirName = ".edcode-workspace"

// New creates a WorkspaceManager for the given workdir.
func New(workdir string) *WorkspaceManager {
	home, _ := os.UserHomeDir()
	return &WorkspaceManager{
		homeDir:   home,
		workdir:   workdir,
		indexPath: filepath.Join(home, ".edcode", indexFileName),
	}
}

// LoadInfo returns the workspace info for the current workdir.
func (wm *WorkspaceManager) LoadInfo() *Info {
	info := &Info{
		Path:         wm.workdir,
		Name:         filepath.Base(wm.workdir),
		HasWorkspace: wm.hasWorkspace(),
	}

	if info.HasWorkspace {
		info.WorkspaceDir = filepath.Join(wm.workdir, WorkspaceDirName)
		info.EnhanceDir = filepath.Join(info.WorkspaceDir, "enhance")
		info.MemoryDir = filepath.Join(info.WorkspaceDir, "memory")
	}

	info.GlobalMemory = filepath.Join(wm.homeDir, ".edcode", "memory")
	info.GlobalEnhance = filepath.Join(wm.homeDir, ".edcode", "enhance")

	// Load AGENTS.md
	agentsPath := filepath.Join(wm.workdir, "AGENTS.md")
	if data, err := os.ReadFile(agentsPath); err == nil {
		info.AgentsMD = string(data)
		info.AgentsMDPath = agentsPath
	}

	// Register this workspace in the index
	wm.register()

	return info
}

// hasWorkspace checks if .edcode-workspace/ exists in the workdir.
func (wm *WorkspaceManager) hasWorkspace() bool {
	p := filepath.Join(wm.workdir, WorkspaceDirName)
	if info, err := os.Stat(p); err == nil {
		return info.IsDir()
	}
	return false
}

// EnsureWorkspace creates .edcode-workspace/ and its subdirectories.
func (wm *WorkspaceManager) EnsureWorkspace() error {
	dir := filepath.Join(wm.workdir, WorkspaceDirName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create workspace dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "enhance"), 0755); err != nil {
		return fmt.Errorf("create workspace enhance dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "memory"), 0755); err != nil {
		return fmt.Errorf("create workspace memory dir: %w", err)
	}
	return nil
}

// List returns all registered workspaces.
func (wm *WorkspaceManager) List() []WorkspaceEntry {
	wm.loadIndex()
	return wm.index
}

// Prune removes entries from the index where .edcode-workspace/ no longer exists.
func (wm *WorkspaceManager) Prune() []string {
	wm.loadIndex()
	removed := []string{}
	kept := []WorkspaceEntry{}
	for _, e := range wm.index {
		if wm.workspaceExists(e.Path) {
			kept = append(kept, e)
		} else {
			removed = append(removed, e.Path)
		}
	}
	wm.index = kept
	wm.saveIndex()
	return removed
}

// ListText returns a formatted string of all registered workspaces.
func (wm *WorkspaceManager) ListText() string {
	entries := wm.List()
	if len(entries) == 0 {
		return "No workspaces registered. Run edcode in a project directory to register it."
	}
	var out string
	for i, e := range entries {
		out += fmt.Sprintf("  %d. %s (%s)\n", i+1, e.Name, e.Path)
	}
	return out
}

// register adds or updates the current workdir in the index.
func (wm *WorkspaceManager) register() {
	wm.loadIndex()
	now := time.Now()
	found := false
	for i, e := range wm.index {
		if e.Path == wm.workdir {
			wm.index[i].LastUsed = now
			found = true
			break
		}
	}
	if !found {
		wm.index = append(wm.index, WorkspaceEntry{
			Path:     wm.workdir,
			Name:     filepath.Base(wm.workdir),
			Added:    now,
			LastUsed: now,
		})
	}
	wm.saveIndex()
}

// loadIndex reads the workspace index from disk.
func (wm *WorkspaceManager) loadIndex() {
	if wm.index != nil {
		return
	}
	data, err := os.ReadFile(wm.indexPath)
	if err != nil {
		wm.index = []WorkspaceEntry{}
		return
	}
	if err := json.Unmarshal(data, &wm.index); err != nil {
		wm.index = []WorkspaceEntry{}
		return
	}
}

// saveIndex writes the workspace index to disk.
func (wm *WorkspaceManager) saveIndex() {
	os.MkdirAll(filepath.Dir(wm.indexPath), 0755)
	data, err := json.MarshalIndent(wm.index, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(wm.indexPath, data, 0644)
}

// workspaceExists checks if .edcode-workspace/ exists in the given path.
func (wm *WorkspaceManager) workspaceExists(path string) bool {
	p := filepath.Join(path, WorkspaceDirName)
	if info, err := os.Stat(p); err == nil {
		return info.IsDir()
	}
	return false
}
