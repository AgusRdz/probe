package scanner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgusRdz/probe/observer"
)

// extractGoStructSchema uses go/ast to parse Go source and extract a Schema
// from a struct type definition by name. Returns nil if not found.
func extractGoStructSchema(src []byte, typeName string) *observer.Schema {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return nil
	}

	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != typeName {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			return structTypeToSchema(st)
		}
	}
	return nil
}

// structTypeToSchema converts an *ast.StructType to a Schema.
func structTypeToSchema(st *ast.StructType) *observer.Schema {
	schema := &observer.Schema{
		Type:       "object",
		Properties: make(map[string]*observer.Schema),
	}

	for _, field := range st.Fields.List {
		// Determine JSON field name from struct tag.
		fieldSchema := normalizeGoType(field.Type)

		// Determine if field is required (non-pointer, no omitempty).
		required := true
		if _, isPtr := field.Type.(*ast.StarExpr); isPtr {
			required = false
			fieldSchema.Nullable = true
		}

		jsonName := ""
		omitempty := false
		if field.Tag != nil {
			tag := strings.Trim(field.Tag.Value, "`")
			jsonName, omitempty = parseJSONTag(tag)
		}
		if omitempty {
			required = false
		}

		for _, name := range field.Names {
			fn := jsonName
			if fn == "" {
				fn = name.Name
			}
			if fn == "-" {
				continue
			}
			s := fieldSchema // copy
			schema.Properties[fn] = &s
			if required {
				schema.Required = append(schema.Required, fn)
			}
		}
		// Embedded field (no names)
		if len(field.Names) == 0 && jsonName != "" && jsonName != "-" {
			s := fieldSchema
			schema.Properties[jsonName] = &s
			if required {
				schema.Required = append(schema.Required, jsonName)
			}
		}
	}

	if len(schema.Properties) == 0 {
		return schema
	}
	return schema
}

// parseJSONTag extracts the JSON field name and omitempty flag from a struct tag string.
func parseJSONTag(tag string) (name string, omitempty bool) {
	// tag looks like: json:"name,omitempty" validate:"required"
	start := strings.Index(tag, `json:"`)
	if start == -1 {
		return "", false
	}
	start += 6
	end := strings.Index(tag[start:], `"`)
	if end == -1 {
		return "", false
	}
	parts := strings.Split(tag[start:start+end], ",")
	name = parts[0]
	for _, p := range parts[1:] {
		if p == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty
}

// normalizeGoType converts a Go AST type expression to an observer.Schema.
func normalizeGoType(expr ast.Expr) observer.Schema {
	switch t := expr.(type) {
	case *ast.Ident:
		return goIdentToSchema(t.Name)
	case *ast.StarExpr:
		s := normalizeGoType(t.X)
		s.Nullable = true
		return s
	case *ast.ArrayType:
		items := normalizeGoType(t.Elt)
		return observer.Schema{Type: "array", Items: &items}
	case *ast.MapType:
		return observer.Schema{Type: "object"}
	case *ast.SelectorExpr:
		// e.g. time.Time, uuid.UUID
		switch t.Sel.Name {
		case "Time":
			return observer.Schema{Type: "string", Format: "date-time"}
		case "UUID":
			return observer.Schema{Type: "string", Format: "uuid"}
		}
		return observer.Schema{Type: "string"}
	case *ast.InterfaceType:
		return observer.Schema{Type: "object"}
	}
	return observer.Schema{Type: "string"}
}

// goIdentToSchema converts a Go built-in type name to an observer.Schema.
func goIdentToSchema(name string) observer.Schema {
	switch name {
	case "string":
		return observer.Schema{Type: "string"}
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return observer.Schema{Type: "integer"}
	case "float32", "float64":
		return observer.Schema{Type: "number"}
	case "bool":
		return observer.Schema{Type: "boolean"}
	case "byte":
		return observer.Schema{Type: "integer"}
	default:
		// Unknown type — treat as nested object reference.
		return observer.Schema{Type: "object"}
	}
}

// walkGoFiles walks dir recursively, calling fn for every .go file.
// Skips vendor, testdata, and hidden directories.
func walkGoFiles(dir string, fn func(path string) error) error {
	return walkWithSkip(dir, map[string]bool{
		"vendor":   true,
		"testdata": true,
		".git":     true,
	}, ".go", fn)
}

// walkWithSkip is a generic recursive file walker that calls fn for files
// matching the given extension, skipping directories in the skip set and
// hidden directories (prefixed with ".").
func walkWithSkip(dir string, skip map[string]bool, ext string, fn func(string) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // directory unreadable — skip silently
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if skip[name] || strings.HasPrefix(name, ".") {
				continue
			}
			if err := walkWithSkip(filepath.Join(dir, name), skip, ext, fn); err != nil {
				return err
			}
			continue
		}
		if strings.HasSuffix(name, ext) {
			if err := fn(filepath.Join(dir, name)); err != nil {
				return err
			}
		}
	}
	return nil
}
