package antopt

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListenBrainzNowPlaying(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		want   *nowPlayingTrack
	}{
		{
			name:   "active listen without a timestamp",
			status: http.StatusOK,
			body:   `{"payload":{"playing_now":true,"count":1,"listens":[{"track_metadata":{"track_name":"Hyperballad","artist_name":"Björk"}}]}}`,
			want:   &nowPlayingTrack{Name: "Hyperballad", Artist: "Björk"},
		},
		{
			name:   "idle even when playing_now is true",
			status: http.StatusOK,
			body:   `{"payload":{"playing_now":true,"count":0,"listens":[]}}`,
		},
		{
			name:   "missing title",
			status: http.StatusOK,
			body:   `{"payload":{"count":1,"listens":[{"track_metadata":{"artist_name":"Björk"}}]}}`,
		},
		{name: "invalid JSON", status: http.StatusOK, body: `{"payload":`},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{"error":"rate limited"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/1/user/zaphodias/playing-now" || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected request: %s %s (authorization present: %t)", r.Method, r.URL, r.Header.Get("Authorization") != "")
				}
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()

			got, err := listenBrainzNowPlaying(t.Context(), server.URL+"/1/user/zaphodias/playing-now")
			wantErr := tt.status != http.StatusOK || tt.name == "invalid JSON"
			if (err != nil) != wantErr {
				t.Fatalf("error = %v, want error: %t", err, wantErr)
			}
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Fatalf("track = %+v, want %+v", got, tt.want)
			}
		})
	}
}
