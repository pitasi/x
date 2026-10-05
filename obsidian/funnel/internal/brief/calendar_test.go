package brief

import (
	"strings"
	"testing"
	"time"
)

func TestCalendarMadridISOAndDST(t *testing.T) {
	now := time.Date(2021, 1, 3, 23, 30, 0, 0, time.UTC)
	weeks, err := SelectWeeks("", now)
	if err != nil || len(weeks) != 3 || weeks[0].Name != "2021-W01" || weeks[1].Name != "2020-W53" || weeks[2].Name != "2020-W52" {
		t.Fatalf("ISO boundary: %+v %v", weeks, err)
	}
	if _, err := ParseWeek("2021-W53"); err == nil {
		t.Fatal("invalid week accepted")
	}
	w, _ := ParseWeek("2024-W13")
	start, end, err := w.Envelope(time.Date(2024, 4, 8, 0, 0, 0, 0, time.UTC))
	if err != nil || start.Format(time.RFC3339) != "2024-03-24T00:00:00Z" || end.Format(time.RFC3339) != "2024-04-02T00:00:00Z" {
		t.Fatal("envelope")
	}
	midnight := time.Date(2024, 3, 31, 0, 0, 0, 0, Madrid)
	if midnight.AddDate(0, 0, 1).Sub(midnight) != 23*time.Hour {
		t.Fatal("missing DST data")
	}
}
func TestMixedZoneAbsoluteOrderingAndFallDST(t *testing.T) {
	w, _ := ParseWeek("2024-W01")
	now := time.Date(2024, 1, 8, 12, 0, 0, 0, time.UTC)
	cues := []Cue{
		{Source: "plex", ID: "b", Start: time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC), Zone: time.FixedZone("UTC-4", -4*3600), Text: "Later absolute"},
		{Source: "plex", ID: "a", Start: time.Date(2024, 1, 2, 8, 0, 0, 0, time.UTC), Zone: time.FixedZone("UTC+14", 14*3600), Text: "Earlier absolute"},
	}
	blocks, err := cueBlocks(w, now, cues)
	if err != nil {
		t.Fatal(err)
	}
	block := blocks["2024-01-02/plex"]
	if strings.Index(block, "Earlier absolute") > strings.Index(block, "Later absolute") {
		t.Fatal("wall-clock order replaced absolute order")
	}
	fall := time.Date(2024, 10, 27, 0, 0, 0, 0, Madrid)
	if fall.AddDate(0, 0, 1).Sub(fall) != 25*time.Hour {
		t.Fatal("fall DST")
	}
}

func TestTravelLocalDatesOrderingAndFuture(t *testing.T) {
	w, _ := ParseWeek("2024-W01")
	now := time.Date(2024, 1, 8, 12, 0, 0, 0, time.UTC)
	cues := []Cue{
		{Source: "plex", ID: "positive", Start: time.Date(2023, 12, 31, 11, 0, 0, 0, time.UTC), Zone: time.FixedZone("UTC+14", 14*3600), Text: "Positive offset"},
		{Source: "plex", ID: "negative", Start: time.Date(2024, 1, 8, 10, 0, 0, 0, time.UTC), Zone: time.FixedZone("UTC-12", -12*3600), Text: "Negative offset"},
		{Source: "plex", ID: "future", Start: now.Add(time.Hour), Text: "Future forbidden"},
	}
	doc, err := Update(w, now, nil, syntheticRefresh(cues))
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc.Bytes())
	if !strings.Contains(text, "Positive offset") || !strings.Contains(text, "Negative offset") || strings.Contains(text, "Future forbidden") {
		t.Fatal("travel grouping", err)
	}
}
