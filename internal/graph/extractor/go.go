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
	symbols = append(symbols, goDeclSymbols(fset, file)...)
	symbols = append(symbols, goRationaleComments(fset, file)...)
	return symbols, goImportPathsAST(file), nil
}

func goImportPathsAST(file *ast.File) []string {
	var imports []string
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		if path := strings.Trim(imp.Path.Value, "\"`"); path != "" {
			imports = append(imports, path)
		}
	}
	return imports
}

func goDeclSymbols(fset *token.FileSet, file *ast.File) []Symbol {
	var symbols []Symbol
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if sym, ok := goFuncSymbol(fset, d); ok {
				symbols = append(symbols, sym)
			}
		case *ast.GenDecl:
			symbols = append(symbols, goTypeSymbols(fset, d)...)
		}
	}
	return symbols
}

func goFuncSymbol(fset *token.FileSet, d *ast.FuncDecl) (Symbol, bool) {
	if d.Name == nil || d.Name.Name == "" || d.Name.Name == "init" || d.Name.Name == "main" {
		return Symbol{}, false
	}
	return Symbol{
		Name:     d.Name.Name,
		Type:     schema.NodeTypeFunction,
		Line:     fset.Position(d.Pos()).Line,
		Exported: d.Name.IsExported(),
	}, true
}

func goTypeSymbols(fset *token.FileSet, d *ast.GenDecl) []Symbol {
	if d.Tok != token.TYPE {
		return nil
	}
	var symbols []Symbol
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || ts.Name == nil {
			continue
		}
		switch ts.Type.(type) {
		case *ast.StructType, *ast.InterfaceType:
			symbols = append(symbols, Symbol{
				Name:     ts.Name.Name,
				Type:     schema.NodeTypeClass,
				Line:     fset.Position(ts.Pos()).Line,
				Exported: ts.Name.IsExported(),
			})
		}
	}
	return symbols
}

func goRationaleComments(fset *token.FileSet, file *ast.File) []Symbol {
	var symbols []Symbol
	for _, cg := range file.Comments {
		for _, comment := range cg.List {
			if text, ok := rationaleText(comment.Text); ok {
				symbols = append(symbols, Symbol{
					Name:     text,
					Type:     schema.NodeTypeRationale,
					Line:     fset.Position(comment.Pos()).Line,
					Exported: false,
				})
			}
		}
	}
	return symbols
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
