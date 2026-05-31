package memory

import (
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Level string

const (
	LevelWorking Level = "working"
	LevelSession Level = "session"
	LevelLongTerm Level = "long_term"
)

type MemoryEntry struct {
	ID        string          `json:"id"`
	Level     Level           `json:"level"`
	Content   string          `json:"content"`
	Type      string          `json:"type"`
	Tags      []string        `json:"tags"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	Score     float64         `json:"score"`
	Source    string          `json:"source"` // "workspace" or "global"
}

type MemoryStore struct {
	mu        sync.RWMutex
	entries   []MemoryEntry
	dataDir   string
	sessionID string
	globalDir string
}

func NewMemoryStore(dataDir, globalDir string) *MemoryStore {
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".edcode-workspace", "memory")
	}
	if globalDir == "" {
		home, _ := os.UserHomeDir()
		globalDir = filepath.Join(home, ".edcode", "memory")
	}
	os.MkdirAll(dataDir, 0755)
	os.MkdirAll(globalDir, 0755)
	return &MemoryStore{
		entries:   []MemoryEntry{},
		dataDir:   dataDir,
		globalDir: globalDir,
	}
}

func (ms *MemoryStore) SetSession(id string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.sessionID = id
}

func (ms *MemoryStore) Add(entry MemoryEntry) string {
	entry.ID = fmt.Sprintf("mem_%s_%d", ms.sessionID, time.Now().UnixNano())
	entry.CreatedAt = time.Now()
	entry.UpdatedAt = entry.CreatedAt
	if entry.Level == "" {
		entry.Level = LevelWorking
	}
	ms.mu.Lock()
	ms.entries = append(ms.entries, entry)
	if entry.Level == LevelSession || entry.Level == LevelLongTerm {
		go ms.persistEntry(entry)
	}
	ms.mu.Unlock()
	return entry.ID
}

func (ms *MemoryStore) Get(id string) *MemoryEntry {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	for i := range ms.entries {
		if ms.entries[i].ID == id {
			return &ms.entries[i]
		}
	}
	return nil
}

func (ms *MemoryStore) Search(query string, tags []string, limit int) []MemoryEntry {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	query = strings.ToLower(query)
	var results []MemoryEntry
	for _, e := range ms.entries {
		score := 0.0
		if query != "" {
			if strings.Contains(strings.ToLower(e.Content), query) {
				score += 5.0
			}
			for _, t := range e.Tags {
				if strings.Contains(strings.ToLower(t), query) {
					score += 3.0
				}
			}
		}
		if len(tags) > 0 {
			tagMatch := 0
			for _, t := range tags {
				for _, et := range e.Tags {
					if strings.EqualFold(t, et) {
						tagMatch++
					}
				}
			}
			if tagMatch == 0 && query == "" {
				continue
			}
			score += float64(tagMatch) * 2.0
		}
		if query == "" && len(tags) == 0 {
			score = 1.0
		}
		e.Score = score
		if score > 0 {
			results = append(results, e)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results
}

func (ms *MemoryStore) Recall(query string) []MemoryEntry {
	return ms.Search(query, nil, 10)
}

func (ms *MemoryStore) Remember(level Level, content string, memType string, tags []string) string {
	return ms.Add(MemoryEntry{
		Level:   level,
		Content: content,
		Type:    memType,
		Tags:    tags,
	})
}

func (ms *MemoryStore) AddToolResult(toolName string, input, output string) {
	ms.Add(MemoryEntry{
		Level:   LevelWorking,
		Content: fmt.Sprintf("Tool %s: %s -> %s", toolName, input, output),
		Type:    "tool_result",
		Tags:    []string{"tool", toolName},
	})
}

func (ms *MemoryStore) AddInsight(insight string, tags []string) string {
	return ms.Add(MemoryEntry{
		Level:   LevelLongTerm,
		Content: insight,
		Type:    "insight",
		Tags:    append(tags, "insight"),
	})
}

func (ms *MemoryStore) AddLesson(lesson string, tags []string) string {
	return ms.Add(MemoryEntry{
		Level:   LevelLongTerm,
		Content: lesson,
		Type:    "lesson",
		Tags:    append(tags, "lesson"),
	})
}

func (ms *MemoryStore) WorkingContext() string {
	entries := ms.Search("", []string{}, 20)
	var parts []string
	for _, e := range entries {
		if e.Type == "insight" || e.Type == "lesson" || e.Type == "decision" {
			parts = append(parts, fmt.Sprintf("- [%s] %s", e.Type, e.Content))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "## Context from Past Sessions\n" + strings.Join(parts, "\n")
}

func (ms *MemoryStore) GetInstructions() string {
	entries := ms.Search("", []string{"lesson", "rule", "preference"}, 10)
	var parts []string
	for _, e := range entries {
		if e.Level == LevelLongTerm {
			parts = append(parts, fmt.Sprintf("- %s (%s)", e.Content, strings.Join(e.Tags, ", ")))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "## Learned Instructions\n" + strings.Join(parts, "\n")
}

func (ms *MemoryStore) persistEntry(entry MemoryEntry) {
	// Persist to workspace dir for session-level, global dir for long-term
	var dir string
	if entry.Level == LevelSession {
		dir = ms.dataDir
	} else {
		dir = ms.globalDir
	}
	path := filepath.Join(dir, fmt.Sprintf("session_%s.gob", ms.sessionID))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	enc := gob.NewEncoder(f)
	enc.Encode(entry)
}

func (ms *MemoryStore) LoadSession(sessionID string) error {
	// Load from workspace dir
	path := filepath.Join(ms.dataDir, fmt.Sprintf("session_%s.gob", sessionID))
	if err := ms.loadFromFile(path); err != nil {
		return err
	}
	// Load from global dir
	globalPath := filepath.Join(ms.globalDir, fmt.Sprintf("session_%s.gob", sessionID))
	return ms.loadFromFile(globalPath)
}

func (ms *MemoryStore) loadFromFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	dec := gob.NewDecoder(f)
	ms.mu.Lock()
	defer ms.mu.Unlock()
	for {
		var entry MemoryEntry
		if err := dec.Decode(&entry); err != nil {
			break
		}
		ms.entries = append(ms.entries, entry)
	}
	return nil
}

func (ms *MemoryStore) LoadAllSessions() error {
	// Load from workspace dir
	files, err := filepath.Glob(filepath.Join(ms.dataDir, "session_*.gob"))
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := ms.loadFromFile(f); err != nil {
			continue
		}
	}
	// Load from global dir
	globalFiles, err := filepath.Glob(filepath.Join(ms.globalDir, "session_*.gob"))
	if err != nil {
		return err
	}
	for _, f := range globalFiles {
		if err := ms.loadFromFile(f); err != nil {
			continue
		}
	}
	return nil
}

func (ms *MemoryStore) SetWorkingMemory(key, value string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	for i := range ms.entries {
		if ms.entries[i].Type == "working_"+key {
			ms.entries[i].Content = value
			ms.entries[i].UpdatedAt = time.Now()
			return
		}
	}
	ms.entries = append(ms.entries, MemoryEntry{
		ID:        fmt.Sprintf("work_%s", key),
		Level:     LevelWorking,
		Content:   value,
		Type:      "working_" + key,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
}

func (ms *MemoryStore) GetWorkingMemory(key string) string {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	for _, e := range ms.entries {
		if e.Type == "working_"+key {
			return e.Content
		}
	}
	return ""
}

func (ms *MemoryStore) SummarizeRecent(since time.Time) string {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	var actions []string
	for _, e := range ms.entries {
		if e.Level == LevelWorking && e.CreatedAt.After(since) {
			actions = append(actions, e.Content)
		}
	}
	if len(actions) == 0 {
		return "(no recent activity)"
	}
	return strings.Join(actions, "\n")
}

type SessionSummary struct {
	SessionID    string    `json:"session_id"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at"`
	Goals        []string  `json:"goals"`
	Achieved     []string  `json:"achieved"`
	KeyDecisions []string  `json:"key_decisions"`
	Lessons      []string  `json:"lessons"`
	FilesChanged int       `json:"files_changed"`
	TokenUsage   int       `json:"token_usage"`
}

func (ms *MemoryStore) SaveSessionSummary(summary SessionSummary) {
	data := fmt.Sprintf(`Session: %s
  Started: %s | Ended: %s
  Files changed: %d | Tokens used: %d
  Goals: %s
  Achieved: %s
  Decisions: %s
  Lessons: %s
`,
		summary.SessionID,
		summary.StartedAt.Format(time.RFC3339), summary.EndedAt.Format(time.RFC3339),
		summary.FilesChanged, summary.TokenUsage,
		strings.Join(summary.Goals, ", "),
		strings.Join(summary.Achieved, ", "),
		strings.Join(summary.KeyDecisions, "; "),
		strings.Join(summary.Lessons, "; "),
	)
	ms.Add(MemoryEntry{
		Level:   LevelLongTerm,
		Content: data,
		Type:    "session_summary",
		Tags:    []string{"session", "summary"},
	})
}
