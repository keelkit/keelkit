package main

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/voffkin/keelkit/reference/internal/blocks"
	"github.com/voffkin/keelkit/reference/internal/content/pages"
)

// allowRe — поле blocks в site.yaml: «<имя>: { type: blocks, allow: [a, b] }». Полный разбор YAML не нужен:
// схему проверяет make spec-validate, здесь — только совпадение списка блоков с кодом.
var allowRe = regexp.MustCompile(`(?m)^\s+(\w+):\s*\{\s*type:\s*blocks,\s*allow:\s*\[([^\]]*)\]`)

// Каждый блок из allow в site.yaml есть в internal/blocks, и раздел админки предлагает ровно эти блоки.
func TestSpecBlocksMatchCode(t *testing.T) {
	data, err := os.ReadFile("../../site.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m := allowRe.FindAllStringSubmatch(string(data), -1)
	if len(m) != 1 || m[0][1] != "body" {
		t.Fatalf("site.yaml: want one blocks field (pages.body), got %v", m)
	}
	var allow []string
	for _, name := range strings.Split(m[0][2], ",") {
		name = strings.TrimSpace(name)
		allow = append(allow, name)
		if !blocks.Known(name) {
			t.Errorf("site.yaml allows block %q, but internal/blocks has no such type", name)
		}
	}
	if !slices.Equal(allow, pages.BodyBlocks) {
		t.Errorf("site.yaml pages.body.allow = %v, pages.BodyBlocks = %v", allow, pages.BodyBlocks)
	}
}
