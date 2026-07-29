package main

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"go.abhg.dev/goldmark/frontmatter"
)

//go:embed templates
var TemplatesFS embed.FS

//go:embed static
var StaticFS embed.FS

func main() {
	obsidianVault := os.Args[1]

	files, _ := filepath.Glob(obsidianVault + "/References/*.md")

	var (
		shows  []frontmatterData
		movies []frontmatterData
		places []frontmatterData
		books  []frontmatterData
	)

	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			panic(fmt.Sprintf("reading %s: %v", f, err))
		}

		data, err := P(f, src)
		if err != nil {
			continue
		}

		if slices.Contains(data.Categories, "[[Shows]]") {
			shows = append(shows, data)
		}
		if slices.Contains(data.Categories, "[[Movies]]") {
			movies = append(movies, data)
		}
		if slices.Contains(data.Categories, "[[Places]]") {
			places = append(places, data)
		}
		if slices.Contains(data.Categories, "[[Books]]") {
			books = append(books, data)
		}
	}

	sortByCreatedDesc(movies)
	sortByCreatedDesc(shows)
	sortByCreatedDesc(books)
	sortByCreatedDesc(places)

	static, err := fs.Sub(StaticFS, "static")
	if err != nil {
		log.Fatal(err)
	}
	http.Handle("GET /style.css", http.FileServer(http.FS(static)))
	http.Handle("GET /app.js", http.FileServer(http.FS(static)))

	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		t, err := template.New("root").
			Funcs(templateFuncs()).
			ParseFS(TemplatesFS, "templates/layout.html", "templates/index.html")
		if err != nil {
			log.Println("template: index.html:", err)
			return
		}

		data := struct {
			Movies collectionPreview
			Shows  collectionPreview
			Books  collectionPreview
			Places collectionPreview
		}{
			Movies: previewFor(movies, 3),
			Shows:  previewFor(shows, 3),
			Books:  previewFor(books, 3),
			Places: previewFor(places, 3),
		}

		_ = t.ExecuteTemplate(w, "layout.html", data)
	})

	http.HandleFunc("GET /ratings/{$}", func(w http.ResponseWriter, r *http.Request) {
		t, err := template.New("root").
			Funcs(templateFuncs()).
			ParseFS(TemplatesFS, "templates/layout.html", "templates/ratings.html")
		if err != nil {
			log.Println("template: ratings.html:", err)
			return
		}

		_ = t.ExecuteTemplate(w, "layout.html", nil)
	})

	http.HandleFunc("GET /shows/{$}", sectionHandler("shows.html", struct {
		Shows []frontmatterData
	}{
		Shows: shows,
	}))
	http.HandleFunc("GET /movies/{$}", sectionHandler("movies.html", struct {
		Movies []frontmatterData
	}{
		Movies: movies,
	}))
	http.HandleFunc("GET /places/{$}", sectionHandler("places.html", struct {
		Places []frontmatterData
	}{
		Places: places,
	}))
	http.HandleFunc("GET /books/{$}", sectionHandler("books.html", struct {
		Books []frontmatterData
	}{
		Books: books,
	}))

	topMovies, topShows := topRated(movies), topRated(shows)
	http.HandleFunc("GET /movies/top/{$}", sectionHandler("top.html", topPage{
		Slug:   "movies",
		Label:  "movies",
		Noun:   "titles",
		Items:  topMovies,
		Genres: genresOf(topMovies),
	}))
	http.HandleFunc("GET /shows/top/{$}", sectionHandler("top.html", topPage{
		Slug:   "shows",
		Label:  "tv shows",
		Noun:   "shows",
		Items:  topShows,
		Genres: genresOf(topShows),
	}))

	log.Println("listening on", "0.0.0.0:8080")
	panic(http.ListenAndServe("0.0.0.0:8080", nil))
}

func sectionHandler(templateName string, data any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, err := template.New("root").
			Funcs(templateFuncs()).
			ParseFS(TemplatesFS, "templates/layout.html", "templates/"+templateName)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("unexpected error"))
			log.Println("err", err)
			return
		}

		err = t.ExecuteTemplate(w, "layout.html", data)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("unexpected error"))
			log.Println("err", err)
			return
		}
	}
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"prettyDate": prettyDate,
		"yyyymmdd":   yyyymmdd,
		"prettyLink": prettyLinks,
		"sortKey":    sortKey,
		"ratingMeter": func(rating int) []bool {
			m := make([]bool, 7)
			for i := 0; i < rating && i < 7; i++ {
				m[i] = true
			}
			return m
		},
		"emptyMeter": func() []bool { return make([]bool, 7) },
		"lower":      strings.ToLower,
		// imdb's cdn serves the untouched poster at ~4MB; ask it for a scaled one
		"coverAt": func(url string, w int) string {
			if base, ok := strings.CutSuffix(url, "._V1_.jpg"); ok {
				return fmt.Sprintf("%s._V1_QL75_UX%d_.jpg", base, w)
			}
			return url
		},
		// pipe-delimited so the client can match a whole genre, not a substring
		"genreAttr": func(gs []string) string {
			if len(gs) == 0 {
				return ""
			}
			var b strings.Builder
			b.WriteByte('|')
			for _, g := range gs {
				b.WriteString(strings.ToLower(prettyLinks(g)))
				b.WriteByte('|')
			}
			return b.String()
		},
		"dateShort": func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.Format("2006-01-02")
		},
		"unixTime": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return strconv.FormatInt(t.Unix(), 10)
		},
	}
}

