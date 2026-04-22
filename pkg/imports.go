package getstarted

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-enry/go-enry/v2"
	sitter "github.com/smacker/go-tree-sitter"
)

const importsFile = ".imports"

// Import represents a single import in a file, with the symbols used from it.
type Import struct {
	RawPath string
	Symbols []string // symbols referenced from this import within the file
}

// ImportGraph maps files (relative to root) to their raw import paths.
type ImportGraph struct {
	Files map[string][]string
}

type importGraphFile struct {
	Version int                 `json:"version"`
	Files   map[string][]string `json:"files"`
}

// importEntry is an internal working struct used while extracting imports.
type importEntry struct {
	rawPath   string
	localName string // how this package is referenced in code
	symbols   []string
}

// ExtractImports parses a file and returns its imports with the symbols used from each.
func ExtractImports(filePath string) ([]Import, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	lang := enry.GetLanguage(filepath.Base(filePath), content)
	tsLang := treeSitterLang(lang)
	if tsLang == nil {
		return nil, nil
	}

	parser := sitter.NewParser()
	parser.SetLanguage(tsLang)
	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil || tree == nil {
		return nil, nil
	}

	var entries []*importEntry
	switch lang {
	case "Go":
		entries = extractGoImports(tree.RootNode(), content, tsLang)
	case "Python":
		entries = extractPythonImports(tree.RootNode(), content, tsLang)
	case "JavaScript", "JSX", "TypeScript", "TSX":
		entries = extractJSImports(tree.RootNode(), content, tsLang)
	}

	result := make([]Import, 0, len(entries))
	for _, e := range entries {
		result = append(result, Import{RawPath: e.rawPath, Symbols: e.symbols})
	}
	return result, nil
}

// extractGoImports walks import_spec nodes then scans selector_expressions to
// find which symbols each package contributes.
func extractGoImports(root *sitter.Node, src []byte, lang *sitter.Language) []*importEntry {
	var entries []*importEntry
	localToEntry := make(map[string]*importEntry)

	var walkImports func(*sitter.Node)
	walkImports = func(n *sitter.Node) {
		if n.Type() == "import_spec" {
			e := &importEntry{}
			for i := 0; i < int(n.NamedChildCount()); i++ {
				child := n.NamedChild(i)
				switch child.Type() {
				case "package_identifier":
					e.localName = child.Content(src)
				case "interpreted_string_literal":
					e.rawPath = strings.Trim(child.Content(src), `"`)
				}
			}
			if e.rawPath == "" || e.localName == "_" || e.localName == "." {
				return
			}
			if e.localName == "" {
				e.localName = goLocalName(e.rawPath)
			}
			entries = append(entries, e)
			localToEntry[e.localName] = e
			return
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walkImports(n.Child(i))
		}
	}
	walkImports(root)

	if len(localToEntry) == 0 {
		return entries
	}

	// Captures are returned in source order: package (@pkg) before symbol (@sym).
	// Two patterns: value selector (pkg.Func()) and qualified type (pkg.Type).
	q, err := sitter.NewQuery([]byte(`
		[
		  (selector_expression
		    operand: (identifier) @pkg
		    field: (field_identifier) @sym)
		  (qualified_type
		    package: (package_identifier) @pkg
		    name: (type_identifier) @sym)
		]
	`), lang)
	if err != nil {
		return entries
	}
	cursor := sitter.NewQueryCursor()
	cursor.Exec(q, root)
	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}
		if len(match.Captures) != 2 {
			continue
		}
		pkg := match.Captures[0].Node.Content(src)
		sym := match.Captures[1].Node.Content(src)
		if e, ok := localToEntry[pkg]; ok {
			e.symbols = appendUniq(e.symbols, sym)
		}
	}
	return entries
}

