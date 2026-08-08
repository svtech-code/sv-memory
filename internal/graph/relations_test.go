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
