package images

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/gen2brain/webp"
)

// Файлы testdata/ генерируются этим тестом и коммитятся; photo.heic сделан отдельно (см. testdata/README.md).
// Перегенерировать: go test -run TestGenerateTestdata -gen
var gen = flag.Bool("gen", false, "перегенерировать testdata/")

// Цвета четвертей показанного (правильно ориентированного) изображения.
var (
	cTL = color.RGBA{220, 30, 30, 255}  // красный
	cTR = color.RGBA{30, 180, 30, 255}  // зелёный
	cBL = color.RGBA{30, 30, 220, 255}  // синий
	cBR = color.RGBA{240, 210, 20, 255} // жёлтый
)

// storedCorners — какой цвет должен стоять в каждом углу сохранённого изображения (TL, TR, BL, BR),
// чтобы после применения ориентации получилась картинка «красный, зелёный / синий, жёлтый».
// Таблица выписана вручную по определению тега Orientation (где оказываются строка 0 и столбец 0),
// а не получена через orient.Apply: иначе ошибка в Apply воспроизвелась бы и в тестовых данных.
var storedCorners = map[int][4]color.RGBA{
	1: {cTL, cTR, cBL, cBR}, // строка 0 сверху, столбец 0 слева
	2: {cTR, cTL, cBR, cBL}, // строка 0 сверху, столбец 0 справа
	3: {cBR, cBL, cTR, cTL}, // строка 0 снизу, столбец 0 справа
	4: {cBL, cBR, cTL, cTR}, // строка 0 снизу, столбец 0 слева
	5: {cTL, cBL, cTR, cBR}, // строка 0 слева, столбец 0 сверху
	6: {cTR, cBR, cTL, cBL}, // строка 0 справа, столбец 0 сверху
	7: {cBR, cTR, cBL, cTL}, // строка 0 справа, столбец 0 снизу
	8: {cBL, cTL, cBR, cTR}, // строка 0 слева, столбец 0 снизу
}

// Размеры показанного изображения в тестах ориентации.
const orientW, orientH = 640, 480

func TestGenerateTestdata(t *testing.T) {
	if !*gen {
		t.Skip("запуск с -gen перегенерирует testdata/")
	}
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join("testdata", name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for o := 1; o <= 8; o++ {
		img := quadrants(o)
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			t.Fatal(err)
		}
		// Чётные — big-endian, нечётные — little-endian: парсер проверяется на обоих порядках байтов.
		write(fmt.Sprintf("orientation-%d.jpg", o), withJPEGExif(buf.Bytes(), tiffWithOrientation(o, o%2 == 0)))
	}

	var buf bytes.Buffer
	if err := webp.Encode(&buf, quadrants(6), webp.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	write("orientation-6.webp", withWebPExif(t, buf.Bytes(), orientH, orientW, tiffWithOrientation(6, false)))

	buf.Reset()
	if err := png.Encode(&buf, transparent()); err != nil {
		t.Fatal(err)
	}
	write("transparent.png", buf.Bytes())

	buf.Reset()
	if err := jpeg.Encode(&buf, gradient(300, 200), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	write("small.jpg", buf.Bytes())

	buf.Reset()
	if err := jpeg.Encode(&buf, gradient(2600, 1734), &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	write("photo.jpg", buf.Bytes())

	write("bomb.png", pngBomb(50_000, 50_000))
}

// quadrants рисует сохранённое изображение для ориентации o: четыре цветных четверти по таблице storedCorners.
func quadrants(o int) *image.RGBA {
	w, h := orientW, orientH
	if o >= 5 {
		w, h = h, w
	}
	c := storedCorners[o]
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			i := 0
			if x >= w/2 {
				i++
			}
			if y >= h/2 {
				i += 2
			}
			img.SetRGBA(x, y, c[i])
		}
	}
	return img
}

// gradient — гладкое «фото» с кругами, чтобы варианты было на что смотреть.
func gradient(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			fx, fy := float64(x)/float64(w), float64(y)/float64(h)
			r := 0.5 + 0.5*math.Sin(fx*7+fy*3)
			g := 0.5 + 0.5*math.Sin(fy*5+1)
			b := 0.5 + 0.5*math.Cos(math.Hypot(fx-0.6, fy-0.4)*25)
			img.SetRGBA(x, y, color.RGBA{uint8(r * 255), uint8(g * 255), uint8(b * 255), 255})
		}
	}
	return img
}

