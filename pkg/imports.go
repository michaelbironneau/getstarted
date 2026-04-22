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

// Import represents a single import statement in a file.
type Import struct {
	RawPath string
}

// ImportGraph maps files (relative to root) to their raw import paths.
type ImportGraph struct {
	Files map[string][]string
}

type importGraphFile struct {
	Version int                 `json:"version"`
	Files   map[string][]string `json:"files"`
}

// ExtractImports parses a file and returns its imports.
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
	query := importQuery(lang)
	if query == "" {
		return nil, nil
	}

	parser := sitter.NewParser()
	parser.SetLanguage(tsLang)
	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil || tree == nil {
		return nil, nil
	}

	q, err := sitter.NewQuery([]byte(query), tsLang)
	if err != nil {
		return nil, nil
	}

	cursor := sitter.NewQueryCursor()
	cursor.Exec(q, tree.RootNode())

	seen := make(map[string]bool)
	var imports []Import
	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}
		for _, cap := range match.Captures {
			raw := cap.Node.Content(content)
			raw = strings.Trim(raw, `"'` + "`")
			if raw == "" || seen[raw] {
				continue
			}
			seen[raw] = true
			imports = append(imports, Import{RawPath: raw})
		}
	}
	return imports, nil
}

func importQuery(lang string) string {
	switch lang {
	case "Go":
		return `(import_spec path: (interpreted_string_literal) @path)`
	case "Python":
		return `[
			(import_statement name: (dotted_name) @path)
			(import_from_statement module_name: (dotted_name) @path)
		]`
	case "JavaScript", "JSX", "TypeScript", "TSX":
		return `(import_statement source: (string) @path)`
	}
	return ""
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
// Matching is best-effort: it checks if any import path contains a path component
// from the target file's directory or base name.
func (g *ImportGraph) Importers(filePath string) []string {
	// Normalise: strip leading "./" and get components
	clean := filepath.ToSlash(strings.TrimPrefix(filePath, "./"))
	dir := filepath.Dir(clean)
	base := strings.TrimSuffix(filepath.Base(clean), filepath.Ext(clean))

	// Build match tokens: the directory (last segment) and the file base name
	var tokens []string
	if dir != "." {
		tokens = append(tokens, "/"+filepath.Base(dir))
	}
	tokens = append(tokens, base)

	seen := make(map[string]bool)
	var result []string
	for file, imports := range g.Files {
		if seen[file] {
			continue
		}
		for _, imp := range imports {
			for _, tok := range tokens {
				if strings.HasSuffix(imp, tok) || strings.Contains(imp, tok+"/") {
					seen[file] = true
					result = append(result, file)
					break
				}
			}
			if seen[file] {
				break
			}
		}
	}
	return result
}
