package inbox

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
var segment = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`)

type Config struct {
	Addr, Origin, ReviewerToken, AgentToken         string
	DBURL, Tenant, Bearer, DBUser, DBKey, Namespace string
	Provider, Model, AIKey, Repository, GitHubToken string
	Execute                                         bool
	AIRequestLimit                                  int
	AIRequestLedger                                 string
}

// LoadEnv supports plain KEY=value and matching quotes, without interpolation or shell execution.
// Existing environment variables always win. Values are never printed.
func LoadEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("cannot open environment file")
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if !ok || !regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`).MatchString(key) {
			return errors.New("invalid environment file syntax")
		}
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, val); err != nil {
				return errors.New("cannot load environment variable")
			}
		}
	}
	return scanner.Err()
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func ReadConfig() (Config, error) {
	c := Config{Addr: env("APP_ADDR", "127.0.0.1:8087"), Origin: env("APP_ORIGIN", "http://127.0.0.1:8087"), ReviewerToken: os.Getenv("REVIEWER_TOKEN"), AgentToken: os.Getenv("AGENT_TOKEN"), DBURL: strings.TrimRight(os.Getenv("OC_BASE_URL"), "/"), Tenant: os.Getenv("OC_TENANT"), Bearer: os.Getenv("OC_BEARER"), DBUser: os.Getenv("OC_DB_USER"), DBKey: os.Getenv("OC_DB_KEY"), Namespace: env("OC_NAMESPACE", "agent_inbox"), Provider: env("AI_PROVIDER", "claude"), Model: os.Getenv("AI_MODEL"), Repository: os.Getenv("GITHUB_REPOSITORY"), GitHubToken: os.Getenv("GITHUB_TOKEN"), Execute: os.Getenv("ENABLE_GITHUB_EXECUTION") == "true"}
	limit, err := strconv.Atoi(env("AI_REQUEST_LIMIT", "3"))
	if err != nil || limit < 1 || limit > 1000 {
		return c, errors.New("AI_REQUEST_LIMIT must be an integer from 1 to 1000")
	}
	c.AIRequestLimit = limit
	c.AIRequestLedger = env("AI_REQUEST_LEDGER", ".local/ai-requests.json")
	if c.Provider == "claude" {
		c.AIKey = os.Getenv("ANTHROPIC_API_KEY")
	} else if c.Provider == "openai" {
		c.AIKey = os.Getenv("OPENAI_API_KEY")
	} else {
		return c, errors.New("AI_PROVIDER must be claude or openai")
	}
	if v := os.Getenv("ENABLE_GITHUB_EXECUTION"); v != "" && v != "true" && v != "false" {
		return c, errors.New("ENABLE_GITHUB_EXECUTION must be true or false")
	}
	if !identifier.MatchString(c.Namespace) || !segment.MatchString(c.Tenant) {
		return c, errors.New("invalid OC_NAMESPACE or OC_TENANT")
	}
	if !repositoryPattern.MatchString(c.Repository) || strings.HasSuffix(c.Repository, "/..") || strings.HasSuffix(c.Repository, "/.") {
		return c, errors.New("GITHUB_REPOSITORY must be owner/repository")
	}
	if c.Bearer == "" {
		return c, errors.New("OC_BEARER is required")
	}
	if (c.DBUser == "") != (c.DBKey == "") {
		return c, errors.New("OC_DB_USER and OC_DB_KEY must be supplied together")
	}
	for _, v := range []string{c.Bearer, c.DBUser, c.DBKey, c.AIKey, c.GitHubToken, c.ReviewerToken, c.AgentToken} {
		if strings.ContainsAny(v, "\r\n") {
			return c, errors.New("credential contains a newline")
		}
	}
	if len(c.ReviewerToken) < 32 || len(c.AgentToken) < 32 || c.ReviewerToken == c.AgentToken {
		return c, errors.New("REVIEWER_TOKEN and AGENT_TOKEN must be different secrets of at least 32 characters")
	}
	if err := originURL(c.DBURL, false); err != nil {
		return c, fmt.Errorf("OC_BASE_URL: %w", err)
	}
	if err := originURL(c.Origin, true); err != nil {
		return c, fmt.Errorf("APP_ORIGIN: %w", err)
	}
	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		return c, errors.New("APP_ADDR must be host:port")
	}
	if c.Execute && c.GitHubToken == "" {
		return c, errors.New("GITHUB_TOKEN is required when execution is enabled")
	}
	return c, nil
}

func originURL(raw string, allowLocal bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return errors.New("expected an origin URL without path, query, or credentials")
	}
	if u.Scheme == "https" {
		return nil
	}
	if allowLocal && u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1") {
		return nil
	}
	return errors.New("HTTPS is required (loopback HTTP is allowed for the app)")
}
