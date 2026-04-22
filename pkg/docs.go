package getstarted

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-enry/go-enry/v2"
	sitter "github.com/smacker/go-tree-sitter"
	sittergo "github.com/smacker/go-tree-sitter/golang"
	sitterjs "github.com/smacker/go-tree-sitter/javascript"
	sitterpy "github.com/smacker/go-tree-sitter/python"
	sitterts "github.com/smacker/go-tree-sitter/typescript/typescript"
)

// DirEntry represents a file or directory in the docs tree.
type DirEntry struct {
	Name     string
	IsDir    bool
	Symbols  []string // extracted symbols (non-dirs only)
	Children []*DirEntry
}

// DocsResult holds the results of the docs walk.
type DocsResult struct {
	MarkdownFiles []string
	Root          *DirEntry
}

// FileEntry is a file path with its extracted symbols.
type FileEntry struct {
	Path    string
	Symbols []string
}

// BuildDocs walks dir up to maxDepth, extracting markdown files and code symbols.
func BuildDocs(dir string, maxDepth int) (*DocsResult, error) {
	result := &DocsResult{}

	// Collect all markdown files (no depth limit)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		if !d.IsDir() && isMarkdown(d.Name()) {
			rel, _ := filepath.Rel(dir, path)
			result.MarkdownFiles = append(result.MarkdownFiles, "./"+rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	result.Root, err = buildDirTree(dir, dir, 0, maxDepth)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func buildDirTree(root, dir string, depth, maxDepth int) (*DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	node := &DirEntry{Name: relPath(root, dir), IsDir: true}

	for _, e := range entries {
		if skipDirs[e.Name()] {
			continue
		}
		if e.IsDir() {
			if depth < maxDepth {
				child, err := buildDirTree(root, filepath.Join(dir, e.Name()), depth+1, maxDepth)
				if err != nil {
					continue
				}
				// Single-child path compression
				for child != nil && len(child.Children) == 1 && child.Children[0].IsDir {
					only := child.Children[0]
					child.Name = child.Name + "/" + filepath.Base(only.Name)
					child.Children = only.Children
				}
				node.Children = append(node.Children, child)
			} else {
				// At maxDepth: just list the dir name, no recursion
				node.Children = append(node.Children, &DirEntry{
					Name:  e.Name() + "/",
					IsDir: true,
				})
			}
		} else {
			fe := &DirEntry{Name: e.Name(), IsDir: false}
			if depth < maxDepth {
				fe.Symbols = extractSymbols(filepath.Join(dir, e.Name()))
			}
			node.Children = append(node.Children, fe)
		}
	}
	return node, nil
}

// FlattenFiles walks the DirEntry tree and returns all files with full relative paths.
// Dotfiles (names starting with ".") are excluded.
func FlattenFiles(entry *DirEntry) []FileEntry {
	var result []FileEntry
	for _, child := range entry.Children {
		if child.IsDir {
			result = append(result, FlattenFiles(child)...)
		} else {
			if strings.HasPrefix(child.Name, ".") {
				continue
			}
			result = append(result, FileEntry{Path: entry.Name + "/" + child.Name, Symbols: child.Symbols})
		}
	}
	return result
}

func relPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	if rel == "." {
		return "."
	}
	return "./" + rel
}

func isMarkdown(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown")
}

func extractSymbols(path string) []string {
	content, err := os.ReadFile(path)
	if err != nil || enry.IsBinary(content) {
		return nil
	}

	lang := enry.GetLanguage(filepath.Base(path), content)
	tsLang := treeSitterLang(lang)
	if tsLang == nil {
		return nil
	}

	parser := sitter.NewParser()
	parser.SetLanguage(tsLang)
	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil || tree == nil {
		return nil
	}

	query := symbolQuery(lang)
	if query == "" {
		return nil
	}

	q, err := sitter.NewQuery([]byte(query), tsLang)
	if err != nil {
		return nil
	}

	cursor := sitter.NewQueryCursor()
	cursor.Exec(q, tree.RootNode())

	seen := make(map[string]bool)
	var symbols []string
	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}
		for _, cap := range match.Captures {
			name := cap.Node.Content(content)
			if name != "" && !seen[name] {
				seen[name] = true
				symbols = append(symbols, name)
			}
		}
	}
	return symbols
}

