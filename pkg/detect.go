package getstarted

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/go-enry/go-enry/v2"
)

const sampleSize = 8192

var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".idea":        true,
	".vscode":      true,
	"__pycache__":  true,
	".tox":         true,
	".mypy_cache":  true,
	"dist":         true,
	"build":        true,
}

type LangStats struct {
	Language   string
	Percentage float64
}

// DetectLanguages walks dir and returns language percentages sorted descending.
func DetectLanguages(dir string) ([]LangStats, error) {
	counts := make(map[string]int64)
	var total int64

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		size := info.Size()
		if size == 0 {
			return nil
		}

		content, err := readSample(path)
		if err != nil {
			return nil
		}
		if enry.IsBinary(content) {
			return nil
		}

		lang := enry.GetLanguage(filepath.Base(path), content)
		if lang == "" {
			return nil
		}
		if enry.GetLanguageType(lang) != enry.Programming && enry.GetLanguageType(lang) != enry.Markup {
			return nil
		}
		counts[lang] += size
		total += size
		return nil
	})
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return nil, nil
	}

	stats := make([]LangStats, 0, len(counts))
	for lang, count := range counts {
		pct := float64(count) / float64(total) * 100
		stats = append(stats, LangStats{Language: lang, Percentage: pct})
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Percentage > stats[j].Percentage
	})
	return stats, nil
}

func readSample(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, sampleSize)
	n, _ := f.Read(buf)
	return buf[:n], nil
}
