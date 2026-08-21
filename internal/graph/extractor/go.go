package extractor

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph/schema"
)

// parseGoSource uses standard library go/parser and go/ast to extract symbols and imports.
func parseGoSource(content []byte, filename string) ([]Symbol, []string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, content, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}

	var symbols []Symbol
	var imports []string

	// 1. Imports
	for _, imp := range file.Imports {
		if imp.Path != nil {
			path := strings.Trim(imp.Path.Value, `"`+"`")
			if path != "" {
				imports = append(imports, path)
			}
		}
	}

	// 2. Declarations (functions, methods, types)
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name != nil && d.Name.Name != "" && d.Name.Name != "init" && d.Name.Name != "main" {
				pos := fset.Position(d.Pos())
				symbols = append(symbols, Symbol{
					Name:     d.Name.Name,
					Type:     schema.NodeTypeFunction,
					Line:     pos.Line,
					Exported: d.Name.IsExported(),
				})
			}
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name != nil {
						switch ts.Type.(type) {
						case *ast.StructType, *ast.InterfaceType:
							pos := fset.Position(ts.Pos())
							symbols = append(symbols, Symbol{
								Name:     ts.Name.Name,
								Type:     schema.NodeTypeClass,
								Line:     pos.Line,
								Exported: ts.Name.IsExported(),
							})
						}
					}
				}
			}
		}
	}

	// 3. Rationale comments (NOTE:, WHY:, HACK:, TODO:, FIXME:)
	for _, cg := range file.Comments {
		for _, comment := range cg.List {
			text := comment.Text
			cleanText := strings.TrimLeft(text, "/#* \t")
			cleanText = strings.TrimRight(cleanText, "*/ \t\r\n")

			upperClean := strings.ToUpper(cleanText)
			prefixes := []string{"NOTE:", "WHY:", "HACK:", "TODO:", "FIXME:"}
			hasPrefix := false
			for _, p := range prefixes {
				if strings.HasPrefix(upperClean, p) {
					hasPrefix = true
					break
				}
			}

			if hasPrefix {
				pos := fset.Position(comment.Pos())
				symbols = append(symbols, Symbol{
					Name:     cleanText,
					Type:     schema.NodeTypeRationale,
					Line:     pos.Line,
					Exported: false,
				})
			}
		}
	}

	return symbols, imports, nil
}

// extractGoCallRefsAST extracts call sites from Go source using the standard go/parser.
func extractGoCallRefsAST(content []byte, filename string) ([]CallRef, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, content, 0)
	if err != nil {
		return nil, err
	}

	var refs []CallRef
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		callee := goCalleeName(call.Fun)
		if callee != "" {
			pos := fset.Position(call.Pos())
			refs = append(refs, CallRef{
				Callee: callee,
				Line:   pos.Line,
				Col:    pos.Column,
			})
		}
		return true
	})

	return refs, nil
}

func goCalleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		if f.Sel != nil {
			return f.Sel.Name
		}
	case *ast.ParenExpr:
		return goCalleeName(f.X)
	}
	return ""
}
