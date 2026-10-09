package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"anto.pt/x/obsidian/funnel/internal/brief"
	"anto.pt/x/obsidian/funnel/internal/dawarich"
	"anto.pt/x/obsidian/funnel/internal/plex"
	"anto.pt/x/obsidian/funnel/internal/source"
)

const help = "Funnel: generated weekly memory cues.\nUsage: funnel sync --vault PATH [--week YYYY-Www] [--dry-run]\nBoth Dawarich and Plex environment configurations are required.\nExit: 0 fresh, 2 safely degraded, 1 fatal.\n"

func reportSourceFailure(diagnostics io.Writer, week, name string, err error) {
	failure := source.WithOperation("refresh", err)
	fmt.Fprintf(diagnostics, "funnel: source refresh failed week=%s source=%s operation=%s category=%s", week, name, failure.Operation, failure.Category)
	if failure.Attempts > 0 {
		fmt.Fprintf(diagnostics, " attempts=%d", failure.Attempts)
	}
	if failure.HTTPStatus > 0 {
		fmt.Fprintf(diagnostics, " http_status=%d", failure.HTTPStatus)
	}
	fmt.Fprintln(diagnostics)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, time.Now().UTC(), nil, os.Stdout, os.Stderr))
}
func run(ctx context.Context, args []string, env func(string) string, now time.Time, client *http.Client, out, diagnostics io.Writer) int {
	fatal := func(message string) int { fmt.Fprintln(diagnostics, "funnel: "+message); return 1 }
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		fmt.Fprint(out, help)
		return 0
	}
	if len(args) == 0 || args[0] != "sync" {
		return fatal("expected sync; use --help")
	}
	flags := flag.NewFlagSet("sync", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	vault := flags.String("vault", "", "vault directory")
	name := flags.String("week", "", "ISO week")
	dry := flags.Bool("dry-run", false, "do not write")
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprint(out, help)
			return 0
		}
		return fatal("invalid options")
	}
	if flags.NArg() != 0 || *vault == "" {
		return fatal("vault and valid options required")
	}
	weeks, err := brief.SelectWeeks(*name, now)
	if err != nil {
		return fatal("invalid ISO week")
	}
	v, err := brief.OpenVault(*vault, weeks)
	if err != nil {
		return fatal("unsafe or unreadable vault/output")
	}
	defer v.Close()
	p, err := plex.New(env("PLEX_URL"), env("PLEX_TOKEN"), env("PLEX_USER_ID"), client)
	if err != nil {
		return fatal("invalid Plex configuration")
	}
	d, err := dawarich.New(env("DAWARICH_URL"), env("DAWARICH_TOKEN"), client)
	if err != nil {
		return fatal("invalid Dawarich configuration")
	}
	for _, w := range weeks {
		if _, _, err := w.Envelope(now); err != nil {
			return fatal("week is unelapsed")
		}
		if prev := v.Previous[w.Name]; prev != nil {
			if prev.State["plex"].ConfigID != p.ConfigID() || prev.State["dawarich"].ConfigID != d.ConfigID() {
				return fatal("source configuration identity mismatch")
			}
		}
	}
	code := 0
	docs := map[string]*brief.Document{}
	for _, w := range weeks {
		start, end, err := w.Envelope(now)
		if err != nil {
			return fatal("week is unelapsed")
		}
		var cues []brief.Cue
		pr, pe := p.Fetch(ctx, start, end)
		dr, de := d.Fetch(ctx, start, end)
		if pe != nil {
			reportSourceFailure(diagnostics, w.Name, "plex", pe)
		}
		if de != nil {
			reportSourceFailure(diagnostics, w.Name, "dawarich", de)
		}
		if ctx.Err() != nil {
			return fatal("cancelled")
		}
		if pe != nil || de != nil {
			code = 2
		}
		if pe == nil {
			for _, c := range pr.Cues {
				cues = append(cues, brief.Cue{Source: "plex", ID: c.ID, Start: c.Time, Text: c.Text})
			}
		}
		if de == nil {
			for _, c := range dr.Cues {
				text := c.Name
				if text == "" {
					text = "Approximate stop"
				}
				uncertainty := "suggested · approximate"
				if c.State == "confirmed" {
					uncertainty = "confirmed in Dawarich · source-reported"
				}
				cues = append(cues, brief.Cue{Source: "dawarich", ID: c.ID, Start: c.Start, End: c.End, Text: text, Uncertainty: uncertainty, Link: c.MapURL})
			}
		}
		reason := func(err error) string {
			if err != nil {
				return "source_failure"
			}
			return ""
		}
		results := map[string]brief.Refresh{
			"plex":     {ConfigID: pr.ConfigID, Identity: pr.Identity, Failure: reason(pe), Cues: cues},
			"dawarich": {ConfigID: dr.ConfigID, Identity: dr.Identity, Failure: reason(de), Cues: cues},
		}
		doc, err := brief.Update(w, now, v.Previous[w.Name], results)
		if err != nil {
			return fatal("unsafe document or source identity mismatch")
		}
		for _, token := range []string{env("PLEX_TOKEN"), env("DAWARICH_TOKEN")} {
			body := string(doc.Bytes())
			if strings.Contains(body, token) || strings.Contains(body, brief.Escape(token)) {
				return fatal("credential-bearing output refused")
			}
		}
		docs[w.Name] = doc
		for _, src := range brief.Sources {
			fmt.Fprintf(diagnostics, "%s %s: %s\n", w.Name, src, doc.State[src].Status)
		}
	}
	if ctx.Err() != nil {
		return fatal("cancelled")
	}
	changed, err := v.Write(docs, *dry)
	if err != nil {
		return fatal("output write/preflight failed")
	}
	for _, path := range changed {
		if *dry {
			fmt.Fprintf(out, "would refresh %s\n", path)
		} else {
			fmt.Fprintln(out, path)
		}
	}
	return code
}
