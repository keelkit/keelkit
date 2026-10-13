// Package files — загрузка, хранение и отдача файлов (документы, видео, векторные картинки) для сайтов Keelkit.
//
// Изображения для вёрстки — модуль images (варианты по ширинам); этот модуль хранит файлы как есть:
//   - форматы: PDF, видео MP4 и WebM, SVG; формат определяется по содержимому, а не по имени и не по
//     Content-Type загрузки; остальное отклоняется с ErrUnsupportedFormat;
//   - лимит размера — Config.MaxBytes (по умолчанию DefaultMaxBytes, 50 МБ; для видео сайт поднимает его);
//   - адрес файла «<id>/<имя>» не меняется при замене (Service.Replace): ссылки на сайте и снаружи остаются
//     рабочими, поэтому кэш — с проверкой (ETag), а не бессрочный;
//   - Handler отдаёт файлы частями (Range): видео перематывается без загрузки целиком;
//   - SVG не очищается, а отдаётся с Content-Security-Policy: sandbox и X-Content-Type-Options: nosniff:
//     скрипты в файле не выполняются, даже если открыть его по прямому адресу. В <img> браузер скрипты SVG
//     не выполняет и так.
//
// Метаданные — в таблицах files_* базы сайта (SQLite, миграции из пакета migrations), файлы — в Storage.
package files

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// DefaultMaxBytes — лимит размера файла, если в Config не задан свой: сканы PDF бывают под 20 МБ.
const DefaultMaxBytes = 50 << 20

// Ошибки проверки загрузки: сайт сопоставляет их через errors.Is и показывает понятное сообщение.
// Текст ошибки уточняет причину.
var (
	ErrTooLarge          = errors.New("files: file too large")
	ErrUnsupportedFormat = errors.New("files: unsupported format")
	ErrInvalidFile       = errors.New("files: invalid file")
	// ErrFormatMismatch — Replace файлом другого формата: адрес (с расширением) и тип остались бы от старого.
	ErrFormatMismatch = errors.New("files: replacement has a different format")
)

// Format — формат файла.
type Format string

const (
	PDF  Format = "pdf"
	MP4  Format = "mp4"
	WebM Format = "webm"
	SVG  Format = "svg"
)

// Ext — расширение файла без точки.
func (f Format) Ext() string { return string(f) }

// ContentType — MIME-тип, с которым Handler отдаёт файл.
func (f Format) ContentType() string {
	switch f {
	case PDF:
		return "application/pdf"
	case MP4:
		return "video/mp4"
	case WebM:
		return "video/webm"
	case SVG:
		return "image/svg+xml"
	}
	return "application/octet-stream"
}

// IsVideo — видео (на сайте — <video>).
func (f Format) IsVideo() bool { return f == MP4 || f == WebM }

// sniffLen — сколько байт начала файла читает detect. SVG может начинаться с длинного комментария
// или DOCTYPE: корневой элемент должен найтись в этих пределах.
const sniffLen = 64 << 10

// detect определяет формат по началу файла head (не больше sniffLen байт; меньше — значит, это весь файл).
func detect(head []byte) (Format, error) {
	switch {
	case len(head) == 0:
		return "", fmt.Errorf("%w: empty file", ErrInvalidFile)
	case bytes.HasPrefix(head, []byte("%PDF-")):
		return PDF, nil
	case len(head) >= 12 && string(head[4:8]) == "ftyp":
		return detectFtyp(string(head[8:12]))
	case bytes.HasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3}): // EBML: Matroska или WebM
		// DocType — в заголовке EBML, в первых десятках байт.
		if bytes.Contains(head[:min(len(head), 64)], []byte("webm")) {
			return WebM, nil
		}
		return "", fmt.Errorf("%w: Matroska (only WebM)", ErrUnsupportedFormat)
	}
	return detectSVG(head)
}

// mp4Brands — основные бренды ISO BMFF, которые отдаются как video/mp4. QuickTime (qt), HEIC, AVIF и
// аудио (M4A) сюда не входят: браузеры их не показывают как видео MP4.
var mp4Brands = map[string]bool{
	"isom": true, "iso2": true, "iso3": true, "iso4": true, "iso5": true, "iso6": true,
	"mp41": true, "mp42": true, "avc1": true, "M4V ": true, "dash": true,
}

func detectFtyp(brand string) (Format, error) {
	if mp4Brands[brand] {
		return MP4, nil
	}
	return "", fmt.Errorf("%w: ISO media brand %q (only MP4 video)", ErrUnsupportedFormat, brand)
}

// detectSVG — текстовый XML с корневым элементом svg (до него — объявление XML, комментарии, DOCTYPE).
func detectSVG(head []byte) (Format, error) {
	text := bytes.TrimPrefix(head, []byte("\xEF\xBB\xBF"))
	if t := bytes.TrimLeft(text, " \t\r\n"); len(t) == 0 || t[0] != '<' {
		return "", fmt.Errorf("%w: not PDF, MP4, WebM or SVG", ErrUnsupportedFormat)
	}
	if !utf8.Valid(trimIncompleteRune(text, len(head) == sniffLen)) {
		return "", fmt.Errorf("%w: not PDF, MP4, WebM or SVG", ErrUnsupportedFormat)
	}
	d := xml.NewDecoder(bytes.NewReader(text))
	d.Strict = false // HTML-сущности в DOCTYPE и прочие вольности не мешают найти корень
	for {
		tok, err := d.RawToken()
		if err != nil {
			if errors.Is(err, io.EOF) || len(head) == sniffLen {
				return "", fmt.Errorf("%w: no root element in the first %d KB", ErrUnsupportedFormat, sniffLen>>10)
			}
			return "", fmt.Errorf("%w: XML: %w", ErrUnsupportedFormat, err)
		}
		if el, ok := tok.(xml.StartElement); ok {
			if el.Name.Local == "svg" {
				return SVG, nil
			}
			return "", fmt.Errorf("%w: XML with root <%s> (only SVG)", ErrUnsupportedFormat, el.Name.Local)
		}
	}
}

// trimIncompleteRune отрезает от обрезанного на sniffLen начала файла неполный последний символ UTF-8.
func trimIncompleteRune(b []byte, truncated bool) []byte {
	if !truncated {
		return b
	}
	for i := 0; i < utf8.UTFMax && len(b) > 0; i++ {
		if r, _ := utf8.DecodeLastRune(b); r != utf8.RuneError {
			break
		}
		b = b[:len(b)-1]
	}
	return b
}

// sniff читает начало r для detect и возвращает формат и reader всего файла (начало + остаток).
func sniff(r io.Reader) (Format, io.Reader, error) {
	br := bufio.NewReaderSize(r, sniffLen)
	head, err := br.Peek(sniffLen)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return "", nil, fmt.Errorf("files: read upload: %w", err)
	}
	f, err := detect(head)
	if err != nil {
		return "", nil, err
	}
	return f, br, nil
}
