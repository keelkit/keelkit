package main

// e2e файлов для поисковиков и данных в <head>: sitemap.xml, rss.xml, robots.txt, canonical и Open Graph,
// редиректы со старых адресов после смены slug.

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type sitemapDoc struct {
	XMLName xml.Name `xml:"http://www.sitemaps.org/schemas/sitemap/0.9 urlset"`
	URLs    []struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod"`
	} `xml:"url"`
}

type rssDoc struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Channel struct {
		Title       string `xml:"title"`
		Description string `xml:"description"`
		Language    string `xml:"language"`
		// <link> канала и <atom:link rel="self">: у них одно локальное имя, различает пространство имён.
		Links []struct {
			XMLName xml.Name
			Value   string `xml:",chardata"`
			Href    string `xml:"href,attr"`
			Rel     string `xml:"rel,attr"`
		} `xml:"link"`
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			GUID        string `xml:"guid"`
			PubDate     string `xml:"pubDate"`
			Description string `xml:"description"`
			Category    string `xml:"category"`
			Enclosure   *struct {
				URL    string `xml:"url,attr"`
				Length int64  `xml:"length,attr"`
				Type   string `xml:"type,attr"`
			} `xml:"enclosure"`
		} `xml:"item"`
	} `xml:"channel"`
}

// fetchXML выполняет GET, проверяет 200 и Content-Type и разбирает XML в v (строгий разбор: битый XML — ошибка).
func fetchXML(t *testing.T, srv *httptest.Server, path, contentType string, v any) {
	t.Helper()
	resp, body := fetch(t, srv.Client(), srv.URL+path)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", path, resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != contentType {
		t.Errorf("GET %s: Content-Type %q, want %q", path, ct, contentType)
	}
	if !strings.HasPrefix(body, xml.Header) {
		t.Errorf("GET %s: no XML declaration", path)
	}
	if err := xml.Unmarshal([]byte(body), v); err != nil {
		t.Fatalf("GET %s: invalid XML: %v\n%.500s", path, err, body)
	}
}

func sitemapLocs(t *testing.T, srv *httptest.Server) map[string]string {
	t.Helper()
	var doc sitemapDoc
	fetchXML(t, srv, "/sitemap.xml", "application/xml; charset=utf-8", &doc)
	locs := map[string]string{}
	for _, u := range doc.URLs {
		if _, dup := locs[u.Loc]; dup {
			t.Errorf("sitemap: duplicate %s", u.Loc)
		}
		locs[u.Loc] = u.LastMod
	}
	return locs
}

// Sitemap: опубликованные страницы (главная — /), лента и видимые посты; без черновиков, будущих постов
// и неопубликованных страниц; адреса абсолютные от BASE_URL.
func TestE2ESitemap(t *testing.T) {
	a := newPageAdmin(t)
	a.create(newPageForm("Только черновик", "draft-only"), "save")

	locs := sitemapLocs(t, a.srv)
	for _, path := range []string{"/", "/about", blogPath, blogPath + "/post-01", blogPath + "/post-13"} {
		if _, ok := locs[testBaseURL+path]; !ok {
			t.Errorf("sitemap: no %s", path)
		}
	}
	for _, path := range []string{"/home", "/draft-only", blogPath + "/" + seedDraftPost, blogPath + "/" + seedFuturePost} {
		if _, ok := locs[testBaseURL+path]; ok {
			t.Errorf("sitemap lists %s", path)
		}
	}
	if want := len(seedPages(nil)) + 1 + 13; len(locs) != want {
		t.Errorf("sitemap: %d URLs, want %d (pages, the feed, 13 posts)", len(locs), want)
	}
	for loc, lastmod := range locs {
		if !strings.HasPrefix(loc, testBaseURL+"/") {
			t.Errorf("sitemap: %s is not under BASE_URL", loc)
		}
		if strings.HasPrefix(loc, testBaseURL+blogPath) {
			if _, err := time.Parse(time.RFC3339, lastmod); err != nil {
				t.Errorf("sitemap: %s lastmod %q: %v", loc, lastmod, err)
			}
		}
	}
	if locs[testBaseURL+blogPath] != locs[testBaseURL+blogPath+"/post-13"] {
		t.Errorf("sitemap: feed lastmod %q, want the newest post's %q", locs[testBaseURL+blogPath], locs[testBaseURL+blogPath+"/post-13"])
	}
}

