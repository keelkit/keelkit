// Package web — общая оболочка HTML-страниц: layout, страницы ошибок, рендер templ-компонентов.
//
// Пакет не знает о типах контента. Данные для шапки (название сайта, меню) собирает cmd/site
// и кладёт в контекст запроса через WithSite; layout читает их через SiteFrom.
package web

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/a-h/templ"

	"github.com/keelkit/keelkit/modules/images"
)

// Site — данные сайта для layout.
type Site struct {
	Name        string
	Description string
	// Menus — меню сайта по именам из site.yaml (menus: main — шапка, footer — подвал): пункты по порядку,
	// у пункта — подпункты. Как их выводить, решает layout сайта.
	Menus   map[string][]Link
	BaseURL string // схема и хост сайта без слеша в конце (BASE_URL): из него строятся абсолютные адреса
	Feed    string // путь RSS-ленты для <link rel="alternate">; пусто — без ленты
	// ShareImage — картинка для соцсетей по умолчанию: og:image страниц, у которых нет своей; nil — без неё.
	ShareImage   *images.Image
	Contacts     Contacts
	Social       []Link // ссылки на соцсети в подвале по порядку; адреса — абсолютные http(s), проверены при сохранении
	Cookie       CookieNotice
	Verification Verification
	// Path — путь текущего запроса с параметрами (WithSite): куда вернуться после согласия с cookie без JavaScript.
	Path string
}

// Contacts — контакты в подвале; пустое поле не выводится.
type Contacts struct {
	Address string // может быть в несколько строк
	Phone   string // как показывать; ссылка tel: — только цифры (TelURL)
	Email   string
}

// CookieNotice — баннер о cookie: показывается, пока посетитель не нажал «Понятно» (cookie CookieNoticeName).
type CookieNotice struct {
	Text     string // пусто — баннера нет
	Link     string // «Подробнее»: путь на сайте или адрес http(s); пусто — без ссылки
	Accepted bool   // посетитель уже согласился (WithSite)
}

// Show — баннер выводится: текст задан, и посетитель ещё не согласился.
func (c CookieNotice) Show() bool { return c.Text != "" && !c.Accepted }

// Verification — коды подтверждения прав на сайт: мета-теги yandex-verification и google-site-verification
// на каждой странице (файлы в корне сайта не нужны); пусто — без тега.
type Verification struct {
	Yandex string
	Google string
}

// Meta — заголовок страницы и данные для поисковиков и соцсетей (<head>).
type Meta struct {
	Title string // пусто — в <title> только название сайта
	// ExactTitle — Title выводится в <title> как есть, без названия сайта (SEO-заголовок, заданный вручную).
	ExactTitle  bool
	Description string // пусто — описание сайта
	// Path — адрес страницы на сайте для canonical и og:url (страницы ленты — с ?page=N). Пусто — без них:
	// страницы ошибок, предпросмотр черновика.
	Path      string
	Article   bool          // og:type article (пост), иначе website
	Published string        // время публикации статьи, RFC 3339 (article:published_time)
	Image     *images.Image // картинка для og:image; nil — без неё
	ImageAlt  string        // og:image:alt; пусто — без него
	NoIndex   bool          // robots noindex: предпросмотр черновика
}

// Link — пункт меню.
type Link struct {
	Title    string
	URL      string // путь на сайте или адрес http(s)
	Children []Link // подпункты
	Current  bool   // ссылка ведёт на текущую страницу (aria-current)
	// Active — текущая страница внутри пункта: это она сама, страница под его адресом (/blog/post-1 для /blog)
	// или один из подпунктов. Для подсветки раздела.
	Active bool
}

// markCurrent — копия пунктов с отметками Current и Active для пути path.
func markCurrent(links []Link, path string) []Link {
	if len(links) == 0 {
		return nil
	}
	out := make([]Link, len(links))
	for i, l := range links {
		l.Children = markCurrent(l.Children, path)
		l.Current = l.URL == path
		l.Active = l.Current || (l.URL != "/" && strings.HasPrefix(l.URL, "/") && strings.HasPrefix(path, l.URL+"/")) ||
			slices.ContainsFunc(l.Children, func(c Link) bool { return c.Active })
		out[i] = l
	}
	return out
}

