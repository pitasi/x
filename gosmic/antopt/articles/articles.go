package articles

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"iter"
	"sort"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"go.abhg.dev/goldmark/frontmatter"

	"anto.pt/x/gosmic/markdown"
)

//go:embed *.md
var articlesFS embed.FS

type Articles struct {
	list   []Article
	bySlug map[string]int
}

type Article struct {
	Title       string
	Slug        string
	Date        time.Time
	Description string
	Categories  []string
	Published   bool
	Content     template.HTML
}

type ArticlesByYear struct {
	Year  string
	Posts []ArticleLink
}

type ArticleLink struct {
	Title       string
	Description string
	Date        string
	URL         string
}

func Load() Articles {
	files, err := fs.ReadDir(articlesFS, ".")
	if err != nil {
		panic(err)
	}

	var list []Article
	for _, file := range files {
		if file.IsDir() {
			// TODO: handle directories
			continue
		}
		if !strings.HasSuffix(file.Name(), ".md") {
			continue
		}

		fileContent, err := fs.ReadFile(articlesFS, file.Name())
		if err != nil {
			panic(err)
		}
		article := NewArticle(strings.TrimSuffix(file.Name(), ".md"), fileContent)

		list = append(list, article)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Date.After(list[j].Date)
	})

	bySlug := make(map[string]int)
	for i, article := range list {
		bySlug[article.Slug] = i
	}

	return Articles{
		list:   list,
		bySlug: bySlug,
	}
}

func (a Articles) Get(slug string) (Article, bool) {
	idx, found := a.bySlug[slug]
	if !found {
		return Article{}, false
	}
	return a.list[idx], true
}

func (a Articles) List() []Article {
	return a.list
}

func (a Articles) Published() iter.Seq2[int, Article] {
	return func(yield func(int, Article) bool) {
		for i, a := range a.List() {
			if !a.Published {
				continue
			}
			if !yield(i, a) {
				return
			}
		}
	}
}

func (a Articles) Flat() []ArticleLink {
	var posts []ArticleLink
	for _, article := range a.Published() {
		posts = append(posts, ArticleLink{
			Title:       article.Title,
			Description: article.Description,
			Date:        article.Date.Format("2006-01-02"),
			URL:         "/articles/" + article.Slug,
		})
	}
	return posts
}

func (a Articles) ByYear() []ArticlesByYear {
	var lastYear string
	var postsByYear []ArticlesByYear
	for _, article := range a.Published() {
		year := article.Date.Format("2006")
		if lastYear != year {
			lastYear = year
			postsByYear = append(postsByYear, ArticlesByYear{
				Year:  year,
				Posts: []ArticleLink{},
			})
		}
		postsByYear[len(postsByYear)-1].Posts = append(postsByYear[len(postsByYear)-1].Posts, ArticleLink{
			Title:       article.Title,
			Description: article.Description,
			Date:        article.Date.Format("02-01"),
			URL:         "/articles/" + article.Slug,
		})
	}
	return postsByYear
}

type frontmatterData struct {
	Title       string
	Date        string
	Description string
	Categories  []string
	Published   bool
}

func NewArticle(slug string, src []byte) Article {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.Linkify,
			extension.GFM,
			extension.Table,
			extension.Footnote,
			extension.Strikethrough,
			markdown.GosmicMarkdownExtension,
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
		panic(fmt.Errorf("parsing markdown for %s: %v", slug, err))
	}

	d := frontmatter.Get(ctx)
	if d == nil {
		panic(fmt.Errorf("no frontmatter found in %s", slug))
	}

	var meta frontmatterData
	if err := d.Decode(&meta); err != nil {
		panic(err)
	}

	return Article{
		Slug:        slug,
		Content:     template.HTML(out.Bytes()),
		Title:       meta.Title,
		Date:        parseDate(meta.Date),
		Description: meta.Description,
		Categories:  meta.Categories,
		Published:   meta.Published,
	}
}

func parseDate(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}
