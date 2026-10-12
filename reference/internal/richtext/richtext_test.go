package richtext

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		want    []string // подстроки, которые должны быть в результате
		notWant []string // подстроки, которых быть не должно
	}{
		{
			name: "table",
			src:  "| a | b |\n|---|---|\n| 1 | 2 |\n",
			want: []string{"<table>", "<th>a</th>", "<td>2</td>"},
		},
		{
			name: "code block keeps language class",
			src:  "```go\nfmt.Println(\"<b>\")\n```\n",
			want: []string{`<code class="language-go">`, "&lt;b&gt;"},
		},
		{
			name:    "unsafe code class dropped",
			src:     "```x\" onclick=\"alert(1)\n1\n```\n",
			notWant: []string{"onclick"},
		},
		{
			name:    "raw script removed",
			src:     "text <script>alert(1)</script>\n\n<script>alert(2)</script>\n",
			notWant: []string{"<script", "alert(2)"},
		},
		{
			name:    "event handler attribute removed",
			src:     `<img src="x" onerror="alert(1)">`,
			notWant: []string{"onerror"},
		},
		{
			name:    "javascript link removed",
			src:     "[click](javascript:alert(1))",
			notWant: []string{"javascript:"},
		},
		{
			name:    "internal link without nofollow",
			src:     "[about](/about)",
			want:    []string{`<a href="/about">about</a>`},
			notWant: []string{"nofollow"},
		},
		{
			name: "external link with nofollow",
			src:  "[go](https://go.dev/)",
			want: []string{`href="https://go.dev/"`, `rel="nofollow"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range tt.want {
				if !strings.Contains(string(got), s) {
					t.Errorf("result does not contain %q:\n%s", s, got)
				}
			}
			for _, s := range tt.notWant {
				if strings.Contains(string(got), s) {
					t.Errorf("result contains %q:\n%s", s, got)
				}
			}
		})
	}
}

func TestPlainText(t *testing.T) {
	src := "## Заголовок\n\nАбзац с **жирным** и [ссылкой](https://example.com).\n\n- раз\n- два\n\n<script>alert(1)</script>\n\n&amp; «кавычки»\n\n| a | b |\n|---|---|\n| 1 | 2 |"
	got := PlainText(src)
	want := "Заголовок Абзац с жирным и ссылкой. раз два & «кавычки» a b 1 2"
	if got != want {
		t.Errorf("PlainText = %q, want %q", got, want)
	}
}

func TestTypography(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"preposition", "Дом в лесу", "<p>Дом в\u00a0лесу</p>\n"},
		{"capitalized and chained", "В лесу и в поле", "<p>В\u00a0лесу и\u00a0в\u00a0поле</p>\n"},
		{"two-letter conjunction", "Тихо, но темно", "<p>Тихо, но\u00a0темно</p>\n"},
		{"long word untouched", "Дом вдали", "<p>Дом вдали</p>\n"},
		{"dash", "Кот — зверь", "<p>Кот\u00a0— зверь</p>\n"},
		{"en dash as dash", "Кот – зверь", "<p>Кот\u00a0– зверь</p>\n"},
		{"range dash untouched", "2020–2025 годы", "<p>2020–2025 годы</p>\n"},
		{"hyphenated preposition", "из-за угла", "<p>из-за\u00a0угла</p>\n"},
		{"soft line break", "дом в\nлесу", "<p>дом в\u00a0лесу</p>\n"},
		{"across inline tag", "дом в **лесу**", "<p>дом в\u00a0<strong>лесу</strong></p>\n"},
		{"preposition inside inline tag", "**в** лесу", "<p><strong>в</strong>\u00a0лесу</p>\n"},
		{"word split by inline tag", "**к**от ест", "<p><strong>к</strong>от ест</p>\n"},
		{"not across blocks", "- в\n- лес", "<ul>\n<li>в</li>\n<li>лес</li>\n</ul>\n"},
		{"code span untouched", "код `a в b` и", "<p>код <code>a в b</code> и</p>\n"},
		{"code block untouched", "```\nв лесу — тихо\n```\n", "<pre><code>в лесу — тихо\n</code></pre>\n"},
		{"escaping kept", "a & в <лес>", "<p>a &amp; в\u00a0&lt;лес&gt;</p>\n"},
		{"link text", "[в лес](/forest)", "<p><a href=\"/forest\">в\u00a0лес</a></p>\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("Render(%q) =\n%q\nwant\n%q", tt.src, got, tt.want)
			}
		})
	}
}