// RSS 2.0: последние видимые посты, новые сверху, без черновика и будущего поста; обложка — вложением JPEG.
func TestE2EFeed(t *testing.T) {
	s, _ := migratedSiteWithImages(t)
	srv := httptest.NewServer(s.handler(config{}))
	t.Cleanup(srv.Close)

	var doc rssDoc
	fetchXML(t, srv, "/rss.xml", "application/rss+xml; charset=utf-8", &doc)
	ch := doc.Channel
	if doc.Version != "2.0" || ch.Title == "" || ch.Description == "" || ch.Language != "ru" {
		t.Errorf("channel: version %q, title %q, description %q, language %q", doc.Version, ch.Title, ch.Description, ch.Language)
	}
	var link, self string
	for _, l := range ch.Links {
		switch l.XMLName.Space {
		case "":
			link = l.Value
		case "http://www.w3.org/2005/Atom":
			if l.Rel == "self" {
				self = l.Href
			}
		}
	}
	if link != testBaseURL+"/" || self != testBaseURL+"/rss.xml" {
		t.Errorf("channel link %q, atom:link self %q", link, self)
	}
	if len(ch.Items) != 13 {
		t.Fatalf("items: %d, want 13 published posts", len(ch.Items))
	}
	var prev time.Time
	covers := 0
	for i, it := range ch.Items {
		if strings.Contains(it.Link, seedDraftPost) || strings.Contains(it.Link, seedFuturePost) {
			t.Errorf("feed lists %s", it.Link)
		}
		if !strings.HasPrefix(it.Link, testBaseURL+blogPath+"/") || it.Title == "" || it.Description == "" || it.Category == "" {
			t.Errorf("item %d: %+v", i, it)
		}
		if !strings.HasPrefix(it.GUID, "post-") {
			t.Errorf("item %d guid %q", i, it.GUID)
		}
		date, err := time.Parse(time.RFC1123Z, it.PubDate)
		if err != nil {
			t.Errorf("item %d pubDate: %v", i, err)
		}
		if i > 0 && date.After(prev) {
			t.Errorf("item %d is newer than item %d", i, i-1)
		}
		prev = date
		if e := it.Enclosure; e != nil {
			covers++
			if !strings.HasPrefix(e.URL, testBaseURL+"/media/") || !strings.HasSuffix(e.URL, ".jpg") || e.Length <= 0 || e.Type != "image/jpeg" {
				t.Errorf("item %d enclosure %+v", i, *e)
			}
			resp, _ := fetch(t, srv.Client(), srv.URL+strings.TrimPrefix(e.URL, testBaseURL))
			if resp.StatusCode != http.StatusOK {
				t.Errorf("enclosure %s: status %d", e.URL, resp.StatusCode)
			}
		}
	}
	if covers != 5 {
		t.Errorf("items with a cover: %d, want 5 (every third seed post)", covers)
	}
	if first := ch.Items[0].Link; first != testBaseURL+blogPath+"/post-13" {
		t.Errorf("first item %s, want the newest post", first)
	}
	if got := ch.Items[0].Description; got != seedPostExcerpt {
		t.Errorf("first item description %q, want the manual excerpt", got)
	}
	if got := ch.Items[1].Description; !strings.HasPrefix(got, "Пост «"+seedPostTitles[1]) {
		t.Errorf("second item description %q, want the start of the text", got)
	}
}

func TestE2ERobots(t *testing.T) {
	srv := e2eServer(t, false)
	resp, body := fetch(t, srv.Client(), srv.URL+"/robots.txt")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("status %d, Content-Type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for _, line := range []string{"User-agent: *", "Disallow: /admin/", "Sitemap: " + testBaseURL + "/sitemap.xml"} {
		if !slices.Contains(strings.Split(body, "\n"), line) {
			t.Errorf("robots.txt: no line %q\n%s", line, body)
		}
	}
}

// head — данные <head> страницы для поисковиков и соцсетей.
type head struct{ doc *goquery.Document }

