package static

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestFileHashReflectsChanges(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/style.css"
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}

	staticFS := NewStaticFS(os.DirFS(dir))
	first := staticFS.FileHash("style.css")
	if len(first) != 16 {
		t.Fatalf("hash length = %d, want 16", len(first))
	}
	if err := os.WriteFile(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if second := staticFS.FileHash("style.css"); second == first {
		t.Fatalf("hash remained %q after file changed", second)
	}
}

func TestCacheHeaders(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/style.css", []byte("body {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := NewStaticFS(os.DirFS(dir)).Handler()

	for _, test := range []struct {
		url, want string
	}{
		{"/style.css", "no-cache"},
		{"/style.css?v=abc123", "public, max-age=31536000, immutable"},
	} {
		t.Run(test.url, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, test.url, nil))
			if got := w.Header().Get("Cache-Control"); got != test.want {
				t.Fatalf("Cache-Control = %q, want %q", got, test.want)
			}
		})
	}
}
