package fixproposals

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemoryStore is an in-process Store for tests. Not for production use.
type MemoryStore struct {
	mu     sync.Mutex
	nextID int
	byID   map[string]FixProposal
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: map[string]FixProposal{}}
}

func (s *MemoryStore) Create(_ context.Context, fp FixProposal) (FixProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	fp.ID = fmt.Sprintf("mem-fix-%d", s.nextID)
	if fp.ApprovalStatus == "" {
		fp.ApprovalStatus = StatusPendingReview
	}
	fp.CreatedAt = time.Now()
	fp.UpdatedAt = fp.CreatedAt
	s.byID[fp.ID] = fp
	return fp, nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (FixProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fp, ok := s.byID[id]
	if !ok {
		return FixProposal{}, ErrNotFound
	}
	return fp, nil
}

func (s *MemoryStore) ListByProject(_ context.Context, projectID string) ([]FixProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []FixProposal
	for _, fp := range s.byID {
		if fp.ProjectID == projectID {
			out = append(out, fp)
		}
	}
	return out, nil
}

func (s *MemoryStore) ListByBug(_ context.Context, bugID string) ([]FixProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []FixProposal
	for _, fp := range s.byID {
		if fp.BugID == bugID {
			out = append(out, fp)
		}
	}
	return out, nil
}

func (s *MemoryStore) UpdateApprovalStatus(_ context.Context, id, status string) (FixProposal, error) {
	if !ValidStatus(status) {
		return FixProposal{}, ErrInvalidStatus
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fp, ok := s.byID[id]
	if !ok {
		return FixProposal{}, ErrNotFound
	}
	fp.ApprovalStatus = status
	fp.UpdatedAt = time.Now()
	s.byID[id] = fp
	return fp, nil
}

func (s *MemoryStore) UpdateRegressionTestIDs(_ context.Context, id string, testIDs []string) (FixProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fp, ok := s.byID[id]
	if !ok {
		return FixProposal{}, ErrNotFound
	}
	fp.RegressionTestIDs = testIDs
	fp.UpdatedAt = time.Now()
	s.byID[id] = fp
	return fp, nil
}

func (s *MemoryStore) RecordWorkspaceApplication(_ context.Context, id, workspaceDir string, results []FileResult, appliedAt time.Time) (FixProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fp, ok := s.byID[id]
	if !ok {
		return FixProposal{}, ErrNotFound
	}
	fp.WorkspaceDir = workspaceDir
	fp.WorkspaceApplyResults = results
	fp.WorkspaceAppliedAt = &appliedAt
	fp.UpdatedAt = time.Now()
	s.byID[id] = fp
	return fp, nil
}

func (s *MemoryStore) RecordRepositoryApplication(_ context.Context, id string, results []FileResult, appliedAt time.Time) (FixProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fp, ok := s.byID[id]
	if !ok {
		return FixProposal{}, ErrNotFound
	}
	if fp.RepositoryAppliedAt != nil {
		return FixProposal{}, ErrAlreadyAppliedToRepository
	}
	fp.RepositoryApplyResults = results
	fp.RepositoryAppliedAt = &appliedAt
	fp.UpdatedAt = time.Now()
	s.byID[id] = fp
	return fp, nil
}
