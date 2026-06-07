package antoph

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"math/rand/v2"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"anto.pt/x/log"

	"anto.pt/x/gosmic/fsx"
	"anto.pt/x/gosmic/httpx"
	"anto.pt/x/gosmic/plausible"
	"anto.pt/x/gosmic/templates"
)

//go:embed pages/*.html
var resources embed.FS

var logger = log.Module("anto.ph")

var featured []string

func init() {
	for s := range strings.SplitSeq(os.Getenv("FEATURED"), ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			featured = append(featured, s)
		}
	}
}

type Index struct {
	Sections []Section
	Tags     []string
}

type Section struct {
	Title string
	Imgs  []Img
}

// TagCount is a tag name with its associated photo count, used for the tag
// wall and tag-page headers.
type TagCount struct {
	Name  string
	Count int
}

// baseData carries the shared header chrome (active nav route + pathbar text).
type baseData struct {
	Route string // "home", "all", "tags", or "" for photo detail
	Path  string // shown in the header pathbar
}

type HomeData struct {
	baseData
	Featured []Img
	Tags     []TagCount
	Total    int
}

type AllData struct {
	baseData
	Sections []Section // one per year, sorted desc
	Total    int
}

type TagData struct {
	baseData
	Tag  string
	Imgs []Img
}

// ImgPageData wraps a single Img with the chrome and back-link metadata needed
// by the photo-detail template. Img is embedded so all of the existing display
// fields stay accessible as `.ID`, `.Meta`, `.Nav`, etc.
type ImgPageData struct {
	Img
	baseData
	BackURL   string
	BackLabel string
}

// SpeculationRules emits a <script type="speculationrules"> block that tells
// Chromium browsers to prerender the previous/next photo pages, so navigation
// becomes effectively instant. Other browsers ignore the rules and fall back
// to the regular prefetch + image preload links above. Returns "" when there
// is nothing to prerender.
//
// Note: this is returned as template.HTML rather than template.JS because
// html/template only recognizes well-known script MIME types as JS contexts
// and would otherwise HTML-escape the JSON quote characters.
func (p ImgPageData) SpeculationRules() template.HTML {
	var urls []string
	if p.Nav.Prev != nil {
		urls = append(urls, string(p.Nav.Prev.URL))
	}
	if p.Nav.Next != nil {
		urls = append(urls, string(p.Nav.Next.URL))
	}
	if len(urls) == 0 {
		return ""
	}
	b, err := json.Marshal(map[string]any{
		"prerender": []map[string]any{{
			"source":    "list",
			"eagerness": "eager",
			"urls":      urls,
		}},
	})
	if err != nil {
		return ""
	}
	return template.HTML(fmt.Sprintf(`<script type="speculationrules">%s</script>`, b))
}

type Img struct {
	ID           string
	CanonicalURL template.URL
	URL          template.URL
	Meta         ImgMeta
	Nav          ImgNav
}

func (i Img) Srcset() string {
	var sb strings.Builder
	if i.Meta.Width >= 1200 {
		sb.WriteString(string(i.CanonicalURL))
		sb.WriteString("/w_1200.webp 1200w, ")
	}
	if i.Meta.Width >= 1900 {
		sb.WriteString(string(i.CanonicalURL))
		sb.WriteString("/w_1900.webp 1900w, ")
	}
	if i.Meta.Width >= 2500 {
		sb.WriteString(string(i.CanonicalURL))
		sb.WriteString("/w_2500.webp 2500w")
	}
	return sb.String()
}

func (i Img) PreloadElement() template.HTML {
	s := fmt.Sprintf(`<link
            rel="preload" as="image" href="%s/w_1900.webp"
            imagesrcset="%s"
            imagesizes="(max-width: 1200px) 1200px, (max-width: 1900px) 1900px, 2500px">`,
		i.CanonicalURL, i.Srcset())
	return template.HTML(s)
}

// ShortID returns a compact label for the photo ID, suitable for in-frame badges.
// Mirrors the timestamp_hash convention on anto.ph: returns the trailing
// underscore-suffix when present; otherwise the last 6 characters.
func (i Img) ShortID() string {
	id := i.ID
	if idx := strings.LastIndex(id, "_"); idx >= 0 && idx < len(id)-1 {
		return id[idx+1:]
	}
	if len(id) > 6 {
		return id[len(id)-6:]
	}
	return id
}

