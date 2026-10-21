package inbox

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestCloudSchemaRowsAndSQLContract(t *testing.T) {
	var schemaBody []byte
	var saved row
	schemaPosts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-cloud-key" {
			t.Error("missing database auth")
		}
		if r.Method == "POST" && r.Header.Get("Idempotency-Key") == "" {
			t.Error("missing write request key")
		}
		switch {
		case r.URL.Path == "/v1/tenants/demo/schemas" && r.Method == "POST":
			schemaPosts++
			if schemaPosts == 1 {
				schemaBody, _ = io.ReadAll(r.Body)
				respond(w, 200, map[string]bool{"ok": true})
			} else {
				w.WriteHeader(409)
			}
		case strings.Contains(r.URL.Path, "/schemas/"):
			if len(schemaBody) == 0 {
				w.WriteHeader(404)
			} else {
				_, _ = w.Write(schemaBody)
			}
		case strings.Contains(r.URL.Path, "/rows/") && r.Method == "POST":
			if json.NewDecoder(r.Body).Decode(&saved) != nil {
				t.Error("bad row")
			}
			respond(w, 200, map[string]bool{"ok": true})
		case strings.Contains(r.URL.Path, "/rows/") && r.Method == "GET":
			if saved.ID == "" {
				w.WriteHeader(404)
			} else {
				respond(w, 200, saved)
			}
		case strings.HasSuffix(r.URL.Path, "/sql"):
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !strings.Contains(body["sql"], "demo.actions_v1") || !strings.Contains(body["sql"], "LIMIT 50") {
				t.Error("bad bounded query")
			}
			respond(w, 200, map[string]any{"rows": []row{saved}})
		default:
			t.Error("unexpected database path")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c := Config{DBURL: server.URL, Tenant: "demo", Namespace: "demo", Bearer: "private-cloud-key"}
	db := NewCloud(c)
	if err := db.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := db.Init(context.Background()); err != nil {
		t.Fatal("existing compatible schema rejected", err)
	}
	if _, err := db.Get(context.Background(), "missing"); err != ErrNotFound {
		t.Fatal("missing row not mapped")
	}
	a := &Action{ID: "test-id", InputHash: "hash", CreatedMS: 1234, Status: "proposed"}
	a.event("agent", "submitted", "Synthetic test")
	if err := db.Put(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	got, err := db.Get(context.Background(), a.ID)
	if err != nil || got.InputHash != "hash" || got.Status != "proposed" {
		t.Fatal("row round trip failed")
	}
	a.Status = "rejected"
	a.event("reviewer", "rejected", "Test")
	if err := db.Put(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	all, err := db.List(context.Background(), 0, 50)
	if err != nil || len(all) != 1 || all[0].Status != "rejected" {
		t.Fatal("update/query round trip failed")
	}
	var altered schema
	_ = toml.Unmarshal(schemaBody, &altered)
	altered.Columns[0].Type = "i64"
	schemaBody, _ = toml.Marshal(altered)
	if err := db.Init(context.Background()); err == nil {
		t.Fatal("incompatible schema accepted")
	}
	if schemaPosts != 1 {
		t.Fatal("startup rewrote an existing schema")
	}
}

func TestCredentialAndDestinationConfigValidation(t *testing.T) {
	for key, value := range map[string]string{"OC_BASE_URL": "https://demo.free.originchain.ai", "OC_TENANT": "tenant", "OC_BEARER": "test-bearer", "OC_NAMESPACE": "demo", "AI_PROVIDER": "claude", "GITHUB_REPOSITORY": "demo/inbox", "REVIEWER_TOKEN": strings.Repeat("r", 32), "AGENT_TOKEN": strings.Repeat("a", 32), "APP_ADDR": "127.0.0.1:8087", "APP_ORIGIN": "http://127.0.0.1:8087", "ENABLE_GITHUB_EXECUTION": "false", "OC_DB_USER": "", "OC_DB_KEY": ""} {
		t.Setenv(key, value)
	}
	if _, err := ReadConfig(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, value string }{{"OC_BASE_URL", "http://remote.example"}, {"OC_BASE_URL", "https://example.com/path"}, {"OC_BASE_URL", "https://user:pass@example.com"}, {"OC_NAMESPACE", "demo; DROP TABLE x"}, {"APP_ORIGIN", "http://public.example"}, {"GITHUB_REPOSITORY", "demo/../bad"}, {"AGENT_TOKEN", strings.Repeat("r", 32)}, {"ENABLE_GITHUB_EXECUTION", "yes"}} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := ReadConfig(); err == nil {
				t.Fatal("unsafe configuration accepted")
			}
		})
	}
}
