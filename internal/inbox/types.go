package inbox

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrNotFound = errors.New("action not found")
var ErrConflict = errors.New("action changed or operation is not allowed in this state; refresh before continuing")

type Draft struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	Rationale string `json:"rationale"`
}
type Event struct {
	At    string `json:"at"`
	Actor string `json:"actor"`
	Kind  string `json:"kind"`
	Note  string `json:"note"`
	Draft *Draft `json:"draft,omitempty"`
}
type Action struct {
	ID        string `json:"id"`
	InputHash string `json:"-"`
	Report    string `json:"report"`
	Draft
	Repository  string  `json:"repository"`
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	Status      string  `json:"status"`
	Version     int     `json:"version"`
	CreatedMS   int64   `json:"created_ms"`
	UpdatedMS   int64   `json:"updated_ms"`
	IssueNumber int     `json:"issue_number,omitempty"`
	IssueURL    string  `json:"issue_url,omitempty"`
	Events      []Event `json:"events"`
}
type Proposal struct {
	RequestID string `json:"request_id"`
	Report    string `json:"report"`
	// A manual draft is useful when another agent has already generated the proposal.
	Draft *Draft `json:"draft,omitempty"`
}
type Decision struct {
	Version     int    `json:"version"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	Note        string `json:"note"`
	IssueNumber int    `json:"issue_number"`
}
type Issue struct {
	Number      int            `json:"number"`
	URL         string         `json:"html_url"`
	Body        string         `json:"body"`
	PullRequest map[string]any `json:"pull_request,omitempty"`
}
type Store interface {
	Get(context.Context, string) (*Action, error)
	Put(context.Context, *Action) error
	List(context.Context, int64, int) ([]Action, error)
}
type Drafter interface {
	Draft(context.Context, string) (Draft, error)
}
type IssueWriter interface {
	Create(context.Context, *Action) (Issue, error)
	Lookup(context.Context, *Action, int) (Issue, error)
}

func validateDraft(d Draft) error {
	if len(strings.TrimSpace(d.Title)) == 0 || len(d.Title) > 180 || len(strings.TrimSpace(d.Body)) == 0 || len(d.Body) > 12000 || len(d.Rationale) > 2000 {
		return errors.New("draft needs a title (1–180 bytes), body (1–12000 bytes), and rationale up to 2000 bytes")
	}
	// The application owns its reconciliation marker; drafts cannot impersonate another action.
	if strings.Contains(d.Body, "agent-action-inbox:") || strings.Contains(d.Title, "agent-action-inbox:") {
		return errors.New("draft contains a reserved reconciliation marker")
	}
	return nil
}
func (a *Action) event(actor, kind, note string) {
	a.Version++
	a.UpdatedMS = time.Now().UTC().UnixMilli()
	e := Event{At: time.Now().UTC().Format(time.RFC3339Nano), Actor: actor, Kind: kind, Note: note}
	if kind == "drafted" || kind == "edited" || kind == "approved" {
		snapshot := a.Draft
		e.Draft = &snapshot
	}
	a.Events = append(a.Events, e)
}
func marker(id string) string { return "<!-- agent-action-inbox:" + id + " -->" }
