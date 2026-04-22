package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	getstarted "github.com/michaelbironneau/getstarted/pkg"
)

const maxImportSymbols = 7

func main() {
	lines := flag.Int("lines", 80, "max source lines to display per symbol (0 = unlimited)")
	doInit := flag.Bool("init", false, "build .imports cache in the current directory and exit")
	flag.Parse()

	if *doInit {
		runInit()
		return
	}

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: godeeper [--lines N] [--init] <file>[:<symbol>] ...")
		os.Exit(1)
	}

	if len(args) == 1 {
		target := args[0]
		// Detect symbol mode: last colon that isn't part of a drive letter (e.g. C:\)
		if idx := strings.LastIndex(target, ":"); idx > 1 {
			runSymbolMode(target[:idx], target[idx+1:], *lines)
		} else {
			runFileMode(target)
		}
	} else {
		runMultiFileMode(args)
	}
}

func runInit() {
	cwd, _ := os.Getwd()
	graph, err := getstarted.BuildImportGraph(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error building import graph: %v\n", err)
		os.Exit(1)
	}
	if err := getstarted.SaveImportGraph(cwd, graph); err != nil {
		fmt.Fprintf(os.Stderr, "error saving import graph: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Import graph saved to .imports (%d files indexed)\n", len(graph.Files))
}

func runSymbolMode(filePath, symbolName string, maxLines int) {
	abs, err := filepath.Abs(filePath)
	if err != nil {
		fatalf("error resolving path: %v\n", err)
	}

	result, err := getstarted.ExtractSymbol(abs, symbolName)
	if err != nil {
		fatalf("error: %v\n", err)
	}

	cwd, _ := os.Getwd()
	relPath := toRel(cwd, abs)

	graph := getOrBuildGraph(cwd)

	out := &strings.Builder{}
	fmt.Fprintf(out, "## Symbol: %s\n\n", symbolName)

	src := result.Source
	if maxLines > 0 {
		src = truncateLines(src, maxLines)
	}
	fmt.Fprintf(out, "### Source: %s (lines %d-%d)\n\n```\n%s\n```\n\n",
		relPath, result.StartLine, result.EndLine, src)

	if result.Docstring != "" {
		fmt.Fprintf(out, "### Docstring\n\n%s\n\n", result.Docstring)
	}

	callers, err := getstarted.FindCallers(cwd, relPath, symbolName, graph)
	if err == nil && len(callers) > 0 {
		fmt.Fprintf(out, "### Called by\n\n")
		for _, c := range callers {
			callerRel := toRel(cwd, c.FilePath)
			fmt.Fprintf(out, "- %s (%s:%d)\n", c.FunctionName, callerRel, c.Line)
		}
		out.WriteString("\n")
	}

	if graph != nil {
		if importers := graph.Importers(relPath); len(importers) > 0 {
			out.WriteString("### Imported by\n\n")
			for _, f := range importers {
				fmt.Fprintf(out, "- %s\n", f)
			}
			out.WriteString("\n")
		}
	}

	fmt.Print(out.String())
}

func runFileMode(filePath string) {
	abs, err := filepath.Abs(filePath)
	if err != nil {
		fatalf("error resolving path: %v\n", err)
	}

	cwd, _ := os.Getwd()
	relPath := toRel(cwd, abs)

	out := &strings.Builder{}
	fmt.Fprintf(out, "## File: %s\n\n", relPath)

	symbols, _ := getstarted.ExtractFileSymbols(abs)
	if len(symbols) > 0 {
		out.WriteString("### Symbols\n\n")
		for _, s := range symbols {
			doc := ""
			if s.Docstring != "" {
				doc = " — " + firstLine(s.Docstring)
			}
			fmt.Fprintf(out, "- %s (line %d)%s\n", s.Name, s.StartLine, doc)
		}
		out.WriteString("\n")
	}

	imports, _ := getstarted.ExtractImports(abs)
	if len(imports) > 0 {
		out.WriteString("### Imports\n\n")
		sourceDir := filepath.Dir(abs)
		for _, imp := range imports {
			displayPath := resolveRelativeImport(sourceDir, imp.RawPath, cwd)
			fmt.Fprintf(out, "- %s%s\n", displayPath, formatImportSymbols(imp.Symbols))
		}
		out.WriteString("\n")
	}

	if graph := getOrBuildGraph(cwd); graph != nil {
		if importers := graph.Importers(relPath); len(importers) > 0 {
			out.WriteString("### Imported by\n\n")
			for _, f := range importers {
				fmt.Fprintf(out, "- %s\n", f)
			}
			out.WriteString("\n")
		}
	}

	fmt.Print(out.String())
}

func runMultiFileMode(filePaths []string) {
	cwd, _ := os.Getwd()

	// Resolve all paths
	type fileInfo struct {
		rel     string
		abs     string
		symbols []getstarted.SymbolResult
	}
	files := make([]fileInfo, 0, len(filePaths))
	for _, fp := range filePaths {
		abs, err := filepath.Abs(fp)
		if err != nil {
			continue
		}
		rel := toRel(cwd, abs)
		symbols, _ := getstarted.ExtractFileSymbols(abs)
		files = append(files, fileInfo{rel: rel, abs: abs, symbols: symbols})
	}

	out := &strings.Builder{}

	relPaths := make([]string, len(files))
	for i, f := range files {
		relPaths[i] = f.rel
	}
	fmt.Fprintf(out, "## Interface: %s\n\n", strings.Join(relPaths, " ↔ "))

	for i := range files {
		for j := range files {
			if i >= j {
				continue
			}
			fa, fb := files[i], files[j]

			// What symbols from fb are used in fa?
			usedInA, _ := getstarted.FindSymbolsUsedFrom(fa.abs, fb.abs)
			// What symbols from fa are used in fb?
			usedInB, _ := getstarted.FindSymbolsUsedFrom(fb.abs, fa.abs)

			if len(usedInA) > 0 {
				fmt.Fprintf(out, "### Symbols from %s used in %s\n\n", fb.rel, fa.rel)
				for _, s := range usedInA {
					fmt.Fprintf(out, "- %s\n", s)
				}
				out.WriteString("\n")
			}
			if len(usedInB) > 0 {
				fmt.Fprintf(out, "### Symbols from %s used in %s\n\n", fa.rel, fb.rel)
				for _, s := range usedInB {
					fmt.Fprintf(out, "- %s\n", s)
				}
				out.WriteString("\n")
			}

			if len(usedInA) == 0 && len(usedInB) == 0 {
				// Fall back to listing both symbol sets
				for _, f := range []fileInfo{fa, fb} {
					fmt.Fprintf(out, "### %s\n\n", f.rel)
					if len(f.symbols) == 0 {
						out.WriteString("(no symbols found)\n\n")
						continue
					}
					for _, s := range f.symbols {
						fmt.Fprintf(out, "- %s (line %d)\n", s.Name, s.StartLine)
					}
					out.WriteString("\n")
				}
			}
		}
	}

	fmt.Print(out.String())
}

// getOrBuildGraph loads the .imports cache if present, otherwise builds the
// import graph by scanning the directory. Returns nil only on error.
func getOrBuildGraph(dir string) *getstarted.ImportGraph {
	if g, err := getstarted.LoadImportGraph(dir); err == nil && g != nil {
		return g
	}
	g, _ := getstarted.BuildImportGraph(dir)
	return g
}

func toRel(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return abs
	}
	return "./" + filepath.ToSlash(rel)
}