type topPage struct {
	Slug   string
	Label  string
	Noun   string
	Items  []frontmatterData
	Genres []string
}

// topRated keeps only the 6s and 7s, best first.
func topRated(items []frontmatterData) []frontmatterData {
	var out []frontmatterData
	for _, it := range items {
		if it.Rating >= 6 {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Rating != out[j].Rating {
			return out[i].Rating > out[j].Rating
		}
		return sortKey(out[i].Title) < sortKey(out[j].Title)
	})
	return out
}

func genresOf(items []frontmatterData) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		for _, g := range it.Genre {
			g = prettyLinks(g)
			if g == "" || seen[g] {
				continue
			}
			seen[g] = true
			out = append(out, g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return sortKey(out[i]) < sortKey(out[j]) })
	return out
}

type collectionPreview struct {
	Count int
	Items []frontmatterData
}

func previewFor(items []frontmatterData, n int) collectionPreview {
	sorted := make([]frontmatterData, len(items))
	copy(sorted, items)
	sortByCreatedDesc(sorted)
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	return collectionPreview{Count: len(items), Items: sorted}
}

func sortByCreatedDesc(items []frontmatterData) {
	sort.SliceStable(items, func(i, j int) bool {
		ai, bi := items[i].Created, items[j].Created
		if !ai.Equal(bi) {
			return ai.After(bi)
		}
		return sortKey(items[i].Title) < sortKey(items[j].Title)
	})
}

func sortKey(s string) string {
	low := strings.ToLower(strings.TrimSpace(s))
	for _, prefix := range []string{"the ", "a ", "an "} {
		if rest, ok := strings.CutPrefix(low, prefix); ok {
			return rest
		}
	}
	return low
}

type frontmatterData struct {
	Title      string
	Source     string    `yaml:"source"`
	Created    time.Time `yaml:"created"`
	Tags       []string  `yaml:"tags"`
	Categories []string  `yaml:"categories"`

	// Movies/Shows

	Genre     []string `yaml:"genre"`
	Rating    int      `yaml:"rating"`
	ScoreIMDB float64  `yaml:"scoreImdb"`
	Cover     string   `yaml:"cover"`
	Plot      string   `yaml:"plot"`
	Year      int      `yaml:"year"`

	// Places

	Loc         []string `yaml:"loc"`
	ScoreGoogle float64  `yaml:"scoreGoogle"`
	Address     string   `yaml:"address"`
	URL         string   `yaml:"url"`

	// Books

	ScoreGR float64 `yaml:"scoreGr"`
}

func P(path string, src []byte) (frontmatterData, error) {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.Linkify,
			extension.GFM,
			extension.Table,
			extension.Footnote,
			extension.Strikethrough,
			&frontmatter.Extender{},
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
		),
	)

	ctx := parser.NewContext()
	out := &bytes.Buffer{}
	if err := md.Convert(src, out, parser.WithContext(ctx)); err != nil {
		panic(err)
	}

	d := frontmatter.Get(ctx)
	if d == nil {
		return frontmatterData{}, errors.New("no frontmatter")
	}

	var meta frontmatterData
	if err := d.Decode(&meta); err != nil {
		return frontmatterData{}, err
	}

	if meta.Title == "" {
		meta.Title = pathToTitle(path)
	}

	return meta, nil
}

func prettyDate(t time.Time) string {
	if t.IsZero() {
		return "2000-01-01"
	}
	return t.Format(time.DateOnly)
}

func yyyymmdd(t time.Time) string {
	return t.Format(time.DateOnly)
}

func pathToTitle(path string) string {
	b := filepath.Base(path)
	b = strings.ReplaceAll(b, "_", " ")
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == '.' {
			return b[0:i]
		}
	}
	return b
}

func prettyLinks(link string) string {
	return strings.TrimPrefix(strings.TrimSuffix(link, "]]"), "[[")
}
