package images

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math"

	"github.com/gen2brain/webp"
	"golang.org/x/image/draw"
	xwebp "golang.org/x/image/webp"

	"github.com/keelkit/keelkit/modules/images/internal/exif"
	"github.com/keelkit/keelkit/modules/images/internal/orient"
)

// Параметры профиля. Меняются только вместе с ProfileVersion.
var profileWidths = []int{480, 960, 1600, 2400}

const (
	webpQuality        = 80
	jpegQuality        = 82
	placeholderQuality = 50
)

// PlaceholderSize — длинная сторона заглушки (Result.Placeholder) в пикселях. Сайт её растягивает и размывает:
// несколько сотен байт прямо в HTML, без отдельного запроса.
const PlaceholderSize = 32

// Process читает файл из r (не больше MaxBytes), проверяет его и строит варианты по текущему профилю.
// Ошибки проверки оборачивают ErrTooLarge, ErrTooManyPixels, ErrUnsupportedFormat или ErrInvalidImage.
// Обработка тяжёлая (до нескольких секунд на большое фото); ctx проверяется между вариантами.
func Process(ctx context.Context, r io.Reader) (*Result, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("images: read: %w", err)
	}
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("%w: more than %d MB", ErrTooLarge, MaxBytes>>20)
	}
	format, err := detect(data)
	if err != nil {
		return nil, err
	}

	// Размеры — по заголовку, до выделения памяти под пиксели (защита от decompression bomb).
	cfg, err := decodeConfig(format, data)
	if err != nil {
		return nil, fmt.Errorf("%w: %s header: %w", ErrInvalidImage, format, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("%w: empty image", ErrInvalidImage)
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return nil, fmt.Errorf("%w: %d×%d is more than %d megapixels", ErrTooManyPixels, cfg.Width, cfg.Height, MaxPixels/1_000_000)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	img, err := decode(format, data)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrInvalidImage, format, err)
	}
	o := orientation(format, data)

	// Масштабируем в сохранённой ориентации, а поворачиваем уже маленький вариант:
	// так не нужна повёрнутая копия полноразмерного изображения.
	sw, sh := img.Bounds().Dx(), img.Bounds().Dy()
	dw, dh := sw, sh // размеры, как их видит человек
	if orient.Swaps(o) {
		dw, dh = sh, sw
	}
	res := &Result{
		ProfileVersion: ProfileVersion,
		Original:       Original{Format: format, Width: dw, Height: dh, Data: data},
	}

	widths := variantWidths(dw)
	res.Variants = make([]Variant, 2*len(widths))
	prev := img
	var smallest *image.RGBA // последний масштабированный, в сохранённой ориентации
	// От большего к меньшему: каждый вариант масштабируется из предыдущего, а не из полноразмерного.
	for i := len(widths) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tw := widths[i]
		th := max(1, int(math.Round(float64(dh)*float64(tw)/float64(dw))))
		stw, sth := tw, th
		if orient.Swaps(o) {
			stw, sth = th, tw
		}
		s := scale(prev, stw, sth)
		prev, smallest = s, s
		v := orient.Apply(s, o)

		var wbuf, jbuf bytes.Buffer
		if err := webp.Encode(&wbuf, v, webp.Options{Quality: webpQuality}); err != nil {
			return nil, fmt.Errorf("images: encode webp %d: %w", tw, err)
		}
		if err := jpeg.Encode(&jbuf, flatten(v), &jpeg.Options{Quality: jpegQuality}); err != nil {
			return nil, fmt.Errorf("images: encode jpeg %d: %w", tw, err)
		}
		res.Variants[2*i] = Variant{Format: WebP, Width: tw, Height: th, Data: wbuf.Bytes()}
		res.Variants[2*i+1] = Variant{Format: JPEG, Width: tw, Height: th, Data: jbuf.Bytes()}
	}
	// Заглушка — из самого узкого варианта: прозрачность в нём видна точнее, чем в 32 px.
	if smallest.Opaque() {
		var err error
		if res.Placeholder, err = placeholder(smallest, o, dw, dh); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// placeholder уменьшает src (в сохранённой ориентации o) до PlaceholderSize по длинной стороне
// (без увеличения), поворачивает и кодирует в data URI WebP. dw×dh — размеры оригинала, как их видит человек.
func placeholder(src *image.RGBA, o, dw, dh int) (string, error) {
	tw, th := dw, dh
	if s := max(dw, dh); s > PlaceholderSize {
		tw = max(1, int(math.Round(float64(dw)*PlaceholderSize/float64(s))))
		th = max(1, int(math.Round(float64(dh)*PlaceholderSize/float64(s))))
	}
	stw, sth := tw, th
	if orient.Swaps(o) {
		stw, sth = th, tw
	}
	var buf bytes.Buffer
	if err := webp.Encode(&buf, orient.Apply(scale(src, stw, sth), o), webp.Options{Quality: placeholderQuality}); err != nil {
		return "", fmt.Errorf("images: encode placeholder: %w", err)
	}
	return "data:image/webp;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// variantWidths — ширины вариантов для оригинала шириной w: профильные не шире оригинала,
// плюс сама w, если она меньше самой большой профильной.
func variantWidths(w int) []int {
	var out []int
	for _, pw := range profileWidths {
		if pw <= w {
			out = append(out, pw)
		}
	}
	if w < profileWidths[len(profileWidths)-1] && (len(out) == 0 || out[len(out)-1] != w) {
		out = append(out, w)
	}
	return out
}

// detect определяет формат по сигнатуре; для известных, но неподдерживаемых форматов называет их.
func detect(data []byte) (Format, error) {
	switch {
	case bytes.HasPrefix(data, []byte("\xFF\xD8\xFF")):
		return JPEG, nil
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return PNG, nil
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		if isAnimatedWebP(data) {
			return "", unsupported("animated WebP")
		}
		return WebP, nil
	case len(data) >= 12 && string(data[4:8]) == "ftyp":
		switch string(data[8:12]) {
		case "heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1":
			return "", unsupported("HEIC/HEIF (supported: JPEG, PNG, WebP)")
		case "avif", "avis":
			return "", unsupported("AVIF (supported: JPEG, PNG, WebP)")
		}
		return "", unsupported("ISO media container (supported: JPEG, PNG, WebP)")
	case bytes.HasPrefix(data, []byte("GIF8")):
		return "", unsupported("GIF (supported: JPEG, PNG, WebP)")
	case bytes.HasPrefix(data, []byte("II*\x00")), bytes.HasPrefix(data, []byte("MM\x00*")):
		return "", unsupported("TIFF (supported: JPEG, PNG, WebP)")
	case bytes.HasPrefix(data, []byte("BM")):
		return "", unsupported("BMP (supported: JPEG, PNG, WebP)")
	}
	return "", unsupported("unknown file type (supported: JPEG, PNG, WebP)")
}

// isAnimatedWebP проверяет флаг анимации в чанке VP8X (он, если есть, идёт первым).
func isAnimatedWebP(data []byte) bool {
	const animationBit = 1 << 1
	return len(data) >= 21 && string(data[12:16]) == "VP8X" && data[20]&animationBit != 0
}

func decodeConfig(f Format, data []byte) (image.Config, error) {
	r := bytes.NewReader(data)
	switch f {
	case JPEG:
		return jpeg.DecodeConfig(r)
	case PNG:
		return png.DecodeConfig(r)
	default:
		return xwebp.DecodeConfig(r)
	}
}

func decode(f Format, data []byte) (image.Image, error) {
	r := bytes.NewReader(data)
	switch f {
	case JPEG:
		return jpeg.Decode(r)
	case PNG:
		return png.Decode(r)
	default:
		return xwebp.Decode(r)
	}
}

func orientation(f Format, data []byte) int {
	switch f {
	case JPEG:
		return exif.Orientation(exif.FromJPEG(data))
	case PNG:
		return exif.Orientation(exif.FromPNG(data))
	default:
		return exif.Orientation(exif.FromWebP(data))
	}
}

// scale масштабирует src до w×h; при совпадении размеров просто копирует в RGBA.
//
// Как в libvips: если источник больше результата хотя бы вдвое, он сначала уменьшается в целое число раз
// усреднением блоков (shrink), а Catmull-Rom работает уже с коэффициентом меньше 2. Это и сглаживает
// мелкие детали, и экономит память: x/image/draw держит промежуточный буфер
// «ширина результата × высота источника × 32 байта» — для 40 Мп и ширины 2400 это 420 МБ.
func scale(src image.Image, w, h int) *image.RGBA {
	sb := src.Bounds()
	if k := min(sb.Dx()/w, sb.Dy()/h); k >= 2 {
		src = shrink(src, k)
		sb = src.Bounds()
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if sb.Dx() == w && sb.Dy() == h {
		draw.Draw(dst, dst.Bounds(), src, sb.Min, draw.Src)
		return dst
	}
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, sb, draw.Src, nil)
	return dst
}

// shrink уменьшает src в k раз по каждой оси, усредняя блоки k×k. Остаток меньше k пикселей у правого
// и нижнего края отбрасывается: искажение пропорций меньше пикселя итогового варианта.
// Источник читается полосами по k строк через draw.Draw — у него быстрые пути для YCbCr, NRGBA и др.
func shrink(src image.Image, k int) *image.RGBA {
	sb := src.Bounds()
	w, h := sb.Dx()/k, sb.Dy()/k
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	strip := image.NewRGBA(image.Rect(0, 0, w*k, k))
	sum := make([]uint32, w*4)
	n := uint32(k * k) // #nosec G115 -- k не больше стороны изображения, k² не больше 40 млн
	for y := range h {
		draw.Draw(strip, strip.Bounds(), src, image.Pt(sb.Min.X, sb.Min.Y+y*k), draw.Src)
		clear(sum)
		for sy := range k {
			row := strip.Pix[sy*strip.Stride:]
			for x := range w {
				px := row[x*k*4 : (x+1)*k*4]
				s := sum[x*4 : x*4+4]
				for i := 0; i < len(px); i += 4 {
					s[0] += uint32(px[i])
					s[1] += uint32(px[i+1])
					s[2] += uint32(px[i+2])
					s[3] += uint32(px[i+3])
				}
			}
		}
		out := dst.Pix[y*dst.Stride:]
		for i, v := range sum {
			out[i] = uint8((v + n/2) / n) // #nosec G115 -- среднее байтов, не больше 255
		}
	}
	return dst
}

// flatten заливает прозрачные области белым: в JPEG нет альфа-канала.
func flatten(img *image.RGBA) *image.RGBA {
	if img.Opaque() {
		return img
	}
	dst := image.NewRGBA(img.Bounds())
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, img.Bounds().Min, draw.Over)
	return dst
}
