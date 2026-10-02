package images

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	xwebp "golang.org/x/image/webp"

	"github.com/voffkin/keelkit/modules/images/internal/exif"
)

func process(t *testing.T, name string) (*Result, error) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return Process(context.Background(), bytes.NewReader(data))
}

func mustProcess(t *testing.T, name string) *Result {
	t.Helper()
	res, err := process(t, name)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

func decodeVariant(t *testing.T, v Variant) image.Image {
	t.Helper()
	var img image.Image
	var err error
	switch v.Format {
	case JPEG:
		img, err = jpeg.Decode(bytes.NewReader(v.Data))
	case WebP:
		img, err = xwebp.Decode(bytes.NewReader(v.Data))
	default:
		t.Fatalf("unexpected variant format %s", v.Format)
	}
	if err != nil {
		t.Fatalf("decode %s: %v", v.Name(), err)
	}
	if b := img.Bounds(); b.Dx() != v.Width || b.Dy() != v.Height {
		t.Fatalf("%s: real size %dx%d, declared %dx%d", v.Name(), b.Dx(), b.Dy(), v.Width, v.Height)
	}
	return img
}

func near(c color.Color, want color.RGBA) bool {
	r, g, b, _ := c.RGBA()
	d := func(x uint32, y uint8) int { return max(int(x>>8)-int(y), int(y)-int(x>>8)) }
	return d(r, want.R) < 40 && d(g, want.G) < 40 && d(b, want.B) < 40
}

// Все 8 ориентаций: каждый вариант (WebP и JPEG) показывает «красный, зелёный / синий, жёлтый».
// Тестовые файлы построены по таблице из определения тега, а не через orient.Apply.
func TestOrientation(t *testing.T) {
	names := []string{"orientation-6.webp"}
	for o := 1; o <= 8; o++ {
		names = append(names, fmt.Sprintf("orientation-%d.jpg", o))
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			res := mustProcess(t, name)
			if res.Original.Width != orientW || res.Original.Height != orientH {
				t.Errorf("original size %dx%d, want %dx%d", res.Original.Width, res.Original.Height, orientW, orientH)
			}
			for _, v := range res.Variants {
				img := decodeVariant(t, v)
				w, h := v.Width, v.Height
				pts := []struct {
					x, y int
					want color.RGBA
				}{{w / 4, h / 4, cTL}, {3 * w / 4, h / 4, cTR}, {w / 4, 3 * h / 4, cBL}, {3 * w / 4, 3 * h / 4, cBR}}
				for _, p := range pts {
					if got := img.At(p.x, p.y); !near(got, p.want) {
						t.Errorf("%s: pixel (%d,%d) = %v, want ≈ %v", v.Name(), p.x, p.y, got, p.want)
					}
				}
			}
		})
	}
}

func TestVariantsNoExif(t *testing.T) {
	res := mustProcess(t, "orientation-6.jpg")
	if exif.FromJPEG(res.Original.Data) == nil {
		t.Fatal("original must keep EXIF as uploaded")
	}
	for _, v := range res.Variants {
		var tiff []byte
		if v.Format == JPEG {
			tiff = exif.FromJPEG(v.Data)
		} else {
			tiff = exif.FromWebP(v.Data)
		}
		if tiff != nil || bytes.Contains(v.Data, []byte("Keelkit test camera")) {
			t.Errorf("%s contains EXIF", v.Name())
		}
	}
}

func widthsOf(res *Result) []int {
	var ws []int
	for _, v := range res.Variants {
		if len(ws) == 0 || ws[len(ws)-1] != v.Width {
			ws = append(ws, v.Width)
		}
	}
	return ws
}

func TestWidths(t *testing.T) {
	tests := []struct {
		file string
		want []int
	}{
		{"photo.jpg", []int{480, 960, 1600, 2400}}, // 2600 px: шире профиля, своя ширина не нужна
		{"transparent.png", []int{480, 800}},       // 800 px: свой размер как самый крупный вариант
		{"small.jpg", []int{300}},                  // 300 px: без увеличения
		{"orientation-6.jpg", []int{480, 640}},     // показанная ширина — 640, а не сохранённые 480
		{"orientation-6.webp", []int{480, 640}},    // то же для WebP
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			res := mustProcess(t, tt.file)
			if got := widthsOf(res); !slices.Equal(got, tt.want) {
				t.Errorf("widths = %v, want %v", got, tt.want)
			}
			if len(res.Variants) != 2*len(tt.want) {
				t.Fatalf("%d variants, want %d", len(res.Variants), 2*len(tt.want))
			}
			for i, v := range res.Variants {
				wantFormat := WebP
				if i%2 == 1 {
					wantFormat = JPEG
				}
				if v.Format != wantFormat {
					t.Errorf("variant %d: format %s, want %s", i, v.Format, wantFormat)
				}
				if v.Width > res.Original.Width {
					t.Errorf("%s is wider than the original (%d)", v.Name(), res.Original.Width)
				}
				// Пропорции сохраняются с точностью до округления.
				wantH := float64(res.Original.Height) * float64(v.Width) / float64(res.Original.Width)
				if d := float64(v.Height) - wantH; d < -1 || d > 1 {
					t.Errorf("%s: height %d, want ≈ %.1f", v.Name(), v.Height, wantH)
				}
				decodeVariant(t, v)
			}
			if res.ProfileVersion != ProfileVersion {
				t.Errorf("ProfileVersion = %d", res.ProfileVersion)
			}
		})
	}
}

