package inbox

import (
	"context"
	"net/http"
	"os"
	"testing"
)

// Browser QA harness only. Not compiled into the application. No external services.
func TestBrowserPreview(t *testing.T) {
	if os.Getenv("INBOX_UI_TEST") != "1" {
		t.Skip("browser fixture server is opt-in")
	}
	c := testConfig()
	c.Addr = "127.0.0.1:8096"
	c.Origin = "http://127.0.0.1:8096"
	c.Namespace = "ui_test_fixture"
	c.Execute = false
	c.AIKey = ""
	c.Model = ""
	c.GitHubToken = ""
	s := NewService(c, newMemory(), &fakeAI{}, &fakeGitHub{})
	_, err := s.Propose(context.Background(), Proposal{RequestID: "browser-fixture-1", Report: "Synthetic browser test: The staging CSV export ignores an active date filter. Expected only the filtered rows. Build and exact steps are unconfirmed.", Draft: &Draft{Title: "CSV export ignores the selected date range", Body: "## Observed behavior\nThe staging CSV export includes records outside the selected date range.\n\n## Expected behavior\nExport only records matching the active filter.\n\n## Reproduction\nNot yet confirmed.\n\n## Missing information\nBuild number, exact steps, and a sample filtered dataset.", Rationale: "This is a manual browser-test fixture. Confirm reproduction details before creating an issue."}}, "agent")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Browser QA fixture listening at http://127.0.0.1:8096. In-memory data; no real AI or database connection.")
	if err := http.ListenAndServe(c.Addr, Handler(c, s)); err != nil {
		t.Fatal(err)
	}
}
