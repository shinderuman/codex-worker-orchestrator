package workflow

import (
	"path/filepath"
	"strings"
)

func isKnownSafeDocumentationPath(path string) bool {
	clean := filepath.ToSlash(path)
	if strings.HasPrefix(clean, "docs/") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(clean))
	if ext == ".txt" {
		return !isDependencyControlTextFile(clean)
	}
	switch ext {
	case ".md", ".markdown", ".rst", ".adoc":
		return true
	default:
		return false
	}
}

func isDependencyControlTextFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return base == "requirements.txt" || base == "constraints.txt" || strings.HasPrefix(base, "requirements-") || strings.HasPrefix(base, "constraints-")
}

func isKnownSafeTestPath(path string) bool {
	clean := filepath.ToSlash(path)
	lower := strings.ToLower(clean)
	for _, marker := range []string{"/test/", "/tests/", "/testdata/", "/fixtures/", "/__tests__/"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, prefix := range []string{"test/", "tests/", "testdata/", "fixtures/", "__tests__/"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	base := strings.ToLower(filepath.Base(clean))
	return strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") || strings.Contains(base, "_test.")
}
