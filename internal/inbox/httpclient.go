package inbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // Never forward credentials through an ambient proxy.
	return &http.Client{Timeout: 60 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("secure random source unavailable")
	}
	return hex.EncodeToString(b)
}

type upstreamError struct {
	service string
	status  int
}

func (e *upstreamError) Error() string {
	return fmt.Sprintf("%s returned HTTP %d; response body omitted", e.service, e.status)
}
func request(ctx context.Context, client *http.Client, service, method, endpoint string, headers map[string]string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s request could not be created", service)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s connection failed or timed out", service)
	}
	defer resp.Body.Close()
	if service == "OriginChainDB" && resp.Header.Get("X-OC-Replication") == "degraded" {
		return nil, errors.New("OriginChainDB reported degraded replication; stop execution and check database health before continuing")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &upstreamError{service, resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil || len(data) > 2*1024*1024 {
		return nil, fmt.Errorf("%s response could not be read within the size limit", service)
	}
	return data, nil
}
func jsonBytes(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("internal JSON encoding error")
	}
	return b
}
func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid JSON fields or values")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return errors.New("expected one JSON value")
	}
	return nil
}
