package session

import (
	"errors"
	"sort"
	"sync"
)

var ErrNotFound = errors.New("会话不存在")

type Store interface {
	List() []Session
	Get(id string) (Session, error)
	Create(Session) error
}

// MemoryStore is a temporary draft store; application exit discards all drafts.
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]Session
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sessions: make(map[string]Session)}
}

func (s *MemoryStore) List() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Session, 0, len(s.sessions))
	for _, item := range s.sessions {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

func (s *MemoryStore) Get(id string) (Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.sessions[id]
	if !ok {
		return Session{}, ErrNotFound
	}
	return item, nil
}

func (s *MemoryStore) Create(item Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[item.ID]; exists {
		return errors.New("会话 ID 已存在")
	}
	s.sessions[item.ID] = item
	return nil
}
