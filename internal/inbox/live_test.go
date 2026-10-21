package inbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Explicit opt-in. Writes isolated synthetic rows and does not delete existing data.
// No GitHub issue is created by this test. Its IssueWriter is a test double.
func TestCloudFreeLive(t *testing.T) {
	if os.Getenv("INBOX_LIVE_TEST") != "1" {
		t.Skip("set INBOX_LIVE_TEST=1 with .env credentials for real Cloud Free verification")
	}
	if err := LoadEnv(filepath.Join("..", "..", ".env")); err != nil {
		t.Fatal(err)
	}
	c, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(c.AIRequestLedger) {
		c.AIRequestLedger = filepath.Join("..", "..", c.AIRequestLedger)
	}
	c.Namespace = "inbox_test_" + randomID()[:12]
	c.Execute = true
	db := NewCloud(c)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := db.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Init(ctx); err != nil {
		t.Fatal("existing schema verification", err)
	}
	liveProvider := NewProvider(c)
	s := NewService(c, db, liveProvider, &fakeGitHub{})
	p := Proposal{RequestID: "cloud-smoke-" + randomID(), Report: "Synthetic verification: a staging CSV export ignores its date filter.", Draft: &Draft{Title: "Synthetic cloud test: filtered CSV export", Body: "Test-only data. Observed: staging export ignores its filter. Missing: confirmed build and reproduction steps.", Rationale: "No customer data."}}
	a, err := s.Propose(ctx, p, "agent")
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := s.Propose(ctx, p, "agent")
	if err != nil || duplicate.ID != a.ID {
		t.Fatal("cloud idempotency failed", err)
	}
	a, err = s.Change(ctx, a.ID, "approve", Decision{Version: a.Version, Title: a.Title, Body: a.Body})
	if err != nil {
		t.Fatal("cloud approval update", err)
	}
	restarted := NewService(c, NewCloud(c), NewProvider(c), &fakeGitHub{})
	got, err := restarted.Get(ctx, a.ID)
	if err != nil || got.Status != "approved" {
		t.Fatal("cloud restart recovery", err)
	}
	got, err = restarted.Change(ctx, got.ID, "reject", Decision{Version: got.Version, Note: "Synthetic smoke complete; no external action."})
	if err != nil || got.Status != "rejected" {
		t.Fatal("cloud reject update", err)
	}
	list, err := restarted.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatal("cloud SQL list", err)
	}
	t.Log("PASS: real Cloud Free schema, row create/update, duplicate submission, SQL list, fresh-client recovery, review history; synthetic data retained in isolated namespace")
	if os.Getenv("INBOX_LIVE_AI_TEST") == "1" {
		live := Proposal{RequestID: "claude-smoke-" + randomID(), Report: p.Report}
		aiAction, err := s.Propose(ctx, live, "agent")
		if err != nil {
			t.Fatal(err)
		}
		if aiAction.Status != "proposed" {
			t.Fatal("live AI draft did not finish; inspect provider configuration")
		}
		if _, err := s.Change(ctx, aiAction.ID, "reject", Decision{Version: aiAction.Version, Note: "Synthetic live AI verification complete."}); err != nil {
			t.Fatal(err)
		}
		t.Log("PASS: real configured AI provider produced a structured draft, saved to Cloud Free for review")
		t.Logf("Provider usage: input_tokens=%d output_tokens=%d", liveProvider.lastUsage.Input, liveProvider.lastUsage.Output)
	}
}
