package antopt

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSharedPagesAndColorPreference(t *testing.T) {
	t.Setenv("LASTFM_API_KEY", "")
	ws := &Website{Colors: []template.CSS{"#b9b5ff", "#ff91bc"}}
	h := ws.Register(false)

	// Use the real form handler and carry its preference to every shared page.
	form := url.Values{"color": {"#ff91bc"}, "redirectTo": {"/uses"}}
	req := httptest.NewRequest(http.MethodPost, "/submit-color", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/uses" {
		t.Fatalf("color submission: status %d, location %q", w.Code, w.Header().Get("Location"))
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "prefs" {
		t.Fatal("color submission did not save the preference")
	}

	for _, path := range []string{"/", "/articles/rust-server-components", "/uses/", "/uses/neovim", "/colophon/"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.AddCookie(cookies[0])
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			body := w.Body.String()
			if strings.Count(body, "<h1 ") != 1 {
				t.Error("page must have one primary heading")
			}
			for _, want := range []string{
				`aria-label="Primary"`, `aria-label="Choose a background color"`,
				`aria-pressed="true"`, `--background: #ff91bc`,
				`href="/articles/feed.atom"`, `href="/colophon"`,
				`href="/static/images/favicon-antonio.png?v=`, `rel="apple-touch-icon"`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("missing %q", want)
				}
			}
		})
	}
}
