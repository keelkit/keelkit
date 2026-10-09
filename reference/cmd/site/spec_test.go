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

// Строки site.yaml: тип контента («  posts:» — отступ в два пробела) и поле blocks внутри него
// («<имя>: { type: blocks, allow: [a, b] }»). Полный разбор YAML не нужен: схему проверяет make spec-validate,
// здесь — только совпадение списков блоков с кодом.
var (
	contentTypeRe = regexp.MustCompile(`^  (\w+):\s*$`)
	allowRe       = regexp.MustCompile(`^\s+(\w+):\s*\{\s*type:\s*blocks,\s*allow:\s*\[([^\]]*)\]`)
)

// Каждый блок из allow в site.yaml есть в internal/blocks, и раздел админки типа предлагает ровно эти блоки.
func TestSpecBlocksMatchCode(t *testing.T) {
	code := map[string][]string{"pages.body": pages.BodyBlocks, "posts.body": posts.BodyBlocks}

	f, err := os.Open("../../site.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	spec := map[string][]string{}
	typ := ""
	for sc := bufio.NewScanner(f); sc.Scan(); {
		line := sc.Text()
		if m := contentTypeRe.FindStringSubmatch(line); m != nil {
			typ = m[1]
			continue
		}
		m := allowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var allow []string
		for _, name := range strings.Split(m[2], ",") {
			allow = append(allow, strings.TrimSpace(name))
		}
		spec[typ+"."+m[1]] = allow
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
