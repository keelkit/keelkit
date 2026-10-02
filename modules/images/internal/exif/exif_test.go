package exif

import (
	"encoding/binary"
	"os"
	"testing"
)

// tiff собирает блок TIFF с одной записью IFD0.
func tiff(bo binary.AppendByteOrder, tag, typ uint16, count uint32, value uint16) []byte {
	b := []byte("II")
	if bo == binary.BigEndian {
		b = []byte("MM")
	}
	b = bo.AppendUint16(b, 42)
	b = bo.AppendUint32(b, 8)
	b = bo.AppendUint16(b, 1)
	b = bo.AppendUint16(b, tag)
	b = bo.AppendUint16(b, typ)
	b = bo.AppendUint32(b, count)
	b = bo.AppendUint16(b, value)
	b = bo.AppendUint16(b, 0)
	return bo.AppendUint32(b, 0)
}

func TestOrientation(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want int
	}{
		{"little-endian", tiff(binary.LittleEndian, 0x0112, 3, 1, 6), 6},
		{"big-endian", tiff(binary.BigEndian, 0x0112, 3, 1, 8), 8},
		{"out of range", tiff(binary.LittleEndian, 0x0112, 3, 1, 9), 1},
		{"zero", tiff(binary.LittleEndian, 0x0112, 3, 1, 0), 1},
		{"wrong type", tiff(binary.LittleEndian, 0x0112, 4, 1, 6), 1},
		{"other tag", tiff(binary.LittleEndian, 0x010F, 3, 1, 6), 1},
		{"nil", nil, 1},
		{"bad magic", []byte("II\x2b\x00\x08\x00\x00\x00"), 1},
		{"ifd out of bounds", []byte("II\x2a\x00\xff\x00\x00\x00"), 1},
		{"entries truncated", tiff(binary.LittleEndian, 0x0112, 3, 1, 6)[:16], 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Orientation(tt.data); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestContainers(t *testing.T) {
	block := tiff(binary.BigEndian, 0x0112, 3, 1, 3)

	app1 := append([]byte("Exif\x00\x00"), block...)
	jpg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x04, 0x00, 0x00} // APP0 до APP1
	jpg = append(jpg, 0xFF, 0xE1)
	jpg = binary.BigEndian.AppendUint16(jpg, uint16(len(app1)+2))
	jpg = append(jpg, app1...)
	jpg = append(jpg, 0xFF, 0xDA, 0x00, 0x02, 0xFF, 0xD9)
	if got := Orientation(FromJPEG(jpg)); got != 3 {
		t.Errorf("jpeg: %d", got)
	}

	png := []byte("\x89PNG\r\n\x1a\n")
	png = binary.BigEndian.AppendUint32(png, uint32(len(block)))
	png = append(png, "eXIf"...)
	png = append(png, block...)
	png = append(png, 0, 0, 0, 0) // CRC не проверяется
	if got := Orientation(FromPNG(png)); got != 3 {
		t.Errorf("png: %d", got)
	}

	for _, prefixed := range []bool{false, true} {
		payload := block
		if prefixed {
			payload = app1
		}
		webp := []byte("RIFF\x00\x00\x00\x00WEBP")
		webp = append(webp, "ICCP\x03\x00\x00\x00abc\x00"...) // нечётный чанк с выравниванием
		webp = append(webp, "EXIF"...)
		webp = binary.LittleEndian.AppendUint32(webp, uint32(len(payload)))
		webp = append(webp, payload...)
		if got := Orientation(FromWebP(webp)); got != 3 {
			t.Errorf("webp (Exif prefix %v): %d", prefixed, got)
		}
	}

	// Без EXIF.
	if FromJPEG([]byte{0xFF, 0xD8, 0xFF, 0xDA, 0x00, 0x02}) != nil || FromPNG([]byte("\x89PNG\r\n\x1a\n")) != nil ||
		FromWebP([]byte("RIFF\x00\x00\x00\x00WEBP")) != nil {
		t.Error("want nil without EXIF")
	}
}

func TestTestdata(t *testing.T) {
	for o, name := range map[int]string{1: "orientation-1.jpg", 6: "orientation-6.jpg", 7: "orientation-7.jpg"} {
		data, err := os.ReadFile("../../testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if got := Orientation(FromJPEG(data)); got != o {
			t.Errorf("%s: %d", name, got)
		}
	}
	data, err := os.ReadFile("../../testdata/orientation-6.webp")
	if err != nil {
		t.Fatal(err)
	}
	if got := Orientation(FromWebP(data)); got != 6 {
		t.Errorf("webp: %d", got)
	}
}

// Вход недоверенный: никаких паник и только значения 1–8.
func FuzzOrientation(f *testing.F) {
	f.Add(tiff(binary.LittleEndian, 0x0112, 3, 1, 6))
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x08, 'E', 'x', 'i', 'f', 0, 0})
	f.Add([]byte("RIFF\x00\x00\x00\x00WEBPEXIF\xff\xff\xff\x7f"))
	f.Add([]byte("\x89PNG\r\n\x1a\n\xff\xff\xff\xffeXIf"))
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, block := range [][]byte{data, FromJPEG(data), FromPNG(data), FromWebP(data)} {
			if o := Orientation(block); o < 1 || o > 8 {
				t.Fatalf("orientation %d", o)
			}
		}
	})
}
