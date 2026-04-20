package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ConfigParser extracts command→markdown context from a config file.
type ConfigParser interface {
	Pattern() *regexp.Regexp
	Parse(path string, content []byte) (map[string]string, error)
}

var registeredParsers []ConfigParser

func registerParser(p ConfigParser) {
	registeredParsers = append(registeredParsers, p)
}

// FindParser returns the first parser whose pattern matches the base filename.
func FindParser(basename string) ConfigParser {
	for _, p := range registeredParsers {
		if p.Pattern().MatchString(basename) {
			return p
		}
	}
	return nil
}

func init() {
	registerParser(&goModParser{})
	registerParser(&packageJSONParser{})
	registerParser(&requirementsParser{})
	registerParser(&pyprojectParser{})
	registerParser(&setupPyParser{})
	registerParser(&pytestIniParser{})
}

// --- go.mod ---

type goModParser struct{}

func (g *goModParser) Pattern() *regexp.Regexp { return regexp.MustCompile(`^go\.mod$`) }

func (g *goModParser) Parse(path string, content []byte) (map[string]string, error) {
	result := make(map[string]string)
	var module, goVersion string
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			module = strings.TrimPrefix(line, "module ")
		}
		if strings.HasPrefix(line, "go ") {
			goVersion = strings.TrimPrefix(line, "go ")
		}
	}
	var sb strings.Builder
	if module != "" {
		fmt.Fprintf(&sb, "Module: `%s`\n", module)
	}
	if goVersion != "" {
		fmt.Fprintf(&sb, "Go version: `%s`\n", goVersion)
	}
	sb.WriteString("\nRun `go mod download` to install dependencies.")
	result["install"] = sb.String()
	result["build"] = "Run `go build ./...` to build all packages."
	result["test"] = "Run `go test ./...` to run all tests."
	return result, nil
}

// --- package.json ---

type packageJSONParser struct{}

func (p *packageJSONParser) Pattern() *regexp.Regexp { return regexp.MustCompile(`^package\.json$`) }

func (p *packageJSONParser) Parse(path string, content []byte) (map[string]string, error) {
	var pkg struct {
		Name         string            `json:"name"`
		Version      string            `json:"version"`
		Scripts      map[string]string `json:"scripts"`
		Dependencies map[string]string `json:"dependencies"`
		DevDeps      map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(content, &pkg); err != nil {
		return nil, err
	}

	result := make(map[string]string)

	// Detect package manager
	pm := "npm"

	// Map scripts to commands
	buildScripts := collectScripts(pkg.Scripts, []string{"build", "compile", "bundle"})
	testScripts := collectScripts(pkg.Scripts, []string{"test", "spec", "e2e", "integration"})
	runScripts := collectScripts(pkg.Scripts, []string{"start", "dev", "serve", "preview"})

	var installSb strings.Builder
	if pkg.Name != "" {
		fmt.Fprintf(&installSb, "Package: `%s`", pkg.Name)
		if pkg.Version != "" {
			fmt.Fprintf(&installSb, " v%s", pkg.Version)
		}
		installSb.WriteString("\n")
	}
	depCount := len(pkg.Dependencies) + len(pkg.DevDeps)
	if depCount > 0 {
		fmt.Fprintf(&installSb, "%d dependencies.\n", depCount)
	}
	fmt.Fprintf(&installSb, "Run `%s install`.", pm)
	result["install"] = installSb.String()

	if len(buildScripts) > 0 {
		result["build"] = formatScripts(pm, buildScripts)
	}
	if len(testScripts) > 0 {
		result["test"] = formatScripts(pm, testScripts)
	}
	if len(runScripts) > 0 {
		result["run"] = formatScripts(pm, runScripts)
	}

	return result, nil
}

func collectScripts(scripts map[string]string, keywords []string) map[string]string {
	out := make(map[string]string)
	for name, cmd := range scripts {
		lower := strings.ToLower(name)
		for _, kw := range keywords {
			if lower == kw || strings.Contains(lower, kw) {
				out[name] = cmd
				break
			}
		}
	}
	return out
}

func formatScripts(pm string, scripts map[string]string) string {
	var lines []string
	for name, cmd := range scripts {
		lines = append(lines, fmt.Sprintf("Run `%s run %s` (%s).", pm, name, cmd))
	}
	return strings.Join(lines, "\n")
}

// --- requirements.txt ---

type requirementsParser struct{}

func (r *requirementsParser) Pattern() *regexp.Regexp {
	return regexp.MustCompile(`^requirements.*\.txt$`)
}

func (r *requirementsParser) Parse(path string, content []byte) (map[string]string, error) {
	var deps []string
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		deps = append(deps, line)
		if len(deps) >= 20 {
			break
		}
	}
	var sb strings.Builder
	sb.WriteString("Run `pip install -r requirements.txt`.\n")
	if len(deps) > 0 {
		sb.WriteString("\nDependencies:\n```\n")
		sb.WriteString(strings.Join(deps, "\n"))
		sb.WriteString("\n```")
	}
	return map[string]string{"install": sb.String()}, nil
}

