package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	getstarted "github.com/michaelbironneau/getstarted/pkg"
)

// MaxSymbolsPerFile is the maximum number of symbols that we'll display for each file.
// We'll truncate additional output with an ellipsis.
const MaxSymbolsPerFile = 5

// SymbolStopWords is a list of words that will be ignored from symbols.
var SymbolStopWords []string = []string{"__init__"}

func main() {
	rearrangeArgs()

	filter := flag.String("filter", "", "comma-separated list of sections: install,build,run,test,docs")
	depth := flag.Int("depth", 2, "max depth for docs section (0=root only)")
	flag.Parse()

	dir := "."
	if args := flag.Args(); len(args) > 0 {
		dir = args[0]
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error resolving dir: %v\n", err)
		os.Exit(1)
	}

	activeFilters := parseFilter(*filter)

	// 1. Detect languages
	stats, err := getstarted.DetectLanguages(absDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error detecting languages: %v\n", err)
		os.Exit(1)
	}

	// 2. Find files matching heuristics
	files, err := getstarted.FindFiles(absDir, stats)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error finding files: %v\n", err)
		os.Exit(1)
	}

	// 3. Build docs
	var docsResult *getstarted.DocsResult
	if isActive(activeFilters, "docs") {
		docsResult, err = getstarted.BuildDocs(absDir, *depth)
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
			parser := getstarted.FindParser(basename)
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
			rel := displayPath(absDir, path, dir)
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
		printDocs(out, docsResult, dir)
	}

	fmt.Print(out.String())
}

// rearrangeArgs moves all flag arguments before positional arguments in os.Args.
// This allows flags to appear after the positional directory argument, e.g.:
//   getstarted subdir --filter=docs
// The standard flag package stops parsing at the first non-flag argument, so
// without rearrangement, flags after the positional arg would be ignored.
func rearrangeArgs() {
	var flagArgs []string
	var posArgs []string
	for i := 1; i < len(os.Args); i++ {
		if strings.HasPrefix(os.Args[i], "-") {
			flagArgs = append(flagArgs, os.Args[i])
			// If the flag doesn't contain =, the next arg may be its value
			if !strings.Contains(os.Args[i], "=") && i+1 < len(os.Args) && !strings.HasPrefix(os.Args[i+1], "-") {
				i++
				flagArgs = append(flagArgs, os.Args[i])
			}
		} else {
			posArgs = append(posArgs, os.Args[i])
		}
	}
	os.Args = append([]string{os.Args[0]}, append(flagArgs, posArgs...)...)
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

func printStack(out *strings.Builder, stats []getstarted.LangStats) {
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

// prefixPath adjusts a path that is relative to the analyzed subdirectory
// so it becomes relative to the original working directory instead.
func prefixPath(path, dir string) string {
	if dir == "." || dir == "" {
		return path
	}
	path = strings.TrimPrefix(path, "./")
	return filepath.Join(dir, path)
}

func printDocs(out *strings.Builder, result *getstarted.DocsResult, dir string) {
	out.WriteString("## Docs\n\n")

	if len(result.MarkdownFiles) > 0 {
		out.WriteString("### Markdown files\n")
		for _, f := range result.MarkdownFiles {
			fmt.Fprintf(out, "* %s\n", prefixPath(f, dir))
		}
		out.WriteString("\n")
	}

	if result.Root != nil {
		files := getstarted.FlattenFiles(result.Root)
		if len(files) > 0 {
			out.WriteString("### Code structure\n(The symbols shown below are not an exhaustive list)\n\n")
			maxLen := 0
			for _, f := range files {
				displayP := prefixPath(f.Path, dir)
				if len(f.Symbols) > 0 && len(displayP) > maxLen {
					maxLen = len(displayP)
				}
			}
			for _, f := range files {
				displayP := prefixPath(f.Path, dir)
				if len(f.Symbols) == 0 {
					fmt.Fprintf(out, "%s\n", displayP)
				} else {
					pad := strings.Repeat(" ", maxLen-len(displayP)+4)
					fmt.Fprintf(out, "%s%s%s\n", displayP, pad, formatSymbols(f.Symbols))
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
