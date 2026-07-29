package bugreports

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MemoryStore is an in-process Store for tests. Not for production use.
type MemoryStore struct {
	mu     sync.Mutex
	nextID int
	byID   map[string]BugReport
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: map[string]BugReport{}}
}

func (s *MemoryStore) UpsertFromFailure(_ context.Context, in UpsertInput) (BugReport, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, b := range s.byID {
		if b.ProjectID == in.ProjectID && b.TestCaseID == in.TestCaseID && b.FailureType == in.FailureType {
			b.FailureID = in.FailureID
			b.Title = in.Title
			b.Severity = in.Severity
			b.AffectedService = in.AffectedService
			b.AffectedRoute = in.AffectedRoute
			b.Preconditions = in.Preconditions
			b.StepsToReproduce = in.StepsToReproduce
			b.ExpectedResult = in.ExpectedResult
			b.ActualResult = in.ActualResult
			b.Evidence = in.Evidence
			b.RootCauseHypothesis = in.RootCauseHypothesis
			b.RootCauseConfidence = in.RootCauseConfidence
			b.FlakyAssessment = in.FlakyAssessment
			b.RelatedGraphPath = in.RelatedGraphPath
			b.RegressionTestIDs = in.RegressionTestIDs
			b.LastObservedAt = in.ObservedAt
			b.Frequency++
			if b.Status == StatusResolved {
				b.Status = StatusReopened
			}
			b.UpdatedAt = in.ObservedAt
			s.byID[id] = b
			return b, false, nil
		}
	}

	var duplicateOf string
	for _, b := range s.byID {
		if b.ProjectID == in.ProjectID && b.TestCaseID != in.TestCaseID && b.FailureType == in.FailureType && b.Status == StatusOpen {
			duplicateOf = b.ID
			break
		}
	}

	s.nextID++
	bug := BugReport{
		ID: fmt.Sprintf("mem-bug-%d", s.nextID), ProjectID: in.ProjectID, FailureID: in.FailureID,
		TestCaseID: in.TestCaseID, EnvironmentID: in.EnvironmentID,
		Title: in.Title, Severity: in.Severity, FailureType: in.FailureType,
		AffectedService: in.AffectedService, AffectedRoute: in.AffectedRoute,
		Preconditions: in.Preconditions, StepsToReproduce: in.StepsToReproduce,
		ExpectedResult: in.ExpectedResult, ActualResult: in.ActualResult, Evidence: in.Evidence,
		FirstObservedAt: in.ObservedAt, LastObservedAt: in.ObservedAt, Frequency: 1,
		RootCauseHypothesis: in.RootCauseHypothesis, RootCauseConfidence: in.RootCauseConfidence,
		FlakyAssessment: in.FlakyAssessment, RelatedGraphPath: in.RelatedGraphPath,
		RegressionTestIDs: in.RegressionTestIDs, PossibleDuplicateOfID: duplicateOf,
		Status: StatusOpen, CreatedAt: in.ObservedAt, UpdatedAt: in.ObservedAt,
	}
	s.byID[bug.ID] = bug
	return bug, true, nil
}

func (s *MemoryStore) List(_ context.Context, filter ListFilter) ([]BugReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []BugReport
	for _, b := range s.byID {
		if filter.ProjectID != "" && b.ProjectID != filter.ProjectID {
			continue
		}
		if filter.Severity != "" && b.Severity != filter.Severity {
			continue
		}
		if filter.Status != "" && b.Status != filter.Status {
			continue
		}
		if filter.EnvironmentID != "" && b.EnvironmentID != filter.EnvironmentID {
			continue
		}
		if filter.Search != "" && !strings.Contains(strings.ToLower(b.Title), strings.ToLower(filter.Search)) {
			continue
		}
		out = append(out, b)
	}
	return out, nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (BugReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.byID[id]
	if !ok {
		return BugReport{}, ErrNotFound
	}
	return b, nil
}

func (s *MemoryStore) UpdateStatus(_ context.Context, id, status string) (BugReport, error) {
	if !ValidStatus(status) {
		return BugReport{}, ErrInvalidStatus
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.byID[id]
	if !ok {
		return BugReport{}, ErrNotFound
	}
	b.Status = status
	b.UpdatedAt = time.Now()
	s.byID[id] = b
	return b, nil
}

func (s *MemoryStore) AddNote(_ context.Context, id, author, text string) (BugReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.byID[id]
	if !ok {
		return BugReport{}, ErrNotFound
	}
	b.Notes = append(b.Notes, Note{Author: author, Text: text, CreatedAt: time.Now()})
	b.UpdatedAt = time.Now()
	s.byID[id] = b
	return b, nil
}
