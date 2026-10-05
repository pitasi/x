// A synthetic two-source server used only by the explicit container check.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

func main() {
	var mode atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/test/mode" {
			switch r.URL.Query().Get("state") {
			case "fresh":
				mode.Store(0)
			case "fail":
				mode.Store(1)
			case "slow":
				mode.Store(2)
			case "dst":
				mode.Store(3)
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		switch mode.Load() {
		case 1:
			http.Error(w, "synthetic failure", 500)
			return
		case 2:
			<-r.Context().Done()
			return
		}
		when := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)
		end := "null"
		if mode.Load() == 3 {
			when = time.Date(2024, 3, 31, 0, 30, 0, 0, time.UTC)
			end = fmt.Sprintf("%q", when.Add(2*time.Hour).Format(time.RFC3339))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/identity":
			fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"synthetic-server"}}`)
		case "/accounts":
			fmt.Fprint(w, `{"MediaContainer":{"Account":[{"id":7,"name":"sample"}]}}`)
		case "/api/v1/users/me":
			fmt.Fprint(w, `{"user":{"email":"synthetic@example.invalid"}}`)
		case "/status/sessions/history/all":
			json.NewEncoder(w).Encode(map[string]any{"MediaContainer": map[string]any{"size": 1, "offset": 0, "totalSize": 1, "Metadata": []map[string]any{{"historyKey": "/status/sessions/history/1", "viewedAt": when.Unix(), "accountID": 7, "type": "movie", "title": "Synthetic movie"}}}})
		case "/api/v1/visits":
			w.Header().Set("X-Current-Page", "1")
			w.Header().Set("X-Total-Pages", "1")
			w.Header().Set("X-Total-Count", "1")
			fmt.Fprintf(w, `[{"id":1,"user_id":7,"started_at":%q,"ended_at":%s,"name":"Synthetic outing","status":"suggested","place":{"latitude":40,"longitude":-3}}]`, when.Format(time.RFC3339), end)
		default:
			http.NotFound(w, r)
		}
	})
	log.Fatal(http.ListenAndServe(":8080", handler))
}