// jsExtensions is the ordered list of extensions tried when resolving an
// extension-less relative import in JS/TS projects.
var jsExtensions = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}

// resolveRelativeImport rewrites a relative import path (starting with ".")
// so it is expressed relative to cwd rather than the source file's directory.
// It also tries to find the actual file on disk by probing common extensions
// and index files when the bare path doesn't exist.
// Non-relative paths (package names, bare module specifiers) are returned as-is.
func resolveRelativeImport(sourceDir, importPath, cwd string) string {
	if !strings.HasPrefix(importPath, ".") {
		return importPath
	}
	abs := filepath.Join(sourceDir, filepath.FromSlash(importPath))
	abs = resolveExtension(abs)
	rel, err := filepath.Rel(cwd, abs)
	if err != nil {
		return importPath
	}
	return "./" + filepath.ToSlash(rel)
}

// resolveExtension finds the actual file for an extension-less path by trying
// common JS/TS extensions and directory index files. Returns absPath unchanged
// if no match is found.
func resolveExtension(absPath string) string {
	if _, err := os.Stat(absPath); err == nil {
		return absPath // already a real path (e.g. already has extension)
	}
	for _, ext := range jsExtensions {
		if _, err := os.Stat(absPath + ext); err == nil {
			return absPath + ext
		}
	}
	for _, ext := range jsExtensions {
		if _, err := os.Stat(filepath.Join(absPath, "index"+ext)); err == nil {
			return filepath.Join(absPath, "index"+ext)
		}
	}
	return absPath
}

func formatImportSymbols(symbols []string) string {
	if len(symbols) == 0 {
		return ""
	}
	if len(symbols) <= maxImportSymbols {
		return " [" + strings.Join(symbols, ", ") + "]"
	}
	more := len(symbols) - maxImportSymbols
	return " [" + strings.Join(symbols[:maxImportSymbols], ", ") + fmt.Sprintf(", ... (+%d more)", more) + "]"
}

func truncateLines(s string, max int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= max {
		return s
	}
	return strings.Join(lines[:max], "\n") +
		fmt.Sprintf("\n... (%d lines truncated)", len(lines)-max)
}

func firstLine(s string) string {
	s = strings.TrimSpace(strings.Trim(s, `"'`+"`"))
	if idx := strings.Index(s, "\n"); idx >= 0 {
		return s[:idx]
	}
	return s
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}