func treeSitterLang(lang string) *sitter.Language {
	switch lang {
	case "Go":
		return sittergo.GetLanguage()
	case "Python":
		return sitterpy.GetLanguage()
	case "JavaScript", "JSX":
		return sitterjs.GetLanguage()
	case "TypeScript", "TSX":
		return sitterts.GetLanguage()
	}
	return nil
}

// symbolQuery returns a tree-sitter query that matches only top-level / module-level
// declarations: exported functions, classes, types, and top-level const/let/var.
// Local variables, loop counters, and nested function definitions are excluded.
func symbolQuery(lang string) string {
	switch lang {
	case "Go":
		// Go function/method declarations can only appear at the top level.
		// Type declarations are anchored to source_file to exclude rare
		// types declared inside function bodies.
		return `[
			(function_declaration name: (identifier) @name)
			(method_declaration name: (field_identifier) @name)
			(source_file (type_declaration (type_spec name: (type_identifier) @name)))
		]`
	case "Python":
		// Anchor to module root; also match decorated definitions.
		return `[
			(module (function_definition name: (identifier) @name))
			(module (class_definition name: (identifier) @name))
			(module (decorated_definition (function_definition name: (identifier) @name)))
			(module (decorated_definition (class_definition name: (identifier) @name)))
		]`
	case "JavaScript", "JSX":
		// Anchor lexical_declaration to program (top-level) or export_statement
		// (which is always top-level) to exclude local variables and loop counters.
		return `[
			(program (function_declaration name: (identifier) @name))
			(program (class_declaration name: (identifier) @name))
			(program (lexical_declaration (variable_declarator name: (identifier) @name)))
			(export_statement (function_declaration name: (identifier) @name))
			(export_statement (class_declaration name: (identifier) @name))
			(export_statement (lexical_declaration (variable_declarator name: (identifier) @name)))
		]`
	case "TypeScript", "TSX":
		return `[
			(program (function_declaration name: (identifier) @name))
			(program (class_declaration name: (type_identifier) @name))
			(program (lexical_declaration (variable_declarator name: (identifier) @name)))
			(program (interface_declaration name: (type_identifier) @name))
			(program (type_alias_declaration name: (type_identifier) @name))
			(export_statement (function_declaration name: (identifier) @name))
			(export_statement (class_declaration name: (type_identifier) @name))
			(export_statement (lexical_declaration (variable_declarator name: (identifier) @name)))
			(export_statement (interface_declaration name: (type_identifier) @name))
			(export_statement (type_alias_declaration name: (type_identifier) @name))
		]`
	}
	return ""
}

// fullSymbolQuery returns a tree-sitter query that matches all symbol declarations
// in a file, including local variables, loop counters, and nested functions.
// Used when --include-local-vars is set, or when searching for a specific named symbol.
func fullSymbolQuery(lang string) string {
	switch lang {
	case "Go":
		return `[
			(function_declaration name: (identifier) @name)
			(method_declaration name: (field_identifier) @name)
			(type_spec name: (type_identifier) @name)
		]`
	case "Python":
		return `[
			(function_definition name: (identifier) @name)
			(class_definition name: (identifier) @name)
		]`
	case "JavaScript", "JSX":
		return `[
			(function_declaration name: (identifier) @name)
			(class_declaration name: (identifier) @name)
			(lexical_declaration (variable_declarator name: (identifier) @name))
		]`
	case "TypeScript", "TSX":
		return `[
			(function_declaration name: (identifier) @name)
			(class_declaration name: (type_identifier) @name)
			(lexical_declaration (variable_declarator name: (identifier) @name))
			(interface_declaration name: (type_identifier) @name)
			(type_alias_declaration name: (type_identifier) @name)
		]`
	}
	return ""
}
