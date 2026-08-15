package graph

import "testing"

// TestResolveImportSubdirectory guards the forward-slash normalization of
// resolved relative imports. On Windows filepath.Join produces backslash paths,
// which must be canonicalized to forward slashes to match node keys (the
// scanner stores paths via filepath.ToSlash). Without the normalization the
// import edge into a subdirectory is silently dropped on Windows.
func TestResolveImportSubdirectory(t *testing.T) {
	nodes := map[string]*Node{
		"index.js":              {ID: "index.js", Path: "index.js"},
		"components/Button.tsx": {ID: "components/Button.tsx", Path: "components/Button.tsx"},
		"components/Button.jsx": {ID: "components/Button.jsx", Path: "components/Button.jsx"},
		"utils.js":              {ID: "utils.js", Path: "utils.js"},
		"src/lib/deep/index.ts": {ID: "src/lib/deep/index.ts", Path: "src/lib/deep/index.ts"},
	}

	cases := []struct {
		source string
		imp    string
		want   string
		ok     bool
	}{
		{source: "index.js", imp: "./components/Button", want: "components/Button.tsx", ok: true},
		{source: "index.js", imp: "./components/Button.tsx", want: "components/Button.tsx", ok: true},
		{source: "index.js", imp: "./utils", want: "utils.js", ok: true},
		{source: "src/main.ts", imp: "../utils", want: "utils.js", ok: true},
		{source: "src/app.ts", imp: "./lib/deep", want: "src/lib/deep/index.ts", ok: true},
		{source: "index.js", imp: "./does/not/exist", want: "", ok: false},
	}

	for _, c := range cases {
		got, ok := resolveImport("", c.source, c.imp, nodes)
		if ok != c.ok {
			t.Errorf("resolveImport(%q, %q) ok=%v, want %v", c.source, c.imp, ok, c.ok)
			continue
		}
		if ok && got != c.want {
			t.Errorf("resolveImport(%q, %q) = %q, want %q", c.source, c.imp, got, c.want)
		}
	}
}

// TestResolveImportPythonRelative guards Python relative imports: tree-sitter
// emits "from .models import X" as ".models" and "from . import sub" as ".",
// which must resolve to project files / package __init__ instead of being
// dropped (the extension list used to be JS/TS-only).
func TestResolveImportPythonRelative(t *testing.T) {
	nodes := map[string]*Node{
		"src/views.py":        {ID: "src/views.py", Path: "src/views.py"},
		"src/models.py":       {ID: "src/models.py", Path: "src/models.py"},
		"src/pkg/__init__.py": {ID: "src/pkg/__init__.py", Path: "src/pkg/__init__.py"},
		"src/pkg/util.py":     {ID: "src/pkg/util.py", Path: "src/pkg/util.py"},
	}

	cases := []struct {
		source string
		imp    string
		want   string
		ok     bool
	}{
		{source: "src/views.py", imp: ".models", want: "src/models.py", ok: true},
		{source: "src/views.py", imp: ".pkg", want: "src/pkg/__init__.py", ok: true},
		{source: "src/pkg/util.py", imp: ".", want: "src/pkg/__init__.py", ok: true},
		{source: "src/views.py", imp: ".missing", want: "", ok: false},
	}

	for _, c := range cases {
		got, ok := resolveImport("", c.source, c.imp, nodes)
		if ok != c.ok {
			t.Errorf("resolveImport(%q, %q) ok=%v, want %v", c.source, c.imp, ok, c.ok)
			continue
		}
		if ok && got != c.want {
			t.Errorf("resolveImport(%q, %q) = %q, want %q", c.source, c.imp, got, c.want)
		}
	}
}
