package antopt

import (
	"embed"
	"html/template"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"anto.pt/x/log"

	"anto.pt/x/gosmic/antopt/components"
	"anto.pt/x/gosmic/antopt/lastfm"
	"anto.pt/x/gosmic/antopt/pages"
	"anto.pt/x/gosmic/fsx"
	"anto.pt/x/gosmic/httpx"
	"anto.pt/x/gosmic/static"
	"anto.pt/x/gosmic/templates"
)

//go:embed components/*.html
//go:embed pages/*.html
var resources embed.FS

//go:embed static/*
var s embed.FS

var logger = log.Module("anto.pt")

type Website struct {
	Colors []template.CSS

	debug components.Debug
}

var _ httpx.Website = &Website{}

func (ws *Website) Register(devmode bool) http.Handler {
	info, ok := debug.ReadBuildInfo()
	if ok {
		ws.debug.GoVersion = info.GoVersion
		for _, kv := range info.Settings {
			if kv.Key == "GOARCH" {
				ws.debug.BuildArch = kv.Value
			}
			if kv.Key == "GOOS" {
				ws.debug.BuildOS = kv.Value
			}
			if kv.Key == "vcs.revision" {
				ws.debug.GosmicVersion = kv.Value
			}
		}
	}

	mux := http.NewServeMux()

	sfs := static.NewStaticFS(fsx.Or(devmode, s, "./antopt"))

	t := templates.New(fsx.Or(devmode, resources, "./antopt"), devmode, template.FuncMap{
		"hash": sfs.FileHash,
	})

	mux.Handle("GET /static/", sfs.Handler())
	mux.HandleFunc("POST /submit-color", ws.preferences)
	ws.articles(t, mux)
	ws.colophon(t, mux)
	ws.cv(t, mux)
	ws.plausible(mux)
	ws.redirects(mux)
	ws.uses(t, mux)
	ws.x(mux)

	lastfmAPIKey := os.Getenv("LASTFM_API_KEY")
	if lastfmAPIKey != "" {
		c := &lastfm.Client{
			APIKey: lastfmAPIKey,
		}
		ws.nowPlaying(c, mux)
	}

	var h http.Handler = mux
	h = httpx.Recoverer(h)
	h = httpx.Logger(h)
	h = httpx.Compress(h)
	h = userMiddleware(h)
	return h
}

func (ws *Website) common(r *http.Request) pages.Common {
	user := GetUser(r.Context())
	var selectedColor template.CSS
	if user != nil {
		selectedColor = user.Preferences.Background
	}

	currentURL := ""
	switch {
	case r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/articles"):
		currentURL = "/"
	case strings.HasPrefix(r.URL.Path, "/uses"):
		currentURL = "/uses"
	}

	return pages.NewCommon(
		[]components.NavItem{
			{Title: "writing", URL: "/"},
			{Title: "uses", URL: "/uses"},
			{Title: "pics", URL: "https://anto.ph"},
			{Title: "cv", URL: "/cv"},
			{Title: "github", URL: "https://github.com/Pitasi"},
			{Title: "rss", URL: "/articles/feed.atom"},
		},
		currentURL,
		ws.Colors,
		selectedColor,
		ws.debug,
	)
}