func (h head) attr(selector, name string) string {
	v, _ := h.doc.Find("head " + selector).Attr(name)
	return v
}
func (h head) og(property string) string { return h.attr(`meta[property="`+property+`"]`, "content") }
func (h head) canonical() string         { return h.attr(`link[rel="canonical"]`, "href") }

// canonical и Open Graph: абсолютные адреса от BASE_URL; у поста — og:type article, дата и обложка.
func TestE2EHeadMeta(t *testing.T) {
	s, _ := migratedSiteWithImages(t)
	srv := httptest.NewServer(s.handler(config{}))
	t.Cleanup(srv.Close)

	for path, want := range map[string]string{
		"/":                         "/",
		"/about":                    "/about",
		blogPath:                    blogPath,
		blogPath + "?page=2":        blogPath + "?page=2",
		blogPath + "/post-13":       blogPath + "/post-13",
		blogPath + "/category/go":   blogPath + "/category/go",
		blogPath + "/post-13?x=foo": blogPath + "/post-13",
	} {
		h := head{page(t, srv, path, http.StatusOK)}
		if got := h.canonical(); got != testBaseURL+want {
			t.Errorf("%s: canonical %q, want %q", path, got, testBaseURL+want)
		}
		if got := h.og("og:url"); got != testBaseURL+want {
			t.Errorf("%s: og:url %q", path, got)
		}
		if h.og("og:title") == "" || h.og("og:site_name") == "" {
			t.Errorf("%s: no og:title or og:site_name", path)
		}
		if got := h.attr(`link[rel="alternate"][type="application/rss+xml"]`, "href"); got != "/rss.xml" {
			t.Errorf("%s: RSS link %q", path, got)
		}
		if h.attr(`meta[name="robots"]`, "content") != "" {
			t.Errorf("%s: robots meta on a public page", path)
		}
	}

	// Пост с обложкой (post-13 — первый в seed, у каждого третьего обложка): article, дата, картинка JPEG.
	h := head{page(t, srv, blogPath+"/post-13", http.StatusOK)}
	if h.og("og:type") != "article" || h.og("og:title") != text(h.doc.Find("main h1")) {
		t.Errorf("post: og:type %q, og:title %q", h.og("og:type"), h.og("og:title"))
	}
	if _, err := time.Parse(time.RFC3339, h.og("article:published_time")); err != nil {
		t.Errorf("post: article:published_time %q: %v", h.og("article:published_time"), err)
	}
	img := h.og("og:image")
	if !strings.HasPrefix(img, testBaseURL+"/media/") || !strings.HasSuffix(img, ".jpg") || h.og("og:image:width") == "" {
		t.Errorf("post: og:image %q, width %q", img, h.og("og:image:width"))
	}
	if got := h.og("og:image:alt"); got == "" || got != h.doc.Find(".post-cover img").AttrOr("alt", "") {
		t.Errorf("post: og:image:alt %q, want the cover alt", got)
	}
	if h.attr(`meta[name="twitter:card"]`, "content") != "summary_large_image" {
		t.Error("post: twitter:card is not summary_large_image")
	}
	if h.og("og:description") == "" || h.og("og:description") != h.attr(`meta[name="description"]`, "content") {
		t.Errorf("post: og:description %q", h.og("og:description"))
	}

	// Страница: website, картинка — первое изображение блоков; главная — og:title — название сайта.
	h = head{page(t, srv, "/about", http.StatusOK)}
	if h.og("og:type") != "website" || !strings.HasPrefix(h.og("og:image"), testBaseURL+"/media/") {
		t.Errorf("/about: og:type %q, og:image %q", h.og("og:type"), h.og("og:image"))
	}
	h = head{page(t, srv, "/", http.StatusOK)}
	if h.og("og:title") != h.og("og:site_name") {
		t.Errorf("home: og:title %q, want the site name %q", h.og("og:title"), h.og("og:site_name"))
	}

	// 404 — без canonical и Open Graph.
	h = head{page(t, srv, "/no-such-page", http.StatusNotFound)}
	if h.canonical() != "" || h.og("og:url") != "" {
		t.Error("404 has canonical or og:url")
	}
}

