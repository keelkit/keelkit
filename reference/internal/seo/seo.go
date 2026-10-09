// Package seo — файлы для поисковиков и читалок: /sitemap.xml, /rss.xml, /robots.txt.
//
// Пакет не знает о типах контента: адреса и записи ленты отдают сами типы (функции с feature sitemap и rss
// из site.yaml), а связывает их cmd/site. Адреса в файлах абсолютные — от базового URL сайта (BASE_URL).
// Вынос в модуль — после второго сайта.
package seo

import (
	"bytes"
	"cmp"
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Пути файлов на сайте.
const (
	SitemapPath = "/sitemap.xml"
	FeedPath    = "/rss.xml"
	RobotsPath  = "/robots.txt"
)

// URL — адрес для sitemap.
type URL struct {
	Path    string    // путь на сайте, например /blog/post-01
	LastMod time.Time // время последнего изменения; нулевое — без lastmod
}

// Source — адреса одного типа контента для sitemap: только то, что видно на сайте.
type Source func(context.Context) ([]URL, error)

type urlset struct {
	XMLName xml.Name     `xml:"urlset"`
	NS      string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

// Sitemap — GET /sitemap.xml: адреса из всех sources по порядку. Один файл без индекса: лимит протокола —
// 50 000 адресов, сайтам Keelkit до него далеко; при росте — sitemap index.
func Sitemap(baseURL string, sources ...Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		set := urlset{NS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
		for _, src := range sources {
			urls, err := src(r.Context())
			if err != nil {
				serverError(w, r, err)
				return
			}
			for _, u := range urls {
				su := sitemapURL{Loc: baseURL + u.Path}
				if !u.LastMod.IsZero() {
					su.LastMod = u.LastMod.UTC().Format(time.RFC3339)
				}
				set.URLs = append(set.URLs, su)
			}
		}
		writeXML(w, r, "application/xml; charset=utf-8", set)
	}
}

// Channel — описание ленты.
type Channel struct {
	Title       string
	Description string // пусто — Title (элемент обязателен)
	Language    string // код языка, например ru
}

// Item — запись ленты.
type Item struct {
	Title       string
	Path        string // адрес на сайте
	GUID        string // постоянный идентификатор: не меняется со сменой адреса
	Description string // анонс без разметки
	Category    string
	Published   time.Time
	Enclosure   *Enclosure // обложка; nil — без неё
}

// Enclosure — файл записи (обложка).
type Enclosure struct {
	Path   string // адрес на сайте
	Length int64  // байт
	Type   string // MIME-тип
}

// FeedSource — описание ленты и её записи, новые сверху.
type FeedSource func(context.Context) (Channel, []Item, error)

type rss struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	AtomNS  string     `xml:"xmlns:atom,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	Language      string    `xml:"language,omitempty"`
	LastBuildDate string    `xml:"lastBuildDate,omitempty"`
	Self          atomLink  `xml:"atom:link"`
	Items         []rssItem `xml:"item"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type rssItem struct {
	Title       string        `xml:"title"`
	Link        string        `xml:"link"`
	GUID        rssGUID       `xml:"guid"`
	PubDate     string        `xml:"pubDate"`
	Description string        `xml:"description,omitempty"`
	Category    string        `xml:"category,omitempty"`
	Enclosure   *rssEnclosure `xml:"enclosure"`
}

type rssGUID struct {
	IsPermaLink bool   `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

type rssEnclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

// Feed — GET /rss.xml: лента RSS 2.0. Даты — в формате RFC 1123 с числовым поясом, как требует RSS.
func Feed(baseURL string, load FeedSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ch, items, err := load(r.Context())
		if err != nil {
			serverError(w, r, err)
			return
		}
		out := rss{Version: "2.0", AtomNS: "http://www.w3.org/2005/Atom", Channel: rssChannel{
			Title:       ch.Title,
			Link:        baseURL + "/",
			Description: cmp.Or(ch.Description, ch.Title),
			Language:    ch.Language,
			Self:        atomLink{Href: baseURL + FeedPath, Rel: "self", Type: "application/rss+xml"},
		}}
		for i, it := range items {
			if i == 0 {
				out.Channel.LastBuildDate = it.Published.Format(time.RFC1123Z)
			}
			ri := rssItem{
				Title:       it.Title,
				Link:        baseURL + it.Path,
				GUID:        rssGUID{Value: it.GUID},
				PubDate:     it.Published.Format(time.RFC1123Z),
				Description: it.Description,
				Category:    it.Category,
			}
			if e := it.Enclosure; e != nil {
				ri.Enclosure = &rssEnclosure{URL: baseURL + e.Path, Length: e.Length, Type: e.Type}
			}
			out.Channel.Items = append(out.Channel.Items, ri)
		}
		writeXML(w, r, "application/rss+xml; charset=utf-8", out)
	}
}

// Robots — GET /robots.txt: всё, кроме disallow (префиксы путей), открыто; ссылка на sitemap.
func Robots(baseURL string, disallow ...string) http.HandlerFunc {
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	if len(disallow) == 0 {
		b.WriteString("Disallow:\n")
	}
	for _, p := range disallow {
		fmt.Fprintf(&b, "Disallow: %s\n", p)
	}
	fmt.Fprintf(&b, "\nSitemap: %s%s\n", baseURL, SitemapPath)
	body := b.String()
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}
}

// writeXML кодирует v в буфер и отдаёт с заголовком XML: ошибка не оставляет оборванный файл.
func writeXML(w http.ResponseWriter, r *http.Request, contentType string, v any) {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(v); err != nil {
		serverError(w, r, err)
		return
	}
	buf.WriteByte('\n')
	w.Header().Set("Content-Type", contentType)
	_, _ = buf.WriteTo(w)
}

func serverError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}
