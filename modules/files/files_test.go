package files

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	ftyp := func(brand string) []byte { return []byte("\x00\x00\x00\x18ftyp" + brand + "\x00\x00\x02\x00isom") }
	ebml := func(doctype string) []byte {
		return append([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x80 | byte(4+len(doctype)), 0x42, 0x82, 0x80 | byte(len(doctype))}, doctype...)
	}
	longComment := "<!--" + strings.Repeat("x", sniffLen) + "-->"
	tests := []struct {
		name string
		head []byte
		want Format
		err  error
	}{
		{"pdf", []byte("%PDF-1.7\n..."), PDF, nil},
		{"mp4 isom", ftyp("isom"), MP4, nil},
		{"mp4 mp42", ftyp("mp42"), MP4, nil},
		{"quicktime", ftyp("qt  "), "", ErrUnsupportedFormat},
		{"heic", ftyp("heic"), "", ErrUnsupportedFormat},
		{"avif", ftyp("avif"), "", ErrUnsupportedFormat},
		{"webm", ebml("webm"), WebM, nil},
		{"matroska", ebml("matroska"), "", ErrUnsupportedFormat},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), SVG, nil},
		{"svg with prolog", []byte("\xEF\xBB\xBF<?xml version=\"1.0\"?>\n<!-- logo -->\n" +
			`<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd">` +
			"\n<svg>&nbsp;</svg>"), SVG, nil},
		{"svg prefixed", []byte(`<svg:svg xmlns:svg="http://www.w3.org/2000/svg"/>`), SVG, nil},
		{"html", []byte("<!DOCTYPE html><html><body>hi</body></html>"), "", ErrUnsupportedFormat},
		{"xhtml with svg inside", []byte(`<html><svg/></html>`), "", ErrUnsupportedFormat},
		{"xml", []byte(`<?xml version="1.0"?><rss/>`), "", ErrUnsupportedFormat},
		{"root too far", []byte(longComment + "<svg/>")[:sniffLen], "", ErrUnsupportedFormat},
		{"utf-16", []byte("\xFF\xFE<\x00s\x00v\x00g\x00"), "", ErrUnsupportedFormat},
		{"text", []byte("hello"), "", ErrUnsupportedFormat},
		{"png", []byte("\x89PNG\r\n\x1a\n"), "", ErrUnsupportedFormat},
		{"empty", nil, "", ErrInvalidFile},
	}
	for _, tt := range tests {
		got, err := detect(tt.head)
		if got != tt.want || !errors.Is(err, tt.err) || (tt.err == nil) != (err == nil) {
			t.Errorf("%s: detect = %q, %v; want %q, %v", tt.name, got, err, tt.want, tt.err)
		}
	}
}

func TestSniffKeepsWholeFile(t *testing.T) {
	data := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("0123456789"), sniffLen/5)...)
	f, r, err := sniff(bytes.NewReader(data))
	if err != nil || f != PDF {
		t.Fatalf("sniff = %q, %v", f, err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil || !bytes.Equal(buf.Bytes(), data) {
		t.Errorf("sniff reader: %d bytes, %v; want the whole %d-byte file", buf.Len(), err, len(data))
	}
}

func TestURLName(t *testing.T) {
	tests := []struct {
		name string
		f    Format
		want string
	}{
		{"Price List 2026.pdf", PDF, "price-list-2026.pdf"},
		{"Прайс — опт.PDF", PDF, "прайс-опт.pdf"},
		{"  ..//__clip__ (final).mp4", MP4, "clip-final.mp4"},
		{"logo.svg.txt", SVG, "logo-svg.svg"},
		{"", SVG, "file.svg"},
		{"!!!.pdf", PDF, "file.pdf"},
		{strings.Repeat("a", 100) + ".pdf", PDF, strings.Repeat("a", maxURLNameRune) + ".pdf"},
	}
	for _, tt := range tests {
		if got := urlName(cleanName(tt.name), tt.f); got != tt.want {
			t.Errorf("urlName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
	if got := filePath("abc", "прайс.pdf"); got != "abc/%D0%BF%D1%80%D0%B0%D0%B9%D1%81.pdf" {
		t.Errorf("filePath = %q, want the name escaped", got)
	}
}
