// Package plex reads per-account Plex playback history, not proof of completion.
// Contract: https://developer.plex.tv/pms/ (List Playback History, Pagination).
package plex

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"anto.pt/x/obsidian/funnel/internal/source"
)

type Cue struct {
	ID   string
	Time time.Time
	Text string
}
type Result struct {
	ConfigID, Identity string
	Cues               []Cue
}
type Provider struct {
	client   *source.Client
	selector string
}

func New(base, token, selector string, client *http.Client) (*Provider, error) {
	if selector == "" || len(selector) > 256 || strings.ContainsAny(selector, "\r\n") {
		return nil, errors.New("configuration")
	}
	c, err := source.New(base, token, "X-Plex-Token", client)
	if err != nil {
		return nil, err
	}
	return &Provider{c, selector}, nil
}
func (p *Provider) ConfigID() string { return p.client.ConfigID(p.selector) }

type item struct {
	HistoryKey                               string `json:"historyKey"`
	AccountID                                *int64 `json:"accountID"`
	ViewedAt                                 int64  `json:"viewedAt"`
	Type, Title, GrandparentTitle, RatingKey string
	Year, ParentIndex, Index                 *int
}
type container struct {
	MachineIdentifier       string
	Size, Offset, TotalSize *int
	Metadata                []item
	Account                 []struct {
		ID   int64
		Name string
	}
}
type response struct{ MediaContainer *container }

var numeric = regexp.MustCompile(`^[0-9]+$`)
var historyID = regexp.MustCompile(`^/status/sessions/history/([0-9]+)$`)

func (p *Provider) Fetch(ctx context.Context, start, end time.Time) (Result, error) {
	result := Result{ConfigID: p.ConfigID()}
	if !start.Before(end) {
		return result, errors.New("interval")
	}
	var identity response
	if _, err := p.client.JSON(ctx, "/identity", nil, nil, &identity); err != nil {
		return result, err
	}
	if identity.MediaContainer == nil || identity.MediaContainer.MachineIdentifier == "" {
		return result, errors.New("identity")
	}
	var accounts response
	if _, err := p.client.JSON(ctx, "/accounts", nil, nil, &accounts); err != nil {
		return result, err
	}
	if accounts.MediaContainer == nil {
		return result, errors.New("identity")
	}
	var uid int64
	matches := 0
	for _, a := range accounts.MediaContainer.Account {
		if strconv.FormatInt(a.ID, 10) == p.selector || a.Name == p.selector {
			uid = a.ID
			matches++
		}
	}
	if matches != 1 || uid <= 0 {
		return result, errors.New("identity")
	}
	result.Identity = source.Fingerprint(result.ConfigID, identity.MediaContainer.MachineIdentifier, strconv.FormatInt(uid, 10))
	seen := map[string]item{}
	enrichment := map[string]item{}
	offset, total := 0, -1
	for page := 0; page < source.MaxPages; page++ {
		var data response
		// The operators are part of the query key; Values.Encode adds the final '='.
		q := url.Values{"accountID": {strconv.FormatInt(uid, 10)}, "viewedAt>": {strconv.FormatInt(start.Unix(), 10)}, "viewedAt<": {strconv.FormatInt(end.Unix(), 10)}, "sort": {"viewedAt:asc"}}
		h := http.Header{"X-Plex-Container-Start": {strconv.Itoa(offset)}, "X-Plex-Container-Size": {"100"}}
		if _, err := p.client.JSON(ctx, "/status/sessions/history/all", q, h, &data); err != nil {
			return result, err
		}
		c := data.MediaContainer
		if c == nil || c.Size == nil || c.Offset == nil || c.TotalSize == nil || *c.Offset != offset || *c.Size != len(c.Metadata) || *c.TotalSize < 0 || *c.TotalSize > 10000 {
			return result, errors.New("pagination")
		}
		if total < 0 {
			total = *c.TotalSize
		}
		if total != *c.TotalSize || offset+len(c.Metadata) > total || (len(c.Metadata) == 0 && offset < total) {
			return result, errors.New("pagination")
		}
		for _, row := range c.Metadata {
			if row.AccountID == nil {
				return result, errors.New("invalid_data")
			}
			if *row.AccountID != uid {
				continue
			}
			switch row.Type {
			case "movie", "episode":
			case "track", "clip", "photo", "album", "artist":
				continue
			default:
				return result, errors.New("invalid_data")
			}
			match := historyID.FindStringSubmatch(row.HistoryKey)
			if match == nil || row.ViewedAt <= 0 || row.ViewedAt > 253402300799 {
				return result, errors.New("invalid_data")
			}
			if old, ok := seen[row.HistoryKey]; ok {
				if !reflect.DeepEqual(old, row) {
					return result, errors.New("invalid_data")
				}
				continue
			}
			seen[row.HistoryKey] = row
			instant := time.Unix(row.ViewedAt, 0).UTC()
			if instant.Before(start) || !instant.Before(end) {
				continue
			}
			limited := false
			if row.Title == "" || (row.Type == "episode" && (row.GrandparentTitle == "" || row.Index == nil || row.ParentIndex == nil)) {
				if numeric.MatchString(row.RatingKey) && len(enrichment) < 100 {
					meta, ok := enrichment[row.RatingKey]
					if !ok {
						var lookup response
						_, err := p.client.JSON(ctx, "/library/metadata/"+row.RatingKey, nil, nil, &lookup)
						if err == nil && lookup.MediaContainer != nil && len(lookup.MediaContainer.Metadata) == 1 && lookup.MediaContainer.Metadata[0].RatingKey == row.RatingKey {
							meta = lookup.MediaContainer.Metadata[0]
						}
						enrichment[row.RatingKey] = meta
					}
					if row.Title == "" {
						row.Title = meta.Title
					}
					if row.GrandparentTitle == "" {
						row.GrandparentTitle = meta.GrandparentTitle
					}
					if row.Index == nil {
						row.Index = meta.Index
					}
					if row.ParentIndex == nil {
						row.ParentIndex = meta.ParentIndex
					}
					if row.Year == nil {
						row.Year = meta.Year
					}
				}
				limited = row.Title == "" || (row.Type == "episode" && (row.GrandparentTitle == "" || row.Index == nil || row.ParentIndex == nil))
			}
			title := row.Title
			if title == "" {
				title = "Unknown title"
			}
			if row.Type == "movie" {
				title = "Movie · " + title
				if row.Year != nil && *row.Year > 0 {
					title += fmt.Sprintf(" (%d)", *row.Year)
				}
			} else {
				show := row.GrandparentTitle
				if show == "" {
					show = "Unknown show"
				}
				numbering := ""
				if row.ParentIndex != nil && *row.ParentIndex >= 0 {
					numbering += fmt.Sprintf(" S%02d", *row.ParentIndex)
				}
				if row.Index != nil && *row.Index >= 0 {
					numbering += fmt.Sprintf("E%02d", *row.Index)
				}
				title = "Episode · " + show + numbering + " · " + title
			}
			if limited {
				title += " · limited metadata"
			}
			result.Cues = append(result.Cues, Cue{match[1], instant, title})
		}
		offset += len(c.Metadata)
		if offset == total {
			return result, nil
		}
	}
	return result, errors.New("page_limit")
}
