package web

import (
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/voffkin/keelkit/modules/images"
)

func variant(f images.Format, w, h int) images.StoredVariant {
	return images.StoredVariant{Format: f, Width: w, Height: h, Path: "abc/v1/" + strconv.Itoa(w) + "." + f.Ext()}
}

func renderPicture(t *testing.T, img *images.Image) *goquery.Selection {
	t.Helper()
	var b strings.Builder
	if err := Picture(img, "Закат", "100vw").Render(t.Context(), &b); err != nil {
		t.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	return doc.Find("picture")
}

func TestPicture(t *testing.T) {
	img := &images.Image{Variants: []images.StoredVariant{
		variant(images.WebP, 480, 320), variant(images.JPEG, 480, 320),
		variant(images.WebP, 960, 640), variant(images.JPEG, 960, 640),
		variant(images.WebP, 1600, 1067), variant(images.JPEG, 1600, 1067),
	}}
	p := renderPicture(t, img)
	if p.Length() != 1 {
		t.Fatal("no <picture>")
	}
	src := p.Find(`source[type="image/webp"]`)
	if got, want := src.AttrOr("srcset", ""), "/media/abc/v1/480.webp 480w, /media/abc/v1/960.webp 960w, /media/abc/v1/1600.webp 1600w"; got != want {
		t.Errorf("webp srcset = %q, want %q", got, want)
	}
	el := p.Find("img")
	for attr, want := range map[string]string{
		"src":    "/media/abc/v1/960.jpg",
		"srcset": "/media/abc/v1/480.jpg 480w, /media/abc/v1/960.jpg 960w, /media/abc/v1/1600.jpg 1600w",
		"sizes":  "100vw",
		"width":  "960",
		"height": "640",
		"alt":    "Закат",
	} {
		if got := el.AttrOr(attr, ""); got != want {
			t.Errorf("img %s = %q, want %q", attr, got, want)
		}
	}
}

func TestPictureFallback(t *testing.T) {
	// Все JPEG шире fallbackWidth — в src самый узкий.
	img := &images.Image{Variants: []images.StoredVariant{variant(images.WebP, 1200, 800), variant(images.JPEG, 1200, 800), variant(images.JPEG, 1600, 1067)}}
	if got := renderPicture(t, img).Find("img").AttrOr("src", ""); got != "/media/abc/v1/1200.jpg" {
		t.Errorf("src = %q, want the narrowest JPEG", got)
	}
	// Без вариантов — ничего.
	if n := renderPicture(t, &images.Image{}).Length(); n != 0 {
		t.Errorf("image without variants rendered %d <picture>", n)
	}
}