// Предпросмотр черновика — noindex и без canonical: адрес админки не должен попасть в поиск.
func TestE2EPreviewNoIndex(t *testing.T) {
	a := newPageAdmin(t)
	edit := a.editLink("О проекте")
	for _, path := range []string{edit + "/view", a.createPost(newPostForm("Пост для превью", "", "", ""), "save") + "/view"} {
		_, body := a.get(path)
		h := head{parse(t, body)}
		if h.attr(`meta[name="robots"]`, "content") != "noindex" || h.canonical() != "" || h.og("og:url") != "" {
			t.Errorf("%s: robots %q, canonical %q", path, h.attr(`meta[name="robots"]`, "content"), h.canonical())
		}
	}
}

// Смена slug при публикации: старый адрес — 301 на новый (и после снятия с публикации и повторной публикации);
// slug, снова занятый опубликованной записью, открывает её, а не редирект.
func TestE2EOldSlugRedirects(t *testing.T) {
	a := newPageAdmin(t)

	// Пост: post-01 → renamed-post.
	var id string
	if err := a.site.db.QueryRow(`SELECT post_id FROM post_contents WHERE kind = 'published' AND slug = 'post-01'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	edit := "/admin/posts/" + id
	form := a.postForm(edit)
	form.Set("slug", "renamed-post")
	if resp, _ := a.submit(edit, form, "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish: status %d", resp.StatusCode)
	}
	assertRedirect(t, a.srv, blogPath+"/post-01", blogPath+"/renamed-post")
	if _, ok := sitemapLocs(t, a.srv)[testBaseURL+blogPath+"/post-01"]; ok {
		t.Error("sitemap lists the old post address")
	}

	// Снят с публикации — 404 по обоим адресам; опубликован снова под третьим slug — оба старых ведут на него.
	if resp, _ := a.submit(edit+"/unpublish", nil, "", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unpublish: status %d", resp.StatusCode)
	}
	page(t, a.srv, blogPath+"/post-01", http.StatusNotFound)
	page(t, a.srv, blogPath+"/renamed-post", http.StatusNotFound)
	form = a.postForm(edit)
	form.Set("slug", "third-slug")
	if resp, _ := a.submit(edit, form, "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("republish: status %d", resp.StatusCode)
	}
	assertRedirect(t, a.srv, blogPath+"/post-01", blogPath+"/third-slug")
	assertRedirect(t, a.srv, blogPath+"/renamed-post", blogPath+"/third-slug")

	// Возврат к прежнему slug: адрес снова открывает пост, а не редирект на самого себя.
	form = a.postForm(edit)
	form.Set("slug", "post-01")
	if resp, _ := a.submit(edit, form, "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish back: status %d", resp.StatusCode)
	}
	page(t, a.srv, blogPath+"/post-01", http.StatusOK)
	assertRedirect(t, a.srv, blogPath+"/third-slug", blogPath+"/post-01")

	// Страница: about → about-us; старый slug заняла новая страница — она и открывается.
	pageEdit := a.editLink("О проекте")
	form = a.form(pageEdit)
	form.Set("slug", "about-us")
	if resp, _ := a.submit(pageEdit, form, "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish page: status %d", resp.StatusCode)
	}
	assertRedirect(t, a.srv, "/about", "/about-us")
	a.create(newPageForm("Новая about", "about"), "publish")
	if got := text(page(t, a.srv, "/about", http.StatusOK).Find("main h1")); got != "Новая about" {
		t.Errorf("/about h1 = %q, want the page that took the slug", got)
	}
}

// Старые адреса поста из формы: любой путь под разделом (перенос с другой CMS) — 301 на пост, пока он опубликован;
// адрес опубликованного поста, раздела категорий или чужой старый адрес — ошибка формы.
func TestE2EPostOldPaths(t *testing.T) {
	a := newPageAdmin(t)
	post := func(slug string) (edit, title string) {
		var id string
		if err := a.site.db.QueryRow(`SELECT post_id, title FROM post_contents WHERE kind = 'published' AND slug = ?`, slug).Scan(&id, &title); err != nil {
			t.Fatal(err)
		}
		return "/admin/posts/" + id, title
	}
	edit, title02 := post("post-02")
	other, title03 := post("post-03")
	save := func(path, oldPaths, slug, action string) (response, *goquery.Document) {
		t.Helper()
		form := a.postForm(path)
		form.Set("old_paths", oldPaths)
		if slug != "" {
			form.Set("slug", slug)
		}
		resp, html := a.submit(path, form, action, nil)
		return resp, parse(t, html)
	}
	oldPaths := func(path string) string {
		t.Helper()
		_, html := a.get(path)
		return parse(t, html).Find(`textarea[name="old_paths"]`).Text()
	}

	// Сохранение черновика: адреса сразу ведут на опубликованный пост; повторы и «/» по краям убраны,
	// %-кодирование раскрыто, в форме — по алфавиту.
	in := "old_slug\n/2019/05/post.html/\n\n old_slug \n%D0%BD%D0%BE%D0%B2%D0%BE%D1%81%D1%82%D1%8C"
	if resp, _ := save(edit, in, "", "save"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: status %d", resp.StatusCode)
	}
	if got, want := oldPaths(edit), "2019/05/post.html\nold_slug\nновость"; got != want {
		t.Errorf("old paths in the form = %q, want %q", got, want)
	}
	for _, path := range []string{"/old_slug", "/old_slug/", "/2019/05/post.html", "/%D0%BD%D0%BE%D0%B2%D0%BE%D1%81%D1%82%D1%8C"} {
		assertRedirect(t, a.srv, blogPath+path, blogPath+"/post-02")
	}
	assertRedirect(t, a.srv, blogPath+"/"+seedOldPath, blogPath+"/post-01") // из seed

	// Ошибки: поле подсвечено, ничего не сохранено.
	for _, tc := range []struct{ path, in, err string }{
		{edit, "post-03", "«post-03» — адрес опубликованного поста «" + title03 + "»: он открывается сам, редирект не нужен."},
		{edit, "post-02", "«post-02» — адрес опубликованного поста «" + title02 + "»: он открывается сам, редирект не нужен."},
		{edit, "category/go", "«category/go»: адреса category/… заняты лентами категорий."},
		{edit, "https://old.example/news/x", "«https://old.example/news/x»: укажите путь без домена: часть адреса после " + blogPath + "/."},
		{edit, "x?id=1", "«x?id=1»: без ?параметров и #якоря: сайт сравнивает только путь."},
		{edit, "/", "«/»: нужен путь после " + blogPath + "/."},
		{other, "old_slug", "«old_slug» уже ведёт на пост «" + title02 + "»: сначала уберите его там."},
	} {
		resp, doc := save(tc.path, tc.in, "", "save")
		if resp.StatusCode != http.StatusUnprocessableEntity || text(doc.Find("#old_paths-error")) != tc.err {
			t.Errorf("%q: status %d, error %q; want 422, %q", tc.in, resp.StatusCode, text(doc.Find("#old_paths-error")), tc.err)
		}
	}
	assertRedirect(t, a.srv, blogPath+"/old_slug", blogPath+"/post-02")

	// Публикация с новым slug: адреса из формы остаются, прежний slug добавляется к ним сам.
	if resp, _ := save(edit, "old_slug", "post-02-new", "publish"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish: status %d", resp.StatusCode)
	}
	if got, want := oldPaths(edit), "old_slug\npost-02"; got != want {
		t.Errorf("old paths after publish = %q, want %q", got, want)
	}
	assertRedirect(t, a.srv, blogPath+"/old_slug", blogPath+"/post-02-new")
	assertRedirect(t, a.srv, blogPath+"/post-02", blogPath+"/post-02-new")
	page(t, a.srv, blogPath+"/2019/05/post.html", http.StatusNotFound)

	// Убрать адрес: редиректа больше нет; теперь его может взять другой пост.
	if resp, _ := save(edit, "post-02", "", "save"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: status %d", resp.StatusCode)
	}
	page(t, a.srv, blogPath+"/old_slug", http.StatusNotFound)
	if resp, _ := save(other, "old_slug", "", "save"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save other: status %d", resp.StatusCode)
	}
	assertRedirect(t, a.srv, blogPath+"/old_slug", blogPath+"/post-03")
}
