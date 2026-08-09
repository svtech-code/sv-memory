package extractor

import (
	"strings"
	"testing"
)

func TestRegexExtractorMarkdownAndSQL(t *testing.T) {
	ext := NewRegexExtractor()
	markdown := []byte("# Overview\n\n[Guide](guide.md) [[Architecture]]\n\n```go\nfmt.Println(1)\n```\n\n```mermaid\ngraph TD\nA-->B\n```\n")
	symbols, imports, err := ext.Extract(markdown, "README.md", ".md")
	if err != nil {
		t.Fatalf("markdown extraction error = %v", err)
	}
	if len(symbols) != 3 {
		t.Fatalf("markdown symbols = %d, want 3", len(symbols))
	}
	if len(imports) != 2 || imports[0] != "guide.md" || imports[1] != "Architecture" {
		t.Errorf("markdown imports = %v, want guide.md and Architecture", imports)
	}

	sql := []byte(`CREATE TABLE public.users (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL
);
CREATE VIEW active_users AS SELECT id FROM users;
CREATE UNIQUE INDEX users_name_idx ON users (name);
CREATE TYPE user_role AS ENUM ('admin', 'user');
ALTER TABLE users ADD FOREIGN KEY (id) REFERENCES accounts (user_id);`)
	symbols, imports, err = ext.Extract(sql, "schema.sql", ".sql")
	if err != nil {
		t.Fatalf("SQL extraction error = %v", err)
	}
	for _, name := range []string{"users", "active_users", "users_name_idx", "user_role"} {
		if !hasSymbol(symbols, name) {
			t.Errorf("SQL symbol %q not found in %#v", name, symbols)
		}
	}
	if len(imports) != 1 || !strings.Contains(imports[0], "accounts") {
		t.Errorf("SQL imports = %v, want accounts reference", imports)
	}
}

func TestRegexExtractorExportsAndUnsupportedExtension(t *testing.T) {
	ext := NewRegexExtractor()
	symbols, imports, err := ext.Extract([]byte("export const value = 1;\nmodule.exports = value;"), "app.js", ".js")
	if err != nil || len(symbols) != 0 || len(imports) != 0 {
		t.Fatalf("unexpected JS extraction: symbols=%v imports=%v err=%v", symbols, imports, err)
	}
	if got := ext.GetExportsCount([]byte("export const value = 1;\nmodule.exports = value;"), ".js"); got != 2 {
		t.Errorf("GetExportsCount() = %d, want 2", got)
	}
	if got := ext.GetExportsCount([]byte("export const value = 1;"), ".md"); got != 0 {
		t.Errorf("GetExportsCount(.md) = %d, want 0", got)
	}
	if _, _, err := ext.Extract([]byte("anything"), "data.txt", ".txt"); err != nil {
		t.Fatalf("unsupported extension returned error: %v", err)
	}
}

func TestMDSemanticExtractor(t *testing.T) {
	ext := NewRegexExtractor()
	src := []byte("# Title\n\n| Name | Value |\n| --- | --- |\n| a | b |\n\n> NOTE: keep this\n\n```mermaid\ngraph TD\nA-->B\n```\n")
	symbols, imports, err := ext.Extract(src, "docs.md", ".md")
	if err != nil {
		t.Fatalf("semantic extraction error = %v", err)
	}
	if len(imports) != 0 {
		t.Errorf("semantic imports = %v, want none", imports)
	}
	if !hasSymbolType(symbols, "Title", "section") || !hasSymbolType(symbols, "mermaid", "diagram") {
		t.Errorf("semantic symbols missing expected entities: %#v", symbols)
	}
}

func TestTreeSitterLanguageResolutionAndFallbacks(t *testing.T) {
	ext := NewTreeSitterExtractor()
	for _, languageExt := range []string{".py", ".js", ".ts", ".rs", ".java", ".rb", ".php", ".html", ".css", ".sh"} {
		if ext.GetLanguage(languageExt) == nil {
			t.Errorf("GetLanguage(%q) returned nil", languageExt)
		}
	}
	if ext.GetLanguage(".unknown") != nil {
		t.Error("GetLanguage(.unknown) returned a language")
	}

	cases := []struct {
		name string
		ext  string
		src  string
	}{
		{name: "php", ext: ".php", src: "<?php function greet() {} class User {}"},
		{name: "rust", ext: ".rs", src: "use std::io; fn main() {}"},
		{name: "ruby", ext: ".rb", src: "require 'json'\ndef run\nend"},
		{name: "java", ext: ".java", src: "import java.util.List; class App {}"},
		{name: "unknown fallback", ext: ".txt", src: "plain text"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := ext.Extract([]byte(test.src), "sample"+test.ext, test.ext); err != nil {
				t.Fatalf("Extract(%q) error = %v", test.ext, err)
			}
		})
	}
}

func hasSymbol(symbols []Symbol, name string) bool {
	for _, symbol := range symbols {
		if symbol.Name == name {
			return true
		}
	}
	return false
}

func hasSymbolType(symbols []Symbol, name, symbolType string) bool {
	for _, symbol := range symbols {
		if symbol.Name == name && symbol.Type == symbolType {
			return true
		}
	}
	return false
}
