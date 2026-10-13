package testkit_test

import (
	"testing"

	"github.com/voffkin/keelkit/modules/richtext"
	"github.com/voffkin/keelkit/modules/richtext/testkit"
)

// TestRun — контрактные тесты на минимальной интеграции: richtext выводится через Render.
func TestRun(t *testing.T) {
	testkit.Run(t, func(t *testing.T, markdown string) string {
		h, err := richtext.Render(markdown)
		if err != nil {
			t.Fatal(err)
		}
		return "<!doctype html><script src=\"/static/htmx.min.js\"></script><main>" + string(h) + "</main>"
	})
}

// TestCheck — проверка находит исполняемый код, если сайт обошёл санитайзер.
func TestCheck(t *testing.T) {
	m := testkit.Marker
	unsafe := []string{
		"<script>alert('" + m + "')</script>",
		"<style>x{background:url(javascript:" + m + ")}</style>",
		`<img src=x onerror="` + m + `">`,
		`<a href="javascript:` + m + `">x</a>`,
		`<a href="  JaVa&#x09;Script:` + m + `">x</a>`,
		`<a href="data:text/html,` + m + `">x</a>`,
		`<p style="x:` + m + `">x</p>`,
		`<iframe src="/x"></iframe>`,
	}
	for _, src := range unsafe {
		if len(testkit.Check(src)) == 0 {
			t.Errorf("Check(%q) ничего не нашёл", src)
		}
	}
	safe := []string{
		"<script src=\"/static/htmx.min.js\"></script>",
		"<p>&lt;script&gt;alert(&#39;" + m + "&#39;)&lt;/script&gt;</p>",
		`<a href="/about">` + m + `</a>`,
		"<pre><code>javascript:" + m + "</code></pre>",
		`<meta name="description" content="javascript:alert('` + m + `')">`,
		`<img src="/x.webp" alt="javascript:` + m + `">`,
	}
	for _, src := range safe {
		if got := testkit.Check(src); len(got) != 0 {
			t.Errorf("Check(%q) = %v, want nothing", src, got)
		}
	}
}