type siteKey struct{}

// SiteFrom возвращает данные сайта из контекста запроса (пустые, если WithSite не применялся).
func SiteFrom(ctx context.Context) Site {
	s, _ := ctx.Value(siteKey{}).(Site)
	return s
}

// WithSite — middleware для HTML-маршрутов: загружает данные сайта и кладёт их в контекст.
// Ошибка загрузки — страница 500.
func WithSite(load func(context.Context) (Site, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			site, err := load(r.Context())
			if err != nil {
				ServerError(w, r, err)
				return
			}
			menus := make(map[string][]Link, len(site.Menus))
			for name, links := range site.Menus {
				menus[name] = markCurrent(links, r.URL.Path)
			}
			site.Menus = menus
			site.Path = r.URL.RequestURI()
			site.Cookie.Accepted = cookieAccepted(r)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), siteKey{}, site)))
		})
	}
}

// Render отрисовывает компонент в буфер и отдаёт его со статусом status.
// Ошибка рендера не оставляет полуотрисованную страницу: клиент получает 500.
func Render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	var buf bytes.Buffer
	if err := c.Render(r.Context(), &buf); err != nil {
		slog.Error("render", "method", r.Method, "path", r.URL.Path, "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// NotFound — оформленная страница 404. Подходит как http.HandlerFunc для маршрута по умолчанию.
func NotFound(w http.ResponseWriter, r *http.Request) {
	Render(w, r, http.StatusNotFound, NotFoundPage())
}

// ServerError логирует ошибку и отдаёт оформленную страницу 500 без подробностей.
func ServerError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	Render(w, r, http.StatusInternalServerError, ErrorPage())
}

// TitleTag — содержимое <title> страницы: Title как есть при ExactTitle, иначе «Заголовок — Сайт» или только
// название сайта. Его же показывает превью сниппета в админке.
func TitleTag(m Meta, siteName string) string {
	if m.ExactTitle && m.Title != "" {
		return m.Title
	}
	return pageTitle(m.Title, siteName)
}

// pageTitle — «Заголовок — Сайт» или только название сайта.
func pageTitle(title, siteName string) string {
	if title == "" || title == siteName {
		return siteName
	}
	if siteName == "" {
		return title
	}
	return title + " — " + siteName
}

// Defaults — данные страницы с подстановками сайта: пустое описание — описание сайта, нет своей картинки —
// картинка для соцсетей из настроек (без alt: он описывал бы картинку страницы). Так их выводит LayoutMeta
// и показывает превью сниппета в админке.
func Defaults(m Meta, site Site) Meta {
	if m.Description == "" {
		m.Description = site.Description
	}
	if m.Image == nil && site.ShareImage != nil {
		m.Image, m.ImageAlt = site.ShareImage, ""
	}
	return m
}

// shareWidth — ширина варианта для соцсетей и RSS: превью показывают до ~1200 px.
const shareWidth = 1600

// ShareImage — JPEG-вариант изображения для og:image и обложки в RSS: самый широкий не шире shareWidth, а если
// все шире — самый узкий. JPEG, а не WebP: его понимают все соцсети, мессенджеры и читалки. ok == false — нет
// JPEG-вариантов (или img == nil).
func ShareImage(img *images.Image) (images.StoredVariant, bool) {
	var best images.StoredVariant
	found := false
	if img == nil {
		return best, false
	}
	for _, v := range img.Variants { // по возрастанию ширины
		if v.Format == images.JPEG && (!found || v.Width <= shareWidth) {
			best, found = v, true
		}
	}
	return best, found
}

// TelURL — ссылка tel: из телефона в любом виде: «+7 (495) 123-45-67» → «tel:+74951234567».
// Без цифр — пусто (телефон выводится без ссылки).
func TelURL(phone string) string {
	var b strings.Builder
	for i, r := range strings.TrimSpace(phone) {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
			b.WriteRune(r)
		}
	}
	if s := b.String(); strings.Trim(s, "+") != "" {
		return "tel:" + s
	}
	return ""
}
