package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type GitHub struct {
	client                  *http.Client
	base, token, repository string
}

func NewGitHub(c Config) *GitHub {
	return &GitHub{newHTTPClient(), "https://api.github.com", c.GitHubToken, c.Repository}
}
func (g *GitHub) call(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	return request(ctx, g.client, "GitHub", method, g.base+"/repos/"+g.repository+path, map[string]string{"Authorization": "Bearer " + g.token, "Accept": "application/vnd.github+json", "Content-Type": "application/json", "X-GitHub-Api-Version": "2026-03-10"}, body)
}
func (g *GitHub) decode(data []byte) (Issue, error) {
	var issue Issue
	if json.Unmarshal(data, &issue) != nil || issue.Number < 1 || issue.PullRequest != nil || issue.URL != "https://github.com/"+g.repository+"/issues/"+strconv.Itoa(issue.Number) {
		return issue, errors.New("GitHub returned an unexpected issue")
	}
	return issue, nil
}
func (g *GitHub) Create(ctx context.Context, a *Action) (Issue, error) {
	if a.Repository != g.repository {
		return Issue{}, errors.New("action destination differs from configured repository")
	}
	// Exactly one POST attempt. No network retries or model-selected URL/tool parameters.
	data, err := g.call(ctx, "POST", "/issues", jsonBytes(map[string]string{"title": a.Title, "body": a.Body + "\n\n" + marker(a.ID)}))
	if err != nil {
		return Issue{}, err
	}
	return g.decode(data)
}
func (g *GitHub) Lookup(ctx context.Context, a *Action, number int) (Issue, error) {
	if a.Repository != g.repository || number < 1 {
		return Issue{}, errors.New("invalid reconciliation destination")
	}
	data, err := g.call(ctx, "GET", fmt.Sprintf("/issues/%d", number), nil)
	if err != nil {
		return Issue{}, err
	}
	issue, err := g.decode(data)
	if err != nil {
		return issue, err
	}
	if !strings.Contains(issue.Body, marker(a.ID)) {
		return Issue{}, errors.New("issue does not contain this action's reconciliation marker")
	}
	return issue, nil
}
