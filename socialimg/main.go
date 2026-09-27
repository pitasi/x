package socialimg

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"

	"github.com/disintegration/imaging"
	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	_ "golang.org/x/image/webp"
)

const (
	imageWidth   = 1200
	imageHeight  = 630
	scale        = float64(imageWidth) / 1024
	layoutWidth  = 1024.0
	layoutHeight = float64(imageHeight) / scale
)

// Config supplies the assets and optional colors for a generator. Nil theme colors use the defaults.
type Config struct {
	Font   io.Reader
	Avatar io.Reader
	Theme  Theme
}

// Theme overrides the generator's colors. Unset colors keep their defaults.
type Theme struct {
	Background       color.Color
	GradientFrom     color.Color
	GradientTo       color.Color
	Title            color.Color
	Subtitle         color.Color
	AvatarBackground color.Color
}

type Generator struct {
	font          func(points float64) font.Face
	propicResized image.Image
	base          image.Image
	theme         Theme
}

func NewGenerator(config Config) (*Generator, error) {
	font, err := loadFont(config.Font)
	if err != nil {
		return nil, fmt.Errorf("loading font: %s", err)
	}

	propic, _, err := image.Decode(config.Avatar)
	if err != nil {
		return nil, fmt.Errorf("opening image file: %s", err)
	}
	width := 132
	propicResized := imaging.Resize(propic, width, 0, imaging.Box)

	gen := &Generator{
		font:          font,
		propicResized: propicResized,
		theme:         config.Theme.withDefaults(),
	}
	gen.generateBase()

	return gen, nil
}

func (gen *Generator) generateBase() {
	dc := gg.NewContext(imageWidth, imageHeight)
	dc.Scale(scale, scale)

	dc.DrawRectangle(0, 0, layoutWidth, layoutHeight)
	dc.SetColor(gen.theme.Background)
	dc.Fill()

	cardX, cardY := 22.0, 20.0
	cardW, cardH := layoutWidth-44, layoutHeight-40
	dc.DrawRoundedRectangle(cardX+6, cardY+6, cardW, cardH, 8)
	dc.SetColor(gen.theme.Title)
	dc.Fill()

	g := gg.NewLinearGradient(0, layoutHeight, layoutWidth, 0)
	g.AddColorStop(0, gen.theme.GradientFrom)
	g.AddColorStop(0.5, gen.theme.GradientFrom)
	g.AddColorStop(1, gen.theme.GradientTo)

	// background
	dc.DrawRoundedRectangle(cardX, cardY, cardW, cardH, 8)
	dc.SetFillStyle(g)
	dc.FillPreserve()
	dc.SetColor(gen.theme.Title)
	dc.SetLineWidth(2)
	dc.Stroke()

	if gen.theme.AvatarBackground != nil {
		dc.DrawCircle(126, 120, 82)
		dc.SetColor(gen.theme.AvatarBackground)
		dc.FillPreserve()
		dc.Clip()
	}
	dc.DrawImage(gen.propicResized, 60, 42)
	dc.ResetClip()

	gen.base = dc.Image()
}

func (gen *Generator) Generate(w io.Writer, title, subtitle string) error {
	dc := gg.NewContext(imageWidth, imageHeight)
	dc.DrawImage(gen.base, 0, 0)

	margin := 55 * scale
	maxWidth := layoutWidth*scale - 2*margin
	top := 270 * scale
	dateMargin := 50 * scale
	dateFontSize := 35 * scale
	dc.SetFontFace(gen.font(dateFontSize))
	dateHeight := dc.FontHeight()
	cardBottom := (layoutHeight - 20) * scale

	var lines []string
	var lineHeight float64
	var dateY float64
	for size := 65 * scale; size >= 32; size-- {
		dc.SetFontFace(gen.font(size))
		lines = dc.WordWrap(title, maxWidth)
		lineHeight = dc.FontHeight()
		dateY = top + lineHeight*float64(len(lines)) + dateMargin
		if dateY+dateHeight*0.3 <= cardBottom-14 {
			break
		}
	}

	dc.SetColor(gen.theme.Title)
	for i, line := range lines {
		dc.DrawString(line, margin, top+lineHeight*float64(i+1))
	}
	dc.SetFontFace(gen.font(dateFontSize))
	dc.SetColor(gen.theme.Subtitle)
	dc.DrawString(subtitle, margin, dateY)

	return jpeg.Encode(w, dc.Image(), &jpeg.Options{Quality: 95})
}

func (theme Theme) withDefaults() Theme {
	if theme.Background == nil {
		theme.Background = color.RGBA{R: 0x1e, G: 0x1a, B: 0x4d, A: 0xff}
	}
	if theme.GradientFrom == nil {
		theme.GradientFrom = color.RGBA{R: 0xf5, G: 0xb3, B: 0xff, A: 0xff}
	}
	if theme.GradientTo == nil {
		theme.GradientTo = color.RGBA{R: 0xfb, G: 0xde, B: 0xff, A: 0xff}
	}
	if theme.Title == nil {
		theme.Title = color.RGBA{R: 0x1e, G: 0x1a, B: 0x4d, A: 0xff}
	}
	if theme.Subtitle == nil {
		theme.Subtitle = color.RGBA{A: 0x70}
	}
	return theme
}

func loadFont(r io.Reader) (func(points float64) font.Face, error) {
	fontBytes, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	f, err := truetype.Parse(fontBytes)
	if err != nil {
		panic(err)
	}

	return func(points float64) font.Face {
		return truetype.NewFace(f, &truetype.Options{
			Size: points,
		})
	}, nil
}
