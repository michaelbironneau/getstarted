package getstarted

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-enry/go-enry/v2"
	sitter "github.com/smacker/go-tree-sitter"
)

// Caller represents a call site that references a symbol.
type Caller struct {
	FunctionName string // enclosing function name, or "<top-level>"
	FilePath     string // absolute path
	Line         int
}

// FindCallers searches dir for call expressions referencing symbolName.
// If graph is non-nil, it is used to narrow the search to likely importers of targetFile.
func FindCallers(dir, targetFile, symbolName string, graph *ImportGraph) ([]Caller, error) {
	var candidates []string

	if graph != nil {
		importers := graph.Importers(targetFile)
		candidates = append(candidates, importers...)
		// Also include the target file itself for self-referential calls
		candidates = append(candidates, targetFile)
	} else {
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
			rel, _ := filepath.Rel(dir, path)
			candidates = append(candidates, "./"+filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	seen := make(map[string]bool)
	var callers []Caller

	for _, relPath := range candidates {
		absPath := filepath.Join(dir, strings.TrimPrefix(relPath, "./"))
		fileCalls, err := findCallsInFile(absPath, symbolName)
		if err != nil {
			continue
		}
		for _, call := range fileCalls {
			key := fmt.Sprintf("%s\x00%s\x00%d", call.FilePath, call.FunctionName, call.Line)
			if !seen[key] {
				seen[key] = true
				callers = append(callers, call)
			}
		}
	}
	return callers, nil
}

// FindSymbolsUsedFrom returns symbols defined in fileB that are called in fileA.
// Matching is by name — imprecise without a type system but good enough for orientation.
func FindSymbolsUsedFrom(fileA, fileB string) ([]string, error) {
	symbolsB, err := ExtractFileSymbols(fileB)
	if err != nil {
		return nil, err
	}
	if len(symbolsB) == 0 {
		return nil, nil
	}
	nameSet := make(map[string]bool)
	for _, s := range symbolsB {
		nameSet[s.Name] = true
	}

	content, err := os.ReadFile(fileA)
	if err != nil {
		return nil, err
	}
	lang := enry.GetLanguage(filepath.Base(fileA), content)
	tsLang := treeSitterLang(lang)
	if tsLang == nil {
		return nil, nil
	}
	query := callQuery(lang)
	if query == "" {
		return nil, nil
	}

	parser := sitter.NewParser()
	parser.SetLanguage(tsLang)
	tree, _ := parser.ParseCtx(context.Background(), nil, content)
	if tree == nil {
		return nil, nil
	}

	q, err := sitter.NewQuery([]byte(query), tsLang)
	if err != nil {
		return nil, nil
	}

	cursor := sitter.NewQueryCursor()
	cursor.Exec(q, tree.RootNode())

	seen := make(map[string]bool)
	var used []string
	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}
		for _, cap := range match.Captures {
			name := cap.Node.Content(content)
			if nameSet[name] && !seen[name] {
				seen[name] = true
				used = append(used, name)
			}
		}
	}
	return used, nil
}

func findCallsInFile(filePath, symbolName string) ([]Caller, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	lang := enry.GetLanguage(filepath.Base(filePath), content)
	tsLang := treeSitterLang(lang)
	if tsLang == nil {
		return nil, nil
	}
	query := callQuery(lang)
	if query == "" {
		return nil, nil
	}

	parser := sitter.NewParser()
	parser.SetLanguage(tsLang)
	tree, _ := parser.ParseCtx(context.Background(), nil, content)
	if tree == nil {
		return nil, nil
	}

	q, err := sitter.NewQuery([]byte(query), tsLang)
	if err != nil {
		return nil, nil
	}

	cursor := sitter.NewQueryCursor()
	cursor.Exec(q, tree.RootNode())

	var callers []Caller
	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}
		for _, cap := range match.Captures {
			if cap.Node.Content(content) != symbolName {
				continue
			}
			line := int(cap.Node.StartPoint().Row) + 1
			enclosing := enclosingFunction(cap.Node, content)
			callers = append(callers, Caller{
				FunctionName: enclosing,
				FilePath:     filePath,
				Line:         line,
			})
		}
	}
	return callers, nil
}

func callQuery(lang string) string {
	switch lang {
	case "Go":
		return `[
			(call_expression function: (identifier) @name)
			(call_expression function: (selector_expression field: (field_identifier) @name))
		]`
	case "Python":
		return `[
			(call function: (identifier) @name)
			(call function: (attribute attribute: (identifier) @name))
		]`
	case "JavaScript", "JSX", "TypeScript", "TSX":
		return `[
			(call_expression function: (identifier) @name)
			(call_expression function: (member_expression property: (property_identifier) @name))
		]`
	}
	return ""
}

// enclosingFunction walks up the AST from node to find the nearest enclosing
// function/method declaration and returns its name.
func enclosingFunction(node *sitter.Node, src []byte) string {
	for node != nil {
		switch node.Type() {
		case "function_declaration", "method_declaration", "function_definition":
			for i := 0; i < int(node.NamedChildCount()); i++ {
				child := node.NamedChild(i)
				t := child.Type()
				if t == "identifier" || t == "field_identifier" || t == "property_identifier" {
					return child.Content(src)
				}
			}
		}
		node = node.Parent()
	}
	return "<top-level>"
}
