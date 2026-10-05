package brief

import (
	"strings"
	"testing"
	"time"

	"anto.pt/x/obsidian/funnel/internal/source"
)

func syntheticRefresh(cues []Cue) map[string]Refresh {
	m := map[string]Refresh{}
	for _, src := range Sources {
		m[src] = Refresh{ConfigID: source.Fingerprint("config", src), Identity: source.Fingerprint("identity", src), Cues: cues}
	}
	return m
}
func TestDocumentFreshStaleEmptyRecoveryAndIdentity(t *testing.T) {
	w, _ := ParseWeek("2024-W01")
	now := time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC)
	cue := Cue{Source: "plex", ID: "1", Start: now.Add(-time.Hour), Text: "Synthetic [[title]]"}
	data := syntheticRefresh([]Cue{cue})
	doc, err := Update(w, now, nil, data)
	if err != nil {
		t.Fatal(err)
	}
	raw := doc.Bytes()
	if !strings.Contains(string(raw), "[[2024-W01]]") {
		t.Fatal("weekly note link missing")
	}
	legacy := strings.NewReplacer("Weekly note: [[2024-W01]]\n\n", "", "- 📍 ", "- ", "- 🍿 ", "- ").Replace(string(raw))
	if _, err := ParseDocument(w, []byte(legacy)); err != nil {
		t.Fatalf("legacy generated document rejected: %v", err)
	}
	saved, err := ParseDocument(w, raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved.Bytes()) != string(raw) {
		t.Fatal("non-deterministic roundtrip")
	}
	data["plex"] = Refresh{ConfigID: data["plex"].ConfigID, Failure: "network"}
	stale, err := Update(w, now.AddDate(0, 0, 2), saved, data)
	if err != nil {
		t.Fatal(err)
	}
	if stale.State["plex"].Status != "stale" || stale.State["plex"].Verified || !strings.Contains(string(stale.Bytes()), "Synthetic") || !strings.Contains(stale.Blocks["2024-01-05/plex"], "Unavailable") {
		t.Fatal("stale lost cues/coverage")
	}
	recovered, err := Update(w, now.AddDate(0, 0, 2), stale, syntheticRefresh(nil))
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State["plex"].Status != "fresh" || strings.Contains(string(recovered.Bytes()), "Synthetic") {
		t.Fatal("empty recovery failed")
	}
	changed := syntheticRefresh(nil)
	r := changed["plex"]
	r.Identity = source.Fingerprint("other")
	changed["plex"] = r
	if _, err := Update(w, now.AddDate(0, 0, 3), stale, changed); err == nil {
		t.Fatal("identity mismatch accepted")
	}
}
func TestDocumentRejectsCorruptionAndInjection(t *testing.T) {
	w, _ := ParseWeek("2024-W01")
	now := time.Date(2024, 1, 8, 12, 0, 0, 0, time.UTC)
	doc, err := Update(w, now, nil, syntheticRefresh(nil))
	if err != nil {
		t.Fatal(err)
	}
	raw := string(doc.Bytes())
	for _, bad := range []string{
		strings.Replace(raw, "funnel_schema: 1", "funnel_schema: 2", 1),
		strings.Replace(raw, `"status":"fresh"`, `"status":"fresh","status":"fresh"`, 1),
		strings.Replace(raw, "funnel:end 2024-01-01 plex", "funnel:end 2024-01-02 plex", 1),
		strings.Replace(raw, "generator: funnel", "generator: personal", 1),
		strings.Replace(raw, "- No Plex history returned.\n", "- [[Journal]]\n", 1),
		strings.Replace(raw, "- No Plex history returned.\n", "- <script>unsafe</script>\n", 1),
		strings.Replace(raw, "- No Plex history returned.\n", "- [Reference](https://example.invalid)\n", 1),
		raw + "personal prose\n",
	} {
		if _, err := ParseDocument(w, []byte(bad)); err == nil {
			t.Fatal("accepted corrupt document")
		}
	}
	bad := syntheticRefresh([]Cue{{Source: "plex", ID: "1", Start: now.AddDate(0, 0, -1), Text: "[[fake]]\n<!-- funnel:begin -->", Link: "https://example.invalid/?token=secret"}})
	if _, err := Update(w, now, nil, bad); err == nil {
		t.Fatal("unsafe URL accepted")
	}
}
