package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
)

type memoryStore struct {
	rows            map[string]*Action
	puts            int
	failAt          int
	commitOnFailure bool
}

func newMemory() *memoryStore { return &memoryStore{rows: map[string]*Action{}} }
func clone(a *Action) *Action {
	var b Action
	_ = json.Unmarshal(jsonBytes(a), &b)
	b.InputHash = a.InputHash
	return &b
}
func (m *memoryStore) Get(_ context.Context, id string) (*Action, error) {
	a, ok := m.rows[id]
	if !ok {
		return nil, ErrNotFound
	}
	return clone(a), nil
}
func (m *memoryStore) Put(_ context.Context, a *Action) error {
	m.puts++
	if m.puts == m.failAt {
		if m.commitOnFailure {
			m.rows[a.ID] = clone(a)
		}
		return errors.New("simulated persistence timeout")
	}
	m.rows[a.ID] = clone(a)
	return nil
}
func (m *memoryStore) List(_ context.Context, _ int64, limit int) ([]Action, error) {
	all := []Action{}
	for _, a := range m.rows {
		all = append(all, *clone(a))
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedMS > all[j].CreatedMS })
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

type fakeAI struct {
	calls int
	err   error
}

func (f *fakeAI) Draft(context.Context, string) (Draft, error) {
	f.calls++
	return Draft{Title: "Filtered CSV exports all records", Body: "Observed: the synthetic date filter is ignored. Missing: build and steps.", Rationale: "Verify the reproduction."}, f.err
}

type fakeGitHub struct {
	calls, lookups int
	err            error
}