// transparent — PNG 800×600: полупрозрачный круг на полностью прозрачном фоне.
func transparent() *image.NRGBA {
	const w, h = 800, 600
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			d := math.Hypot(float64(x-w/2), float64(y-h/2)) / 250
			if d < 1 {
				img.SetNRGBA(x, y, color.NRGBA{20, 120, 200, uint8(255 * (1 - d*d))})
			}
		}
	}
	return img
}

// tiffWithOrientation строит блок TIFF с IFD0 из двух тегов: Make (ASCII) и Orientation.
func tiffWithOrientation(o int, bigEndian bool) []byte {
	var bo binary.AppendByteOrder = binary.LittleEndian
	hdr := "II"
	if bigEndian {
		bo, hdr = binary.BigEndian, "MM"
	}
	model := []byte("Keelkit test camera\x00")
	b := []byte(hdr)
	b = bo.AppendUint16(b, 42)
	b = bo.AppendUint32(b, 8) // IFD0 сразу за заголовком
	b = bo.AppendUint16(b, 2) // число записей
	makeOff := 8 + 2 + 2*12 + 4
	// Make: тип ASCII (2), значение не помещается в 4 байта — смещение.
	b = bo.AppendUint16(b, 0x010F)
	b = bo.AppendUint16(b, 2)
	b = bo.AppendUint32(b, uint32(len(model)))
	b = bo.AppendUint32(b, uint32(makeOff))
	// Orientation: тип SHORT (3), одно значение в первых двух байтах поля.
	b = bo.AppendUint16(b, 0x0112)
	b = bo.AppendUint16(b, 3)
	b = bo.AppendUint32(b, 1)
	b = bo.AppendUint16(b, uint16(o))
	b = bo.AppendUint16(b, 0)
	b = bo.AppendUint32(b, 0) // следующего IFD нет
	return append(b, model...)
}

// withJPEGExif вставляет сегмент APP1 «Exif» сразу после SOI.
func withJPEGExif(jpg, tiff []byte) []byte {
	seg := append([]byte("Exif\x00\x00"), tiff...)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1}
	out = binary.BigEndian.AppendUint16(out, uint16(len(seg)+2))
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

// withWebPExif превращает простой WebP (RIFF + VP8) в расширенный: VP8X с флагом EXIF, VP8, EXIF.
func withWebPExif(t *testing.T, simple []byte, w, h int, tiff []byte) []byte {
	t.Helper()
	if string(simple[12:16]) != "VP8 " {
		t.Fatalf("ожидался простой WebP с чанком VP8, а не %q", simple[12:16])
	}
	chunk := func(typ string, data []byte) []byte {
		c := append([]byte(typ), binary.LittleEndian.AppendUint32(nil, uint32(len(data)))...)
		c = append(c, data...)
		if len(data)%2 == 1 {
			c = append(c, 0)
		}
		return c
	}
	vp8x := make([]byte, 10)
	vp8x[0] = 1 << 3 // EXIF
	putUint24 := func(b []byte, v int) { b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16) }
	putUint24(vp8x[4:], w-1)
	putUint24(vp8x[7:], h-1)

	body := []byte("WEBP")
	body = append(body, chunk("VP8X", vp8x)...)
	body = append(body, simple[12:]...) // чанк VP8 как есть
	body = append(body, chunk("EXIF", tiff)...)
	out := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...)
	return append(out, body...)
}

// pngBomb — корректный заголовок PNG с огромными размерами и пустыми данными: декодер выделил бы
// гигабайты памяти, но размеры проверяются по IHDR раньше.
func pngBomb(w, h int) []byte {
	chunk := func(typ string, data []byte) []byte {
		c := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
		c = append(c, typ...)
		c = append(c, data...)
		return binary.BigEndian.AppendUint32(c, crc32.ChecksumIEEE(append([]byte(typ), data...)))
	}
	ihdr := binary.BigEndian.AppendUint32(nil, uint32(w))
	ihdr = binary.BigEndian.AppendUint32(ihdr, uint32(h))
	ihdr = append(ihdr, 8, 6, 0, 0, 0) // 8 бит, RGBA
	out := []byte("\x89PNG\r\n\x1a\n")
	out = append(out, chunk("IHDR", ihdr)...)
	out = append(out, chunk("IDAT", nil)...)
	return append(out, chunk("IEND", nil)...)
}
