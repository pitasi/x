package dawarich

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVisitsStatesBoundariesAndValidation(t *testing.T) {
	for _, bad := range []string{"", "coordinates", "interval", "state", "later-page"} {
		t.Run(bad, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer secret-sentinel" {
					t.Error("missing auth")
				}
				if r.URL.Path == "/api/v1/users/me" {
					fmt.Fprint(w, `{"user":{"email":"synthetic@example.invalid"}}`)
					return
				}
				if r.URL.Query().Get("start_at") == "" || r.URL.Query().Get("end_at") == "" {
					t.Error("unbounded request")
				}
				if r.URL.Query().Get("page") == "2" {
					http.Error(w, "secret-sentinel", 500)
					return
				}
				w.Header().Set("X-Current-Page", "1")
				w.Header().Set("X-Total-Pages", "1")
				w.Header().Set("X-Total-Count", "3")
				coord := "40"
				end := "2024-01-10T10:00:00Z"
				state := "suggested"
				switch bad {
				case "coordinates":
					coord = "91"
				case "interval":
					end = "2023-01-01T00:00:00Z"
				case "state":
					state = "unknown"
				case "later-page":
					w.Header().Set("X-Total-Pages", "2")
					w.Header().Set("X-Total-Count", "4")
				}
				fmt.Fprintf(w, `[{"id":1,"user_id":7,"started_at":"2024-01-01T10:00:00Z","ended_at":%q,"name":"Example","status":%q,"place":{"latitude":%s,"longitude":-3}},
     {"id":2,"user_id":7,"started_at":"2024-01-02T10:00:00Z","status":"confirmed","place":{"latitude":"40.1","longitude":"-3.1"}},
     {"id":3,"user_id":7,"status":"declined"}]`, end, state, coord)
			}))
			defer server.Close()
			p, err := New(server.URL, "secret-sentinel", nil)
			if err != nil {
				t.Fatal(err)
			}
			a := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
			result, err := p.Fetch(context.Background(), a, a.AddDate(0, 0, 7))
			if bad != "" {
				if err == nil || strings.Contains(err.Error(), "secret-sentinel") {
					t.Fatalf("unsafe result %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Cues) != 2 || result.Cues[1].End != nil || !strings.HasPrefix(result.Cues[0].MapURL, "https://www.google.com/maps/search/") {
				t.Fatalf("bad mapping: %+v", result)
			}
		})
	}
}

func TestVisitsCompletePaginationDeduplicates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/users/me" {
			fmt.Fprint(w, `{"user":{"email":"synthetic@example.invalid"}}`)
			return
		}
		page := r.URL.Query().Get("page")
		w.Header().Set("X-Current-Page", page)
		w.Header().Set("X-Total-Pages", "2")
		w.Header().Set("X-Total-Count", "3")
		row := `{"id":1,"user_id":7,"started_at":"2024-01-02T12:00:00Z","status":"suggested","place":{"latitude":40,"longitude":-3}}`
		if page == "1" {
			fmt.Fprintf(w, "[%s,%s]", row, row)
		} else {
			fmt.Fprint(w, `[{"id":2,"user_id":7,"started_at":"2024-01-03T12:00:00Z","status":"confirmed","place":{"latitude":40,"longitude":-3}}]`)
		}
	}))
	defer server.Close()
	p, _ := New(server.URL, "synthetic-token", nil)
	a := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	result, err := p.Fetch(context.Background(), a, a.AddDate(0, 0, 7))
	if err != nil || len(result.Cues) != 2 || result.Cues[0].ID == result.Cues[1].ID {
		t.Fatal("incomplete/duplicated result", err)
	}
}

func TestEmptyVisitsRequireCompletePagination(t *testing.T) {
	for _, headers := range []bool{true, false} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/users/me" {
				fmt.Fprint(w, `{"user":{"email":"example@example.invalid"}}`)
				return
			}
			if headers {
				w.Header().Set("X-Current-Page", "1")
				w.Header().Set("X-Total-Pages", "0")
				w.Header().Set("X-Total-Count", "0")
			}
			fmt.Fprint(w, `[]`)
		}))
		p, _ := New(server.URL, "token", nil)
		a := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		_, err := p.Fetch(context.Background(), a, a.AddDate(0, 0, 7))
		server.Close()
		if headers != (err == nil) {
			t.Fatalf("completeness mismatch: %v", err)
		}
	}
}