func (i Img) FirstKeyword() string {
	for _, k := range i.Meta.Keywords {
		if k != "" {
			return k
		}
	}
	return ""
}

func (i Img) Alt() string {
	if kw := i.FirstKeyword(); kw != "" {
		return fmt.Sprintf("Photo from %s, %s", kw, i.Meta.Date.Format("2 Jan 2006"))
	}
	return fmt.Sprintf("Photo taken on %s", i.Meta.Date.Format("2 Jan 2006"))
}

func (i Img) Exposure() string {
	var parts []string
	if i.Meta.Aperture != "" {
		parts = append(parts, "f/"+i.Meta.Aperture)
	}
	if i.Meta.ShutterSpeed != "" {
		parts = append(parts, i.Meta.ShutterSpeed+"s")
	}
	if i.Meta.ISO != 0 {
		parts = append(parts, fmt.Sprintf("iso %d", i.Meta.ISO))
	}
	return strings.Join(parts, " · ")
}

// FeatClass returns the contact-sheet cell modifier ("big", "wide", "tall", or "")
// for the photo at position idx in a featured grid. The classification is derived
// from aspect ratio so the layout adapts to the actual photo set.
func (i Img) FeatClass(idx int) string {
	if i.Meta.Width == 0 || i.Meta.Height == 0 {
		return ""
	}
	ratio := float64(i.Meta.Width) / float64(i.Meta.Height)
	if idx == 0 && ratio >= 1.2 {
		return "big"
	}
	if ratio < 0.85 {
		return "tall"
	}
	if ratio >= 1.7 {
		return "wide"
	}
	return ""
}

type ImgMeta struct {
	Width        int
	Height       int
	Date         time.Time
	Camera       string
	Lens         string
	ISO          int
	ShutterSpeed string
	Aperture     string
	Keywords     []string
}

type ImgNav struct {
	Prev *Img
	Next *Img
}

type Images []Img

func (imgs Images) Sort() {
	sort.Slice(imgs, func(i, j int) bool { return imgs[i].Meta.Date.After(imgs[j].Meta.Date) })
}

func (imgs Images) FindByID(id string) (int, bool) {
	for i, img := range imgs {
		if img.ID == id {
			return i, true
		}
	}
	return -1, false
}

func openPhotoDB(base string) (Images, error) {
	var imgs []Img
	return imgs, fs.WalkDir(os.DirFS(base), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		if d.IsDir() {
			id := p
			meta := extractMeta(path.Join(base, p))
			url := fmt.Sprintf("/pic/%s", id)
			imgs = append(imgs, Img{
				ID:           id,
				Meta:         meta,
				CanonicalURL: template.URL(url),
				URL:          template.URL(url),
			})
		}
		return nil
	})
}

func byYear(imgs []Img) map[int][]Img {
	y := make(map[int][]Img)
	for _, img := range imgs {
		y[img.Meta.Date.Year()] = append(y[img.Meta.Date.Year()], img)
	}
	return y
}

type ByKeywordView struct {
	keyword string
	ids     map[string]int
	imgs    []Img
}

func newByKeywordView(keyword string) *ByKeywordView {
	return &ByKeywordView{
		keyword: keyword,
		ids:     make(map[string]int),
	}
}

func (v *ByKeywordView) Append(img Img) {
	img.URL = template.URL(fmt.Sprintf("/tags/%s/pic/%s", v.keyword, img.ID))
	if len(v.imgs) > 0 {
		img.Nav.Prev = &v.imgs[len(v.imgs)-1]
		v.imgs[len(v.imgs)-1].Nav.Next = &img
	}
	v.ids[img.ID] = len(v.imgs)
	v.imgs = append(v.imgs, img)
}

func (v *ByKeywordView) Len() int {
	return len(v.imgs)
}

func (v *ByKeywordView) Images() []Img {
	return v.imgs
}

func (v *ByKeywordView) Get(id string) (Img, bool) {
	idx, ok := v.ids[id]
	if !ok {
		return Img{}, false
	}
	return v.imgs[idx], true
}

