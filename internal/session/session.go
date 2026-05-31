package session

import (
	"fmt"
	"sync"
	"time"

	"github.com/edmundo/edcode/internal/provider"
)

type Session struct {
	ID        string
	ParentID  string
	CreatedAt time.Time
	Messages  []provider.Message
	mu        sync.RWMutex
	Metadata  map[string]string
}

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

func (m *Manager) Create(parentID string) *Session {
	s := &Session{
		ID:        fmt.Sprintf("ses_%d", time.Now().UnixNano()),
		ParentID:  parentID,
		CreatedAt: time.Now(),
		Messages:  []provider.Message{},
		Metadata:  make(map[string]string),
	}
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	return s
}

func (m *Manager) Get(id string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

func (m *Manager) AddMessage(sessionID string, msg provider.Message) {
	s := m.Get(sessionID)
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = append(s.Messages, msg)
}

func (m *Manager) GetMessages(sessionID string) []provider.Message {
	s := m.Get(sessionID)
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make([]provider.Message, len(s.Messages))
	copy(cp, s.Messages)
	return cp
}

func (m *Manager) Delete(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

func (s *Session) ContextWindow(maxTokens int) []provider.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Messages) == 0 {
		return nil
	}
	tokens := 0
	end := len(s.Messages)
	for i := len(s.Messages) - 1; i >= 0; i-- {
		for _, c := range s.Messages[i].Content {
			tokens += len(c.Text) / 4
		}
		if tokens > maxTokens && i > 0 {
			end = i + 1
			break
		}
	}
	cp := make([]provider.Message, len(s.Messages[end-1:]))
	copy(cp, s.Messages[end-1:])
	return cp
}
