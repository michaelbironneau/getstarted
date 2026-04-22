package getstarted

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-enry/go-enry/v2"
	sitter "github.com/smacker/go-tree-sitter"
)

// SymbolResult holds information about a specific named symbol.
type SymbolResult struct {
	Name      string
	FilePath  string
	StartLine int
	EndLine   int
	Source    string
	Docstring string
}

// ExtractSymbol finds a named symbol in filePath and returns its full source.
func ExtractSymbol(filePath, symbolName string) (*SymbolResult, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	lang := enry.GetLanguage(filepath.Base(filePath), content)
	tsLang := treeSitterLang(lang)
	if tsLang == nil {
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}
	// Use fullSymbolQuery so an explicitly named symbol is found regardless of nesting.
	query := fullSymbolQuery(lang)
	if query == "" {
		return nil, fmt.Errorf("no symbol query for language: %s", lang)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(tsLang)
	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil || tree == nil {
		return nil, fmt.Errorf("parse error for %s", filePath)
	}

	q, err := sitter.NewQuery([]byte(query), tsLang)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	cursor := sitter.NewQueryCursor()
	cursor.Exec(q, tree.RootNode())

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}
		for _, cap := range match.Captures {
			if cap.Node.Content(content) != symbolName {
				continue
			}
			declNode := declarationNode(cap.Node)
			return &SymbolResult{
				Name:      symbolName,
				FilePath:  filePath,
				StartLine: int(declNode.StartPoint().Row) + 1,
				EndLine:   int(declNode.EndPoint().Row) + 1,
				Source:    declNode.Content(content),
				Docstring: extractDocstring(lang, declNode, content),
			}, nil
		}
	}
	return nil, fmt.Errorf("symbol %q not found in %s", symbolName, filePath)
}

// ExtractFileSymbols returns all symbols in a file, each with their source and docstring.
// When includeLocalVars is false (the default), only top-level / module-level declarations
// are returned. Set includeLocalVars to true to also include local variables, loop
// counters, and nested function definitions.
func ExtractFileSymbols(filePath string, includeLocalVars bool) ([]SymbolResult, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	lang := enry.GetLanguage(filepath.Base(filePath), content)
	tsLang := treeSitterLang(lang)
	if tsLang == nil {
		return nil, nil
	}
	qfn := symbolQuery
	if includeLocalVars {
		qfn = fullSymbolQuery
	}
	query := qfn(lang)
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
	var results []SymbolResult
	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}
		for _, cap := range match.Captures {
			name := cap.Node.Content(content)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			declNode := declarationNode(cap.Node)
			results = append(results, SymbolResult{
				Name:      name,
				FilePath:  filePath,
				StartLine: int(declNode.StartPoint().Row) + 1,
				EndLine:   int(declNode.EndPoint().Row) + 1,
				Source:    declNode.Content(content),
				Docstring: extractDocstring(lang, declNode, content),
			})
		}
	}
	return results, nil
}

// declarationNode walks up from a name node to its full enclosing declaration.
func declarationNode(nameNode *sitter.Node) *sitter.Node {
	parent := nameNode.Parent()
	if parent == nil {
		return nameNode
	}
	// type_spec and variable_declarator are intermediate wrappers; go one level higher.
	switch parent.Type() {
	case "type_spec", "variable_declarator":
		if gp := parent.Parent(); gp != nil {
			return gp
		}
	}
	return parent
}

// extractDocstring returns comments or docstring text for a declaration.
func extractDocstring(lang string, declNode *sitter.Node, src []byte) string {
	if lang == "Python" {
		return pythonDocstring(declNode, src)
	}
	return precedingComments(declNode, src)
}

func precedingComments(declNode *sitter.Node, src []byte) string {
	var comments []string
	prev := declNode.PrevNamedSibling()
	for prev != nil && prev.Type() == "comment" {
		comments = append([]string{prev.Content(src)}, comments...)
		prev = prev.PrevNamedSibling()
	}
	return strings.Join(comments, "\n")
}

func pythonDocstring(declNode *sitter.Node, src []byte) string {
	for i := 0; i < int(declNode.NamedChildCount()); i++ {
		child := declNode.NamedChild(i)
		if child.Type() == "block" && child.NamedChildCount() > 0 {
			first := child.NamedChild(0)
			if first.Type() == "expression_statement" && first.NamedChildCount() > 0 {
				inner := first.NamedChild(0)
				if inner.Type() == "string" {
					return inner.Content(src)
				}
			}
		}
	}
	return ""
}
