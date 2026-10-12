package main

import (
	"bufio"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/voffkin/keelkit/reference/internal/blocks"
	"github.com/voffkin/keelkit/reference/internal/content/pages"
	"github.com/voffkin/keelkit/reference/internal/content/posts"
)

// seoTypes — типы, подключившие SEO записи (internal/seofields): seofields.Section в форме, Fields.Apply в viewMeta,
// колонки seo_* в копии записи.
var seoTypes = []string{"pages", "posts"}

// Строки site.yaml: тип контента («  posts:» — отступ в два пробела) и поле blocks внутри него
// («<имя>: { type: blocks, allow: [a, b] }»). Полный разбор YAML не нужен: схему проверяет make spec-validate,
// здесь — только совпадение списков блоков с кодом.
var (
	contentTypeRe = regexp.MustCompile(`^  (\w+):\s*$`)
	allowRe       = regexp.MustCompile(`^\s+(\w+):\s*\{\s*type:\s*blocks,\s*allow:\s*\[([^\]]*)\]`)
	featuresRe    = regexp.MustCompile(`^\s+features:\s*\[([^\]]*)\]`)
)

// specLines — строки site.yaml с типом контента, в котором они стоят (пусто — вне content).
func specLines(t *testing.T) (types, lines []string) {
	t.Helper()
	f, err := os.Open("../../site.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	typ := ""
	for sc := bufio.NewScanner(f); sc.Scan(); {
		line := sc.Text()
		if m := contentTypeRe.FindStringSubmatch(line); m != nil {
			typ = m[1]
		}
		types, lines = append(types, typ), append(lines, line)
	}
	return types, lines
}

// splitList — элементы списка YAML в одну строку: «a, b» → [a b].
func splitList(s string) []string {
	var out []string
	for _, name := range strings.Split(s, ",") {
		out = append(out, strings.TrimSpace(name))
	}
	return out
}

// Каждый блок из allow в site.yaml есть в internal/blocks, и раздел админки типа предлагает ровно эти блоки.
func TestSpecBlocksMatchCode(t *testing.T) {
	code := map[string][]string{"pages.body": pages.BodyBlocks, "posts.body": posts.BodyBlocks}

	types, lines := specLines(t)
	spec := map[string][]string{}
	for i, line := range lines {
		if m := allowRe.FindStringSubmatch(line); m != nil {
			spec[types[i]+"."+m[1]] = splitList(m[2])
		}
	}

	for field, allow := range spec {
		for _, name := range allow {
			if !blocks.Known(name) {
				t.Errorf("site.yaml %s allows block %q, but internal/blocks has no such type", field, name)
			}
		}
		want, ok := code[field]
		if !ok {
			t.Errorf("site.yaml has blocks field %s, but the test does not know its code list", field)
			continue
		}
		if !slices.Equal(allow, want) {
			t.Errorf("site.yaml %s.allow = %v, code = %v", field, allow, want)
		}
	}
	for field := range code {
		if _, ok := spec[field]; !ok {
			t.Errorf("site.yaml: no blocks field %s", field)
		}
	}
}

// Типы с feature sitemap в site.yaml — ровно те, чьи адреса собирает sitemapSources (в том же порядке),
// с feature rss — feedTypes, с feature seo — seoTypes.
func TestSpecFeaturesMatchCode(t *testing.T) {
	types, lines := specLines(t)
	spec := map[string][]string{}
	for i, line := range lines {
		if m := featuresRe.FindStringSubmatch(line); m != nil {
			for _, f := range splitList(m[1]) {
				spec[f] = append(spec[f], types[i])
			}
		}
	}

	var sitemap []string
	for _, s := range sitemapSources(nil, nil) {
		sitemap = append(sitemap, s.Type)
	}
	if !slices.Equal(spec["sitemap"], sitemap) {
		t.Errorf("site.yaml types with feature sitemap = %v, sitemapSources = %v", spec["sitemap"], sitemap)
	}
	if !slices.Equal(spec["rss"], feedTypes) {
		t.Errorf("site.yaml types with feature rss = %v, feedTypes = %v", spec["rss"], feedTypes)
	}
	if !slices.Equal(spec["seo"], seoTypes) {
		t.Errorf("site.yaml types with feature seo = %v, seoTypes = %v", spec["seo"], seoTypes)
	}
}
