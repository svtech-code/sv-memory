package extractor

import (
	"testing"
)

func TestExtractCallRefsAST(t *testing.T) {
	refExt := NewTreeSitterExtractor()
	_, ok := interface{}(refExt).(CallRefExtractor)
	if !ok {
		t.Fatal("TreeSitterExtractor should implement CallRefExtractor")
	}

	cases := []struct {
		name      string
		content   string
		ext       string
		wantCalls []string
	}{
		{
			name: "go-falls-back",
			// Go is intentionally routed to the heuristic (upstream parser bug);
			// ExtractCallRefs must signal no AST coverage.
			content: "package x\nfunc a() {}\nfunc b() { a() }\n",
			ext:     ".go",
		},
		{
			name:      "python",
			content:   "def a():\n    pass\ndef b():\n    a()\n",
			ext:       ".py",
			wantCalls: []string{"a"},
		},
		{
			name:      "js",
			content:   "function a() {}\nfunction b() { a(); }\n",
			ext:       ".js",
			wantCalls: []string{"a"},
		},
		{
			name:      "typescript-member-access",
			content:   "class A { m() {} }\nfunction b() { new A().m(); }\n",
			ext:       ".ts",
			wantCalls: []string{"m"},
		},
		{
			name:      "rust",
			content:   "fn a() {}\nfn b() { a(); }\n",
			ext:       ".rs",
			wantCalls: []string{"a"},
		},
		{
			name:      "java",
			content:   "class A { void a() {} void b() { a(); } }\n",
			ext:       ".java",
			wantCalls: []string{"a"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refs, err := refExt.ExtractCallRefs([]byte(tc.content), "test"+tc.ext, tc.ext)
			if tc.ext == ".go" {
				if err == nil {
					t.Fatal("expected ErrNoASTCallRefs for .go")
				}
				return
			}
			if err != nil {
				t.Fatalf("ExtractCallRefs failed: %v", err)
			}
			if len(refs) == 0 && len(tc.wantCalls) > 0 {
				t.Fatalf("expected call refs, got none")
			}
			for _, want := range tc.wantCalls {
				found := false
				for _, r := range refs {
					if r.Callee == want {
						found = true
						if r.Line <= 0 || r.Col <= 0 {
							t.Errorf("call %q must have 1-based line/col, got L%d:%d", want, r.Line, r.Col)
						}
					}
				}
				if !found {
					t.Errorf("expected callee %q in refs, got %+v", want, refs)
				}
			}
		})
	}
}

func TestExtractCallRefsDoesNotCaptureStrings(t *testing.T) {
	refExt := NewTreeSitterExtractor()
	_, ok := interface{}(refExt).(CallRefExtractor)
	if !ok {
		t.Fatal("TreeSitterExtractor should implement CallRefExtractor")
	}

	// A bare identifier inside a string/comment must NOT be reported as a call.
	content := `function helper() {}
const s = "helper";
// helper
`
	refs, err := refExt.ExtractCallRefs([]byte(content), "test.js", ".js")
	if err != nil {
		t.Fatalf("ExtractCallRefs failed: %v", err)
	}
	for _, r := range refs {
		if r.Callee == "helper" {
			t.Errorf("string/comment 'helper' must not be a call ref, got %+v", r)
		}
	}
}
