package ctxmgr

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/edmundo/edcode/internal/memory"
	"github.com/edmundo/edcode/internal/provider"
)

type CompactionLevel int

const (
	CompactionNone       CompactionLevel = 0
	CompactionLight      CompactionLevel = 1
	CompactionAggressive CompactionLevel = 2
)

type Manager struct {
	mu             sync.RWMutex
	maxTokens      int
	reservedTokens int
	memoryStore    *memory.MemoryStore
}

func NewManager(maxTokens int, memStore *memory.MemoryStore) *Manager {
	return &Manager{
		maxTokens:      maxTokens,
		reservedTokens: maxTokens / 5,
		memoryStore:    memStore,
	}
}

func (m *Manager) estimateTokens(msg provider.Message) int {
	total := 0
	for _, c := range msg.Content {
		total += len(c.Text) / 4
	}
	return total
}

func (m *Manager) estimateMessages(msgs []provider.Message) int {
	total := 0
	for _, msg := range msgs {
		total += m.estimateTokens(msg)
	}
	return total
}

func (m *Manager) ShouldCompact(msgs []provider.Message) bool {
	return m.estimateMessages(msgs) > m.maxTokens-m.reservedTokens
}

type CompactResult struct {
	Messages        []provider.Message
	Summary         string
	Compacted       bool
	TokensSaved     int
	CompactionLevel CompactionLevel
}

func (m *Manager) Compact(msgs []provider.Message, level CompactionLevel) *CompactResult {
	if !m.ShouldCompact(msgs) && level == CompactionNone {
		return &CompactResult{Messages: msgs, Compacted: false}
	}
	if level == CompactionNone {
		level = CompactionLight
	}
	beforeTokens := m.estimateMessages(msgs)
	switch level {
	case CompactionAggressive:
		return m.compactAggressive(msgs, beforeTokens)
	default:
		return m.compactLight(msgs, beforeTokens)
	}
}

func (m *Manager) compactLight(msgs []provider.Message, beforeTokens int) *CompactResult {
	tailCount := 5
	if len(msgs) < tailCount*2 {
		return &CompactResult{Messages: msgs, Compacted: false}
	}
	tail := msgs[len(msgs)-tailCount:]
	head := msgs[:len(msgs)-tailCount]
	summary := m.summarizeMessages(head)
	summaryMsg := provider.Message{
		Role: provider.RoleSystem,
		Content: []provider.Content{{
			Type: provider.ContentText,
			Text: fmt.Sprintf("[Compacted - previous messages summarized]\n%s", summary),
		}},
	}
	result := append([]provider.Message{summaryMsg}, tail...)
	return &CompactResult{
		Messages:        result,
		Summary:         summary,
		Compacted:       true,
		TokensSaved:     beforeTokens - m.estimateMessages(result),
		CompactionLevel: CompactionLight,
	}
}

func (m *Manager) compactAggressive(msgs []provider.Message, beforeTokens int) *CompactResult {
	tailCount := 3
	if len(msgs) < tailCount*2 {
		return m.compactLight(msgs, beforeTokens)
	}
	tail := msgs[len(msgs)-tailCount:]
	head := msgs[:len(msgs)-tailCount]
	summary := m.summarizeMessages(head)
	summaryMsg := provider.Message{
		Role: provider.RoleSystem,
		Content: []provider.Content{{
			Type: provider.ContentText,
			Text: fmt.Sprintf("[Aggressively compacted]\n%s", summary),
		}},
	}
	result := append([]provider.Message{summaryMsg}, tail...)
	return &CompactResult{
		Messages:        result,
		Summary:         summary,
		Compacted:       true,
		TokensSaved:     beforeTokens - m.estimateMessages(result),
		CompactionLevel: CompactionAggressive,
	}
}

func (m *Manager) summarizeMessages(msgs []provider.Message) string {
	var parts []string
	for _, msg := range msgs {
		role := string(msg.Role)
		for _, c := range msg.Content {
			switch c.Type {
			case provider.ContentText:
				text := c.Text
				if len(text) > 200 {
					text = text[:200] + "..."
				}
				if text != "" {
					parts = append(parts, fmt.Sprintf("[%s] %s", role, text))
				}
			case provider.ContentToolCall:
				parts = append(parts, fmt.Sprintf("[%s] tool: %s(%s)", role, c.ToolCall.Name, truncate(c.ToolCall.Arguments, 80)))
			case provider.ContentToolResult:
				parts = append(parts, fmt.Sprintf("[%s] result: %s", role, truncate(c.ToolResult.Result, 80)))
			}
		}
	}
	if len(parts) == 0 {
		return "(empty)"
	}
	return strings.Join(parts, "\n")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func (m *Manager) EnrichWithMemory(systemPrompt string) string {
	ctx := m.memoryStore.WorkingContext()
	instructions := m.memoryStore.GetInstructions()
	var enrichments []string
	if ctx != "" {
		enrichments = append(enrichments, ctx)
	}
	if instructions != "" {
		enrichments = append(enrichments, instructions)
	}
	if len(enrichments) == 0 {
		return systemPrompt
	}
	return systemPrompt + "\n\n" + strings.Join(enrichments, "\n\n")
}

func (m *Manager) AutoCompact(msgs []provider.Message, force bool) *CompactResult {
	if !force && !m.ShouldCompact(msgs) {
		return &CompactResult{Messages: msgs, Compacted: false}
	}
	total := m.estimateMessages(msgs)
	ratio := float64(total) / float64(m.maxTokens)
	level := CompactionLight
	if ratio > 0.85 {
		level = CompactionAggressive
	}
	return m.Compact(msgs, level)
}

var _ = memory.MemoryStore{}
var _ = time.Second
var _ = provider.RoleSystem
var _ = provider.ContentText
var _ = provider.ContentToolCall
var _ = provider.ContentToolResult
var _ = sync.RWMutex{}
