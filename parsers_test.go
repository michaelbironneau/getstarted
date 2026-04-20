package main

import (
	"testing"
)

func TestGoModParser(t *testing.T) {
	content := []byte(`module github.com/example/myapp

go 1.21

require (
	github.com/some/dep v1.0.0
)
`)
	p := &goModParser{}
	result, err := p.Parse("go.mod", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result["install"]; !ok {
		t.Error("expected install key")
	}
	if _, ok := result["build"]; !ok {
		t.Error("expected build key")
	}
	if _, ok := result["test"]; !ok {
		t.Error("expected test key")
	}
	if got := result["install"]; got == "" {
		t.Error("install value should not be empty")
	}
}

func TestPackageJSONParser(t *testing.T) {
	content := []byte(`{
		"name": "my-app",
		"version": "1.0.0",
		"scripts": {
			"build": "tsc",
			"test": "jest",
			"start": "node dist/index.js",
			"dev": "ts-node src/index.ts"
		},
		"dependencies": {
			"express": "^4.18.0"
		},
		"devDependencies": {
			"typescript": "^5.0.0",
			"jest": "^29.0.0"
		}
	}`)
	p := &packageJSONParser{}
	result, err := p.Parse("package.json", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result["install"]; !ok {
		t.Error("expected install key")
	}
	if _, ok := result["build"]; !ok {
		t.Error("expected build key from 'build' script")
	}
	if _, ok := result["test"]; !ok {
		t.Error("expected test key from 'test' script")
	}
	if _, ok := result["run"]; !ok {
		t.Error("expected run key from 'start'/'dev' scripts")
	}
}

func TestRequirementsParser(t *testing.T) {
	content := []byte(`# dependencies
flask==2.0.0
requests>=2.28.0
# dev
pytest==7.0.0
`)
	p := &requirementsParser{}
	result, err := p.Parse("requirements.txt", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	install, ok := result["install"]
	if !ok {
		t.Fatal("expected install key")
	}
	if install == "" {
		t.Error("install value should not be empty")
	}
	// Should contain actual dep names (not comments)
	for _, dep := range []string{"flask", "requests"} {
		found := false
		for _, line := range []string{install} {
			if contains(line, dep) {
				found = true
			}
		}
		if !found {
			t.Errorf("expected %q to appear in install output", dep)
		}
	}
}

func TestPyprojectParser(t *testing.T) {
	content := []byte(`[project]
name = "my-project"
version = "0.1.0"

[build-system]
requires = ["setuptools", "wheel"]
build-backend = "setuptools.build_meta"

[project.scripts]
my-tool = "my_project.cli:main"

[tool.pytest.ini_options]
addopts = "-v --tb=short"
testpaths = ["tests"]
`)
	p := &pyprojectParser{}
	result, err := p.Parse("pyproject.toml", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result["install"]; !ok {
		t.Error("expected install key from [project]")
	}
	if _, ok := result["build"]; !ok {
		t.Error("expected build key from [build-system]")
	}
	if _, ok := result["run"]; !ok {
		t.Error("expected run key from [project.scripts]")
	}
	if _, ok := result["test"]; !ok {
		t.Error("expected test key from [tool.pytest.ini_options]")
	}
}

func TestPytestIniParser(t *testing.T) {
	content := []byte(`[pytest]
addopts = -v --tb=short
testpaths = tests
`)
	p := &pytestIniParser{}
	result, err := p.Parse("pytest.ini", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	test, ok := result["test"]
	if !ok {
		t.Fatal("expected test key")
	}
	if !contains(test, "pytest") {
		t.Error("expected 'pytest' in test output")
	}
}

func TestFindParser(t *testing.T) {
	cases := []struct {
		name   string
		expect bool
	}{
		{"go.mod", true},
		{"package.json", true},
		{"requirements.txt", true},
		{"requirements-dev.txt", true},
		{"pyproject.toml", true},
		{"setup.py", true},
		{"pytest.ini", true},
		{"unknown.xyz", false},
	}
	for _, tc := range cases {
		p := FindParser(tc.name)
		if tc.expect && p == nil {
			t.Errorf("expected parser for %q but got nil", tc.name)
		}
		if !tc.expect && p != nil {
			t.Errorf("expected no parser for %q but got one", tc.name)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
