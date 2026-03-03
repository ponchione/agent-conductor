package util

import (
	"path/filepath"
	"strings"
)

// MatchesAnyGlob returns true if relPath matches at least one of the patterns.
func MatchesAnyGlob(patterns []string, relPath string) bool {
	for _, pat := range patterns {
		if MatchesGlob(pat, relPath) {
			return true
		}
	}
	return false
}

// MatchesGlob returns true if relPath matches the given glob pattern.
// Supports "**/" prefix for recursive matching and trailing "/" for directory prefixes.
func MatchesGlob(pattern, relPath string) bool {
	if dir, ok := strings.CutSuffix(pattern, "/"); ok {
		dir = strings.TrimPrefix(dir, "**/")
		return strings.HasPrefix(relPath, dir+"/") || strings.Contains(relPath, "/"+dir+"/")
	}

	if ok, _ := filepath.Match(pattern, relPath); ok {
		return true
	}

	if strings.HasPrefix(pattern, "**/") {
		suffix := pattern[3:]
		if ok, _ := filepath.Match(suffix, filepath.Base(relPath)); ok {
			return true
		}
		if ok, _ := filepath.Match(suffix, relPath); ok {
			return true
		}
		parts := strings.Split(relPath, "/")
		for i := range parts {
			sub := strings.Join(parts[i:], "/")
			if ok, _ := filepath.Match(suffix, sub); ok {
				return true
			}
		}
	}

	return false
}
