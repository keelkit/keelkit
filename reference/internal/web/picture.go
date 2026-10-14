package web

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/a-h/templ"

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

// placeholderStyle — style у <img> c классом ph: размытая заглушка фоном, пока грузится картинка (style.css, img.ph).
// --ph — заглушка img, --ph-m — заглушка mobile на экране уже MobileMedia (если mobile выводится, см. newPicture;
// none, если у неё заглушки нет: заглушка img — другой кадр). "" — заглушек нет: изображения с прозрачностью
// (заглушка осталась бы видна под прозрачными местами) и построенные профилем v1 до reprocess-images.
func placeholderStyle(img, mobile *images.Image) templ.SafeCSS {
	ph := placeholderURL(img)
	var b strings.Builder
	if ph != "" {
		b.WriteString("--ph:" + ph + ";")
	}
	if _, ok := newPicture(mobile); ok {
		m := placeholderURL(mobile)
		if m == "" && ph == "" {
			return ""
		}
		if m == "" {
			m = "none"
		}
		b.WriteString("--ph-m:" + m + ";")
	}
	return templ.SafeCSS(b.String())
}

// placeholderURL — url(...) с SVG, который растягивает заглушку изображения на всю картинку и размывает
// (feGaussianBlur; feFuncA убирает полупрозрачный ободок, который размытие даёт по краям); "" — заглушки нет.
// Заглушка — data URI WebP из модуля images; всё, кроме base64, отбрасывается: значение попадает в CSS.
func placeholderURL(img *images.Image) string {
	const prefix = "data:image/webp;base64,"
	data, ok := strings.CutPrefix(img.Placeholder, prefix)
	if !ok || data == "" || img.Width <= 0 || img.Height <= 0 || strings.ContainsFunc(data, notBase64) {
		return ""
	}
	w, h := strconv.Itoa(img.Width), strconv.Itoa(img.Height)
	blur := strconv.Itoa(max(1, max(img.Width, img.Height)/20)) // ~1,5 пикселя заглушки в 32 px
	svg := "<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 " + w + " " + h + "' preserveAspectRatio='none'>" +
		"<filter id='b' color-interpolation-filters='sRGB'><feGaussianBlur stdDeviation='" + blur + "'/>" +
		"<feComponentTransfer><feFuncA type='discrete' tableValues='1 1'/></feComponentTransfer></filter>" +
		"<image width='100%' height='100%' preserveAspectRatio='none' filter='url(#b)' href='" + prefix + data + "'/></svg>"
	// Без кавычек и с процентным кодированием всего, кроме букв, цифр и «-._~$&+,;=:@»: templ экранирует
	// значение style дважды, и кавычки или «&» в нём сломались бы.
	return "url(data:image/svg+xml," + url.PathEscape(svg) + ")"
}

func notBase64(r rune) bool {
	return !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=", r)
}
