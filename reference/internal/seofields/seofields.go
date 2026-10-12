// Package seofields — SEO записи, заданное вручную: заголовок и описание для поисковиков, картинка для соцсетей.
// Общий компонент типов со своим адресом (страницы, посты): поля формы, проверка, раздел формы с превью сниппета
// и наложение на web.Meta. Пустое поле — подстановка, её задаёт тип: web.Meta, собранный из записи (у поста —
// анонс и обложка, у страницы — начало текста и первое изображение).
//
// Пакет не знает о типах контента. Поля хранятся в копии записи (колонки seo_title, seo_description, seo_image
// таблицы <type>_contents): правка SEO опубликованной записи видна на сайте после «Опубликовать».
package seofields

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/voffkin/keelkit/modules/images"

	"github.com/voffkin/keelkit/reference/internal/web"
)

// Имена полей формы; ошибки — под теми же ключами.
const (
	FieldTitle       = "seo_title"
	FieldDescription = "seo_description"
	FieldImage       = "seo_image"
)

// Ограничения полей и то, сколько показывают поисковики (превью предупреждает, что текст обрежут).
const (
	MaxTitleLen          = 200 // символов
	MaxDescriptionLen    = 500 // символов
	searchTitleLen       = 60
	searchDescriptionLen = 160
)

// Fields — SEO записи; пустое поле — подстановка от типа.
type Fields struct {
	Title       string // <title> и og:title как есть, без названия сайта
	Description string // meta description и og:description
	Image       string // id изображения для og:image
}

// FromDB — поля из колонок копии записи.
func FromDB(title, description string, image sql.NullString) Fields {
	return Fields{Title: title, Description: description, Image: image.String}
}

// DBImage — значение колонки seo_image: NULL, если картинка не выбрана.
func (f Fields) DBImage() sql.NullString {
	return sql.NullString{String: f.Image, Valid: f.Image != ""}
}

// Parse — поля из присланной формы (без проверки).
func Parse(form url.Values) Fields {
	return Fields{Title: form.Get(FieldTitle), Description: form.Get(FieldDescription), Image: form.Get(FieldImage)}
}

// Validate нормализует поля (пробелы и переводы строк — один пробел: это одна строка в <head>) и записывает
// ошибки в errs. imageFound — выбранная картинка есть в медиатеке (Resolve вернул её).
func (f *Fields) Validate(errs map[string]string, imageFound bool) {
	f.Title = oneLine(f.Title)
	f.Description = oneLine(f.Description)
	f.Image = strings.TrimSpace(f.Image)
	if utf8.RuneCountInString(f.Title) > MaxTitleLen {
		errs[FieldTitle] = fmt.Sprintf("SEO-заголовок длиннее %d символов.", MaxTitleLen)
	}
	if utf8.RuneCountInString(f.Description) > MaxDescriptionLen {
		errs[FieldDescription] = fmt.Sprintf("SEO-описание длиннее %d символов.", MaxDescriptionLen)
	}
	if f.Image != "" && !imageFound {
		errs[FieldImage] = "Изображения нет в медиатеке: выберите другое."
	}
}

// Apply накладывает заданные поля на подстановку m, собранную типом; img — выбранная картинка (Resolve).
// Поля нормализуются, как в Validate: превью из непроверенной формы показывает то, что будет сохранено.
func (f Fields) Apply(m web.Meta, img *images.Image) web.Meta {
	if t := oneLine(f.Title); t != "" {
		m.Title, m.ExactTitle = t, true
	}
	if d := oneLine(f.Description); d != "" {
		m.Description = d
	}
	if img != nil {
		m.Image, m.ImageAlt = img, ""
	}
	return m
}

// Images — то, что пакету нужно от модуля images.
type Images interface {
	Get(ctx context.Context, id string) (*images.Image, error)
}

// Resolve — выбранная картинка; не выбрана или её нет в медиатеке — nil.
func (f Fields) Resolve(ctx context.Context, imgs Images) (*images.Image, error) {
	if f.Image == "" {
		return nil, nil
	}
	img, err := imgs.Get(ctx, f.Image)
	if errors.Is(err, images.ErrNotFound) {
		return nil, nil
	}
	return img, err
}

// Env — данные раздела формы.
type Env struct {
	Errors   map[string]string // ошибки формы записи: пакет читает свои поля
	Library  []*images.Image   // выбор картинки: последние загруженные
	Image    *images.Image     // выбранная картинка (Resolve): в выборе, даже если её нет в Library
	Meta     web.Meta          // что получит страница записи (подстановка с наложенными полями) — для превью
	Snippet  string            // адрес POST (htmx): превью из присланной формы записи
	MediaURL string            // медиатека: ссылка «загрузите в медиатеку»
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// open — раздел формы раскрыт: что-то задано вручную или есть ошибка в его полях.
func open(f Fields, errs map[string]string) bool {
	return f != Fields{} || errs[FieldTitle] != "" || errs[FieldDescription] != "" || errs[FieldImage] != ""
}

// options — картинки для выбора: Library и выбранная, если её там нет (загружена давно).
func options(e Env) []*images.Image {
	if e.Image == nil {
		return e.Library
	}
	for _, l := range e.Library {
		if l.ID == e.Image.ID {
			return e.Library
		}
	}
	return append([]*images.Image{e.Image}, e.Library...)
}

func optionLabel(img *images.Image) string {
	name := img.Name
	if name == "" {
		name = img.ID
	}
	return fmt.Sprintf("%s · %d×%d", name, img.Width, img.Height)
}

// displayURL — адрес записи в превью: от BASE_URL, без схемы.
func displayURL(base, path string) string {
	u := strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
	if path == "" || path == "/" {
		return u
	}
	return u + path
}

func tooLong(s string, n int) bool { return utf8.RuneCountInString(s) > n }
