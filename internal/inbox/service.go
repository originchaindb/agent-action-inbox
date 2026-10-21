package inbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

type Service struct {
	mu      sync.Mutex
	store   Store
	drafter Drafter
	github  IssueWriter
	config  Config
}

func NewService(c Config, s Store, d Drafter, g IssueWriter) *Service {
	return &Service{store: s, drafter: d, github: g, config: c}
}

func (s *Service) Propose(ctx context.Context, p Proposal, actor string) (*Action, error) {
	if len(p.RequestID) < 8 || len(p.RequestID) > 100 || !segment.MatchString(p.RequestID) {
		return nil, errors.New("request_id must be 8–100 letters, digits, underscores, or hyphens")
	}
	if len(strings.TrimSpace(p.Report)) < 10 || len(p.Report) > 12000 {
		return nil, errors.New("report must be 10–12000 bytes")
	}
	if p.Draft != nil {
		if err := validateDraft(*p.Draft); err != nil {
			return nil, err
		}
	}
	idDigest := sha256.Sum256([]byte(p.RequestID))
	id := hex.EncodeToString(idDigest[:16])
	inputDigest := sha256.Sum256(jsonBytes(p))
	inputHash := hex.EncodeToString(inputDigest[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, err := s.store.Get(ctx, id)
	if err == nil {
		if existing.InputHash != inputHash {
			return nil, ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if p.Draft == nil && (s.config.AIKey == "" || s.config.Model == "") {
		return nil, errors.New("Claude/OpenAI drafting needs the server API key and AI_MODEL; a manual proposal is still available")
	}
	a := &Action{ID: id, InputHash: inputHash, Report: p.Report, Repository: s.config.Repository, Provider: s.config.Provider, Model: s.config.Model, Status: "drafting", CreatedMS: time.Now().UnixMilli(), Events: []Event{}}
	a.event(actor, "submitted", "Proposal received; no external action authorized.")
	if p.Draft != nil {
		a.Draft = *p.Draft
		a.Provider = "manual"
		a.Model = ""
		a.Status = "proposed"
		a.event(actor, "drafted", "Submitted draft is waiting for human review.")
		if err := s.store.Put(ctx, a); err != nil {
			return nil, err
		}
		return a, nil
	}
	// Persist before spending an AI request. Repeating this request ID never silently redrafts.
	if err := s.store.Put(ctx, a); err != nil {
		return nil, err
	}
	draft, err := s.drafter.Draft(ctx, p.Report)
	if err != nil {
		a.Status = "draft_failed"
		a.event("system", "draft_failed", err.Error())
	} else {
		a.Draft = draft
		a.Status = "proposed"
		a.event("system", "drafted", "AI draft saved for human review.")
	}
	saveCtx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	if err := s.store.Put(saveCtx, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Service) Change(ctx context.Context, id, operation string, d Decision) (*Action, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if d.Version != a.Version {
		return nil, ErrConflict
	}
	if len(d.Note) > 2000 {
		return nil, errors.New("review note is too long")
	}
	if len(a.Events) >= 40 && (operation == "edit" || operation == "approve") {
		return nil, errors.New("review revision limit reached; reject this draft and submit a new proposal")
	}
	switch operation {
	case "edit", "approve":
		if a.Status != "proposed" && !(operation == "edit" && a.Status == "approved") {
			return nil, ErrConflict
		}
		draft := Draft{Title: strings.TrimSpace(d.Title), Body: strings.TrimSpace(d.Body), Rationale: a.Rationale}
		if err := validateDraft(draft); err != nil {
			return nil, err
		}
		a.Draft = draft
		if operation == "approve" {
			a.Status = "approved"
			a.event("reviewer", "approved", d.Note)
		} else {
			a.Status = "proposed"
			a.event("reviewer", "edited", d.Note)
		}
	case "reject":
		if a.Status != "proposed" && a.Status != "approved" && a.Status != "drafting" && a.Status != "draft_failed" {
			return nil, ErrConflict
		}
		a.Status = "rejected"
		a.event("reviewer", "rejected", d.Note)
	case "execute":
		if a.Status != "approved" {
			return nil, ErrConflict
		}
		if !s.config.Execute {
			return nil, errors.New("GitHub execution is disabled in server settings")
		}
		if a.Repository != s.config.Repository {
			return nil, errors.New("repository configuration changed; create and review a new proposal")
		}
		a.Status = "executing"
		a.event("reviewer", "execution_started", "One GitHub creation attempt authorized.")
		// If this write fails or is ambiguous, do not contact GitHub.
		if err := s.store.Put(ctx, a); err != nil {
			return nil, err
		}
		// Finish and record the attempt even if the browser disconnects.
		runCtx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
		issue, runErr := s.github.Create(runCtx, a)
		cancel()
		if runErr != nil {
			a.Status = "uncertain"
			a.event("system", "execution_uncertain", runErr.Error()+". Check GitHub before any new proposal; automatic retry is disabled.")
		} else {
			a.Status = "succeeded"
			a.IssueNumber = issue.Number
			a.IssueURL = issue.URL
			a.event("system", "executed", "GitHub confirmed issue creation.")
		}
		saveCtx, saveCancel := context.WithTimeout(context.Background(), 65*time.Second)
		defer saveCancel()
		if err := s.store.Put(saveCtx, a); err != nil {
			return nil, errors.New("could not record execution result; refresh and reconcile with GitHub; do not create a replacement yet")
		}
		return a, nil
	case "reconcile":
		if a.Status != "uncertain" && a.Status != "executing" {
			return nil, ErrConflict
		}
		if s.config.GitHubToken == "" {
			return nil, errors.New("GitHub credentials are required to verify an issue")
		}
		issue, err := s.github.Lookup(ctx, a, d.IssueNumber)
		if err != nil {
			return nil, err
		}
		a.Status = "succeeded"
		a.IssueNumber = issue.Number
		a.IssueURL = issue.URL
		a.event("reviewer", "reconciled", "Existing issue verified using its repository and action marker. "+d.Note)
	default:
		return nil, errors.New("unknown operation")
	}
	if err := s.store.Put(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Action, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.Get(ctx, id)
}
func (s *Service) List(ctx context.Context) ([]Action, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.List(ctx, 0, 50)
}
