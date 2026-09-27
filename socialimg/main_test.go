package socialimg

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"testing"

	"golang.org/x/image/font/gofont/gobold"
)

func TestGenerateUsesOGSizeAndCircularAvatar(t *testing.T) {
	avatar := image.NewRGBA(image.Rect(0, 0, 40, 40))
	draw.Draw(avatar, avatar.Bounds(), &image.Uniform{C: color.RGBA{G: 255, A: 255}}, image.Point{}, draw.Src)
	var avatarPNG bytes.Buffer
	if err := png.Encode(&avatarPNG, avatar); err != nil {
		t.Fatal(err)
	}

	gen, err := NewGenerator(Config{
		Font:   bytes.NewReader(gobold.TTF),
		Avatar: bytes.NewReader(avatarPNG.Bytes()),
		Theme: Theme{
			Background:       color.RGBA{R: 185, G: 181, B: 255, A: 255},
			GradientFrom:     color.White,
			GradientTo:       color.White,
			Title:            color.RGBA{R: 32, G: 35, B: 66, A: 255},
			Subtitle:         color.RGBA{R: 80, G: 84, B: 107, A: 255},
			AvatarBackground: color.RGBA{R: 255, G: 145, B: 188, A: 255},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := gen.Generate(&output, "Build your own ResponseWriter: safer HTTP in Go", "written on 04 May 2025"); err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds().Size(); got != image.Pt(1200, 630) {
		t.Fatalf("image size = %v, want 1200x630", got)
	}

	corner := color.NRGBAModel.Convert(img.At(74, 50)).(color.NRGBA)
	if corner.R < 150 {
		t.Fatalf("avatar corner = %v, want it cropped outside the circle", corner)
	}
	pink := color.NRGBAModel.Convert(img.At(150, 47)).(color.NRGBA)
	if pink.R < 200 || pink.G < 70 || pink.G > 200 {
		t.Fatalf("circle background = %v, want pink", pink)
	}
	for y := 620; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if c.R < 140 && c.G < 150 && c.B < 180 {
				t.Fatalf("dark text overflow at (%d, %d): %v", x, y, c)
			}
		}
	}
}

func TestThemeDefaults(t *testing.T) {
	custom := color.RGBA{R: 1, G: 2, B: 3, A: 255}
	theme := (Theme{Title: custom}).withDefaults()

	if theme.Title != custom {
		t.Fatalf("custom title color = %v, want %v", theme.Title, custom)
	}
	if theme.Background == nil || theme.GradientFrom == nil || theme.GradientTo == nil || theme.Subtitle == nil {
		t.Fatal("unset theme colors must use defaults")
	}
}
