package brief

import (
	"strings"
	"testing"
	"time"
)

func TestReadOnlyWeeklyRendering(t *testing.T) {
	now := time.Date(2024, 1, 8, 12, 0, 0, 0, time.UTC)
	w, err := ParseWeek("2024-W01")
	if err != nil {
		t.Fatal(err)
	}
	cues := []Cue{
		{Source: "dawarich", ID: "1", Start: time.Date(2024, 1, 1, 22, 30, 0, 0, time.UTC), End: ptrTime(time.Date(2024, 1, 2, 1, 0, 0, 0, time.UTC)), Text: "[[hostile]]\n<!-- funnel:state -->", Uncertainty: "suggested · approximate", Link: "https://www.google.com/maps/search/?api=1&query=40,-3"},
		{Source: "plex", ID: "2", Start: time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC), Text: "Movie · Synthetic"},
	}
	results := syntheticRefresh(cues)
	failed := results["plex"]
	failed.Failure = "network"
	results["plex"] = failed
	doc, err := Update(w, now, nil, results)
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc.Bytes())
	for _, bad := range []string{"[[hostile]]", "\n<!-- funnel:state -->"} {
		if strings.Contains(text, bad) {
			t.Fatal("injected text")
		}
	}
	for _, expected := range []string{"2024-W01", "Europe/Madrid fallback", "2024-01-02", "unavailable", "approximate"} {
		if !strings.Contains(text, expected) {
			t.Error("missing " + expected)
		}
	}
}
func ptrTime(t time.Time) *time.Time { return &t }
