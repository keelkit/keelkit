// Package exif — минимальный читатель EXIF: достаёт блок TIFF из JPEG, PNG и WebP и читает из него
// тег Orientation (0x0112). Остальные теги модулю не нужны: варианты кодируются заново и EXIF не содержат.
//
// Свой парсер вместо внешней библиотеки — решение в docs/decisions.md монорепо. Вход недоверенный:
// любые некорректные данные дают «EXIF нет» или ориентацию 1, но не панику (см. FuzzOrientation).
package exif

import (
	"bytes"
	"encoding/binary"
)

// exifHeader предваряет TIFF в сегменте APP1 JPEG и иногда — в чанке EXIF WebP.
var exifHeader = []byte("Exif\x00\x00")

// FromJPEG возвращает блок TIFF из сегмента APP1 «Exif» или nil.
// Маркеры просматриваются до начала данных скана (SOS): метаданные всегда раньше.
func FromJPEG(data []byte) []byte {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil
	}
	pos := 2
	for pos+4 <= len(data) {
		if data[pos] != 0xFF {
			return nil
		}
		marker := data[pos+1]
		if marker == 0xFF { // байты-заполнители перед маркером
			pos++
			continue
		}
		pos += 2
		switch {
		case marker == 0x01 || (marker >= 0xD0 && marker <= 0xD8):
			continue // маркеры без длины
		case marker == 0xDA || marker == 0xD9:
			return nil // SOS или EOI
		}
		n := int(binary.BigEndian.Uint16(data[pos:]))
		if n < 2 || pos+n > len(data) {
			return nil
		}
		seg := data[pos+2 : pos+n]
		if marker == 0xE1 && bytes.HasPrefix(seg, exifHeader) {
			return seg[len(exifHeader):]
		}
		pos += n
	}
	return nil
}

// FromPNG возвращает содержимое чанка eXIf или nil.
func FromPNG(data []byte) []byte {
	const sig = "\x89PNG\r\n\x1a\n"
	if !bytes.HasPrefix(data, []byte(sig)) {
		return nil
	}
	pos := len(sig)
	for pos+8 <= len(data) {
		n := int(binary.BigEndian.Uint32(data[pos:]))
		typ := string(data[pos+4 : pos+8])
		start := pos + 8
		if n < 0 || n > len(data)-start-4 { // данные + CRC должны поместиться
			return nil
		}
		switch typ {
		case "eXIf":
			return data[start : start+n]
		case "IEND":
			return nil
		}
		pos = start + n + 4
	}
	return nil
}

// FromWebP возвращает содержимое чанка EXIF (без префикса «Exif\0\0», если он есть) или nil.
func FromWebP(data []byte) []byte {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return nil
	}
	pos := 12
	for pos+8 <= len(data) {
		typ := string(data[pos : pos+4])
		n := int(binary.LittleEndian.Uint32(data[pos+4:]))
		start := pos + 8
		if n < 0 || n > len(data)-start {
			return nil
		}
		if typ == "EXIF" {
			return bytes.TrimPrefix(data[start:start+n], exifHeader)
		}
		pos = start + n + n%2 // чанки выровнены по чётной границе
	}
	return nil
}

// Orientation читает тег Orientation из IFD0 блока TIFF. Возвращает 1–8;
// 1 («как есть») — если тега нет, блок повреждён или значение вне диапазона.
func Orientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(tiff[0:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	if bo.Uint16(tiff[2:]) != 42 {
		return 1
	}
	ifd := int64(bo.Uint32(tiff[4:]))
	if ifd < 8 || ifd+2 > int64(len(tiff)) {
		return 1
	}
	count := int64(bo.Uint16(tiff[ifd:]))
	for i := range count {
		e := ifd + 2 + i*12
		if e+12 > int64(len(tiff)) {
			return 1
		}
		const tagOrientation, typeShort = 0x0112, 3
		if bo.Uint16(tiff[e:]) != tagOrientation {
			continue
		}
		if bo.Uint16(tiff[e+2:]) != typeShort || bo.Uint32(tiff[e+4:]) != 1 {
			return 1
		}
		if v := int(bo.Uint16(tiff[e+8:])); v >= 1 && v <= 8 {
			return v
		}
		return 1
	}
	return 1
}
