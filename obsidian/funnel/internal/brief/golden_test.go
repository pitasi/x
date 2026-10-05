package brief

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCanonicalWeeklyGolden(t *testing.T) {
	w, _ := ParseWeek("2024-W01")
	now := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)
	cues := []Cue{
		{Source: "dawarich", ID: "1", Start: time.Date(2024, 1, 1, 22, 30, 0, 0, time.UTC), End: ptrTime(time.Date(2024, 1, 2, 1, 0, 0, 0, time.UTC)), Text: "Example Street", Uncertainty: "suggested · approximate", Link: "https://www.google.com/maps/search/?api=1&query=40,-3"},
		{Source: "plex", ID: "1", Start: time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC), Text: "Example Show S02E04 · Example episode"},
	}
	results := map[string]Refresh{
		"dawarich": {ConfigID: strings.Repeat("a", 64), Identity: strings.Repeat("b", 64), Cues: cues},
		"plex":     {ConfigID: strings.Repeat("c", 64), Identity: strings.Repeat("d", 64), Cues: cues},
	}
	doc, err := Update(w, now, nil, results)
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/2024-W01.golden.md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(doc.Bytes(), golden) {
		t.Fatal("canonical weekly output differs from synthetic golden")
	}
	if !bytes.Contains(golden, []byte("- 📍 23:30")) || !bytes.Contains(golden, []byte("- 🍿 13:00")) {
		t.Fatal("source cues are missing their scan emojis")
	}
	if _, err := ParseDocument(w, golden); err != nil {
		t.Fatal(err)
	}
}
