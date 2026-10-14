// Package images — загрузка, обработка и хранение изображений для сайтов Keelkit.
//
// Process проверяет и обрабатывает загруженный файл по текущему профилю (ProfileVersion),
// Result.Save кладёт оригинал и варианты в Storage. Профиль v2:
//   - вход: JPEG, PNG, WebP (без анимации); HEIC, AVIF и прочее отклоняются с ErrUnsupportedFormat;
//   - лимиты: 20 МБ на файл и 40 мегапикселей; размеры проверяются по заголовку до декодирования;
//   - ориентация EXIF применяется к пикселям; варианты кодируются заново и не содержат EXIF;
//   - ширины 480, 960, 1600, 2400 px без увеличения; если оригинал меньше 2400 px в ширину,
//     добавляется вариант его собственной ширины, чтобы самый крупный вариант не терял разрешение;
//   - каждая ширина — WebP q=80 и JPEG q=82 (фолбэк); прозрачность в JPEG заливается белым;
//   - заглушка на время загрузки (Result.Placeholder, с v2): WebP ~32 px по длинной стороне в data URI;
//     у изображений с прозрачностью её нет — под прозрачными местами она осталась бы видна.
//
// Профиль v1 — то же без заглушки.
//
// Оригинал сохраняется побайтно, вместе с EXIF (в том числе GPS): без него невозможна повторная
// обработка новой версией профиля. Поэтому оригинал не отдаётся публично — только варианты.
//
// ICC-профили не переносятся: варианты считаются sRGB. Фото в Display P3 станут чуть менее насыщенными;
// перенос профиля — кандидат на следующую версию профиля.
//
// WebP кодирует github.com/gen2brain/webp (libwebp, транслированная в Go, без CGO). Если в системе
// установлена libwebp, библиотека подгружает её; сборка с тегом nodynamic отключает это и даёт
// одинаковый результат на всех машинах. Входные WebP декодирует golang.org/x/image/webp на чистом Go,
// поэтому системная libwebp недоверенных данных не видит.
package images

import (
	"errors"
	"fmt"
	"strconv"
)

// ProfileVersion — версия текущего профиля обработки. Хранится у каждого файла; растёт, когда меняются
// ширины, форматы или качество. Старые файлы получают новый профиль только через повторную обработку.
const ProfileVersion = 2

// Лимиты профиля.
const (
	MaxBytes  = 20 << 20   // размер загружаемого файла
	MaxPixels = 40_000_000 // ширина × высота
)

// Ошибки проверки загрузки: сайт сопоставляет их через errors.Is и показывает понятное сообщение.
// Текст ошибки уточняет причину (например, «HEIC»).
var (
	ErrTooLarge          = errors.New("images: file too large")
	ErrTooManyPixels     = errors.New("images: image dimensions too large")
	ErrUnsupportedFormat = errors.New("images: unsupported format")
	ErrInvalidImage      = errors.New("images: invalid image")
)

// Format — формат файла.
type Format string

const (
	JPEG Format = "jpeg"
	PNG  Format = "png"
	WebP Format = "webp"
)

// Ext — расширение файла без точки.
func (f Format) Ext() string {
	if f == JPEG {
		return "jpg"
	}
	return string(f)
}

// ContentType — MIME-тип.
func (f Format) ContentType() string { return "image/" + string(f) }

// Result — итог обработки одного файла.
type Result struct {
	ProfileVersion int
	Original       Original
	Variants       []Variant // по возрастанию ширины; для каждой ширины — WebP, затем JPEG
	// Placeholder — заглушка на время загрузки: «data:image/webp;base64,…», не больше PlaceholderSize
	// по длинной стороне, с пропорциями оригинала. Пустая у изображений с прозрачностью.
	Placeholder string
}

// Original — загруженный файл как есть. Width и Height — с учётом ориентации EXIF, как его видит человек.
type Original struct {
	Format        Format
	Width, Height int
	Data          []byte
}

// Name — имя файла оригинала в хранилище.
func (o Original) Name() string { return "original." + o.Format.Ext() }

// Variant — один вариант изображения.
type Variant struct {
	Format        Format
	Width, Height int
	Data          []byte
}

// Name — имя файла варианта в хранилище, например «960.webp».
func (v Variant) Name() string { return strconv.Itoa(v.Width) + "." + v.Format.Ext() }

func unsupported(what string) error { return fmt.Errorf("%w: %s", ErrUnsupportedFormat, what) }
