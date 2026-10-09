// Package source contains the bounded transport used by both concrete providers.
package source

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const MaxPages = 100
const MaxBody = 4 << 20
const maxRetries = 3
const retryInterval = time.Second

type Failure struct {
	Operation  string
	Category   string
	HTTPStatus int
	Attempts   int
}

func (e *Failure) Error() string { return e.Category }

func WithOperation(operation string, err error) *Failure {
	var failure *Failure
	if errors.As(err, &failure) {
		copy := *failure
		if copy.Operation == "" {
			copy.Operation = operation
		}
		return &copy
	}
	category := err.Error()
	switch category {
	case "authentication", "configuration", "http", "identity", "invalid_data", "interval", "network", "page_limit", "pagination", "response_limit":
	default:
		category = "source_error"
	}
	return &Failure{Operation: operation, Category: category}
}

type Client struct {
	base          url.URL
	token, header string
	http          http.Client
}

func New(raw, token, header string, client *http.Client) (*Client, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(token, "\r\n") || token == "" {
		return nil, errors.New("configuration")
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	h := http.Client{}
	if client != nil {
		h = *client
	}
	h.Timeout = 20 * time.Second
	// Refuse all redirects: credentials must never reach an unexpected origin.
	h.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: *u, token: token, header: header, http: h}, nil
}

func Fingerprint(parts ...string) string {
	b, _ := json.Marshal(parts)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (c *Client) ConfigID(selector string) string { return Fingerprint(c.base.String(), selector) }

func (c *Client) JSON(ctx context.Context, path string, query url.Values, headers http.Header, out any) (http.Header, error) {
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
		return nil, errors.New("invalid_data")
	}
	u := c.base
	u.Path += path
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, &Failure{Category: "configuration"}
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Accept", "application/json")
	token := c.token
	if c.header == "Authorization" {
		token = "Bearer " + token
	}
	req.Header.Set(c.header, token)
	var res *http.Response
	attempts := 0
	for attempt := 0; ; attempt++ {
		attempts++
		res, err = c.http.Do(req)
		retry := err != nil || res.StatusCode == http.StatusRequestTimeout || res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500
		if !retry || attempt == maxRetries || ctx.Err() != nil {
			break
		}
		if res != nil {
			res.Body.Close()
		}
		select {
		case <-ctx.Done():
			return nil, &Failure{Category: "network", Attempts: attempts}
		case <-time.After(retryInterval):
		}
	}
	if err != nil {
		return nil, &Failure{Category: "network", Attempts: attempts}
	}
	defer res.Body.Close()
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return nil, &Failure{Category: "authentication", HTTPStatus: res.StatusCode, Attempts: attempts}
	}
	if res.StatusCode != 200 {
		return nil, &Failure{Category: "http", HTTPStatus: res.StatusCode, Attempts: attempts}
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, MaxBody+1))
	if err != nil || len(data) > MaxBody {
		return nil, &Failure{Category: "response_limit", HTTPStatus: res.StatusCode, Attempts: attempts}
	}
	if err = json.Unmarshal(data, out); err != nil {
		return nil, &Failure{Category: "invalid_data", HTTPStatus: res.StatusCode, Attempts: attempts}
	}
	normalized, _ := json.Marshal(out)
	secret, _ := json.Marshal(c.token)
	if bytes.Contains(data, []byte(c.token)) || bytes.Contains(normalized, secret[1:len(secret)-1]) {
		return nil, &Failure{Category: "invalid_data", HTTPStatus: res.StatusCode, Attempts: attempts}
	}
	return res.Header, nil
}
