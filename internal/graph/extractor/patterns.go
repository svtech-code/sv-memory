package extractor

import "regexp"

// Legacy fallback extraction patterns, grouped by language family.
var (
	jsFuncRegex   = regexp.MustCompile(`(?m)(?:export\s+)?(?:\basync\s+)?\bfunction\s+(\w+)`)
	jsClassRegex  = regexp.MustCompile(`(?m)(?:export\s+)?\bclass\s+(\w+)`)
	jsExportRegex = regexp.MustCompile(`(?m)(?:export\s+(?:default\s+)?(?:function|class|const|let|var)\s+\w+|module\.exports\s*=|exports\.\w+)`)

	pyFuncRegex  = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+(\w+)`)
	pyClassRegex = regexp.MustCompile(`(?m)^\s*class\s+(\w+)`)

	goFuncRegex   = regexp.MustCompile(`(?m)^\s*func\s+(\w+)`)
	goStructRegex = regexp.MustCompile(`(?m)^\s*type\s+(\w+)\s+struct`)

	phpFuncRegex = regexp.MustCompile(`(?m)(?:function\s+(\w+)|class\s+(\w+))`)

	// Import regex patterns
	jsImportRegex      = regexp.MustCompile(`(?m)(?:import\s+(?:[^'"]+\s+from\s+)?['"]([^'"]+)['"])|(?:require\s*\(\s*['"]([^'"]+)['"]\s*\))`)
	pyImportRegex      = regexp.MustCompile(`(?m)(?:import\s+([\w\.,\s]+)|from\s+([\w\.]+)\s+import\s*(?:\(([^)]+)\)|([\w\.,\s]+)))`)
	goImportRegex      = regexp.MustCompile("(?m)(?:import\\s+[\"`]([^\"`]+)[\"`])|(?:import\\s*\\(([\\s\\S]*?)\\))")
	goImportBlockRegex = regexp.MustCompile("(?m)[\"`]([^\"`]+)[\"`]")
	rbImportRegex      = regexp.MustCompile(`(?m)(?:require|require_relative)\s+['"]([^'"]+)['"]`)
	rsImportRegex      = regexp.MustCompile(`(?m)use\s+([\w\d:]+);`)
	javaImportRegex    = regexp.MustCompile(`(?m)import\s+([\w\.]+);`)
	phpImportRegex     = regexp.MustCompile(`(?m)(?:include|require)(?:_once)?\s*\(?\s*['"]([^'"]+)['"]\s*\)?|use\s+([\w\\]+)(?:\s+as\s+\w+)?;`)
	cssImportRegex     = regexp.MustCompile(`(?m)@import\s+(?:url\()?['"]([^'"]+)['"]`)
	htmlImportRegex    = regexp.MustCompile(`(?m)<script\s+[^>]*src=['"]([^'"]+)['"]|<link\s+[^>]*href=['"]([^'"]+)['"]`)
	mdLinkRegex        = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
	mdWikilinkRegex    = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]*)?\]\]`)
	mdHeadingRegex     = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)
	shImportRegex      = regexp.MustCompile(`(?m)^\s*(?:source|\.)\s+['"]?([^'"\s#;]+)['"]?`)
	luaImportRegex     = regexp.MustCompile(`(?m)(?:require|dofile|loadfile)\s*\(?\s*['"]([^'"]+)['"]\s*\)?`)

	// SQL patterns
	sqlCreateTableRegex = regexp.MustCompile(`(?mi)(?:CREATE\s+(?:TEMP(?:ORARY)?\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:(\w+)\.)?(\w+)\s*\()`)
	sqlCreateViewRegex  = regexp.MustCompile(`(?mi)(?:CREATE\s+(?:OR\s+REPLACE\s+)?(?:TEMP(?:ORARY)?\s+)?VIEW\s+(?:(\w+)\.)?(\w+)\s+AS\s+)`)
	sqlAlterFKRegex     = regexp.MustCompile(`(?mi)(?:ALTER\s+TABLE\s+(?:(\w+)\.)?(\w+)\s+ADD\s+(?:CONSTRAINT\s+\w+\s+)?FOREIGN\s+KEY\s*\(([^)]+)\)\s*REFERENCES\s+(?:(\w+)\.)?(\w+)\s*\(([^)]+)\))`)
	sqlCreateIndexRegex = regexp.MustCompile(`(?mi)CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?(?:(\w+)\.)?(\w+)\s+ON\s+(?:ONLY\s+)?(?:(\w+)\.)?(\w+)\s*\(`)
	sqlCreateTypeRegex  = regexp.MustCompile(`(?mi)(?:CREATE\s+TYPE\s+(?:(\w+)\.)?(\w+)\s+AS\s+ENUM\s*\(([^)]+)\))`)
)
