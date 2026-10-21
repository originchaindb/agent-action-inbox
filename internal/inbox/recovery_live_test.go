package inbox

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A deliberately simulated lost-result fixture, backed by real Cloud Free and
// an existing synthetic GitHub issue. This test never creates a GitHub issue.
func TestLiveRecoveryFromExistingDemoIssue(t *testing.T) {
	if os.Getenv("INBOX_LIVE_RECOVERY_TEST") != "1" {
		t.Skip("explicit live recovery verification only")
	}
	if err := LoadEnv(filepath.Join("..", "..", ".env")); err != nil {
		t.Fatal(err)
	}
	c, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.GitHubToken == "" {
		t.Fatal("GitHub token required for read-only issue verification")
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "live-verification.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		ActionID string `json:"action_id"`
	}
	if json.Unmarshal(data, &receipt) != nil || !actionIDPattern.MatchString(receipt.ActionID) {
		t.Fatal("valid synthetic verification receipt required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	source, err := NewCloud(c).Get(ctx, receipt.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	if source.Status != "succeeded" || source.IssueNumber < 1 {
		t.Fatal("existing successfully executed demo required")
	}
	number := source.IssueNumber
	c.Namespace = "inbox_recovery_" + randomID()[:12]
	c.Execute = false
	fixture := NewCloud(c)
	if err := fixture.Init(ctx); err != nil {
		t.Fatal(err)
	}
	source.Status = "executing"
	source.IssueNumber = 0
	source.IssueURL = ""
	retained := make([]Event, 0, len(source.Events))
	for _, event := range source.Events {
		if event.Kind != "executed" {
			retained = append(retained, event)
		}
	}
	source.Events = retained
	source.Version = len(retained)
	source.event("test", "recovery_fixture", "Simulated missing execution result in an isolated test namespace; original completed action unchanged.")
	if err := fixture.Put(ctx, source); err != nil {
		t.Fatal(err)
	}
	restarted := NewService(c, NewCloud(c), NewProvider(c), NewGitHub(c))
	result, err := restarted.Change(ctx, source.ID, "reconcile", Decision{Version: source.Version, IssueNumber: number, Note: "Verified the existing synthetic GitHub issue; no new issue POST."})
	if err != nil || result.Status != "succeeded" {
		t.Fatal("live reconciliation failed", err)
	}
	saved, err := NewCloud(c).Get(ctx, source.ID)
	if err != nil || saved.IssueNumber != number || saved.Status != "succeeded" {
		t.Fatal("reconciliation result was not durable", err)
	}
	t.Log("PASS: isolated lost-result fixture reconciled through a real GitHub marker read and real Cloud Free persistence; no GitHub creation attempted")
}
