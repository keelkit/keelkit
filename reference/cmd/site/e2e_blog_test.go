package main

// e2e публичной части блога: лента и её страницы, лента категории, пост; черновик и пост с датой в будущем
// на сайте не видны.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// cardTitles — заголовки постов в ленте по порядку.
func cardTitles(doc *goquery.Document) []string {
	var titles []string
	doc.Find(".post-card h2").Each(func(_ int, s *goquery.Selection) { titles = append(titles, text(s)) })
	return titles
}

func TestE2EBlog(t *testing.T) {
	srv := e2eServer(t, true)

	// Первая страница: 10 новых постов из 13 опубликованных, новые сверху; ссылка на следующую страницу.
	doc := page(t, srv, "/blog", http.StatusOK)
	if got := text(doc.Find("title")); got != "Блог — Keelkit — эталонный сайт" {
		t.Errorf("title = %q", got)
	}
	if href, _ := doc.Find(`.site-nav a[aria-current="page"]`).Attr("href"); href != "/blog" {
		t.Errorf("current nav item = %q, want /blog", href)
	}
	titles := cardTitles(doc)
	if len(titles) != perPageBlog || titles[0] != seedPostTitles[0] || titles[9] != seedPostTitles[9] {
		t.Fatalf("page 1 titles = %q", titles)
	}
	first := doc.Find(".post-card").First()
	if href, _ := first.Find("h2 a").Attr("href"); href != "/blog/post-13" {
		t.Errorf("first post link = %q", href)
	}
	if href, _ := first.Find(".post-meta a").Attr("href"); href != "/blog/category/go" || text(first.Find(".post-meta a")) != "Go" {
		t.Errorf("first post category = %q %q", text(first.Find(".post-meta a")), href)
	}
	if dt, ok := first.Find(".post-meta time").Attr("datetime"); !ok || dt == "" || text(first.Find(".post-meta time")) == "" {
		t.Error("first post: no publish date")
	}
	// Анонс: у первого поста seed — ручной, у остальных — начало текста.
	if got := text(first.Find(".post-card-body > p:last-child")); got != seedPostExcerpt {
		t.Errorf("summary = %q, want the manual excerpt", got)
	}
	second := doc.Find(".post-card").Eq(1)
	if !strings.HasPrefix(text(second.Find(".post-card-body > p:last-child")), "Пост «"+seedPostTitles[1]+"» из dev-данных") {
		t.Errorf("second summary = %q", text(second.Find(".post-card-body > p:last-child")))
	}
	if doc.Find(`.pagination a[rel="prev"]`).Length() != 0 {
		t.Error("page 1: prev link")
	}
	if href, _ := doc.Find(`.pagination a[rel="next"]`).Attr("href"); href != "/blog?page=2" {
		t.Errorf("next link = %q", href)
	}

	// Вторая страница: оставшиеся 3, ссылка назад — на /blog без ?page.
	doc = page(t, srv, "/blog?page=2", http.StatusOK)
	if got := cardTitles(doc); len(got) != 3 || got[2] != "Первый пост" {
		t.Errorf("page 2 titles = %q", got)
	}
	if href, _ := doc.Find(`.pagination a[rel="prev"]`).Attr("href"); href != "/blog" {
		t.Errorf("prev link = %q", href)
	}
	if doc.Find(`.pagination a[rel="next"]`).Length() != 0 {
		t.Error("page 2: next link")
	}
	if got := text(doc.Find("title")); got != "Блог — страница 2 — Keelkit — эталонный сайт" {
		t.Errorf("page 2 title = %q", got)
	}

	// Черновик и пост с датой в будущем в ленту не попадают.
	for _, path := range []string{"/blog", "/blog?page=2"} {
		_, body := fetch(t, srv.Client(), srv.URL+path)
		for _, slug := range []string{seedDraftPost, seedFuturePost} {
			if strings.Contains(body, slug) {
				t.Errorf("%s lists %s", path, slug)
			}
		}
	}

	// Несуществующие страницы ленты — 404.
	for _, path := range []string{"/blog?page=3", "/blog?page=0", "/blog?page=x", "/blog?page="} {
		page(t, srv, path, http.StatusNotFound)
	}
}