// extractPythonImports handles both `import X` (qualified usage) and
// `from X import a, b` (explicit named imports).
func extractPythonImports(root *sitter.Node, src []byte, lang *sitter.Language) []*importEntry {
	var entries []*importEntry
	localToEntry := make(map[string]*importEntry) // only for `import X` style
	seen := make(map[string]bool)

	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		switch n.Type() {
		case "import_statement":
			for i := 0; i < int(n.NamedChildCount()); i++ {
				child := n.NamedChild(i)
				switch child.Type() {
				case "dotted_name":
					path := child.Content(src)
					if !seen[path] {
						seen[path] = true
						e := &importEntry{rawPath: path, localName: lastDotSegment(path)}
						entries = append(entries, e)
						localToEntry[e.localName] = e
					}
				case "aliased_import":
					// import X as Y
					var path, alias string
					for j := 0; j < int(child.NamedChildCount()); j++ {
						gc := child.NamedChild(j)
						switch gc.Type() {
						case "dotted_name":
							path = gc.Content(src)
						case "identifier":
							alias = gc.Content(src)
						}
					}
					if path != "" && !seen[path] {
						seen[path] = true
						localName := alias
						if localName == "" {
							localName = lastDotSegment(path)
						}
						e := &importEntry{rawPath: path, localName: localName}
						entries = append(entries, e)
						localToEntry[localName] = e
					}
				}
			}
			return

		case "import_from_statement":
			// from X import a, b  OR  from X import (a, b)
			if n.NamedChildCount() == 0 {
				return
			}
			// First named child is always the module (dotted_name or relative_import).
			moduleNode := n.NamedChild(0)
			modulePath := moduleNode.Content(src)
			if seen[modulePath] {
				return
			}
			seen[modulePath] = true

			var namedSymbols []string
			for i := 1; i < int(n.NamedChildCount()); i++ {
				child := n.NamedChild(i)
				switch child.Type() {
				case "dotted_name":
					namedSymbols = append(namedSymbols, child.Content(src))
				case "import_list":
					for j := 0; j < int(child.NamedChildCount()); j++ {
						gc := child.NamedChild(j)
						switch gc.Type() {
						case "dotted_name":
							namedSymbols = append(namedSymbols, gc.Content(src))
						case "aliased_import":
							// from X import a as b — report the module-side name (a)
							if gc.NamedChildCount() > 0 {
								if first := gc.NamedChild(0); first.Type() == "dotted_name" {
									namedSymbols = append(namedSymbols, first.Content(src))
								}
							}
						}
					}
				case "wildcard_import":
					namedSymbols = append(namedSymbols, "*")
				case "aliased_import":
					if child.NamedChildCount() > 0 {
						if first := child.NamedChild(0); first.Type() == "dotted_name" {
							namedSymbols = append(namedSymbols, first.Content(src))
						}
					}
				}
			}

			e := &importEntry{
				rawPath:   modulePath,
				localName: lastDotSegment(modulePath),
				symbols:   namedSymbols,
			}
			entries = append(entries, e)
			// Don't add to localToEntry: named imports don't need attribute scanning.
			return
		}

		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)

	// For `import X` style, scan attribute access to find X.foo() usages.
	if len(localToEntry) == 0 {
		return entries
	}
	// Captures: object (@obj) before attribute (@attr) in source order.
	q, err := sitter.NewQuery([]byte(`
		(attribute
		  object: (identifier) @obj
		  attribute: (identifier) @attr)
	`), lang)
	if err != nil {
		return entries
	}
	cursor := sitter.NewQueryCursor()
	cursor.Exec(q, root)
	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}
		if len(match.Captures) != 2 {
			continue
		}
		obj := match.Captures[0].Node.Content(src)
		attr := match.Captures[1].Node.Content(src)
		if e, ok := localToEntry[obj]; ok {
			e.symbols = appendUniq(e.symbols, attr)
		}
	}
	return entries
}

// extractJSImports handles named imports ({a, b}), default imports, and
// namespace imports (* as X), scanning member_expressions for the latter two.
func extractJSImports(root *sitter.Node, src []byte, lang *sitter.Language) []*importEntry {
	var entries []*importEntry
	localToEntry := make(map[string]*importEntry)
	seen := make(map[string]bool)

	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n.Type() != "import_statement" {
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i))
			}
			return
		}

		var rawPath string
		var importClause *sitter.Node
		for i := 0; i < int(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			switch child.Type() {
			case "string":
				rawPath = strings.Trim(child.Content(src), `"'`+"`")
			case "import_clause":
				importClause = child
			}
		}
		if rawPath == "" || seen[rawPath] {
			return
		}
		seen[rawPath] = true
		e := &importEntry{rawPath: rawPath}
		entries = append(entries, e)

		if importClause == nil {
			return // side-effect import
		}

		for i := 0; i < int(importClause.NamedChildCount()); i++ {
			child := importClause.NamedChild(i)
			switch child.Type() {
			case "identifier":
				// Default import: import X from 'Y'
				e.localName = child.Content(src)
				localToEntry[e.localName] = e

			case "named_imports":
				// import { a, b as c } from 'Y'
				for j := 0; j < int(child.NamedChildCount()); j++ {
					spec := child.NamedChild(j)
					if spec.Type() == "import_specifier" && spec.NamedChildCount() > 0 {
						// First named child is the exported name from the module.
						if nameNode := spec.NamedChild(0); nameNode.Type() == "identifier" {
							e.symbols = append(e.symbols, nameNode.Content(src))
						}
					}
				}

			case "namespace_import":
				// import * as X from 'Y'
				for j := 0; j < int(child.NamedChildCount()); j++ {
					if gc := child.NamedChild(j); gc.Type() == "identifier" {
						e.localName = gc.Content(src)
						localToEntry[e.localName] = e
						break
					}
				}
			}
		}
	}
	walk(root)

	// Scan member_expressions for default/namespace imports (X.foo).
	if len(localToEntry) == 0 {
		return entries
	}
	// Captures: object (@obj) before property (@prop) in source order.
	q, err := sitter.NewQuery([]byte(`
		(member_expression
		  object: (identifier) @obj
		  property: (property_identifier) @prop)
	`), lang)
	if err != nil {
		return entries
	}
	cursor := sitter.NewQueryCursor()
	cursor.Exec(q, root)
	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}
		if len(match.Captures) != 2 {
			continue
		}
		obj := match.Captures[0].Node.Content(src)
		prop := match.Captures[1].Node.Content(src)
		if e, ok := localToEntry[obj]; ok {
			e.symbols = appendUniq(e.symbols, prop)
		}
	}
	return entries
}

