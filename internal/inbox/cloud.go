package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"

	"github.com/pelletier/go-toml/v2"
)

type Cloud struct {
	client                         *http.Client
	base, prefix, namespace, table string
	headers                        map[string]string
}

func NewCloud(c Config) *Cloud {
	h := map[string]string{"Authorization": "Bearer " + c.Bearer}
	if c.DBUser != "" {
		h["X-OC-DB-User"] = c.DBUser
		h["X-OC-DB-Key"] = c.DBKey
	}
	return &Cloud{client: newHTTPClient(), base: c.DBURL, prefix: "/v1/tenants/" + c.Tenant, namespace: c.Namespace, table: c.Namespace + ".actions_v1", headers: h}
}
func (c *Cloud) call(ctx context.Context, method, path, contentType string, body []byte) ([]byte, error) {
	h := map[string]string{}
	for k, v := range c.headers {
		h[k] = v
	}
	h["Content-Type"] = contentType
	if method != "GET" {
		h["Idempotency-Key"] = randomID()
	}
	// No automatic retries: a timed-out write has an unknown outcome.
	return request(ctx, c.client, "OriginChainDB", method, c.base+c.prefix+path, h, body)
}

type column struct {
	Name     string `toml:"name"`
	Type     string `toml:"ty"`
	Required bool   `toml:"required"`
}
type schema struct {
	Namespace  string   `toml:"namespace"`
	Table      string   `toml:"table"`
	PrimaryKey []string `toml:"primary_key"`
	Columns    []column `toml:"columns"`
}

func (c *Cloud) Init(ctx context.Context) error {
	expected := schema{Namespace: c.namespace, Table: "actions_v1", PrimaryKey: []string{"id"}, Columns: []column{{"id", "str", true}, {"created_ms", "i64", true}, {"updated_ms", "i64", true}, {"payload", "str", true}, {"input_hash", "str", true}, {"status", "str", true}, {"title", "str", true}, {"repository", "str", true}}}
	// Registration may update an existing manifest. Read first so startup never
	// overwrites another application's schema. A namespace has one writer.
	data, err := c.call(ctx, "GET", "/schemas/"+c.table, "", nil)
	var upstream *upstreamError
	if errors.As(err, &upstream) && upstream.status == 404 {
		manifest, encodeErr := toml.Marshal(expected)
		if encodeErr != nil {
			return errors.New("could not build database schema")
		}
		_, err = c.call(ctx, "POST", "/schemas", "text/plain", manifest)
		if err != nil && (!errors.As(err, &upstream) || upstream.status != 409) {
			return err
		}
		data, err = c.call(ctx, "GET", "/schemas/"+c.table, "", nil)
	}
	if err != nil {
		return err
	}
	var actual schema
	if toml.Unmarshal(data, &actual) != nil || actual.Namespace != expected.Namespace || actual.Table != expected.Table || !reflect.DeepEqual(actual.PrimaryKey, expected.PrimaryKey) {
		return errors.New("existing database schema is incompatible; choose a fresh OC_NAMESPACE")
	}
	actualColumns := map[string]column{}
	for _, col := range actual.Columns {
		actualColumns[col.Name] = col
	}
	if len(actualColumns) != len(expected.Columns) {
		return errors.New("existing database columns are incompatible; choose a fresh OC_NAMESPACE")
	}
	for _, col := range expected.Columns {
		if actualColumns[col.Name] != col {
			return errors.New("existing database columns are incompatible; choose a fresh OC_NAMESPACE")
		}
	}
	return nil
}

type row struct {
	ID         string `json:"id"`
	CreatedMS  int64  `json:"created_ms"`
	UpdatedMS  int64  `json:"updated_ms"`
	Payload    string `json:"payload"`
	InputHash  string `json:"input_hash"`
	Status     string `json:"status"`
	Title      string `json:"title"`
	Repository string `json:"repository"`
}

func (c *Cloud) Put(ctx context.Context, a *Action) error {
	r := row{ID: a.ID, CreatedMS: a.CreatedMS, UpdatedMS: a.UpdatedMS, Payload: string(jsonBytes(a)), InputHash: a.InputHash, Status: a.Status, Title: a.Title, Repository: a.Repository}
	_, err := c.call(ctx, "POST", "/rows/"+c.table, "application/json", jsonBytes(r))
	return err
}
func decodeRow(r row) (*Action, error) {
	var a Action
	if json.Unmarshal([]byte(r.Payload), &a) != nil || a.ID != r.ID || a.CreatedMS != r.CreatedMS || a.Version < 1 || len(a.Events) == 0 {
		return nil, errors.New("database returned an invalid action record")
	}
	a.InputHash = r.InputHash
	return &a, nil
}
func (c *Cloud) Get(ctx context.Context, id string) (*Action, error) {
	data, err := c.call(ctx, "GET", "/rows/"+c.table+"/"+url.PathEscape(id), "", nil)
	var upstream *upstreamError
	if errors.As(err, &upstream) && upstream.status == 404 {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var r row
	if json.Unmarshal(data, &r) != nil {
		return nil, errors.New("invalid database row response")
	}
	return decodeRow(r)
}
func (c *Cloud) List(ctx context.Context, before int64, limit int) ([]Action, error) {
	if limit < 1 || limit > 50 {
		limit = 50
	}
	if before <= 0 {
		before = 9223372036854775807
	}
	// Identifiers are validated at configuration time; cursor/limit are integers. No user SQL.
	sql := fmt.Sprintf("SELECT id, created_ms, updated_ms, status, title, repository FROM %s WHERE created_ms < %d ORDER BY created_ms DESC LIMIT %d", c.table, before, limit)
	data, err := c.call(ctx, "POST", "/sql", "application/json", jsonBytes(map[string]string{"sql": sql}))
	if err != nil {
		return nil, err
	}
	var result struct {
		Rows []row `json:"rows"`
	}
	if json.Unmarshal(data, &result) != nil || result.Rows == nil {
		return nil, errors.New("invalid database query response")
	}
	actions := make([]Action, 0, len(result.Rows))
	for _, r := range result.Rows {
		if r.ID == "" || r.Status == "" {
			return nil, errors.New("invalid database action summary")
		}
		actions = append(actions, Action{ID: r.ID, CreatedMS: r.CreatedMS, UpdatedMS: r.UpdatedMS, Status: r.Status, Repository: r.Repository, Draft: Draft{Title: r.Title}})
	}
	return actions, nil
}
