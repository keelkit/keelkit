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
