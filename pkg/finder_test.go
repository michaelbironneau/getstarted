package getstarted

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHeuristics(t *testing.T) {
	h, err := loadHeuristics()
	if err != nil {
		t.Fatalf("failed to load heuristics: %v", err)
	}
	if len(h) == 0 {
		t.Error("heuristics should not be empty")
	}
	for _, lang := range []string{"python", "nodejs", "go"} {
		if _, ok := h[lang]; !ok {
			t.Errorf("expected language %q in heuristics", lang)
		}
	}
}

func TestHeuristicsKey(t *testing.T) {
	cases := []struct {
		lang   string
		expect string
	}{
		{"Go", "go"},
		{"Python", "python"},
		{"JavaScript", "nodejs"},
		{"TypeScript", "nodejs"},
		{"Ruby", "ruby"},
	}
	for _, tc := range cases {
		got := heuristicsKey(tc.lang)
		if got != tc.expect {
			t.Errorf("heuristicsKey(%q) = %q, want %q", tc.lang, got, tc.expect)
		}
	}
}

func TestFindFiles(t *testing.T) {
	// Create a small temp directory with known files
	dir := t.TempDir()

	// Create go.mod
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Create a test file
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	stats := []LangStats{{Language: "Go", Percentage: 100}}
	files, err := FindFiles(dir, stats)
	if err != nil {
		t.Fatalf("FindFiles failed: %v", err)
	}

	if len(files["install"]) == 0 {
		t.Error("expected go.mod in install files")
	}
	if len(files["build"]) == 0 {
		t.Error("expected go.mod in build files")
	}
	if len(files["test"]) == 0 {
		t.Error("expected main_test.go in test files")
	}
	if !containsPath(files["test"], filepath.Join(dir, "go.mod")) {
		t.Error("expected go.mod in test files")
	}
}

func TestFindFiles_NodeJS(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	stats := []LangStats{{Language: "JavaScript", Percentage: 100}}
	files, err := FindFiles(dir, stats)
	if err != nil {
		t.Fatalf("FindFiles failed: %v", err)
	}

	if len(files["install"]) == 0 {
		t.Error("expected package.json in install files for nodejs")
	}
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