// --- pyproject.toml ---

type pyprojectParser struct{}

func (p *pyprojectParser) Pattern() *regexp.Regexp { return regexp.MustCompile(`^pyproject\.toml$`) }

func (p *pyprojectParser) Parse(path string, content []byte) (map[string]string, error) {
	result := make(map[string]string)
	sections := parseTOMLSections(content)

	// [project]
	if proj, ok := sections["project"]; ok {
		var installSb strings.Builder
		if name := proj["name"]; name != "" {
			fmt.Fprintf(&installSb, "Package: `%s`", name)
			if ver := proj["version"]; ver != "" {
				fmt.Fprintf(&installSb, " v%s", ver)
			}
			installSb.WriteString("\n")
		}
		installSb.WriteString("Run `pip install .` or `pip install -e .` for editable install.")
		result["install"] = installSb.String()
	}

	// [build-system]
	if bs, ok := sections["build-system"]; ok {
		if requires := bs["requires"]; requires != "" {
			result["build"] = fmt.Sprintf("Build system requires: %s\nRun `python -m build`.", requires)
		}
	}

	// [project.scripts]
	if scripts, ok := sections["project.scripts"]; ok && len(scripts) > 0 {
		var sb strings.Builder
		sb.WriteString("Entry points:\n")
		for name, cmd := range scripts {
			fmt.Fprintf(&sb, "- `%s` → `%s`\n", name, cmd)
		}
		result["run"] = sb.String()
	}

	// [tool.pytest.ini_options]
	if pytest, ok := sections["tool.pytest.ini_options"]; ok {
		var sb strings.Builder
		sb.WriteString("Run `pytest`.")
		if addopts := pytest["addopts"]; addopts != "" {
			fmt.Fprintf(&sb, "\nOptions: `%s`", addopts)
		}
		if testpaths := pytest["testpaths"]; testpaths != "" {
			fmt.Fprintf(&sb, "\nTest paths: %s", testpaths)
		}
		result["test"] = sb.String()
	}

	return result, nil
}

// parseTOMLSections is a lightweight TOML section reader (no external dep).
// Returns map[section]map[key]value for simple key=value pairs.
func parseTOMLSections(content []byte) map[string]map[string]string {
	sections := make(map[string]map[string]string)
	current := ""
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			current = strings.TrimSpace(current)
			if sections[current] == nil {
				sections[current] = make(map[string]string)
			}
			continue
		}
		if current == "" {
			continue
		}
		if idx := strings.IndexByte(line, '='); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			val = strings.Trim(val, `"'`)
			sections[current][key] = val
		}
	}
	return sections
}

// --- setup.py ---

type setupPyParser struct{}

func (s *setupPyParser) Pattern() *regexp.Regexp { return regexp.MustCompile(`^setup\.py$`) }

func (s *setupPyParser) Parse(path string, content []byte) (map[string]string, error) {
	result := make(map[string]string)
	result["install"] = "Run `pip install .` or `python setup.py install`."

	// Look for console_scripts
	text := string(content)
	if idx := strings.Index(text, "console_scripts"); idx >= 0 {
		snippet := text[idx:]
		end := strings.Index(snippet, "]")
		if end > 0 {
			scripts := snippet[:end+1]
			result["run"] = fmt.Sprintf("Console scripts defined:\n```python\n%s\n```", strings.TrimSpace(scripts))
		}
	}
	return result, nil
}

// --- pytest.ini ---

type pytestIniParser struct{}

func (p *pytestIniParser) Pattern() *regexp.Regexp { return regexp.MustCompile(`^pytest\.ini$`) }

func (p *pytestIniParser) Parse(path string, content []byte) (map[string]string, error) {
	var sb strings.Builder
	sb.WriteString("Run `pytest`.")
	scanner := bufio.NewScanner(bytes.NewReader(content))
	inPytest := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "[pytest]" {
			inPytest = true
			continue
		}
		if inPytest && strings.HasPrefix(line, "[") {
			break
		}
		if inPytest && line != "" && !strings.HasPrefix(line, "#") {
			fmt.Fprintf(&sb, "\n%s", line)
		}
	}
	return map[string]string{"test": sb.String()}, nil
}
