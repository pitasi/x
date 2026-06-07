package antoph

import (
	"bytes"
	"html/template"
	"net/http"
	"strings"
	"testing"
	"time"

	"anto.pt/x/gosmic/templates"
)

func sampleImgs() []Img {
	d := time.Date(2025, 7, 2, 17, 14, 32, 0, time.UTC)
	return []Img{
		{
			ID:           "2025-07-02_17:14:32_b424",
			CanonicalURL: "/pic/2025-07-02_17:14:32_b424",
			URL:          "/pic/2025-07-02_17:14:32_b424",
			Meta: ImgMeta{
				Width: 2400, Height: 1600,
				Date:         d,
				Camera:       "fujifilm x100v",
				Lens:         "23mm f/2",
				ISO:          400,
				ShutterSpeed: "1/250",
				Aperture:     "2.8",
				Keywords:     []string{"madrid", "street"},
			},
		},
		{
			ID:           "2025-06-27_17:36:38_7288",
			CanonicalURL: "/pic/2025-06-27_17:36:38_7288",
			URL:          "/pic/2025-06-27_17:36:38_7288",
			Meta: ImgMeta{
				Width: 1600, Height: 2400,
				Date:         d.AddDate(0, -1, -5),
				Camera:       "leica q2",
				Lens:         "28mm f/1.7",
				ISO:          100,
				ShutterSpeed: "1/1000",
				Aperture:     "2",
				Keywords:     []string{"barcelona", "street"},
			},
		},
		{
			ID:           "2024-09-15_11:40:29_b90e",
			CanonicalURL: "/pic/2024-09-15_11:40:29_b90e",
			URL:          "/all/pic/2024-09-15_11:40:29_b90e",
			Meta: ImgMeta{
				Width: 1800, Height: 1200,
				Date:         d.AddDate(-1, 2, -16),
				Camera:       "ricoh gr iii",
				Lens:         "28mm f/2.8",
				ISO:          200,
				ShutterSpeed: "1/400",
				Aperture:     "8",
				Keywords:     []string{"marrakech", "street", "morocco"},
			},
		},
	}
}

func newTestTemplates(t *testing.T) *templates.T {
	t.Helper()
	return templates.New(resources, false, template.FuncMap{
		"prettyDate": prettyDate,
		"monthLabel": func(t time.Time) string { return strings.ToLower(t.Format("Jan")) },
	})
}

func renderOK(t *testing.T, tpl *templates.T, name string, data any) string {
	t.Helper()
	w := &fakeRW{header: make(http.Header)}
	tpl.Render(w, name, data)
	if w.status != 0 && w.status != 200 {
		t.Fatalf("%s rendered with status %d: %s", name, w.status, w.buf.String())
	}
	body := w.buf.String()
	if !strings.Contains(body, "<html") {
		t.Fatalf("%s: rendered body has no <html>: %.200s", name, body)
	}
	return body
}

func TestHomeTemplate(t *testing.T) {
	tpl := newTestTemplates(t)
	imgs := sampleImgs()
	body := renderOK(t, tpl, "home.html", HomeData{
		baseData: baseData{Route: "home", Path: "/"},
		Featured: imgs,
		Tags:     []TagCount{{Name: "madrid", Count: 1}, {Name: "street", Count: 3}},
		Total:    3,
	})
	for _, want := range []string{"# top pic(k)s", "#b424", "selected by hand", "tagwall", "23mm f/2"} {
		if !strings.Contains(body, want) {
			t.Errorf("home: missing %q in body", want)
		}
	}
}

func TestAllTemplate(t *testing.T) {
	tpl := newTestTemplates(t)
	imgs := sampleImgs()
	body := renderOK(t, tpl, "all.html", AllData{
		baseData: baseData{Route: "all", Path: "/all"},
		Sections: []Section{
			{Title: "2025", Imgs: imgs[:2]},
			{Title: "2024", Imgs: imgs[2:]},
		},
		Total: 3,
	})
	for _, want := range []string{"all photos", "2025", "2024", "ls -t ~/photos", "#b424"} {
		if !strings.Contains(body, want) {
			t.Errorf("all: missing %q in body", want)
		}
	}
}

func TestTagTemplate(t *testing.T) {
	tpl := newTestTemplates(t)
	imgs := sampleImgs()
	body := renderOK(t, tpl, "tag.html", TagData{
		baseData: baseData{Route: "tags", Path: "/tags/street/"},
		Tag:      "street",
		Imgs:     imgs,
	})
	for _, want := range []string{"#street", "/tags/street", "← home", "#b424"} {
		if !strings.Contains(body, want) {
			t.Errorf("tag: missing %q in body", want)
		}
	}
}

func TestImageTemplate(t *testing.T) {
	tpl := newTestTemplates(t)
	imgs := sampleImgs()
	imgs[0].Nav.Next = &imgs[1]
	imgs[1].Nav.Prev = &imgs[0]
	body := renderOK(t, tpl, "image.html", ImgPageData{
		Img:       imgs[0],
		baseData:  baseData{Path: "/pic/" + imgs[0].ID},
		BackURL:   "/",
		BackLabel: "selected",
	})
	for _, want := range []string{
		"exif · #b424", "fujifilm x100v", "iso 400", "f/2.8",
		"/pic/2025-07-02_17:14:32_b424/w_2500.webp",
		"/pic/2025-07-02_17:14:32_b424/w_1200.webp",
		"← selected", "older ›",
		`type="speculationrules"`, `"prerender"`, `"eagerness":"eager"`,
		"/pic/2025-06-27_17:36:38_7288",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("image: missing %q in body", want)
		}
	}
}

func TestSharedStyles(t *testing.T) {
	tpl := newTestTemplates(t)
	body := renderOK(t, tpl, "home.html", HomeData{
		baseData: baseData{Route: "home", Path: "/"},
		Featured: sampleImgs(),
	})
	for _, want := range []string{"@view-transition", "document.prerendering"} {
		if !strings.Contains(body, want) {
			t.Errorf("shared chrome: missing %q in body", want)
		}
	}
}

// fakeRW is a minimal http.ResponseWriter for capturing template renders.
type fakeRW struct {
	buf    bytes.Buffer
	header http.Header
	status int
}

func (f *fakeRW) Header() http.Header         { return f.header }
func (f *fakeRW) Write(b []byte) (int, error) { return f.buf.Write(b) }
func (f *fakeRW) WriteHeader(s int)           { f.status = s }