// perPageBlog — постов на странице ленты (posts.perPage не экспортирован; совпадение проверяет TestE2EBlog).
const perPageBlog = 10

func TestE2EBlogCategory(t *testing.T) {
	srv := e2eServer(t, true)

	// Категория Go — каждый третий опубликованный пост seed (5 из 13).
	doc := page(t, srv, "/blog/category/go", http.StatusOK)
	if got := text(doc.Find("main h1")); got != "Go" {
		t.Errorf("h1 = %q", got)
	}
	if href, _ := doc.Find(".breadcrumbs a").Attr("href"); href != "/blog" {
		t.Errorf("breadcrumbs = %q", href)
	}
	titles := cardTitles(doc)
	want := []string{seedPostTitles[0], seedPostTitles[3], seedPostTitles[6], seedPostTitles[9], seedPostTitles[12]}
	if strings.Join(titles, "|") != strings.Join(want, "|") {
		t.Errorf("titles = %q, want %q", titles, want)
	}
	doc.Find(".post-meta a").Each(func(_ int, s *goquery.Selection) {
		if text(s) != "Go" {
			t.Errorf("category link %q in category Go", text(s))
		}
	})
	if doc.Find(".pagination").Length() != 0 {
		t.Error("one page: pagination shown")
	}

	// Пост из будущего — в категории notes, но не виден.
	_, body := fetch(t, srv.Client(), srv.URL+"/blog/category/notes")
	if strings.Contains(body, seedFuturePost) {
		t.Error("category notes lists the future post")
	}

	page(t, srv, "/blog/category/nope", http.StatusNotFound)
	page(t, srv, "/blog/category/go?page=2", http.StatusNotFound)
	page(t, srv, "/blog/category", http.StatusNotFound) // поста со slug category нет
}

func TestE2EPost(t *testing.T) {
	srv := e2eServer(t, true)

	doc := page(t, srv, "/blog/post-13", http.StatusOK)
	if got := text(doc.Find("main h1")); got != seedPostTitles[0] {
		t.Errorf("h1 = %q", got)
	}
	if got := text(doc.Find("title")); got != seedPostTitles[0]+" — Keelkit — эталонный сайт" {
		t.Errorf("title = %q", got)
	}
	if d, _ := doc.Find(`meta[name="description"]`).Attr("content"); d != seedPostExcerpt {
		t.Errorf("description = %q, want the manual excerpt", d)
	}
	if got := text(doc.Find("article .post-lead")); got != seedPostLead {
		t.Errorf("lead = %q", got)
	}
	if href, _ := doc.Find("article .post-meta a").Attr("href"); href != "/blog/category/go" {
		t.Errorf("category link = %q", href)
	}
	if got := text(doc.Find("#block-intro h2")); got != "Подробнее" {
		t.Errorf("block h2 = %q", got)
	}
	if doc.Find(".draft-banner").Length() != 0 {
		t.Error("public post has the draft banner")
	}

	// Без лида и анонса: лида нет, описание — начало текста.
	doc = page(t, srv, "/blog/post-12", http.StatusOK)
	if doc.Find(".post-lead").Length() != 0 {
		t.Error("post without a lead has .post-lead")
	}
	if d, _ := doc.Find(`meta[name="description"]`).Attr("content"); !strings.HasPrefix(d, "Пост «"+seedPostTitles[1]) {
		t.Errorf("description = %q", d)
	}

	for _, slug := range []string{seedDraftPost, seedFuturePost, "nope"} {
		page(t, srv, "/blog/"+slug, http.StatusNotFound)
	}
}
