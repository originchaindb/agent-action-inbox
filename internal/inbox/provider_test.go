package inbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderContracts(t *testing.T) {
	for _, kind := range []string{"claude", "openai"} {
		t.Run(kind, func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("bad body")
				}
				if body["model"] != "test-model" {
					t.Error("model was substituted")
				}
				draft := Draft{Title: "Synthetic issue", Body: "Observed behavior needs reproduction.", Rationale: "Verify the report."}
				if kind == "claude" {
					if r.Header.Get("x-api-key") != "test-key" || r.Header.Get("anthropic-version") != "2023-06-01" {
						t.Error("bad Claude authentication")
					}
					if body["output_config"] == nil {
						t.Error("missing Claude schema")
					}
					respond(w, 200, map[string]any{"stop_reason": "end_turn", "content": []map[string]string{{"type": "text", "text": string(jsonBytes(draft))}}})
				} else {
					if r.Header.Get("Authorization") != "Bearer test-key" || body["store"] != false || body["text"] == nil {
						t.Error("bad OpenAI request")
					}
					respond(w, 200, map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "content": []map[string]string{{"type": "output_text", "text": string(jsonBytes(draft))}}}}})
				}
			}))
			defer fake.Close()
			c := testConfig()
			c.Provider = kind
			p := testProvider(t, c)
			p.endpoint = fake.URL
			draft, err := p.Draft(context.Background(), "Synthetic report, not an instruction.")
			if err != nil || draft.Title != "Synthetic issue" {
				t.Fatalf("provider parsing failed: %v", err)
			}
		})
	}
}
func TestProviderRejectsRefusalTruncationAndMalformedOutput(t *testing.T) {
	cases := []struct{ kind, body string }{
		{"claude", `{"stop_reason":"refusal","content":[{"type":"text","text":"refused"}]}`},
		{"claude", `{"stop_reason":"max_tokens","content":[{"type":"text","text":"{}"}]}`},
		{"claude", `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"title\":\"Only title\"}"}]}`},
		{"openai", `{"status":"incomplete","output":[]}`},
		{"openai", `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"no"}]}]}`},
		{"openai", `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"{} {}"}]}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.kind+tc.body[:20], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer server.Close()
			c := testConfig()
			c.Provider = tc.kind
			p := testProvider(t, c)
			p.endpoint = server.URL
			if _, err := p.Draft(context.Background(), "Synthetic report"); err == nil {
				t.Fatal("unsafe provider output accepted")
			}
		})
	}
}
func TestUpstreamErrorsDoNotLeakBodiesAndRedirectsAreBlocked(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++ }))
	defer target.Close()
	for _, status := range []int{401, 429, 500, 307} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", target.URL)
			w.WriteHeader(status)
			_, _ = w.Write([]byte("secret-token-and-private-report"))
		}))
		p := testProvider(t, testConfig())
		p.endpoint = server.URL
		_, err := p.Draft(context.Background(), "Synthetic report")
		server.Close()
		if err == nil || strings.Contains(err.Error(), "secret-token") {
			t.Fatal("raw provider body leaked")
		}
	}
	if targetCalls != 0 {
		t.Fatal("redirect forwarded credentials")
	}
}
func TestGitHubDestinationAndReconciliation(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/repos/demo/inbox/issues" && r.URL.Path != "/repos/demo/inbox/issues/7" {
			t.Error("unexpected destination")
		}
		body := "wrong marker"
		if r.Method == "POST" {
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			body = payload["body"]
			if !strings.Contains(body, marker("abc")) {
				t.Error("missing marker")
			}
		}
		respond(w, 200, Issue{Number: 7, URL: "https://github.com/demo/inbox/issues/7", Body: body})
	}))
	defer server.Close()
	g := NewGitHub(testConfig())
	g.base = server.URL
	a := &Action{ID: "abc", Repository: "demo/inbox", Draft: Draft{Title: "Test", Body: "Synthetic"}}
	if _, err := g.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Lookup(context.Background(), a, 7); err == nil {
		t.Fatal("wrong marker accepted")
	}
	a.Repository = "other/repository"
	if _, err := g.Create(context.Background(), a); err == nil || calls != 2 {
		t.Fatal("model-selected destination accepted")
	}
}

func testProvider(t *testing.T, c Config) *Provider {
	t.Helper()
	c.AIRequestLimit = 20
	c.AIRequestLedger = t.TempDir() + "/requests.json"
	return NewProvider(c)
}