func (f *fakeGitHub) Create(context.Context, *Action) (Issue, error) {
	f.calls++
	return Issue{Number: 7, URL: "https://github.com/demo/inbox/issues/7"}, f.err
}
func (f *fakeGitHub) Lookup(_ context.Context, _ *Action, n int) (Issue, error) {
	f.lookups++
	if n != 7 {
		return Issue{}, errors.New("marker mismatch")
	}
	return Issue{Number: 7, URL: "https://github.com/demo/inbox/issues/7"}, nil
}
func testConfig() Config {
	return Config{Addr: "127.0.0.1:8087", Origin: "http://127.0.0.1:8087", ReviewerToken: strings.Repeat("r", 32), AgentToken: strings.Repeat("a", 32), Provider: "claude", Model: "test-model", AIKey: "test-key", Repository: "demo/inbox", GitHubToken: "test-gh-key", Execute: true}
}
func fixture() (*Service, *memoryStore, *fakeAI, *fakeGitHub) {
	m, d, g := newMemory(), &fakeAI{}, &fakeGitHub{}
	return NewService(testConfig(), m, d, g), m, d, g
}
func proposal() Proposal {
	return Proposal{RequestID: "test-request-123", Report: "Synthetic report: CSV export ignores the active date filter."}
}
func mustPropose(t *testing.T, s *Service) *Action {
	t.Helper()
	a, e := s.Propose(context.Background(), proposal(), "agent")
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func approve(t *testing.T, s *Service, a *Action) *Action {
	t.Helper()
	a, e := s.Change(context.Background(), a.ID, "approve", Decision{Version: a.Version, Title: a.Title, Body: a.Body})
	if e != nil {
		t.Fatal(e)
	}
	return a
}

func TestProposalIdempotencyAndCollision(t *testing.T) {
	s, m, ai, gh := fixture()
	a := mustPropose(t, s)
	b := mustPropose(t, s)
	if a.ID != b.ID || ai.calls != 1 || len(m.rows) != 1 || gh.calls != 0 {
		t.Fatal("duplicate proposal or unexpected side effect")
	}
	p := proposal()
	p.Report += " changed"
	if _, err := s.Propose(context.Background(), p, "agent"); !errors.Is(err, ErrConflict) {
		t.Fatal("reused request ID must reject changed content")
	}
}
func TestApprovalAndConcurrentExecution(t *testing.T) {
	s, _, _, gh := fixture()
	a := mustPropose(t, s)
	if _, err := s.Change(context.Background(), a.ID, "execute", Decision{Version: a.Version}); !errors.Is(err, ErrConflict) {
		t.Fatal("unapproved action executed")
	}
	a = approve(t, s, a)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.Change(context.Background(), a.ID, "execute", Decision{Version: a.Version})
		}()
	}
	wg.Wait()
	if gh.calls != 1 {
		t.Fatalf("expected one POST, got %d", gh.calls)
	}
	got, _ := s.Get(context.Background(), a.ID)
	if got.Status != "succeeded" || got.IssueNumber != 7 {
		t.Fatal("result not recorded")
	}
}
func TestRejectNeverExecutes(t *testing.T) {
	s, _, _, gh := fixture()
	a := mustPropose(t, s)
	a, err := s.Change(context.Background(), a.ID, "reject", Decision{Version: a.Version, Note: "Missing reproduction"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Change(context.Background(), a.ID, "execute", Decision{Version: a.Version})
	if !errors.Is(err, ErrConflict) || gh.calls != 0 {
		t.Fatal("rejected action reached GitHub")
	}
}
func TestEditsRevokeApprovalAndStaleVersionsFail(t *testing.T) {
	s, _, _, gh := fixture()
	a := approve(t, s, mustPropose(t, s))
	stale := a.Version
	a, err := s.Change(context.Background(), a.ID, "edit", Decision{Version: a.Version, Title: "Corrected title", Body: a.Body})
	if err != nil || a.Status != "proposed" {
		t.Fatal("editing must require fresh approval")
	}
	for _, version := range []int{stale, a.Version} {
		if _, err := s.Change(context.Background(), a.ID, "execute", Decision{Version: version}); !errors.Is(err, ErrConflict) {
			t.Fatal("edit bypassed review")
		}
	}
	if gh.calls != 0 {
		t.Fatal("unexpected issue")
	}
}
func TestUncertainExecutionSurvivesRestart(t *testing.T) {
	s, m, ai, gh := fixture()
	gh.err = errors.New("connection timed out after sending request")
	a := approve(t, s, mustPropose(t, s))
	a, err := s.Change(context.Background(), a.ID, "execute", Decision{Version: a.Version})
	if err != nil || a.Status != "uncertain" {
		t.Fatal("timeout must be uncertain")
	}
	restarted := NewService(testConfig(), m, ai, gh)
	if _, err = restarted.Change(context.Background(), a.ID, "execute", Decision{Version: a.Version}); !errors.Is(err, ErrConflict) {
		t.Fatal("restart allowed duplicate execution")
	}
	if _, err = restarted.Change(context.Background(), a.ID, "reconcile", Decision{Version: a.Version, IssueNumber: 8}); err == nil {
		t.Fatal("unrelated issue accepted")
	}
	a, err = restarted.Change(context.Background(), a.ID, "reconcile", Decision{Version: a.Version, IssueNumber: 7})
	if err != nil || a.Status != "succeeded" || gh.calls != 1 {
		t.Fatal("reconciliation should verify without posting")
	}
}
func TestPersistenceFailuresDoNotRepeatSideEffects(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "before_write", true: "ambiguous_write"}[commit], func(t *testing.T) {
			s, m, _, gh := fixture()
			a := approve(t, s, mustPropose(t, s))
			m.failAt = m.puts + 1
			m.commitOnFailure = commit
			if _, err := s.Change(context.Background(), a.ID, "execute", Decision{Version: a.Version}); err == nil {
				t.Fatal("expected persistence failure")
			}
			if gh.calls != 0 {
				t.Fatal("GitHub called before durable execution intent")
			}
		})
	}
	t.Run("after_GitHub", func(t *testing.T) {
		s, m, ai, gh := fixture()
		a := approve(t, s, mustPropose(t, s))
		m.failAt = m.puts + 2
		if _, err := s.Change(context.Background(), a.ID, "execute", Decision{Version: a.Version}); err == nil {
			t.Fatal("expected result persistence failure")
		}
		restarted := NewService(testConfig(), m, ai, gh)
		stored, _ := restarted.Get(context.Background(), a.ID)
		if stored.Status != "executing" {
			t.Fatal("must retain execution intent")
		}
		if _, err := restarted.Change(context.Background(), a.ID, "execute", Decision{Version: stored.Version}); !errors.Is(err, ErrConflict) || gh.calls != 1 {
			t.Fatal("duplicate POST after restart")
		}
	})
}
func TestDraftFailureAndInterruptedDraftDoNotRedraft(t *testing.T) {
	s, m, ai, _ := fixture()
	ai.err = errors.New("provider unavailable")
	a := mustPropose(t, s)
	if a.Status != "draft_failed" {
		t.Fatal("failure not recorded")
	}
	mustPropose(t, s)
	if ai.calls != 1 {
		t.Fatal("repeated AI charge")
	}
	s, m, ai, _ = fixture()
	m.failAt = 2
	_, err := s.Propose(context.Background(), proposal(), "agent")
	if err == nil {
		t.Fatal("expected save failure")
	}
	m.failAt = 0
	a = mustPropose(t, s)
	if a.Status != "drafting" || ai.calls != 1 {
		t.Fatal("interrupted draft must stay visible without retry")
	}
}
func TestManualProposalWorksWithoutAI(t *testing.T) {
	s, _, ai, gh := fixture()
	s.config.AIKey = ""
	p := proposal()
	p.Draft = &Draft{Title: "Manual issue", Body: "Synthetic report with missing reproduction details."}
	a, err := s.Propose(context.Background(), p, "agent")
	if err != nil || a.Provider != "manual" || a.Status != "proposed" || ai.calls != 0 || gh.calls != 0 {
		t.Fatal("manual proposal is not isolated from providers")
	}
}
func TestDisabledExecutionAndChangedDestination(t *testing.T) {
	for _, changedRepo := range []bool{false, true} {
		s, _, _, gh := fixture()
		a := approve(t, s, mustPropose(t, s))
		if changedRepo {
			s.config.Repository = "other/repository"
		} else {
			s.config.Execute = false
		}
		if _, err := s.Change(context.Background(), a.ID, "execute", Decision{Version: a.Version}); err == nil || gh.calls != 0 {
			t.Fatal("execution configuration bypassed")
		}
	}
}

func TestReviewHistoryPreservesApprovedContent(t *testing.T) {
	s, _, _, _ := fixture()
	a := approve(t, s, mustPropose(t, s))
	original := a.Title
	a, err := s.Change(context.Background(), a.ID, "edit", Decision{Version: a.Version, Title: "Corrected title", Body: a.Body})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range a.Events {
		if event.Kind == "approved" {
			found = true
			if event.Draft == nil || event.Draft.Title != original {
				t.Fatal("approved content overwritten by later edits")
			}
		}
	}
	if !found {
		t.Fatal("missing approval history")
	}
}
