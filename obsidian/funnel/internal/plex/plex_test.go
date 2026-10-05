package plex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHistoryIsolationPaginationAndRewatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "secret-sentinel" {
			t.Error("token missing")
		}
		switch r.URL.Path {
		case "/identity":
			fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"synthetic-server"}}`)
		case "/accounts":
			fmt.Fprint(w, `{"MediaContainer":{"Account":[{"id":7,"name":"sample"},{"id":8,"name":"other"}]}}`)
		case "/status/sessions/history/all":
			if r.URL.Query().Get("accountID") != "7" || r.URL.Query().Get("viewedAt>") == "" || r.URL.Query().Get("viewedAt<") == "" {
				t.Error("unbounded history request")
			}
			offset, _ := strconv.Atoi(r.Header.Get("X-Plex-Container-Start"))
			rows := []map[string]any{}
			for _, id := range []int{1, 1, 2, 3}[offset:min(offset+2, 4)] {
				account := 7
				if id == 3 {
					account = 8
				}
				rows = append(rows, map[string]any{"historyKey": fmt.Sprintf("/status/sessions/history/%d", id), "viewedAt": 1704110400 + id, "accountID": account, "type": "movie", "ratingKey": "9", "title": "Movie"})
			}
			json.NewEncoder(w).Encode(map[string]any{"MediaContainer": map[string]any{"size": len(rows), "offset": offset, "totalSize": 4, "Metadata": rows}})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p, err := New(server.URL, "secret-sentinel", "sample", nil)
	if err != nil {
		t.Fatal(err)
	}
	a := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	result, err := p.Fetch(context.Background(), a, a.AddDate(0, 0, 2))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Cues) != 2 || result.Identity == "" || result.ConfigID == "" {
		t.Fatalf("bad result: %+v", result)
	}
}

func TestAmbiguousOrMissingAccountCannotReplaceHistory(t *testing.T) {
	for _, accounts := range []string{`{"MediaContainer":{"Account":[]}}`, `{"MediaContainer":{"Account":[{"id":7,"name":"sample"},{"id":8,"name":"sample"}]}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/identity" {
				fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"synthetic"}}`)
			} else if r.URL.Path == "/accounts" {
				fmt.Fprint(w, accounts)
			} else {
				t.Error("history queried before identity resolved")
			}
		}))
		p, _ := New(server.URL, "synthetic-token", "sample", nil)
		a := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		_, err := p.Fetch(context.Background(), a, a.AddDate(0, 0, 7))
		server.Close()
		if err == nil {
			t.Fatal("ambiguous/missing account accepted")
		}
	}
}

func TestEpisodeMetadataEnrichmentAndSafeGaps(t *testing.T) {
	for _, available := range []bool{true, false} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/identity":
				fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"synthetic"}}`)
			case "/accounts":
				fmt.Fprint(w, `{"MediaContainer":{"Account":[{"id":7,"name":"sample"}]}}`)
			case "/status/sessions/history/all":
				fmt.Fprint(w, `{"MediaContainer":{"size":1,"offset":0,"totalSize":1,"Metadata":[{"accountID":7,"type":"episode","historyKey":"/status/sessions/history/1","viewedAt":1704110400,"ratingKey":"9"}]}}`)
			case "/library/metadata/9":
				if available {
					fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"9","type":"episode","title":"Example episode","grandparentTitle":"Example Show","parentIndex":2,"index":4}]}}`)
				} else {
					http.Error(w, "secret-sentinel", 500)
				}
			default:
				t.Error("unexpected request")
				http.NotFound(w, r)
			}
		}))
		p, _ := New(server.URL, "secret-sentinel", "sample", nil)
		a := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		result, err := p.Fetch(context.Background(), a, a.AddDate(0, 0, 2))
		server.Close()
		if err != nil || len(result.Cues) != 1 {
			t.Fatal("metadata failure discarded valid history", err)
		}
		text := result.Cues[0].Text
		if available && !strings.Contains(text, "Example Show S02E04") {
			t.Fatal("episode formatting")
		}
		if !available && (!strings.Contains(text, "Unknown title") || !strings.Contains(text, "limited metadata")) {
			t.Fatal("metadata gaps not labelled")
		}
	}
}

func TestHistoryFailuresCannotLookEmpty(t *testing.T) {
	for _, body := range []string{
		`{"MediaContainer":{"size":0,"offset":0,"totalSize":0}}`,
		`{"MediaContainer":{"size":1,"offset":0,"totalSize":1,"Metadata":[{"accountID":7,"type":"movie","historyKey":"/status/sessions/history/1","viewedAt":0}]}}`,
		`{"MediaContainer":{"size":1,"offset":0,"totalSize":2,"Metadata":[{"accountID":7,"type":"movie","historyKey":"/status/sessions/history/1","viewedAt":1704110400}]}}`,
		`{"MediaContainer":{"size":1,"offset":0,"totalSize":1,"Metadata":[{"accountID":7,"historyKey":"/status/sessions/history/1","viewedAt":1704110400}]}}`,
		`{}`,
	} {
		t.Run(body[:min(40, len(body))], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/identity":
					fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"s"}}`)
				case "/accounts":
					fmt.Fprint(w, `{"MediaContainer":{"Account":[{"id":7,"name":"sample"}]}}`)
				default:
					if r.Header.Get("X-Plex-Container-Start") != "0" {
						http.Error(w, "secret-sentinel", 500)
					} else {
						fmt.Fprint(w, body)
					}
				}
			}))
			defer server.Close()
			p, _ := New(server.URL, "secret-sentinel", "7", nil)
			a := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
			res, err := p.Fetch(context.Background(), a, a.AddDate(0, 0, 2))
			empty := strings.Contains(body, `"size":0`)
			if empty && (err != nil || len(res.Cues) != 0) {
				t.Fatal("valid empty failed")
			}
			if !empty && (err == nil || strings.Contains(err.Error(), "secret-sentinel")) {
				t.Fatalf("unsafe failure: %v", err)
			}
		})
	}
}
