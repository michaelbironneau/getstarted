package getstarted

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLanguages(t *testing.T) {
	dir := t.TempDir()

	goSrc := []byte(`package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), goSrc, 0644); err != nil {
		t.Fatal(err)
	}

	stats, err := DetectLanguages(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stats) == 0 {
		t.Fatal("expected at least one language detected")
	}
	if stats[0].Language != "Go" {
		t.Errorf("expected Go as top language, got %q", stats[0].Language)
	}
	if stats[0].Percentage <= 0 || stats[0].Percentage > 100 {
		t.Errorf("unexpected percentage: %v", stats[0].Percentage)
	}
}

func TestDetectLanguages_Python(t *testing.T) {
	dir := t.TempDir()

	pySrc := []byte(`#!/usr/bin/env python3
def main():
    print("hello")

if __name__ == "__main__":
    main()
`)
	if err := os.WriteFile(filepath.Join(dir, "main.py"), pySrc, 0644); err != nil {
		t.Fatal(err)
	}

	stats, err := DetectLanguages(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stats) == 0 {
		t.Fatal("expected at least one language detected")
	}
	found := false
	for _, s := range stats {
		if s.Language == "Python" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Python in detected languages, got %v", stats)
	}
}

func TestDetectLanguages_Empty(t *testing.T) {
	dir := t.TempDir()
	stats, err := DetectLanguages(dir)
	if err != nil {
		t.Fatalf("unexpected error on empty dir: %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("expected no languages for empty dir, got %v", stats)
	}
}