func byKeywords(imgs []Img) map[string]*ByKeywordView {
	views := make(map[string]*ByKeywordView)
	for _, img := range imgs {
		for _, keyword := range img.Meta.Keywords {
			if keyword == "" {
				logger.Warn("img has empty keyword", "id", img.ID)
				continue
			}
			if _, ok := views[keyword]; !ok {
				views[keyword] = newByKeywordView(keyword)
			}
			views[keyword].Append(img)
		}
	}
	return views
}

type AllView struct {
	ids  map[string]int
	imgs []Img
}

func (v *AllView) Append(img Img) {
	img.URL = template.URL(fmt.Sprintf("/all/pic/%s", img.ID))
	if len(v.imgs) > 0 {
		img.Nav.Prev = &v.imgs[len(v.imgs)-1]
		v.imgs[len(v.imgs)-1].Nav.Next = &img
	}
	v.ids[img.ID] = len(v.imgs)
	v.imgs = append(v.imgs, img)
}

func (v *AllView) Len() int {
	return len(v.imgs)
}

func (v *AllView) Images() []Img {
	return v.imgs
}

func (v *AllView) Get(id string) (Img, bool) {
	idx, ok := v.ids[id]
	if !ok {
		return Img{}, false
	}
	return v.imgs[idx], true
}

func (v *AllView) ByYear() Index {
	var data = Index{}
	for y, imgs := range byYear(v.imgs) {
		data.Sections = append(data.Sections, Section{
			Title: strconv.Itoa(y),
			Imgs:  imgs,
		})
	}
	sort.Slice(data.Sections, func(i, j int) bool { return data.Sections[i].Title > data.Sections[j].Title })
	return data
}

func newAllView(imgs []Img) AllView {
	v := AllView{
		ids:  make(map[string]int),
		imgs: make([]Img, 0, len(imgs)),
	}
	for _, i := range imgs {
		v.Append(i)
	}
	return v
}

type FeaturedView struct {
	ids  map[string]int
	imgs []Img
}

func newFeaturedView(imgs []Img) *FeaturedView {
	v := &FeaturedView{
		ids:  make(map[string]int),
		imgs: make([]Img, 0, len(imgs)),
	}

	for _, img := range imgs {
		for _, feat := range featured {
			if img.ID == feat {
				v.Append(img)
			}
		}

	}

	return v
}

func (v *FeaturedView) Images() []Img { return v.imgs }

func (v *FeaturedView) Get(id string) (Img, bool) {
	idx, ok := v.ids[id]
	if !ok {
		return Img{}, false
	}
	return v.imgs[idx], true
}

func (v *FeaturedView) Append(img Img) {
	img.URL = template.URL(fmt.Sprintf("/pic/%s", img.ID))
	if len(v.imgs) > 0 {
		img.Nav.Prev = &v.imgs[len(v.imgs)-1]
		v.imgs[len(v.imgs)-1].Nav.Next = &img
	}
	v.ids[img.ID] = len(v.imgs)
	v.imgs = append(v.imgs, img)
}

func prettyDate(t time.Time) string {
	return t.Format(time.RFC822)
}

type Website struct{}

var _ httpx.Website = Website{}

