package spec

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaCompiles(t *testing.T) {
	if _, err := compiledSchema(); err != nil {
		t.Fatal(err)
	}
}

func TestExamplesValid(t *testing.T) {
	files, err := filepath.Glob("examples/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples found: %v", err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			if err := Validate(readFile(t, f)); err != nil {
				t.Errorf("want valid, got:\n%v", err)
			}
		})
	}
}

// Каждый файл в examples/invalid начинается со строк «# expect: <текст>»:
// каждый такой текст должен встретиться в сообщении об ошибке.
func TestExamplesInvalid(t *testing.T) {
	files, err := filepath.Glob("examples/invalid/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no invalid examples found: %v", err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			data := readFile(t, f)
			expects := expectations(string(data))
			if len(expects) == 0 {
				t.Fatal("file has no '# expect:' lines")
			}

			err := Validate(data)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("want *ValidationError, got %v", err)
			}
			for _, want := range expects {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error does not contain %q:\n%v", want, err)
				}
			}
			for _, is := range ve.Issues {
				if is.Line == 0 {
					t.Errorf("issue without position: %v", is)
				}
			}
		})
	}
}

func TestSyntaxError(t *testing.T) {
	err := Validate([]byte("site: [unclosed"))
	if err == nil || !strings.Contains(err.Error(), "parse yaml") {
		t.Errorf("want yaml parse error, got %v", err)
	}
}

func TestEmptyDocument(t *testing.T) {
	err := Validate(nil)
	if err == nil || !strings.Contains(err.Error(), "document is empty") {
		t.Errorf("want empty document error, got %v", err)
	}
}

func TestIssuesSortedByPosition(t *testing.T) {
	err := Validate([]byte(`spec_version: 1
site:
  name: My Blog
  languages: [ru, en]
content:
  pages:
    fields:
      title: { type: strng }
`))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %v", err)
	}
	if len(ve.Issues) != 3 {
		t.Fatalf("want 3 issues, got:\n%v", err)
	}
	for i := 1; i < len(ve.Issues); i++ {
		if ve.Issues[i].Line < ve.Issues[i-1].Line {
			t.Errorf("issues not sorted:\n%v", err)
		}
	}
}

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func expectations(src string) []string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if want, ok := strings.CutPrefix(line, "# expect: "); ok {
			out = append(out, strings.TrimSpace(want))
		}
	}
	return out
}
