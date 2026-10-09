// Package spec — спецификация сайта Keelkit (site.yaml): JSON Schema и валидатор.
//
// Validate проверяет документ по schema.json, а затем — правила, которые
// в JSON Schema не выражаются (ссылки ref на существующие типы контента и т. п.).
package spec

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"go.yaml.in/yaml/v3"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

//go:embed schema.json
var schemaJSON []byte

const schemaURL = "https://raw.githubusercontent.com/voffkin/keelkit/main/spec/schema.json"

// reservedFields — служебные колонки, которые генерируются для каждого типа контента и его features.
var reservedFields = []string{"id", "created_at", "updated_at", "draft", "publish_date"}

// Issue — одна найденная проблема в спецификации.
type Issue struct {
	Path    string // JSON Pointer к значению, например /content/posts/fields/category
	Line    int    // строка в YAML (с 1); 0 — неизвестна
	Column  int    // столбец в YAML (с 1); 0 — неизвестен
	Message string
}

func (i Issue) String() string {
	return fmt.Sprintf("%d:%d: %s: %s", i.Line, i.Column, i.Path, i.Message)
}

// ValidationError — спецификация не прошла проверку; Issues отсортированы по позиции в файле.
type ValidationError struct {
	Issues []Issue
}

func (e *ValidationError) Error() string {
	lines := make([]string, len(e.Issues))
	for i, is := range e.Issues {
		lines[i] = is.String()
	}
	return strings.Join(lines, "\n")
}

// Validate проверяет содержимое site.yaml. Ошибка синтаксиса YAML возвращается как есть,
// ошибки спецификации — как *ValidationError.
func Validate(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse yaml: %w", err)
	}
	if len(doc.Content) == 0 {
		return &ValidationError{Issues: []Issue{{Path: "/", Line: 1, Column: 1, Message: "document is empty"}}}
	}
	root := doc.Content[0]

	var issues []Issue
	value := toJSON(root, "", &issues)
	if len(issues) == 0 {
		sch, err := compiledSchema()
		if err != nil {
			return err
		}
		if err := sch.Validate(value); err != nil {
			var ve *jsonschema.ValidationError
			if !errors.As(err, &ve) {
				return err
			}
			issues = schemaIssues(ve, root)
		}
	}
	// Смысловые проверки опираются на структуру, которую гарантирует схема.
	if len(issues) == 0 {
		issues = semanticIssues(root)
	}
	if len(issues) == 0 {
		return nil
	}

	slices.SortStableFunc(issues, func(a, b Issue) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return a.Column - b.Column
	})
	return &ValidationError{Issues: slices.Compact(issues)}
}

var compiledSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(string(schemaJSON)))
	if err != nil {
		return nil, fmt.Errorf("parse schema.json: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaURL, doc); err != nil {
		return nil, fmt.Errorf("add schema.json: %w", err)
	}
	sch, err := c.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("compile schema.json: %w", err)
	}
	return sch, nil
})

// toJSON переводит узел YAML в значение, пригодное для валидатора JSON Schema
// (map[string]any, []any, string, json.Number, bool, nil). Проблемы, которые
// JSON Schema не увидит (повторяющиеся и составные ключи), складываются в issues.
func toJSON(n *yaml.Node, path string, issues *[]Issue) any {
	switch n.Kind {
	case yaml.AliasNode:
		return toJSON(n.Alias, path, issues)
	case yaml.MappingNode:
		obj := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Kind != yaml.ScalarNode || k.ShortTag() == "!!merge" {
				*issues = append(*issues, issueAt(path, k, "only plain scalar keys are supported"))
				continue
			}
			p := path + "/" + escapePointer(k.Value)
			if _, dup := obj[k.Value]; dup {
				*issues = append(*issues, issueAt(p, k, fmt.Sprintf("duplicate key %q", k.Value)))
				continue
			}
			obj[k.Value] = toJSON(v, p, issues)
		}
		return obj
	case yaml.SequenceNode:
		arr := make([]any, len(n.Content))
		for i, item := range n.Content {
			arr[i] = toJSON(item, path+"/"+strconv.Itoa(i), issues)
		}
		return arr
	case yaml.ScalarNode:
		switch n.ShortTag() {
		case "!!null":
			return nil
		case "!!bool":
			var b bool
			if err := n.Decode(&b); err == nil {
				return b
			}
		case "!!int":
			var i int64
			if err := n.Decode(&i); err == nil {
				return json.Number(strconv.FormatInt(i, 10))
			}
		case "!!float":
			var f float64
			if err := n.Decode(&f); err == nil {
				return json.Number(strconv.FormatFloat(f, 'g', -1, 64))
			}
		}
		return n.Value
	}
	return nil
}

