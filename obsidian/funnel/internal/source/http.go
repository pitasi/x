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
		return nil, errors.New("configuration")
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
	for attempt := 0; ; attempt++ {
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
			return nil, errors.New("network")
		case <-time.After(retryInterval):
		}
	}
	if err != nil {
		return nil, errors.New("network")
	}
	defer res.Body.Close()
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return nil, errors.New("authentication")
	}
	if res.StatusCode != 200 {
		return nil, errors.New("http")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, MaxBody+1))
	if err != nil || len(data) > MaxBody {
		return nil, errors.New("response_limit")
	}
	if err = json.Unmarshal(data, out); err != nil {
		return nil, errors.New("invalid_data")
	}
	normalized, _ := json.Marshal(out)
	secret, _ := json.Marshal(c.token)
	if bytes.Contains(data, []byte(c.token)) || bytes.Contains(normalized, secret[1:len(secret)-1]) {
		return nil, errors.New("invalid_data")
	}
	return res.Header, nil
}
