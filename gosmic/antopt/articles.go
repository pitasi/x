package antopt

import (
	"fmt"
	"net/http"

	"anto.pt/x/gosmic/antopt/articles"
	"anto.pt/x/gosmic/antopt/pages"
	"anto.pt/x/gosmic/templates"
	"anto.pt/x/socialimg"
)

func (ws *Website) articles(t *templates.T, mux *http.ServeMux) {
	a := articles.Load()
	articlesList := a.Flat()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		pages.RenderIndex(t, w, ws.common(r), articlesList)
	})

	propic, err := s.Open("static/images/propic_nobg.png")
	if err != nil {
		panic(fmt.Sprintf("can't open propic_nobg.png: %s", err))
	}

	font, err := s.Open("static/fonts/PPWriter-Bold.ttf")
	if err != nil {
		panic(fmt.Sprintf("can't open propic_nobg.png: %s", err))
	}

	coverGenerator, err := socialimg.NewGenerator(font, propic)
	if err != nil {
		panic(fmt.Sprintf("can't create cover generator: %s", err))
	}

	mux.HandleFunc("GET /socialimg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=31536000")
		if err := coverGenerator.Generate(w, "Antonio Pitasi", "https://anto.pt"); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			logger.Error("generating social image", "err", err)
			return
		}
	})

	mux.HandleFunc("GET /articles/{slug}", func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		article, found := a.Get(slug)
		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		pages.RenderArticle(t, w, ws.common(r), article)
	})

	mux.HandleFunc("GET /articles/covers/{slug}", func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		article, found := a.Get(slug)
		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=31536000")
		subtitle := fmt.Sprintf("written on %s", article.Date.Format("02 Jan 2006"))
		if err := coverGenerator.Generate(w, article.Title, subtitle); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			logger.Error("generating social image", "err", err)
			return
		}
	})

	feed, err := buildArticlesAtomFeed(a)
	if err != nil {
		panic(fmt.Sprintf("can't build atom feed: %v", err))
	}

	mux.HandleFunc("GET /articles/feed.atom", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n"))
		_, _ = w.Write(feed)
	})
}
