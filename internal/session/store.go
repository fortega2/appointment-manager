package session

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

const (
	CookieName = "session_id"

	sessionDuration = 24 * time.Hour
	bytesPerSession = 32
	tickerInterval  = 10 * time.Minute
)

type Session struct {
	UserID    string
	Email     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type Store struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewStore() *Store {
	s := &Store{
		sessions: make(map[string]*Session),
	}

	go s.cleanupLoop()
	return s
}

func (s *Store) Create(userID, email string) (string, error) {
	id, err := generateID()
	if err != nil {
		return "", fmt.Errorf("create: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[id] = &Session{
		UserID:    userID,
		Email:     email,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(sessionDuration),
	}

	return id, nil
}

func (s *Store) Get(id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("get: %w", ErrSessionNotFound)
	}
	if time.Now().After(session.ExpiresAt) {
		return nil, fmt.Errorf("get: %w", ErrSessionExpired)
	}
	return session, nil
}

func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *Store) cleanupLoop() {
	ticker := time.NewTicker(tickerInterval)
	defer ticker.Stop()
	for range ticker.C {
		s.cleanup()
	}
}

func (s *Store) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
}

func generateID() (string, error) {
	b := make([]byte, bytesPerSession)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate ID: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