func (Website) Register(devmode bool) http.Handler {
	mux := http.NewServeMux()

	photodbPath := os.Getenv("PHOTODB_PATH")
	if photodbPath == "" {
		logger.Warn("PHOTODB_PATH not set")
		return nil
	}

	imgs, err := openPhotoDB(photodbPath)
	if err != nil {
		panic(err)
	}

	imgs.Sort()
	imgsByKeywords := byKeywords(imgs)

	var tagCounts []TagCount
	for k, v := range imgsByKeywords {
		tagCounts = append(tagCounts, TagCount{Name: k, Count: v.Len()})
	}
	sort.Slice(tagCounts, func(i, j int) bool {
		if tagCounts[i].Count != tagCounts[j].Count {
			return tagCounts[i].Count > tagCounts[j].Count
		}
		return tagCounts[i].Name < tagCounts[j].Name
	})

	t := templates.New(fsx.Or(devmode, resources, "./antoph"), devmode, template.FuncMap{
		"prettyDate": prettyDate,
		"monthLabel": func(t time.Time) string { return strings.ToLower(t.Format("Jan")) },
	})

	featuredView := newFeaturedView(imgs)
	allView := newAllView(imgs)
	allViewData := allView.ByYear()

	allData := AllData{
		baseData: baseData{Route: "all", Path: "/all"},
		Sections: allViewData.Sections,
		Total:    len(imgs),
	}

	if len(featured) == 0 {
		// fallback: homepage shows the whole archive
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
			t.Render(w, "all.html", AllData{
				baseData: baseData{Route: "home", Path: "/"},
				Sections: allViewData.Sections,
				Total:    len(imgs),
			})
		})
	} else {
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
			t.Render(w, "home.html", HomeData{
				baseData: baseData{Route: "home", Path: "/"},
				Featured: featuredView.Images(),
				Tags:     tagCounts,
				Total:    len(imgs),
			})
		})
	}

	mux.HandleFunc("GET /all/{$}", func(w http.ResponseWriter, r *http.Request) {
		t.Render(w, "all.html", allData)
	})

	mux.HandleFunc("GET /all/pic/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		img, ok := allView.Get(id)
		if !ok {
			http.NotFound(w, r)
			return
		}

		if !devmode {
			w.Header().Set("Cache-Control", "public, max-age=300")
		}
		t.Render(w, "image.html", ImgPageData{
			Img:       img,
			baseData:  baseData{Path: "/all/pic/" + id},
			BackURL:   "/all",
			BackLabel: "all photos",
		})
	})

	mux.HandleFunc("GET /random/{$}", func(w http.ResponseWriter, r *http.Request) {
		random := rand.IntN(len(imgs))
		id := imgs[random].ID
		http.Redirect(w, r, fmt.Sprintf("/all/pic/%s", id), http.StatusFound)
	})

	mux.HandleFunc("GET /pic/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		img, ok := featuredView.Get(id)
		if !ok {
			// the image is not featured, but let's see if it exists and redirect
			_, ok := allView.Get(id)
			if !ok {
				http.NotFound(w, r)
				return
			}

			http.Redirect(w, r, fmt.Sprintf("/all/pic/%s", id), http.StatusFound)
			return
		}

		if !devmode {
			w.Header().Set("Cache-Control", "public, max-age=300")
		}
		t.Render(w, "image.html", ImgPageData{
			Img:       img,
			baseData:  baseData{Path: "/pic/" + id},
			BackURL:   "/",
			BackLabel: "selected",
		})
	})

	mux.HandleFunc("GET /tags/{tag}/", func(w http.ResponseWriter, r *http.Request) {
		tag := r.PathValue("tag")
		view, ok := imgsByKeywords[tag]
		if !ok {
			http.NotFound(w, r)
			return
		}

		t.Render(w, "tag.html", TagData{
			baseData: baseData{Route: "tags", Path: "/tags/" + tag + "/"},
			Tag:      tag,
			Imgs:     view.Images(),
		})
	})

	mux.HandleFunc("GET /tags/{tag}/pic/{id}", func(w http.ResponseWriter, r *http.Request) {
		tag := r.PathValue("tag")

		view, ok := imgsByKeywords[tag]
		if !ok {
			http.NotFound(w, r)
			return
		}

		id := r.PathValue("id")
		img, ok := view.Get(id)
		if !ok {
			http.NotFound(w, r)
			return
		}

		if !devmode {
			w.Header().Set("Cache-Control", "public, max-age=300")
		}
		t.Render(w, "image.html", ImgPageData{
			Img:       img,
			baseData:  baseData{Path: "/tags/" + tag + "/pic/" + id},
			BackURL:   "/tags/" + tag + "/",
			BackLabel: "#" + tag,
		})
	})

	imageFilenames := []string{
		"blur.webp",
		"q_500.webp",
		"q_1000.webp",
		"q_2000.webp",
		"w_1200.webp",
		"w_1900.webp",
		"w_2500.webp",
	}
	for _, name := range imageFilenames {
		mux.HandleFunc("GET /pic/{id}/"+name, func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			if !devmode {
				w.Header().Set("Cache-Control", "public, max-age=31536000")
			}
			http.ServeFile(w, r, path.Join(photodbPath, id, name))
		})
	}

	mux.Handle("GET /js/ps.js", plausible.Proxy)
	mux.Handle("POST /api/event", plausible.Proxy)

	return mux
}