// goLocalName returns the local package identifier for a Go import path.
// It strips common version suffixes (/v2, /v3, etc.) and falls back to the
// penultimate segment, which is usually the real package name.
func goLocalName(importPath string) string {
	parts := strings.Split(importPath, "/")
	last := parts[len(parts)-1]
	if len(parts) >= 2 && len(last) >= 2 && last[0] == 'v' {
		allDigits := true
		for _, c := range last[1:] {
			if c < '0' || c > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return parts[len(parts)-2]
		}
	}
	return last
}

// lastDotSegment returns the last component of a dotted path (e.g. "os.path" → "path").
func lastDotSegment(s string) string {
	if idx := strings.LastIndex(s, "."); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

func appendUniq(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}

// BuildImportGraph walks dir and builds the full import graph.
func BuildImportGraph(dir string) (*ImportGraph, error) {
	graph := &ImportGraph{Files: make(map[string][]string)}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		imports, err := ExtractImports(path)
		if err != nil || len(imports) == 0 {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		rel = "./" + filepath.ToSlash(rel)
		var paths []string
		for _, imp := range imports {
			paths = append(paths, imp.RawPath)
		}
		graph.Files[rel] = paths
		return nil
	})
	return graph, err
}

// LoadImportGraph loads a cached .imports file from dir. Returns nil if not found.
func LoadImportGraph(dir string) (*ImportGraph, error) {
	data, err := os.ReadFile(filepath.Join(dir, importsFile))
	if err != nil {
		return nil, nil //nolint — expected when cache doesn't exist
	}
	var gf importGraphFile
	if err := json.Unmarshal(data, &gf); err != nil {
		return nil, err
	}
	return &ImportGraph{Files: gf.Files}, nil
}

// SaveImportGraph writes the import graph to <dir>/.imports as JSON.
func SaveImportGraph(dir string, g *ImportGraph) error {
	gf := importGraphFile{Version: 1, Files: g.Files}
	data, err := json.MarshalIndent(gf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, importsFile), data, 0644)
}

// Importers returns file paths (from the graph) that likely import the given file.
//
// Matching strategy differs by file type:
//   - Go files (.go): package-level matching — the import path need only end with
//     the package directory name (e.g. "/pkg"), because all files in a package
//     share a single import path.
//   - All other files (TS/JS/etc.): file-level matching — the import path must
//     end with "parentDir/base" (e.g. "helpers/mssql"), combining two path
//     components to avoid false positives from shared directory names like
//     "helpers" or "utils" appearing in unrelated import paths.
func (g *ImportGraph) Importers(filePath string) []string {
	clean := filepath.ToSlash(strings.TrimPrefix(filePath, "./"))
	ext := filepath.Ext(clean)
	dir := filepath.Dir(clean)
	base := strings.TrimSuffix(filepath.Base(clean), ext)

	var token string
	if ext == ".go" {
		// Package-level: the directory name is the import identifier.
		if dir == "." {
			token = base
		} else {
			token = "/" + filepath.Base(dir)
		}
	} else {
		// File-level: combine parent directory + base so that a shared directory
		// name alone (e.g. "/helpers") cannot produce a false match.
		if dir == "." {
			token = "/" + base
		} else {
			token = filepath.Base(dir) + "/" + base
		}
	}

	seen := make(map[string]bool)
	var result []string
	for file, imports := range g.Files {
		if seen[file] {
			continue
		}
		for _, imp := range imports {
			if strings.HasSuffix(imp, token) || strings.Contains(imp, "/"+token+"/") {
				seen[file] = true
				result = append(result, file)
				break
			}
		}
	}
	return result
}
