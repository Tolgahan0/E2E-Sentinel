package auth

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryStore is an in-process Store for tests. Not for production use.
type MemoryStore struct {
	mu            sync.Mutex
	nextUserID    int
	nextSessionID int
	usersByID     map[string]User
	sessionsByID  map[string]Session
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{usersByID: map[string]User{}, sessionsByID: map[string]Session{}}
}

func (s *MemoryStore) CreateUser(_ context.Context, u User) (User, error) {
	if !ValidRole(u.Role) {
		return User{}, ErrInvalidRole
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.usersByID {
		if strings.EqualFold(existing.Email, u.Email) {
			return User{}, ErrEmailTaken
		}
	}
	s.nextUserID++
	u.ID = fmt.Sprintf("mem-user-%d", s.nextUserID)
	u.CreatedAt = time.Now()
	u.UpdatedAt = u.CreatedAt
	s.usersByID[u.ID] = u
	return u, nil
}

func (s *MemoryStore) GetUserByEmail(_ context.Context, email string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.usersByID {
		if strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

func (s *MemoryStore) GetUserByID(_ context.Context, id string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.usersByID[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (s *MemoryStore) CountUsers(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.usersByID), nil
}

func (s *MemoryStore) ListUsers(_ context.Context) ([]User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	users := make([]User, 0, len(s.usersByID))
	for _, u := range s.usersByID {
		users = append(users, u)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Email < users[j].Email })
	return users, nil
}

func (s *MemoryStore) CreateSession(_ context.Context, sess Session) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextSessionID++
	sess.ID = fmt.Sprintf("mem-session-%d", s.nextSessionID)
	sess.CreatedAt = time.Now()
	s.sessionsByID[sess.ID] = sess
	return sess, nil
}

func (s *MemoryStore) GetSessionByTokenHash(_ context.Context, tokenHash string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessionsByID {
		if sess.TokenHash == tokenHash {
			if time.Now().After(sess.ExpiresAt) {
				return Session{}, ErrSessionExpired
			}
			return sess, nil
		}
	}
	return Session{}, ErrSessionNotFound
}

func (s *MemoryStore) DeleteSession(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessionsByID, id)
	return nil
}
