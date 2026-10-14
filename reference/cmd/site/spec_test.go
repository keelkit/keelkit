package main

import (
	"bufio"
	"context"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/keelkit/keelkit/reference/internal/blocks"
	"github.com/keelkit/keelkit/reference/internal/content/pages"
	"github.com/keelkit/keelkit/reference/internal/content/posts"
)

// seoTypes — типы, подключившие SEO записи (internal/seofields): seofields.Section в форме, Fields.Apply в viewMeta,
// колонки seo_* в копии записи.
var seoTypes = []string{"pages", "posts"}

// orderTypes — типы с ручным порядком (feature order): колонка sort_order, кнопки ↑ ↓ в списке админки.
var orderTypes = []string{"people"}

// Строки site.yaml: тип контента («  posts:» — отступ в два пробела) и поле blocks внутри него
// («<имя>: { type: blocks, allow: [a, b] }»). Полный разбор YAML не нужен: схему проверяет make spec-validate,
// здесь — только совпадение списков блоков с кодом.
var (
	contentTypeRe = regexp.MustCompile(`^  (\w+):\s*$`)
	allowRe       = regexp.MustCompile(`^\s+(\w+):\s*\{\s*type:\s*blocks,\s*allow:\s*\[([^\]]*)\]`)
	featuresRe    = regexp.MustCompile(`^\s+features:\s*\[([^\]]*)\]`)
	menusRe       = regexp.MustCompile(`^menus:\s*\[([^\]]*)\]`)
	moduleRe      = regexp.MustCompile(`^  (\w+):\s*(v\d+)\s*(#.*)?$`)
	goModRe       = regexp.MustCompile(`^\s*github\.com/keelkit/keelkit/modules/(\w+)(/v(\d+))?\s+v(\d+)\.`)
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
// с feature rss — feedTypes, с feature seo — seoTypes, с feature order — orderTypes (и у их таблиц есть sort_order).
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
	if !slices.Equal(spec["order"], orderTypes) {
		t.Errorf("site.yaml types with feature order = %v, orderTypes = %v", spec["order"], orderTypes)
	}
	db := migratedSite(t, false).db
	for _, typ := range orderTypes {
		var n int
		err := db.QueryRowContext(context.Background(),
			`SELECT count(*) FROM pragma_table_info(?) WHERE name = 'sort_order'`, typ).Scan(&n)
		if err != nil || n != 1 {
			t.Errorf("table %s: no sort_order column (err %v)", typ, err)
		}
	}
}

// Каждый блок-список (blocks.ListKinds) получает записи из blockLists, и в blockLists нет лишних.
func TestBlockListsMatchKinds(t *testing.T) {
	kinds := blocks.ListKinds()
	code := slices.Sorted(maps.Keys(blockLists(nil)))
	if !slices.Equal(slices.Sorted(slices.Values(kinds)), code) {
		t.Errorf("blocks.ListKinds = %v, blockLists = %v", kinds, code)
	}
}

// settingsFieldRe — поле настроек в site.yaml: «    <имя>: { type: … }» или «    <имя>:» с описанием ниже
// (поле list); отступ — четыре пробела под settings.fields, поля элемента списка глубже и не совпадают.
var settingsFieldRe = regexp.MustCompile(`^    (\w+):`)

// Поля settings в site.yaml — ровно колонки site_settings (кроме служебной id) после миграций.
func TestSpecSettingsMatchCode(t *testing.T) {
	_, lines := specLines(t)
	var spec []string
	in := false
	for _, line := range lines {
		switch {
		case line == "settings:":
			in = true
		case line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#"):
			in = false
		case in:
			if m := settingsFieldRe.FindStringSubmatch(line); m != nil {
				spec = append(spec, m[1])
			}
		}
	}
	if len(spec) == 0 {
		t.Fatal("site.yaml: no settings fields")
	}

	rows, err := migratedSite(t, false).db.QueryContext(context.Background(), `SELECT name FROM pragma_table_info('site_settings') ORDER BY cid`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if name != "id" {
			columns = append(columns, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(spec, columns) {
		t.Errorf("site.yaml settings fields = %v, site_settings columns = %v", spec, columns)
	}
}

// Меню в site.yaml (menus) — ровно siteMenus, в том же порядке.
func TestSpecMenusMatchCode(t *testing.T) {
	_, lines := specLines(t)
	var spec []string
	for _, line := range lines {
		if m := menusRe.FindStringSubmatch(line); m != nil {
			spec = splitList(m[1])
		}
	}
	var code []string
	for _, m := range siteMenus {
		code = append(code, m.Name)
	}
	if !slices.Equal(spec, code) {
		t.Errorf("site.yaml menus = %v, siteMenus = %v", spec, code)
	}
}

// Модули в site.yaml — ровно модули Keelkit из go.mod с теми же мажорными версиями, а модули с миграциями стоят
// в том же порядке, что в migrationSources.
func TestSpecModulesMatchCode(t *testing.T) {
	var specOrder []string
	spec := map[string]string{}
	inModules := false
	_, lines := specLines(t)
	for _, line := range lines {
		if line != "" && line[0] != ' ' && line[0] != '#' {
			inModules = line == "modules:"
			continue
		}
		if m := moduleRe.FindStringSubmatch(line); inModules && m != nil {
			specOrder = append(specOrder, m[1])
			spec[m[1]] = m[2]
		}
	}

	goMod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	code := map[string]string{}
	for line := range strings.Lines(string(goMod)) {
		if m := goModRe.FindStringSubmatch(line); m != nil {
			code[m[1]] = "v" + m[4]
		}
	}
	if !maps.Equal(spec, code) {
		t.Errorf("modules in site.yaml = %v, Keelkit modules in go.mod = %v", spec, code)
	}

	var migrated []string
	for _, src := range migrationSources {
		if src.Name != "site" {
			migrated = append(migrated, src.Name)
		}
	}
	specMigrated := slices.DeleteFunc(slices.Clone(specOrder), func(name string) bool { return !slices.Contains(migrated, name) })
	if !slices.Equal(specMigrated, migrated) {
		t.Errorf("modules with migrations in site.yaml order = %v, migrationSources = %v", specMigrated, migrated)
	}
}
