package main

import (
	_ "embed"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)


//go:embed heuristics.yaml
var heuristicsYAML []byte

// heuristics maps language → command → []glob
type heuristics map[string]map[string][]string

var languageAliases = map[string]string{
	"JavaScript":  "nodejs",
	"TypeScript":  "nodejs",
	"Vue":         "nodejs",
	"CoffeeScript": "nodejs",
}

func loadHeuristics() (heuristics, error) {
	var h heuristics
	if err := yaml.Unmarshal(heuristicsYAML, &h); err != nil {
		return nil, err
	}
	return h, nil
}

// FindFiles returns a map of command → deduplicated list of matched file paths.
func FindFiles(dir string, languages []LangStats) (map[string][]string, error) {
	h, err := loadHeuristics()
	if err != nil {
		return nil, err
	}

	result := make(map[string][]string)
	seen := make(map[string]map[string]bool) // command → set of paths

	addFile := func(cmd, path string) {
		if seen[cmd] == nil {
			seen[cmd] = make(map[string]bool)
		}
		if !seen[cmd][path] {
			seen[cmd][path] = true
			result[cmd] = append(result[cmd], path)
		}
	}

	for _, ls := range languages {
		hkey := heuristicsKey(ls.Language)
		cmds, ok := h[hkey]
		if !ok {
			continue
		}
		for cmd, globs := range cmds {
			for _, pattern := range globs {
				full := filepath.Join(dir, pattern)
				matches, err := doublestar.FilepathGlob(full)
				if err != nil {
					continue
				}
				for _, m := range matches {
					addFile(cmd, m)
				}
			}
		}
	}
	return result, nil
}

func heuristicsKey(language string) string {
	if alias, ok := languageAliases[language]; ok {
		return alias
	}
	return strings.ToLower(language)
}
