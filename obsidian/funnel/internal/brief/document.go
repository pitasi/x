package brief

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type Coverage struct {
	At    time.Time `json:"at"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}
type State struct {
	ConfigID string    `json:"config_id"`
	Identity string    `json:"identity,omitempty"`
	Verified bool      `json:"verified"`
	Status   string    `json:"status"`
	Attempt  Coverage  `json:"attempt"`
	Success  *Coverage `json:"success,omitempty"`
	Reason   string    `json:"reason,omitempty"`
}
type Refresh struct {
	ConfigID, Identity, Failure string
	Cues                        []Cue
}
type Document struct {
	Week   Week
	State  map[string]State
	Blocks map[string]string
}

var fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var escapedPunctuation = regexp.MustCompile(`&#[0-9]{1,3};`)
var reasons = map[string]bool{"network": true, "authentication": true, "http": true, "identity": true, "invalid_data": true, "pagination": true, "page_limit": true, "response_limit": true, "source_failure": true}

func Update(w Week, now time.Time, prior *Document, results map[string]Refresh) (*Document, error) {
	start, end, err := w.Envelope(now)
	if err != nil {
		return nil, err
	}
	doc := &Document{w, map[string]State{}, map[string]string{}}
	for _, src := range Sources {
		r, ok := results[src]
		if !ok || !fingerprintPattern.MatchString(r.ConfigID) || (r.Identity != "" && !fingerprintPattern.MatchString(r.Identity)) {
			return nil, errors.New("source identity")
		}
		old := State{}
		if prior != nil {
			if prior.Week.Name != w.Name {
				return nil, errors.New("week mismatch")
			}
			old = prior.State[src]
			if old.ConfigID != r.ConfigID || (old.Identity != "" && r.Identity != "" && old.Identity != r.Identity) {
				return nil, errors.New("source identity mismatch")
			}
			if now.Before(old.Attempt.At) {
				return nil, errors.New("clock moved backwards")
			}
		}
		s := State{ConfigID: r.ConfigID, Identity: r.Identity, Verified: r.Identity != "", Attempt: Coverage{now.UTC(), start, end}, Success: old.Success}
		if s.Identity == "" {
			s.Identity = old.Identity
		}
		if r.Failure == "" {
			if r.Identity == "" {
				return nil, errors.New("unverified source")
			}
			s.Status = "fresh"
			success := s.Attempt
			s.Success = &success
			blocks, err := cueBlocks(w, now, r.Cues)
			if err != nil {
				return nil, err
			}
			for _, day := range w.Days() {
				key := day + "/" + src
				block := blocks[key]
				if block == "" {
					if day > now.In(Madrid).Format("2006-01-02") {
						block = "- Not elapsed.\n"
					} else {
						block = emptyBlock(src)
					}
				}
				doc.Blocks[key] = block
			}
		} else {
			if !reasons[r.Failure] {
				return nil, errors.New("unsafe reason")
			}
			s.Reason = r.Failure
			s.Status = "unavailable"
			if s.Success != nil {
				s.Status = "stale"
			}
			for _, day := range w.Days() {
				key := day + "/" + src
				block := ""
				if prior != nil && old.Success != nil {
					previous := prior.Blocks[key]
					dayStart, _ := time.ParseInLocation("2006-01-02", day, Madrid)
					if dayStart.Before(old.Success.End) || (previous != emptyBlock(src) && previous != "- Not elapsed.\n" && previous != "- Unavailable.\n") {
						block = previous
					}
				}
				if block == "" || block == "- Not elapsed.\n" {
					if day > now.In(Madrid).Format("2006-01-02") {
						block = "- Not elapsed.\n"
					} else {
						block = "- Unavailable.\n"
					}
				}
				doc.Blocks[key] = block
			}
		}
		doc.State[src] = s
	}
	if err := doc.validate(); err != nil {
		return nil, err
	}
	return doc, nil
}
func emptyBlock(src string) string {
	if src == "plex" {
		return "- No Plex history returned.\n"
	}
	return "- No visit suggestions returned.\n"
}
func frontmatter(w Week) string {
	return fmt.Sprintf("---\ngenerator: funnel\nfunnel_schema: 1\nfunnel_week: %s\n---\n", w.Name)
}
func (d *Document) Bytes() []byte { return d.bytes(true, true) }

func (d *Document) bytes(weeklyLink, emojis bool) []byte {
	var b strings.Builder
	b.WriteString(frontmatter(d.Week))
	state, _ := json.Marshal(d.State)
	fmt.Fprintf(&b, "<!-- funnel:state %s -->\n\n# Memory cues · %s\n\n", state, d.Week.Name)
	if weeklyLink {
		fmt.Fprintf(&b, "Weekly note: [[%s]]\n\n", d.Week.Name)
	}
	b.WriteString("Generated memory cues — write personal notes elsewhere.\n\nTimes: Europe/Madrid fallback unless otherwise labelled.\n")
	for _, src := range Sources {
		s := d.State[src]
		fmt.Fprintf(&b, "\n%s: %s; checked %s", src, s.Status, s.Attempt.At.Format(time.RFC3339Nano))
		if s.Success != nil {
			fmt.Fprintf(&b, "; last success %s", s.Success.At.Format(time.RFC3339Nano))
		}
		if s.Reason != "" {
			fmt.Fprintf(&b, "; reason %s", s.Reason)
		}
		if !s.Verified {
			b.WriteString("; account unverified on this attempt")
		}
		b.WriteByte('\n')
	}
	b.WriteString("\nDawarich visit detection may be incomplete. Plex history does not prove completion.\n")
	for _, day := range d.Week.Days() {
		date, _ := time.Parse("2006-01-02", day)
		fmt.Fprintf(&b, "\n## %s · %s\n", date.Weekday(), day)
		for _, src := range Sources {
			title := "Places"
			if src == "plex" {
				title = "Plex history"
			}
			fmt.Fprintf(&b, "\n### %s · %s\n<!-- funnel:begin %s %s -->\n", title, d.State[src].Status, day, src)
			block := d.Blocks[day+"/"+src]
			if !emojis {
				block = strings.ReplaceAll(strings.ReplaceAll(block, "- 📍 ", "- "), "- 🍿 ", "- ")
			}
			b.WriteString(block)
			fmt.Fprintf(&b, "<!-- funnel:end %s %s -->\n", day, src)
		}
	}
	return []byte(b.String())
}
func validCoverage(w Week, c Coverage) bool {
	a, b, err := w.Envelope(c.At)
	return err == nil && !c.At.IsZero() && c.Start.Equal(a) && c.End.Equal(b) && !c.End.After(c.At)
}
func (d *Document) validate() error {
	if len(d.State) != 2 || len(d.Blocks) != 14 {
		return errors.New("invalid document")
	}
	for _, src := range Sources {
		s, ok := d.State[src]
		if !ok || !fingerprintPattern.MatchString(s.ConfigID) || (s.Identity != "" && !fingerprintPattern.MatchString(s.Identity)) || !validCoverage(d.Week, s.Attempt) {
			return errors.New("invalid state")
		}
		if s.Success != nil && (!validCoverage(d.Week, *s.Success) || s.Success.At.After(s.Attempt.At) || s.Identity == "") {
			return errors.New("invalid coverage")
		}
		switch s.Status {
		case "fresh":
			if s.Success == nil || *s.Success != s.Attempt || s.Reason != "" || !s.Verified || s.Identity == "" {
				return errors.New("invalid fresh state")
			}
		case "stale":
			if s.Success == nil || !reasons[s.Reason] {
				return errors.New("invalid stale state")
			}
		case "unavailable":
			if s.Success != nil || !reasons[s.Reason] {
				return errors.New("invalid unavailable state")
			}
		default:
			return errors.New("invalid status")
		}
		if s.Verified && s.Identity == "" {
			return errors.New("invalid identity")
		}
		for _, day := range d.Week.Days() {
			block, ok := d.Blocks[day+"/"+src]
			if !ok || block == "" || !strings.HasSuffix(block, "\n") || strings.Contains(block, "<!--") || strings.ContainsAny(block, "\r\x00") {
				return errors.New("invalid cue block")
			}
			for _, line := range strings.Split(strings.TrimSuffix(block, "\n"), "\n") {
				if !strings.HasPrefix(line, "- ") {
					return errors.New("invalid cue line")
				}
				plain := line
				if index := strings.Index(line, "["); index >= 0 {
					suffix := line[index:]
					if !strings.HasSuffix(line[:index], " · ") || !strings.HasPrefix(suffix, "[Map](") || !strings.HasSuffix(suffix, ")") || !safeMap(strings.TrimSuffix(strings.TrimPrefix(suffix, "[Map]("), ")")) {
						return errors.New("unsafe saved link")
					}
					plain = line[:index]
				}
				if strings.ContainsAny(escapedPunctuation.ReplaceAllString(plain, ""), "<>[]`*_#\\") {
					return errors.New("unsafe saved cue")
				}
				for _, r := range plain {
					if unicode.IsControl(r) {
						return errors.New("unsafe saved control")
					}
				}
			}
		}
	}
	return nil
}

func ParseDocument(w Week, raw []byte) (*Document, error) {
	if len(raw) > 4<<20 {
		return nil, errors.New("document limit")
	}
	text := string(raw)
	prefix := frontmatter(w) + "<!-- funnel:state "
	if !strings.HasPrefix(text, prefix) {
		return nil, errors.New("unowned or unsupported document")
	}
	remaining := strings.TrimPrefix(text, prefix)
	line, _, ok := strings.Cut(remaining, "\n")
	if !ok || !strings.HasSuffix(line, " -->") {
		return nil, errors.New("invalid state comment")
	}
	doc := &Document{Week: w, Blocks: map[string]string{}}
	dec := json.NewDecoder(strings.NewReader(strings.TrimSuffix(line, " -->")))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc.State); err != nil {
		return nil, errors.New("invalid state JSON")
	}
	for _, day := range w.Days() {
		for _, src := range Sources {
			begin := fmt.Sprintf("<!-- funnel:begin %s %s -->\n", day, src)
			end := fmt.Sprintf("<!-- funnel:end %s %s -->\n", day, src)
			if strings.Count(text, begin) != 1 || strings.Count(text, end) != 1 {
				return nil, errors.New("ambiguous cue markers")
			}
			_, rest, _ := strings.Cut(text, begin)
			block, _, ok := strings.Cut(rest, end)
			if !ok {
				return nil, errors.New("unbalanced markers")
			}
			doc.Blocks[day+"/"+src] = block
		}
	}
	if err := doc.validate(); err != nil {
		return nil, err
	}
	// Canonical byte equality also rejects duplicate JSON keys, unknown fields,
	// out-of-order/nested markers and conflicting ownership or personal sections.
	canonical := bytes.Equal(raw, doc.Bytes())
	for _, link := range []bool{false, true} {
		for _, emojis := range []bool{false, true} {
			canonical = canonical || bytes.Equal(raw, doc.bytes(link, emojis))
		}
	}
	if !canonical {
		return nil, errors.New("noncanonical document")
	}
	return doc, nil
}
