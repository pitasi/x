package source

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTransportBoundsAuthenticationAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth":
			http.Error(w, "secret-sentinel", 401)
		case "/large":
			w.Write([]byte(strings.Repeat("x", MaxBody+1)))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()
	c, _ := New(server.URL, "secret-sentinel", "Authorization", nil)
	for _, path := range []string{"/auth", "/large"} {
		if _, err := c.JSON(context.Background(), path, nil, nil, &struct{}{}); err == nil || strings.Contains(err.Error(), "secret-sentinel") {
			t.Fatal("unsafe response")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.JSON(ctx, "/", nil, nil, &struct{}{}); err == nil {
		t.Fatal("cancellation ignored")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportNetworkFailureReportsAttempts(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, errors.New("sensitive transport error")
	})}
	c, err := New("https://example.invalid", "secret-sentinel", "Authorization", client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.JSON(context.Background(), "/", nil, nil, &struct{}{})
	var failure *Failure
	if !errors.As(err, &failure) || failure.Category != "network" || failure.Attempts != 4 || failure.HTTPStatus != 0 {
		t.Fatalf("unexpected failure details: %#v", failure)
	}
	if attempts != 4 || strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), "secret-sentinel") {
		t.Fatalf("unsafe or inaccurate diagnostic: attempts=%d err=%v", attempts, err)
	}
}

func TestTransportFailureReportsSanitizedRetryDetails(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, "sensitive response body", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	c, err := New(server.URL, "secret-sentinel", "Authorization", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.JSON(context.Background(), "/", nil, nil, &struct{}{})
	var failure *Failure
	if !errors.As(err, &failure) || failure.Category != "http" || failure.HTTPStatus != 503 || failure.Attempts != 4 {
		t.Fatalf("unexpected failure details: %#v, err=%v", failure, err)
	}
	if attempts != 4 || strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), "secret-sentinel") {
		t.Fatalf("unsafe or inaccurate diagnostic: attempts=%d err=%v", attempts, err)
	}
}

func TestTransportRetriesTemporaryServerErrors(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 3 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	c, err := New(server.URL, "token", "Authorization", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.JSON(context.Background(), "/", nil, nil, &struct{}{}); err != nil {
		t.Fatal(err)
	}
	if attempts != 4 {
		t.Fatalf("got %d attempts, want 4", attempts)
	}
}

func TestTransportRejectsEchoedCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/escaped" {
			w.Write([]byte(`{"title":"secret-\u0073entinel"}`))
		} else {
			w.Write([]byte(`{"title":"secret-sentinel"}`))
		}
	}))
	defer server.Close()
	c, _ := New(server.URL, "secret-sentinel", "Authorization", nil)
	for _, path := range []string{"/plain", "/escaped"} {
		var data struct{ Title string }
		if _, err := c.JSON(context.Background(), path, nil, nil, &data); err == nil {
			t.Fatal("credential echo accepted")
		}
	}
}

func TestTransportRedactsAndRejectsRedirects(t *testing.T) {
	leaked := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-sentinel" {
			t.Error("missing header")
		}
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer server.Close()
	c, err := New(server.URL, "secret-sentinel", "Authorization", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.JSON(context.Background(), "/", nil, nil, &struct{}{})
	if err == nil || strings.Contains(err.Error(), "secret-sentinel") || leaked {
		t.Fatalf("unsafe redirect/error: %v", err)
	}
	for _, url := range []string{"https://user:password@example.invalid", "https://example.invalid?token=x", "file:///tmp/x", "https://example.invalid/#x"} {
		if _, err := New(url, "s", "Authorization", nil); err == nil {
			t.Error("accepted unsafe config")
		}
	}
	if Fingerprint("origin", "user") == Fingerprint("origin", "other") {
		t.Fatal("identity collision")
	}
}
