package inbox

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPAuthorizationAndRequestBoundaries(t *testing.T) {
	s, _, _, gh := fixture()
	c := testConfig()
	h := Handler(c, s)
	cases := []struct {
		name, method, path, token, origin, host, body, contentType string
		want                                                       int
	}{
		{"no auth", "GET", "/api/actions", "", "", "127.0.0.1:8087", "", "", 401},
		{"agent cannot read inbox", "GET", "/api/actions", c.AgentToken, "", "127.0.0.1:8087", "", "", 403},
		{"agent cannot approve", "POST", "/api/actions/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/approve", c.AgentToken, "", "127.0.0.1:8087", "{}", "application/json", 403},
		{"wrong origin", "GET", "/api/actions", c.ReviewerToken, "https://evil.example", "127.0.0.1:8087", "", "", 403},
		{"host spoof", "GET", "/api/actions", c.ReviewerToken, "", "evil.example", "", "", 421},
		{"wrong content type", "POST", "/api/actions", c.ReviewerToken, "", "127.0.0.1:8087", "{}", "text/plain", 415},
		{"extra tool destination", "POST", "/api/actions", c.AgentToken, "", "127.0.0.1:8087", `{"request_id":"request-123","report":"synthetic report","repository":"other/repo"}`, "application/json", 400},
		{"too large", "POST", "/api/actions", c.AgentToken, "", "127.0.0.1:8087", strings.Repeat("x", 33000), "application/json", 413},
		{"reviewer list", "GET", "/api/actions", c.ReviewerToken, "", "127.0.0.1:8087", "", "", 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, c.Origin+tc.path, bytes.NewBufferString(tc.body))
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", tc.contentType)
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("got %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	if gh.calls != 0 {
		t.Fatal("unauthorized external action")
	}
}
func TestSettingsNeverReturnSecrets(t *testing.T) {
	s, _, _, _ := fixture()
	c := testConfig()
	r := httptest.NewRequest("GET", c.Origin+"/api/settings", nil)
	r.Header.Set("Authorization", "Bearer "+c.ReviewerToken)
	w := httptest.NewRecorder()
	Handler(c, s).ServeHTTP(w, r)
	for _, secret := range []string{c.ReviewerToken, c.AgentToken, c.AIKey, c.GitHubToken} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("settings leaked a secret")
		}
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("missing browser isolation policy")
	}
}