func TestOriginalKeptAsIs(t *testing.T) {
	data, err := os.ReadFile("testdata/orientation-3.jpg")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Process(context.Background(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(res.Original.Data, data) || res.Original.Format != JPEG || res.Original.Name() != "original.jpg" {
		t.Error("original must be stored byte for byte")
	}
}

func TestTransparency(t *testing.T) {
	res := mustProcess(t, "transparent.png")
	for _, v := range res.Variants {
		img := decodeVariant(t, v)
		_, _, _, a := img.At(0, 0).RGBA()
		switch v.Format {
		case WebP:
			if a>>8 > 10 {
				t.Errorf("%s: corner alpha %d, want transparent", v.Name(), a>>8)
			}
		case JPEG:
			if !near(img.At(0, 0), color.RGBA{255, 255, 255, 255}) {
				t.Errorf("%s: corner %v, want white background", v.Name(), img.At(0, 0))
			}
		}
	}
}

func TestRejected(t *testing.T) {
	tests := []struct {
		name    string
		data    func(t *testing.T) []byte
		wantErr error
		wantMsg string
	}{
		{"bomb", file("bomb.png"), ErrTooManyPixels, "50000×50000"},
		{"heic", file("photo.heic"), ErrUnsupportedFormat, "HEIC"},
		{"too large", func(*testing.T) []byte {
			return append([]byte("\xFF\xD8\xFF"), make([]byte, MaxBytes)...)
		}, ErrTooLarge, "20 MB"},
		{"gif", bytesOf("GIF89a\x01\x00\x01\x00"), ErrUnsupportedFormat, "GIF"},
		{"text", bytesOf("hello, world"), ErrUnsupportedFormat, "unknown"},
		{"empty", bytesOf(""), ErrUnsupportedFormat, "unknown"},
		{"animated webp", bytesOf("RIFF\x00\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02\x00\x00\x00"), ErrUnsupportedFormat, "animated"},
		{"truncated jpeg", func(t *testing.T) []byte {
			return file("photo.jpg")(t)[:5000]
		}, ErrInvalidImage, ""},
		{"jpeg without frame", bytesOf("\xFF\xD8\xFF\xD9"), ErrInvalidImage, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Process(context.Background(), bytes.NewReader(tt.data(t)))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("err = %q, want it to mention %q", err, tt.wantMsg)
			}
		})
	}
}

func TestCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	data, err := os.ReadFile("testdata/photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Process(ctx, bytes.NewReader(data)); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func file(name string) func(t *testing.T) []byte {
	return func(t *testing.T) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
}

func bytesOf(s string) func(*testing.T) []byte {
	return func(*testing.T) []byte { return []byte(s) }
}

// Уменьшение в целое число раз + Catmull-Rom: цвета четвертей и прозрачность сохраняются.
func TestScaleWithShrink(t *testing.T) {
	src := quadrants(1) // 640×480
	for y := range 240 {
		for x := range 320 {
			src.SetRGBA(x, y, color.RGBA{}) // левая верхняя четверть прозрачная
		}
	}
	dst := scale(src, 100, 75) // k = 6
	if dst.Bounds().Dx() != 100 || dst.Bounds().Dy() != 75 {
		t.Fatalf("size %v", dst.Bounds())
	}
	if _, _, _, a := dst.At(20, 20).RGBA(); a != 0 {
		t.Errorf("transparent quarter: alpha %d", a>>8)
	}
	for _, p := range []struct {
		x, y int
		want color.RGBA
	}{{80, 20, cTR}, {20, 60, cBL}, {80, 60, cBR}} {
		if got := dst.At(p.x, p.y); !near(got, p.want) {
			t.Errorf("(%d,%d) = %v, want ≈ %v", p.x, p.y, got, p.want)
		}
	}
}
