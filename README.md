# Getstarted

This repo contains two complementary CLI utilities for LLM codebase orientation:

- **`getstarted`** — breadth-first: where to find tests, how to build, what the stack is
- **`godeeper`** — depth-first: drill into a specific file or symbol to understand its structure, callers, and dependencies

## Getting started

To get started with `getstarted`, just `go install michaelbironneau/getstarted`.

## Command-line interface

Just run `getstarted` and it will print relevant context from what it has found, on how to:

* Install dependencies
* Build (if applicable)
* Run tests (if applicable)
* Run the project or scripts
* Document the project


An optional `--filter` flag allows the user to filter by a specific type of command (install, build, run, test, or document).

An optional `--dir` flag allows the user to restrict the searches to a subdirectory. This may result in some or all of the commands coming back blank (the parents will not be searched), unless the project contains multiple languages or is a monorepo, in which case it may be sensible to run `getstarted` in each subdirectory separately. It can be combined with `--filter=docs` so that you only return docs for a subfolder - sensible to keep context requirements lower. We will look at implementing monorepo/multi-language detection and splitting in the future. 

An optional `--depth` flag allows the user to specify a max depth for the docs context. See Docs further down. Defaults to 2. Depth = 0 means the contents of `--dir`, Depth = 1 means its children, and so on.

---

# Godeeper

`godeeper` is a depth-first companion to `getstarted`. Where `getstarted` gives you a broad map of a codebase, `godeeper` lets you zoom in on a specific symbol or file to understand its source, documentation, callers, and import relationships.

## Usage

```
godeeper [--lines N] [--init] <target> [<target2> ...]
```

| Flag | Default | Description |
|---|---|---|
| `--lines N` | 80 | Truncate symbol source at N lines (0 = no limit) |
| `--init` | — | Build a `.imports` cache in the current directory for faster lookups |

### Modes

**Symbol mode** — `godeeper <file>:<symbol>`

Shows the symbol's full source, docstring, callers, and the blast radius of the file that contains it.

**File mode** — `godeeper <file>`

Lists all symbols in the file with one-line docstrings, what it imports, and what imports it.

**Multi-file mode** — `godeeper <file1> <file2> ...`

Shows the interface between the files: which symbols from each file are used in the other(s).

**Init mode** — `godeeper --init`

Walks the codebase and writes a `.imports` cache to the current directory. Subsequent runs use this cache to narrow caller searches instead of scanning the whole tree — useful for large codebases.

## Sample output

### Symbol mode: `godeeper pkg/docs.go:BuildDocs`

```
## Symbol: BuildDocs

### Source: ./pkg/docs.go (lines 39-65)

​```
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
​```

### Docstring

// BuildDocs walks dir up to maxDepth, extracting markdown files and code symbols.

### Called by

- main (./cmd/getstarted/main.go:53)

### Imported by

- ./cmd/godeeper/main.go
- ./cmd/getstarted/main.go
```

### File mode: `godeeper pkg/detect.go`

```
## File: ./pkg/detect.go

### Symbols

- LangStats (line 27)
- DetectLanguages (line 33) — // DetectLanguages walks dir and returns language percentages sorted descending.
- readSample (line 93)

### Imports

- io/fs
- os
- path/filepath
- sort
- github.com/go-enry/go-enry/v2

### Imported by

- ./cmd/godeeper/main.go
- ./cmd/getstarted/main.go
```

### Multi-file mode: `godeeper pkg/docs.go cmd/getstarted/main.go`

```
## Interface: ./pkg/docs.go ↔ ./cmd/getstarted/main.go

### Symbols from ./pkg/docs.go used in ./cmd/getstarted/main.go

- BuildDocs
- FlattenFiles
```

## Language support

`godeeper` supports Go, Python, JavaScript, and TypeScript via tree-sitter.

---

# Getstarted

## Language Support

Initially, `getstarted` will support:

* Python
* Node.js (Typescript + Javascript)
* Go

## How it works

1. Infer the language(s) used in the codebase using `go-enry/go-enry`.
2. Search for files based on language-specific heuristics. 
3. Use `smacker/go-tree-sitter` to extract salient classes/methods with line numbers if required. 
4. Assemble the relevant context by concatenating all the results under the correct headings.


No LLMs or AI dependencies. `getstarted` output can be piped to an LLM to generate something more readable. Or, it can be searched, indexed, or whatever you like. It's fast and lightweight.

## Heuristics

### Files of interest

A single YAML file in the following format that gets embedded into our Go binary. This allows us to specify heuristics in configuration:

