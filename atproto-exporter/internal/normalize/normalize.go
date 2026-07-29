// Package normalize maps dirty, user-controlled AT Protocol fields (collection
// NSIDs and language tags) onto bounded, allowlisted label values. This is the
// backbone of the exporter's cardinality guarantee: anything not on an allowlist
// collapses to a fixed "other" (or "unknown") bucket, so no attacker-minted
// value can ever become a Prometheus label.
package normalize

import "strings"

// Allowlist holds the sets of permitted collection NSIDs and language codes.
// It is read-only after construction and safe for concurrent use.
type Allowlist struct {
	collections map[string]struct{}
	langs       map[string]struct{}
}

// New builds an Allowlist from the configured collection NSIDs and language
// codes. Language codes are normalized to lowercase base tags on the way in so
// lookups match the normalization applied by Language.
func New(collections, langs []string) *Allowlist {
	a := &Allowlist{
		collections: make(map[string]struct{}, len(collections)),
		langs:       make(map[string]struct{}, len(langs)),
	}
	for _, c := range collections {
		a.collections[c] = struct{}{}
	}
	for _, l := range langs {
		a.langs[baseLang(l)] = struct{}{}
	}
	return a
}

// Collection returns nsid unchanged if it is allowlisted, otherwise "other".
// This is the hottest path in the event pipeline (one call per commit).
func (a *Allowlist) Collection(nsid string) string {
	if _, ok := a.collections[nsid]; ok {
		return nsid
	}
	return "other"
}

// Language normalizes a BCP-47 tag to an allowlisted ISO-639 base code, or
// "other". An empty tag returns "unknown". Region and script subtags are
// dropped ("en-US" and "en-Latn" both become "en") and the tag is lowercased.
func (a *Allowlist) Language(tag string) string {
	base := baseLang(tag)
	if base == "" {
		return "unknown"
	}
	if _, ok := a.langs[base]; ok {
		return base
	}
	return "other"
}

// baseLang lowercases, trims, and strips any subtags after the first hyphen.
func baseLang(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "" {
		return ""
	}
	base, _, _ := strings.Cut(tag, "-")
	return base
}
