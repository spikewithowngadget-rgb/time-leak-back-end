package handler

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestOpenAPICoversEveryRegisteredRoute(t *testing.T) {
	var document map[string]any
	if err := json.Unmarshal([]byte(swaggerSpec), &document); err != nil {
		t.Fatal(err)
	}
	paths := document["paths"].(map[string]any)
	file, err := parser.ParseFile(token.NewFileSet(), "handler.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) < 1 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "HandleFunc" {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			return true
		}
		pattern, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.SplitN(pattern, " ", 2)
		if len(parts) != 2 {
			return true
		}
		path := strings.ReplaceAll(parts[1], "{path...}", "{path}")
		item, ok := paths[path].(map[string]any)
		if !ok {
			t.Errorf("undocumented path: %s", pattern)
			return true
		}
		if _, ok := item[strings.ToLower(parts[0])]; !ok {
			t.Errorf("undocumented method: %s", pattern)
		}
		return true
	})
	var walk func(any)
	walk = func(value any) {
		switch item := value.(type) {
		case map[string]any:
			if reference, ok := item["$ref"].(string); ok {
				var current any = document
				for _, part := range strings.Split(strings.TrimPrefix(reference, "#/"), "/") {
					object, ok := current.(map[string]any)
					if !ok {
						t.Fatalf("invalid reference: %s", reference)
					}
					current, ok = object[part]
					if !ok {
						t.Fatalf("missing reference: %s", reference)
					}
				}
			}
			if props, ok := item["properties"].(map[string]any); ok {
				for name, property := range props {
					if _, ok := property.(map[string]any); !ok {
						t.Errorf("property %s is not a schema", name)
					}
				}
				if required, ok := item["required"].([]any); ok {
					for _, name := range required {
						if _, ok := props[name.(string)]; !ok {
							t.Errorf("required property %s missing", name)
						}
					}
				}
			}
			for _, child := range item {
				walk(child)
			}
		case []any:
			for _, child := range item {
				walk(child)
			}
		}
	}
	walk(document)
}
