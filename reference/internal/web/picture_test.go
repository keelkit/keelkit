package web

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/keelkit/keelkit/modules/images"
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

func TestPictureMobile(t *testing.T) {
	img := &images.Image{Variants: []images.StoredVariant{variant(images.WebP, 960, 640), variant(images.JPEG, 960, 640)}}
	mobile := &images.Image{Variants: []images.StoredVariant{
		{Format: images.WebP, Width: 480, Height: 720, Path: "m/v1/480.webp"},
		{Format: images.JPEG, Width: 480, Height: 720, Path: "m/v1/480.jpg"},
	}}
	render := func(mobile *images.Image) (*goquery.Selection, string) {
		t.Helper()
		var b strings.Builder
		if err := PictureMobile(img, mobile, "Закат", "100vw").Render(t.Context(), &b); err != nil {
			t.Fatal(err)
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(b.String()))
		if err != nil {
			t.Fatal(err)
		}
		return doc.Find("picture"), b.String()
	}

	sources := func(p *goquery.Selection) []string {
		var got []string
		p.Find("source").Each(func(_ int, s *goquery.Selection) {
			got = append(got, s.AttrOr("media", "")+"|"+s.AttrOr("type", "")+"|"+s.AttrOr("srcset", "")+"|"+
				s.AttrOr("width", "")+"x"+s.AttrOr("height", ""))
		})
		return got
	}

	// С мобильной: два <source media> первыми (WebP, затем JPEG) с её размерами, потом основная пара.
	p, _ := render(mobile)
	want := []string{
		MobileMedia + "|image/webp|/media/m/v1/480.webp 480w|480x720",
		MobileMedia + "|image/jpeg|/media/m/v1/480.jpg 480w|480x720",
		"|image/webp|/media/abc/v1/960.webp 960w|x",
	}
	if got := sources(p); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("sources =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got := p.Find("img").AttrOr("src", ""); got != "/media/abc/v1/960.jpg" {
		t.Errorf("img src = %q, want the main image", got)
	}

	// Без мобильной (nil или без вариантов) — разметка как у Picture.
	var plain strings.Builder
	if err := Picture(img, "Закат", "100vw").Render(t.Context(), &plain); err != nil {
		t.Fatal(err)
	}
	for _, m := range []*images.Image{nil, {}} {
		if _, html := render(m); html != plain.String() {
			t.Errorf("PictureMobile(img, %v) =\n%s\nwant Picture output\n%s", m, html, plain.String())
		}
	}

	// Мобильная без основной — ничего: основная обязательна.
	var b strings.Builder
	if err := PictureMobile(&images.Image{}, mobile, "", "100vw").Render(t.Context(), &b); err != nil {
		t.Fatal(err)
	}
	if b.Len() != 0 {
		t.Errorf("mobile without main rendered %q", b.String())
	}
}

// svgOf — SVG из первого url(data:image/svg+xml,…) в style.
func svgOf(t *testing.T, style string) string {
	t.Helper()
	_, enc, _ := strings.Cut(style, "url(data:image/svg+xml,")
	enc, _, _ = strings.Cut(enc, ")")
	svg, err := url.PathUnescape(enc)
	if err != nil {
		t.Fatalf("style %q: %v", style, err)
	}
	return svg
}

func TestPicturePlaceholder(t *testing.T) {
	const ph = "data:image/webp;base64,UklGRg+/="
	withPH := func(w, h int, placeholder, path string) *images.Image {
		return &images.Image{Width: w, Height: h, Placeholder: placeholder, Variants: []images.StoredVariant{
			{Format: images.JPEG, Width: 480, Height: 480 * h / w, Path: path},
		}}
	}
	render := func(img, mobile *images.Image) *goquery.Selection {
		t.Helper()
		var b strings.Builder
		if err := PictureMobile(img, mobile, "", "100vw").Render(t.Context(), &b); err != nil {
			t.Fatal(err)
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(b.String()))
		if err != nil {
			t.Fatal(err)
		}
		return doc.Find("img")
	}

	// Заглушка — SVG с размытием поверх WebP из модуля, в --ph; класс ph включает фон (style.css).
	el := render(withPH(1600, 1000, ph, "a/v2/480.jpg"), nil)
	style := el.AttrOr("style", "")
	if !el.HasClass("ph") || !strings.HasPrefix(style, "--ph:url(data:image/svg+xml,") ||
		!strings.Contains(svgOf(t, style), "viewBox='0 0 1600 1000'") || !strings.Contains(svgOf(t, style), "href='"+ph+"'") ||
		!strings.Contains(svgOf(t, style), "feGaussianBlur stdDeviation='80'") || strings.ContainsAny(style, `<>'"& `) ||
		strings.Contains(style, "--ph-m") {
		t.Errorf("class %q, style %q", el.AttrOr("class", ""), style)
	}

	// Без заглушки (прозрачность, профиль v1) или с чем-то кроме base64 — ни класса, ни style.
	for _, p := range []string{"", "data:image/webp;base64,", "data:image/webp;base64,AA')}x{", "data:image/png;base64,AAAA"} {
		if el := render(withPH(800, 600, p, "a/v2/480.jpg"), nil); el.HasClass("ph") || el.AttrOr("style", "") != "" {
			t.Errorf("placeholder %q: class %q, style %q; want none", p, el.AttrOr("class", ""), el.AttrOr("style", ""))
		}
	}

	// С мобильной: её заглушка — в --ph-m; без заглушки у мобильной — none (не чужой кадр).
	style = render(withPH(1600, 1000, ph, "a/v2/480.jpg"), withPH(600, 900, ph, "m/v2/480.jpg")).AttrOr("style", "")
	if _, m, _ := strings.Cut(style, "--ph-m:"); !strings.Contains(svgOf(t, m), "viewBox='0 0 600 900'") {
		t.Errorf("mobile placeholder: style %q", style)
	}
	style = render(withPH(1600, 1000, ph, "a/v2/480.jpg"), withPH(600, 900, "", "m/v2/480.jpg")).AttrOr("style", "")
	if !strings.HasSuffix(style, "--ph-m:none;") || !strings.HasPrefix(style, "--ph:url(") {
		t.Errorf("mobile without placeholder: style %q", style)
	}
	// Заглушка только у мобильной — --ph:none не пишется, фон на широком экране пуст.
	el = render(withPH(1600, 1000, "", "a/v2/480.jpg"), withPH(600, 900, ph, "m/v2/480.jpg"))
	if style := el.AttrOr("style", ""); !el.HasClass("ph") || !strings.HasPrefix(style, "--ph-m:url(") {
		t.Errorf("only mobile placeholder: style %q", style)
	}
}
