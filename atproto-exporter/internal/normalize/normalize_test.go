package normalize

import "testing"

func newTestAllowlist() *Allowlist {
	return New(
		[]string{"app.bsky.feed.post", "app.bsky.feed.like", "app.bsky.graph.follow"},
		[]string{"en", "ja", "pt"},
	)
}

func TestCollection(t *testing.T) {
	a := newTestAllowlist()
	cases := map[string]string{
		"app.bsky.feed.post":    "app.bsky.feed.post",
		"app.bsky.feed.like":    "app.bsky.feed.like",
		"app.bsky.graph.follow": "app.bsky.graph.follow",
		"com.example.custom":    "other", // attacker-minted NSID
		"":                      "other",
		"app.bsky.feed.repost":  "other", // valid NSID but not allowlisted
	}
	for in, want := range cases {
		if got := a.Collection(in); got != want {
			t.Errorf("Collection(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLanguage(t *testing.T) {
	a := newTestAllowlist()
	cases := map[string]string{
		"en":      "en",
		"EN":      "en", // case-insensitive
		"en-US":   "en", // region subtag dropped
		"en-Latn": "en", // script subtag dropped
		"pt-BR":   "pt",
		"ja":      "ja",
		"":        "unknown", // empty
		"xx":      "other",   // not allowlisted
		"klingon": "other",   // junk
		"zh-Hant": "other",   // base not in allowlist
	}
	for in, want := range cases {
		if got := a.Language(in); got != want {
			t.Errorf("Language(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLanguageWhitespace(t *testing.T) {
	a := newTestAllowlist()
	if got := a.Language("  en  "); got != "en" {
		t.Errorf("Language with surrounding whitespace = %q, want en", got)
	}
}

// Cardinality guarantee: no matter what junk arrives, the set of possible
// return values stays bounded by the allowlist plus the two fixed buckets.
func TestBoundedOutputs(t *testing.T) {
	a := newTestAllowlist()
	allowedColl := map[string]bool{"app.bsky.feed.post": true, "app.bsky.feed.like": true, "app.bsky.graph.follow": true, "other": true}
	for _, junk := range []string{"a", "b.c.d", "app.bsky.feed.post.extra", "\x00", "app.bsky.feed.like"} {
		if !allowedColl[a.Collection(junk)] {
			t.Errorf("Collection(%q) escaped the allowlist: %q", junk, a.Collection(junk))
		}
	}
	allowedLang := map[string]bool{"en": true, "ja": true, "pt": true, "other": true, "unknown": true}
	for _, junk := range []string{"EN-us", "de", "", "zh-Hans", "!!!"} {
		if !allowedLang[a.Language(junk)] {
			t.Errorf("Language(%q) escaped the allowlist: %q", junk, a.Language(junk))
		}
	}
}