Example:

```yaml
python:
  build: ["setup.py", "pyproject.toml", "Makefile"]
  test: ["pytest.ini", "setup.cfg", "tox.ini", "**/test_*.py", "**/*_test.py"]
  install: ["requirements*.txt", "Pipfile", "pyproject.toml"]
  run: ["Procfile", "manage.py", "app.py", "main.py"]
go:
  test: ["**/*_test.go"]
  build: ["Makefile", "go.mod"]
  ...
```

Based on these glob patterns, we can identify files of interest easily for the next step.

### Parsing out necessary context

Some types of files have more information than others. For example, `pyproject.toml` is likely to contain a good deal of information on how to install, build and run a project, whereas `requirements.txt` is limited to information about dependencies. 

`main.py` Is likely to tell us a bit about the app - is it command-line, etc. 

Without using a human or LLM to fully understand the file, and without extensively parsing it, we limit ourselves to returning interesting sections of the file. For instance, in `package.json`, the `scripts` section is particularly instructive, and many scripts align with keywords like "build", "test", or "install". 

We have a `ConfigParser` interface that takes a relevant file and returns a `map[string]string`. The keys are commands (e.g. `build`, `test`, `run`, `install`, `docs`) and the values are Markdown-formatted context relevant to that. For example, we might map `"install"` to `"Run npm install in this folder."`

The `ConfigParser` interfaces register with a filename regex that they match. For example, we can have one for `package.json`. This allows us to keep this parsing extensible should we decide to extend support. 

Whatever is assembling the context should prepend the `--dir`, if it is specified, and detect duplicate entries so that we're not adding `Run npm install in this folder` five times to the context.

There's no need to go about elaborate duplicate detection or conflict elimination - just get rid of exact duplicates that match on a key and value, and leave conflict elimination to downstream processes.

#### ConfigParser support

Initially we will support:

* Python: setup.py, pyproject.toml, requirements.txt, pytest.ini
* Node.js: package.json
* Golang: go.mod

Makefiles are currently out of scope.

### Docs

These are going to do two things:

1. Look for Markdown files like README.md, add these to context
2. Use tree-sitter to get a list of classes/methods/exports of each code file, add these to context with heading being the filename (full path to file). The way this works is that when the depth relative to `--dir` (or cwd if dir is unspecified) is less than `--depth`, we return classes/method signatures/exports for each filename. When depth is equal to `--depth`, we just return filenames. We don't go any deeper than `--depth`. If the calling process wants to dig deeper, they can call `getstarted` again on a subdirectory or increase `--depth`.


## Output

All the context-gathering thus far occurs under branches like `build`, `test`, `docs` and `install` (based on the results of previous section). Group entries first by command, then by filename:

The context starts with a brief stack summary based on go-entry output.

Here's an example run on the getstarted repo itself.

```
## Stack

The repository is composed of:
    * 100% Go

## Install

### Source: ./go.mod

Module: `github.com/michaelbironneau/getstarted`
Go version: `1.25.5`

Run `go mod download` to install dependencies.

## Build

Run `go build ./...` to build all packages.

### Source: ./go.mod

Run `go build ./...` to build all packages.

## Run

No relevant files found.

## Test

Run `go test ./...` to run all tests.

## Docs

### Markdown files
* ./README.md

### Code structure
(The symbols shown against some files below are not an exhaustive list)

./.claude/settings.local.json
./README.md
./detect.go          [LangStats, DetectLanguages, readSample]
./detect_test.go     [TestDetectLanguages, TestDetectLanguages_Python, TestDetectLanguages_Empty]
./docs.go            [DirEntry, DocsResult, BuildDocs, buildDirTree, fileEntry, ...]
./docs_test.go       [TestExtractSymbols_Go, TestExtractSymbols_Python, TestExtractSymbols_TypeScript, TestSingleChildCompression, TestFlattenFiles, ...]
./finder.go          [heuristics, loadHeuristics, FindFiles, heuristicsKey]
./finder_test.go     [TestLoadHeuristics, TestHeuristicsKey, TestFindFiles, TestFindFiles_NodeJS]
./getstarted
./go.mod
./go.sum
./heuristics.yaml
./main.go            [main, parseFilter, isActive, printStack, displayPath, ...]
./parsers.go         [ConfigParser, registerParser, FindParser, init, goModParser, ...]
./parsers_test.go    [TestGoModParser, TestPackageJSONParser, TestRequirementsParser, TestPyprojectParser, TestPytestIniParser, ...]

```