// schemaIssues превращает дерево ошибок валидатора в плоский список с позициями в YAML.
func schemaIssues(ve *jsonschema.ValidationError, root *yaml.Node) []Issue {
	p := message.NewPrinter(language.English)
	var issues []Issue
	var walk func(e *jsonschema.ValidationError, keyLoc []string)
	walk = func(e *jsonschema.ValidationError, keyLoc []string) {
		// Имя свойства валидатор проверяет как отдельное значение и теряет его место
		// в документе; запоминаем путь к ключу для вложенных ошибок.
		if pn, ok := e.ErrorKind.(*kind.PropertyNames); ok {
			keyLoc = propertyLoc(root, e.InstanceLocation, pn.Property)
		}
		if len(e.Causes) > 0 {
			for _, c := range e.Causes {
				walk(c, keyLoc)
			}
			return
		}
		loc := e.InstanceLocation
		if keyLoc != nil {
			loc = keyLoc
		}
		// Для лишних свойств указываем на сам ключ, а не на объект.
		if ap, ok := e.ErrorKind.(*kind.AdditionalProperties); ok && len(ap.Properties) > 0 {
			loc = append(slices.Clone(loc), ap.Properties[0])
		}
		issues = append(issues, issueAt(pointer(loc), lookup(root, loc), e.ErrorKind.LocalizedString(p)))
	}
	walk(ve, nil)
	return issues
}

// propertyLoc — путь к ключу name объекта loc. jsonschema v6.0.3 для propertyNames сохраняет InstanceLocation
// без копии, и проверка соседних свойств может затереть его (порядок обхода map случаен — ошибка плавающая).
// Если у объекта по loc такого ключа нет, путь ищется по документу: первый объект с ключом name.
func propertyLoc(root *yaml.Node, loc []string, name string) []string {
	if k, _ := childPair(lookupValue(root, loc), name); k != nil {
		return append(slices.Clone(loc), name)
	}
	if found := findKey(root, nil, name); found != nil {
		return found
	}
	return append(slices.Clone(loc), name)
}

// lookupValue — узел значения по пути (lookup возвращает узел ключа).
func lookupValue(n *yaml.Node, tokens []string) *yaml.Node {
	for _, tok := range tokens {
		for n != nil && n.Kind == yaml.AliasNode {
			n = n.Alias
		}
		switch {
		case n == nil:
			return nil
		case n.Kind == yaml.MappingNode:
			n = child(n, tok)
		case n.Kind == yaml.SequenceNode:
			i, err := strconv.Atoi(tok)
			if err != nil || i < 0 || i >= len(n.Content) {
				return nil
			}
			n = n.Content[i]
		default:
			return nil
		}
	}
	return n
}

// findKey — путь к первому (в порядке документа) ключу name в объектах под n.
func findKey(n *yaml.Node, path []string, name string) []string {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) > 0 {
			return findKey(n.Content[0], path, name)
		}
	case yaml.MappingNode:
		for _, kv := range pairs(n) {
			p := append(slices.Clone(path), kv[0].Value)
			if kv[0].Value == name {
				return p
			}
			if found := findKey(kv[1], p, name); found != nil {
				return found
			}
		}
	case yaml.SequenceNode:
		for i, item := range n.Content {
			if found := findKey(item, append(slices.Clone(path), strconv.Itoa(i)), name); found != nil {
				return found
			}
		}
	}
	return nil
}

