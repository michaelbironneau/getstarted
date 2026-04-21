package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// MaxSymbolsPerFile is the maximum number of symbols that we'll display for each file.
// We'll truncate additional output with an ellipsis.
const MaxSymbolsPerFile = 5

// StopWords is a list of words that will be ignored from symbols.
var SymbolStopWords []string = []string{"__init__"}

func main() {
	dir := flag.String("dir", ".", "directory to analyze")
	filter := flag.String("filter", "", "comma-separated list of sections: install,build,run,test,docs")
	depth := flag.Int("depth", 2, "max depth for docs section (0=root only)")
	flag.Parse()

	absDir, err := filepath.Abs(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error resolving dir: %v\n", err)
		os.Exit(1)
	}

	activeFilters := parseFilter(*filter)

	// 1. Detect languages
	stats, err := DetectLanguages(absDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error detecting languages: %v\n", err)
		os.Exit(1)
	}

	// 2. Find files matching heuristics
	files, err := FindFiles(absDir, stats)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error finding files: %v\n", err)
		os.Exit(1)
	}

	// 3. Build docs
	var docsResult *DocsResult
	if isActive(activeFilters, "docs") {
		docsResult, err = BuildDocs(absDir, *depth)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error building docs: %v\n", err)
			os.Exit(1)
		}
	}

	// 4. Output
	out := &strings.Builder{}

	// Stack
	if isActive(activeFilters, "stack") || len(activeFilters) == 0 {
		printStack(out, stats)
	}

	// Command sections
	sections := []string{"install", "build", "run", "test"}
	for _, cmd := range sections {
		if !isActive(activeFilters, cmd) {
			continue
		}
		paths := files[cmd]

		fmt.Fprintf(out, "## %s\n\n", strings.ToUpper(cmd[:1])+cmd[1:])
		if len(paths) == 0 {
			fmt.Fprintf(out, "No relevant files found.\n\n\n")
			continue
		}
		seen := make(map[string]bool)
		for _, path := range paths {
			content, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			basename := filepath.Base(path)
			parser := FindParser(basename)
			if parser == nil {
				continue
			}
			result, err := parser.Parse(path, content)
			if err != nil {
				continue
			}
			val, ok := result[cmd]
			if !ok || val == "" {
				continue
			}
			rel := displayPath(absDir, path, *dir)
			key := rel + "\x00" + val
			if seen[key] {
				continue
			}
			seen[key] = true
			fmt.Fprintf(out, "### Source: %s\n\n%s\n\n", rel, val)
		}
	}

	// Docs
	if isActive(activeFilters, "docs") && docsResult != nil {
		printDocs(out, docsResult)
	}

	fmt.Print(out.String())
}

func parseFilter(f string) map[string]bool {
	if f == "" {
		return map[string]bool{
			"stack": true, "install": true, "build": true, "run": true, "test": true, "docs": true,
		}
	}
	m := make(map[string]bool)
	for _, part := range strings.Split(f, ",") {
		m[strings.TrimSpace(strings.ToLower(part))] = true
	}
	return m
}

func isActive(filters map[string]bool, section string) bool {
	return filters[section]
}

func printStack(out *strings.Builder, stats []LangStats) {
	if len(stats) == 0 {
		return
	}
	out.WriteString("## Stack\n\nThe repository is composed of:\n")
	for _, s := range stats {
		if s.Percentage < 1.0 {
			continue
		}
		fmt.Fprintf(out, "    * %.0f%% %s\n", s.Percentage, s.Language)
	}
	out.WriteString("\n")
}

func displayPath(absDir, path, dirFlag string) string {
	rel, err := filepath.Rel(absDir, path)
	if err != nil {
		return path
	}
	if dirFlag != "." && dirFlag != "" {
		return filepath.Join(dirFlag, rel)
	}
	return "./" + rel
}

func printDocs(out *strings.Builder, result *DocsResult) {
	out.WriteString("## Docs\n\n")

	if len(result.MarkdownFiles) > 0 {
		out.WriteString("### Markdown files\n")
		for _, f := range result.MarkdownFiles {
			fmt.Fprintf(out, "* %s\n", f)
		}
		out.WriteString("\n")
	}

	if result.Root != nil {
		files := flattenFiles(result.Root)
		if len(files) > 0 {
			out.WriteString("### Code structure\n(The symbols shown below are not an exhaustive list)\n\n")
			maxLen := 0
			for _, f := range files {
				if len(f.Symbols) > 0 && len(f.Path) > maxLen {
					maxLen = len(f.Path)
				}
			}
			for _, f := range files {
				if len(f.Symbols) == 0 {
					fmt.Fprintf(out, "%s\n", f.Path)
				} else {
					pad := strings.Repeat(" ", maxLen-len(f.Path)+4)
					fmt.Fprintf(out, "%s%s%s\n", f.Path, pad, formatSymbols(f.Symbols))
				}
			}
			out.WriteString("\n")
		}
	}
}

func formatSymbols(symbols []string) string {
	var filteredSymbols []string
	for _, symbol := range symbols {
		if !slices.Contains(SymbolStopWords, symbol) {
			filteredSymbols = append(filteredSymbols, symbol)
		}
	}

	if len(filteredSymbols) <= MaxSymbolsPerFile {
		return "[" + strings.Join(filteredSymbols, ", ") + "]"
	}
	hidden := "( + " + strconv.Itoa(len(filteredSymbols)-MaxSymbolsPerFile) + " hidden)"
	return "[" + strings.Join(filteredSymbols[:MaxSymbolsPerFile], ", ") + ", ... " + hidden + "]"
}
