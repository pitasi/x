// Package dawarich consumes existing visit suggestions, never raw GPS points.
// Contract: https://github.com/Freika/dawarich/blob/master/app/controllers/api/v1/visits_controller.rb
// and app/services/visits/find_in_time.rb.
package dawarich

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"anto.pt/x/obsidian/funnel/internal/source"
)

type Cue struct {
	ID                  string
	Start               time.Time
	End                 *time.Time
	Name, State, MapURL string
}
type Result struct {
	ConfigID, Identity string
	Cues               []Cue
}
type Provider struct{ client *source.Client }

func New(base, token string, client *http.Client) (*Provider, error) {
	c, err := source.New(base, token, "Authorization", client)
	if err != nil {
		return nil, err
	}
	return &Provider{c}, nil
}
func (p *Provider) ConfigID() string { return p.client.ConfigID("dawarich") }

type coordinate float64

func (c *coordinate) UnmarshalJSON(b []byte) error {
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil {
		return err
	}
	*c = coordinate(f)
	return nil
}

type visit struct {
	ID           int64
	UserID       int64   `json:"user_id"`
	StartedAt    string  `json:"started_at"`
	EndedAt      *string `json:"ended_at"`
	Name, Status string
	Place        struct{ Latitude, Longitude *coordinate }
}

func (p *Provider) Fetch(ctx context.Context, start, end time.Time) (result Result, err error) {
	operation := "interval_validation"
	defer func() {
		if err != nil {
			err = source.WithOperation(operation, err)
		}
	}()
	result = Result{ConfigID: p.ConfigID()}
	if !start.Before(end) {
		return result, errors.New("interval")
	}
	operation = "users_me"
	var me struct{ User struct{ Email string } }
	if _, err := p.client.JSON(ctx, "/api/v1/users/me", nil, nil, &me); err != nil {
		return result, err
	}
	if strings.TrimSpace(me.User.Email) == "" {
		return result, errors.New("identity")
	}
	result.Identity = source.Fingerprint(result.ConfigID, me.User.Email)
	seen := map[int64]visit{}
	pages, total, count := -1, -1, 0
	var user int64
	for page := 1; page <= source.MaxPages; page++ {
		operation = fmt.Sprintf("visits_page_%d", page)
		var rows []visit
		q := url.Values{"start_at": {start.UTC().Format(time.RFC3339)}, "end_at": {end.UTC().Format(time.RFC3339)}, "page": {strconv.Itoa(page)}, "per_page": {"100"}}
		headers, err := p.client.JSON(ctx, "/api/v1/visits", q, nil, &rows)
		if err != nil {
			return result, err
		}
		current, e1 := strconv.Atoi(headers.Get("X-Current-Page"))
		last, e2 := strconv.Atoi(headers.Get("X-Total-Pages"))
		size, e3 := strconv.Atoi(headers.Get("X-Total-Count"))
		if e1 != nil || e2 != nil || e3 != nil || current != page || last < 0 || last > source.MaxPages || size < 0 || size > 10000 || rows == nil {
			return result, errors.New("pagination")
		}
		if pages < 0 {
			pages = last
			total = size
		}
		if last != pages || size != total || (len(rows) == 0 && count < total) {
			return result, errors.New("pagination")
		}
		count += len(rows)
		for _, row := range rows {
			if row.ID <= 0 || row.UserID <= 0 {
				return result, errors.New("invalid_data")
			}
			if user == 0 {
				user = row.UserID
			}
			if row.UserID != user {
				return result, errors.New("identity")
			}
			if old, ok := seen[row.ID]; ok {
				if !reflect.DeepEqual(old, row) {
					return result, errors.New("invalid_data")
				}
				continue
			}
			seen[row.ID] = row
			if row.Status == "declined" {
				continue
			}
			if row.Status != "suggested" && row.Status != "confirmed" {
				return result, errors.New("invalid_data")
			}
			instant, err := time.Parse(time.RFC3339Nano, row.StartedAt)
			if err != nil || instant.Year() < 1 || instant.Year() > 9999 {
				return result, errors.New("invalid_data")
			}
			var until *time.Time
			if row.EndedAt != nil {
				parsed, err := time.Parse(time.RFC3339Nano, *row.EndedAt)
				if err != nil || !parsed.After(instant) {
					return result, errors.New("invalid_data")
				}
				until = &parsed
			}
			if row.Place.Latitude == nil || row.Place.Longitude == nil {
				return result, errors.New("invalid_data")
			}
			lat, lon := float64(*row.Place.Latitude), float64(*row.Place.Longitude)
			if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
				return result, errors.New("invalid_data")
			}
			if instant.Before(start) || !instant.Before(end) {
				continue
			}
			coords := strconv.FormatFloat(lat, 'f', -1, 64) + "," + strconv.FormatFloat(lon, 'f', -1, 64)
			link := "https://www.google.com/maps/search/?" + url.Values{"api": {"1"}, "query": {coords}}.Encode()
			result.Cues = append(result.Cues, Cue{strconv.FormatInt(row.ID, 10), instant, until, row.Name, row.Status, link})
		}
		if page >= pages {
			if count != total {
				return result, errors.New("pagination")
			}
			return result, nil
		}
	}
	return result, errors.New("page_limit")
}