// semanticIssues — правила, которые не выражаются в JSON Schema.
// Вызывается только для документа, прошедшего проверку схемой.
func semanticIssues(root *yaml.Node) []Issue {
	content := child(root, "content")
	var types []string
	for _, kv := range pairs(content) {
		types = append(types, kv[0].Value)
	}

	var issues []Issue
	for _, tkv := range pairs(content) {
		typeName := tkv[0].Value
		var slugs []string
		for _, fkv := range pairs(child(tkv[1], "fields")) {
			name, field := fkv[0], fkv[1]
			path := "/content/" + escapePointer(typeName) + "/fields/" + escapePointer(name.Value)

			if slices.Contains(reservedFields, name.Value) {
				issues = append(issues, issueAt(path, name, fmt.Sprintf(
					"field name %q is reserved (generated for every content type or by features); reserved: %s",
					name.Value, strings.Join(reservedFields, ", "))))
			}

			fieldType := child(field, "type").Value
			to := child(field, "to")
			switch {
			case fieldType != "ref" && to != nil:
				issues = append(issues, issueAt(path+"/to", to, fmt.Sprintf(
					"'to' is allowed only for fields of type ref, got type %s", fieldType)))
			case fieldType == "ref" && !slices.Contains(types, to.Value):
				issues = append(issues, issueAt(path+"/to", to, fmt.Sprintf(
					"ref target %q is not a content type; known types: %s", to.Value, strings.Join(types, ", "))))
			}
			if allow := child(field, "allow"); fieldType != "blocks" && allow != nil {
				issues = append(issues, issueAt(path+"/allow", allow, fmt.Sprintf(
					"'allow' is allowed only for fields of type blocks, got type %s", fieldType)))
			}

			if fieldType == "slug" {
				slugs = append(slugs, name.Value)
				if len(slugs) == 2 {
					issues = append(issues, issueAt(path, name, fmt.Sprintf(
						"content type %q has more than one slug field (%s, %s)", typeName, slugs[0], slugs[1])))
				}
			}
		}
	}
	return issues
}

func issueAt(path string, n *yaml.Node, msg string) Issue {
	if path == "" {
		path = "/"
	}
	is := Issue{Path: path, Message: msg}
	if n != nil {
		is.Line, is.Column = n.Line, n.Column
	}
	return is
}

// lookup находит узел по пути из токенов JSON Pointer. Для элемента объекта
// возвращает узел ключа: так позиция указывает на строку с именем поля.
func lookup(n *yaml.Node, tokens []string) *yaml.Node {
	for i, tok := range tokens {
		for n.Kind == yaml.AliasNode {
			n = n.Alias
		}
		switch n.Kind {
		case yaml.MappingNode:
			key, val := childPair(n, tok)
			if key == nil {
				return n
			}
			if i == len(tokens)-1 {
				return key
			}
			n = val
		case yaml.SequenceNode:
			i, err := strconv.Atoi(tok)
			if err != nil || i < 0 || i >= len(n.Content) {
				return n
			}
			n = n.Content[i]
		default:
			return n
		}
	}
	return n
}

func child(n *yaml.Node, key string) *yaml.Node {
	_, v := childPair(n, key)
	return v
}

func childPair(n *yaml.Node, key string) (k, v *yaml.Node) {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i], n.Content[i+1]
		}
	}
	return nil, nil
}

func pairs(n *yaml.Node) [][2]*yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	out := make([][2]*yaml.Node, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, [2]*yaml.Node{n.Content[i], n.Content[i+1]})
	}
	return out
}

func pointer(tokens []string) string {
	if len(tokens) == 0 {
		return "/"
	}
	var sb strings.Builder
	for _, t := range tokens {
		sb.WriteByte('/')
		sb.WriteString(escapePointer(t))
	}
	return sb.String()
}

func escapePointer(tok string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(tok)
}
