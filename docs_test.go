package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractSymbols_Go(t *testing.T) {
	dir := t.TempDir()
	src := []byte(`package main

import "fmt"

type Server struct{}

func (s *Server) Start() {
	fmt.Println("starting")
}

func NewServer() *Server {
	return &Server{}
}
`)
	path := filepath.Join(dir, "server.go")
	if err := os.WriteFile(path, src, 0644); err != nil {
		t.Fatal(err)
	}

	symbols := extractSymbols(path)
	if len(symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}
	found := map[string]bool{}
	for _, s := range symbols {
		found[s] = true
	}
	for _, want := range []string{"Start", "NewServer", "Server"} {
		if !found[want] {
			t.Errorf("expected symbol %q, got %v", want, symbols)
		}
	}
}

func TestExtractSymbols_Python(t *testing.T) {
	dir := t.TempDir()
	src := []byte(`class MyClass:
    def method(self):
        pass

def standalone_func():
    pass
`)
	path := filepath.Join(dir, "app.py")
	if err := os.WriteFile(path, src, 0644); err != nil {
		t.Fatal(err)
	}

	symbols := extractSymbols(path)
	if len(symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}
	found := map[string]bool{}
	for _, s := range symbols {
		found[s] = true
	}
	for _, want := range []string{"MyClass", "method", "standalone_func"} {
		if !found[want] {
			t.Errorf("expected symbol %q, got %v", want, symbols)
		}
	}
}

func TestExtractSymbols_TypeScript(t *testing.T) {
	dir := t.TempDir()
	src := []byte(`
export function greet(name: string): string {
  return "hello";
}

export class MyService {
  doThing(): void {}
}

export const helper = (x: number) => x * 2;

export interface Config {
  timeout: number;
}

export type Handler = (req: any) => any;
`)
	path := filepath.Join(dir, "service.ts")
	if err := os.WriteFile(path, src, 0644); err != nil {
		t.Fatal(err)
	}

	symbols := extractSymbols(path)
	if len(symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}
	found := map[string]bool{}
	for _, s := range symbols {
		found[s] = true
	}
	for _, want := range []string{"greet", "MyService", "helper", "Config", "Handler"} {
		if !found[want] {
			t.Errorf("expected symbol %q, got %v", want, symbols)
		}
	}
}

func TestSingleChildCompression(t *testing.T) {
	dir := t.TempDir()

	// Create: dir/auth/[...all]/handler.go
	authDir := filepath.Join(dir, "auth")
	allDir := filepath.Join(authDir, "[...all]")
	if err := os.MkdirAll(allDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(allDir, "handler.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := BuildDocs(dir, 2)
	if err != nil {
		t.Fatalf("BuildDocs error: %v", err)
	}
	if result.Root == nil {
		t.Fatal("expected non-nil root")
	}
	// Find the compressed path entry
	var findCompressed func(entry *DirEntry) bool
	findCompressed = func(entry *DirEntry) bool {
		for _, child := range entry.Children {
			if child.IsDir {
				// Check if path is compressed (contains slash)
				if len(child.Name) > 0 {
					for _, r := range child.Name {
						if r == '/' {
							return true
						}
					}
				}
				if findCompressed(child) {
					return true
				}
			}
		}
		return false
	}
	if !findCompressed(result.Root) {
		t.Error("expected single-child path compression to produce a slash-combined entry")
	}
}

func TestFlattenFiles(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.o\n"), 0644)
	os.WriteFile(filepath.Join(subdir, "util.go"), []byte("package pkg\nfunc Helper() {}\n"), 0644)

	result, err := BuildDocs(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	files := flattenFiles(result.Root)

	paths := map[string]bool{}
	for _, f := range files {
		paths[f.Path] = true
		if strings.HasPrefix(filepath.Base(f.Path), ".") {
			t.Errorf("dotfile should be excluded: %s", f.Path)
		}
	}
	if !paths["./main.go"] {
		t.Error("expected ./main.go in flat list")
	}
	if !paths["./pkg/util.go"] {
		t.Error("expected ./pkg/util.go in flat list")
	}
}

func TestBuildDocs_MarkdownFiles(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "docs")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"README.md", filepath.Join("docs", "API.md")} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# Title\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := BuildDocs(dir, 2)
	if err != nil {
		t.Fatalf("BuildDocs error: %v", err)
	}
	if len(result.MarkdownFiles) < 2 {
		t.Errorf("expected at least 2 markdown files, got %v", result.MarkdownFiles)
	}
}
