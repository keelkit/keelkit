package web

import (
	"strconv"
	"strings"

	"github.com/keelkit/keelkit/modules/images"
)

// MediaPrefix — путь, под которым смонтирован images.Service.Handler (newMux в cmd/site).
const MediaPrefix = "/media/"

// MobileMedia — условие media для своей картинки телефона (экран уже 768 px): <source media> в PictureMobile.
// Граница одна на сайт; кнопка «Телефон» живого предпросмотра уже её.
const MobileMedia = "(max-width: 767.98px)"

// fallbackWidth — ширина JPEG в src у <img>: браузер без srcset (и поисковики) получает его.
const fallbackWidth = 960

// MediaURL — адрес варианта изображения на сайте.
func MediaURL(v images.StoredVariant) string { return MediaPrefix + v.Path }

// picture — данные для разметки <picture>: srcset WebP и JPEG и фолбэк-вариант для src.
type picture struct {
	webp, jpeg string
	src        images.StoredVariant
}

// newPicture собирает srcset из вариантов изображения; ok == false, если изображения нет (nil) или JPEG-вариантов нет.
// Фолбэк — самый широкий JPEG не шире fallbackWidth, а если все шире — самый узкий.
func newPicture(img *images.Image) (picture, bool) {
	if img == nil {
		return picture{}, false
	}
	var webp, jpeg []string
	var p picture
	found := false
	for _, v := range img.Variants { // по возрастанию ширины
		item := MediaURL(v) + " " + strconv.Itoa(v.Width) + "w"
		switch v.Format {
		case images.WebP:
			webp = append(webp, item)
		case images.JPEG:
			jpeg = append(jpeg, item)
			if !found || v.Width <= fallbackWidth {
				p.src, found = v, true
			}
		}
	}
	p.webp, p.jpeg = strings.Join(webp, ", "), strings.Join(jpeg, ", ")
	return p, found
}
