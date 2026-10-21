package inbox

import (
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

//go:embed web/*
var webFiles embed.FS
var actionIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func constantEqual(a, b string) bool {
	x, y := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func problem(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}

func Handler(c Config, s *Service) http.Handler {
	public, _ := fs.Sub(webFiles, "web")
	mux := http.NewServeMux()
	static := http.FileServer(http.FS(public))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			problem(w, 405, "Method not allowed.")
			return
		}
		static.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "alive"}) })
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			problem(w, 401, "Enter the reviewer access key.")
			return
		}
		role := ""
		if constantEqual(token, c.ReviewerToken) {
			role = "reviewer"
		} else if constantEqual(token, c.AgentToken) {
			role = "agent"
		}
		if role == "" {
			problem(w, 401, "Access key was not accepted.")
			return
		}
		if role == "agent" && !(r.Method == "POST" && r.URL.Path == "/api/actions") {
			problem(w, 403, "Agent access can only submit proposals.")
			return
		}
		if r.Method != "GET" {
			kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || kind != "application/json" {
				problem(w, 415, "Use application/json.")
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
		var value any
		var err error
		switch {
		case r.URL.Path == "/api/settings" && r.Method == "GET":
			_, probeErr := s.List(r.Context())
			databaseName := "OriginChainDB Cloud"
			if _, cloudStore := s.store.(*Cloud); !cloudStore {
				databaseName = "In-memory test fixture"
			}
			dbStatus := "connected"
			if probeErr != nil {
				dbStatus = "unavailable"
			}
			value = map[string]any{"database": databaseName, "database_status": dbStatus, "namespace": c.Namespace, "provider": c.Provider, "model": c.Model, "ai_configured": c.AIKey != "" && c.Model != "", "repository": c.Repository, "execution_enabled": c.Execute, "github_configured": c.GitHubToken != "", "list_limit": 50}
		case r.URL.Path == "/api/actions" && r.Method == "GET":
			value, err = s.List(r.Context())
		case r.URL.Path == "/api/actions" && r.Method == "POST":
			var p Proposal
			if !readInput(w, r, &p) {
				return
			}
			value, err = s.Propose(r.Context(), p, role)
		default:
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/actions/"), "/")
			if !strings.HasPrefix(r.URL.Path, "/api/actions/") || len(parts) > 2 || !actionIDPattern.MatchString(parts[0]) {
				problem(w, 404, "Not found.")
				return
			}
			if r.Method == "GET" && len(parts) == 1 {
				value, err = s.Get(r.Context(), parts[0])
			} else if r.Method == "POST" && len(parts) == 2 {
				var d Decision
				if !readInput(w, r, &d) {
					return
				}
				value, err = s.Change(r.Context(), parts[0], parts[1], d)
			} else {
				problem(w, 405, "Method not allowed.")
				return
			}
		}
		if err != nil {
			status := 400
			var upstream *upstreamError
			if errors.Is(err, ErrConflict) {
				status = 409
			} else if errors.Is(err, ErrNotFound) {
				status = 404
			} else if errors.As(err, &upstream) {
				status = 502
			}
			problem(w, status, err.Error())
			return
		}
		respond(w, 200, value)
	})
	origin, _ := url.Parse(c.Origin)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if !strings.EqualFold(r.Host, origin.Host) {
			problem(w, 421, "Host does not match APP_ORIGIN.")
			return
		}
		if requestOrigin := r.Header.Get("Origin"); requestOrigin != "" && requestOrigin != c.Origin {
			problem(w, 403, "Origin is not allowed.")
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func readInput(w http.ResponseWriter, r *http.Request, v any) bool {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		problem(w, 413, "Request body exceeds the limit.")
		return false
	}
	if err := decodeStrict(data, v); err != nil {
		problem(w, 400, err.Error())
		return false
	}
	return true
}
